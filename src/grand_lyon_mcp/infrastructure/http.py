"""Shared HTTP client with allowlist, timeouts, and redaction-safe headers."""

from __future__ import annotations

import asyncio
from collections.abc import AsyncIterator
from contextlib import asynccontextmanager
from typing import Any
from urllib.parse import urlparse

import httpx

from grand_lyon_mcp.infrastructure.logging import get_logger
from grand_lyon_mcp.infrastructure.rate_limit import ConcurrencyLimiter
from grand_lyon_mcp.infrastructure.retry import RETRYABLE_STATUS, RetryableHttpError
from grand_lyon_mcp.version import __version__

logger = get_logger("http")

DEFAULT_ALLOWLIST = frozenset(
    {
        "data.grandlyon.com",
        "download.data.grandlyon.com",
    }
)

TRANSITOUS_HOST = "api.transitous.org"
MAX_RESPONSE_BYTES = 8 * 1024 * 1024
MAX_REDIRECTS = 5


class HostNotAllowedError(ValueError):
    pass


class ResponseTooLargeError(ValueError):
    pass


class ResponseEncodingError(ValueError):
    pass


def assert_response_size(response: httpx.Response, limit: int) -> None:
    """Reject an excessive declared body before reading it."""
    value = response.headers.get("content-length")
    if value is not None:
        try:
            size = int(value)
        except ValueError:
            raise ValueError("Invalid response Content-Length") from None
        if size < 0:
            raise ValueError("Invalid response Content-Length")
        if size > limit:
            raise ResponseTooLargeError("HTTP response exceeds the byte limit")


async def bounded_chunks(response: httpx.Response, limit: int) -> AsyncIterator[bytes]:
    """Bound actual bytes without an unbounded decompression step."""
    if limit <= 0:
        raise ValueError("The response byte limit must be positive")
    encoding = response.headers.get("content-encoding", "identity").strip().lower()
    if encoding not in ("", "identity"):
        raise ResponseEncodingError("HTTP content encoding must be identity")
    assert_response_size(response, limit)
    total = 0
    async for chunk in response.aiter_bytes(chunk_size=min(64 * 1024, limit + 1)):
        total += len(chunk)
        if total > limit:
            raise ResponseTooLargeError("HTTP response exceeds the byte limit")
        yield chunk


class HttpClient:
    """Process-shared HTTP client wrapper."""

    def __init__(
        self,
        *,
        connect_timeout: float = 5.0,
        read_timeout: float = 20.0,
        max_parallel: int = 6,
        verify_tls: bool = True,
        allowlist: frozenset[str] | None = None,
        transitous_enabled: bool = False,
        auth: httpx.Auth | None = None,
        offline: bool = False,
        max_response_bytes: int = MAX_RESPONSE_BYTES,
        total_timeout: float = 60.0,
        transport: httpx.AsyncBaseTransport | None = None,
    ) -> None:
        if max_response_bytes <= 0 or total_timeout <= 0:
            raise ValueError("HTTP limits must be positive")
        self.offline = offline
        self.max_response_bytes = max_response_bytes
        self.total_timeout = total_timeout
        hosts = set(DEFAULT_ALLOWLIST if allowlist is None else allowlist)
        if transitous_enabled:
            hosts.add(TRANSITOUS_HOST)
        self.allowlist = frozenset(hosts)
        self._limiter = ConcurrencyLimiter(max_parallel)
        if not verify_tls:
            # La désactivation de la vérification TLS expose les identifiants Basic à un
            # intercepteur (MITM). N'utiliser qu'en développement / réseau de confiance.
            logger.warning(
                "tls_verification_disabled",
                extra={"provider": "http"},
            )
        timeout = httpx.Timeout(connect=connect_timeout, read=read_timeout, write=20.0, pool=20.0)
        self._client = httpx.AsyncClient(
            timeout=timeout,
            verify=verify_tls,
            auth=auth,
            headers={
                "User-Agent": f"grand-lyon-mcp/{__version__}",
                "Accept": "application/json",
                "Accept-Encoding": "identity",
            },
            follow_redirects=False,
            trust_env=False,
            transport=transport,
            # Each redirect uses the same origin check before network access.
            event_hooks={"request": [self._enforce_allowlist]},
        )

    async def _enforce_allowlist(self, request: httpx.Request) -> None:
        self.assert_allowed(str(request.url))

    def assert_allowed(self, url: str) -> None:
        try:
            parsed = urlparse(url)
            allowed = (
                parsed.scheme == "https"
                and parsed.hostname in self.allowlist
                and parsed.port in (None, 443)
                and parsed.username is None
                and parsed.password is None
                and not parsed.fragment
            )
        except ValueError:
            allowed = False
        if not allowed:
            raise HostNotAllowedError("Request origin does not match the HTTPS port 443 allowlist")

    async def request(
        self,
        method: str,
        url: str,
        *,
        params: dict[str, Any] | None = None,
        headers: dict[str, str] | None = None,
        auth: httpx.Auth | None = None,
        maximum_attempts: int = 3,
    ) -> httpx.Response:
        async with self.stream(
            method,
            url,
            params=params,
            headers=headers,
            auth=auth,
            maximum_attempts=maximum_attempts,
        ) as response:
            content = bytearray()
            async for chunk in bounded_chunks(response, self.max_response_bytes):
                content.extend(chunk)
            # The body is already decoded. Do not apply Content-Encoding twice.
            result_headers = response.headers.copy()
            for name in ("content-encoding", "content-length", "transfer-encoding"):
                result_headers.pop(name, None)
            return httpx.Response(
                response.status_code,
                headers=result_headers,
                content=bytes(content),
                request=response.request,
                extensions=response.extensions,
            )

    @asynccontextmanager
    async def stream(
        self,
        method: str,
        url: str,
        *,
        params: dict[str, Any] | None = None,
        headers: dict[str, str] | None = None,
        auth: httpx.Auth | None = None,
        maximum_attempts: int = 3,
    ) -> AsyncIterator[httpx.Response]:
        """Keep redirects, retries, body consumption, and queue waits inside one deadline."""
        if self.offline:
            raise RuntimeError("HTTP disabled in offline mode")
        self.assert_allowed(url)
        if not 1 <= maximum_attempts <= 5:
            raise ValueError("maximum_attempts must be between 1 and 5")
        method_u = method.upper()
        async with asyncio.timeout(self.total_timeout):
            for attempt in range(1, maximum_attempts + 1):
                exposed = False
                try:
                    async with self._limiter.acquire():
                        request = self._client.build_request(
                            method_u, url, params=params, headers=headers
                        )
                        request_auth = auth
                        response: httpx.Response | None = None
                        try:
                            for hop in range(MAX_REDIRECTS + 1):
                                self.assert_allowed(str(request.url))
                                response = await self._client.send(
                                    request, stream=True, auth=request_auth
                                )
                                next_request = response.next_request
                                if next_request is None:
                                    break
                                # HTTPX removes authorization when the origin changes.
                                # Do not reapply credentials to its redirect request.
                                self.assert_allowed(str(next_request.url))
                                await response.aclose()
                                if hop == MAX_REDIRECTS:
                                    raise HostNotAllowedError("HTTP redirect limit exceeded")
                                request, request_auth = next_request, None
                            assert response is not None
                            if (
                                response.status_code in RETRYABLE_STATUS
                                and attempt < maximum_attempts
                            ):
                                raise RetryableHttpError(response.status_code)
                            exposed = True
                            yield response
                            return
                        finally:
                            if response is not None:
                                await response.aclose()
                except (RetryableHttpError, httpx.TransportError) as exc:
                    if exposed or attempt >= maximum_attempts:
                        raise
                    logger.info(
                        "http_retry",
                        extra={
                            "status_code": getattr(exc, "status_code", None),
                            "retry_count": attempt,
                        },
                    )
                    await asyncio.sleep(0.2 * (2 ** (attempt - 1)))

    async def get(self, url: str, **kwargs: Any) -> httpx.Response:
        return await self.request("GET", url, **kwargs)

    async def aclose(self) -> None:
        await self._client.aclose()
