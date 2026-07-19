"""Shared HTTP client with allowlist, timeouts, and redaction-safe headers."""

from __future__ import annotations

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


class HostNotAllowedError(ValueError):
    pass


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
    ) -> None:
        self.offline = offline
        hosts = set(allowlist or DEFAULT_ALLOWLIST)
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
                "Accept-Encoding": "gzip, deflate",
            },
            follow_redirects=True,
            # Anti-SSRF : re-valide l'allowlist sur CHAQUE requête, y compris les cibles
            # des redirections 3xx (sinon un 302 vers un host arbitraire serait suivi).
            event_hooks={"request": [self._enforce_allowlist]},
        )

    async def _enforce_allowlist(self, request: httpx.Request) -> None:
        self.assert_allowed(str(request.url))

    def assert_allowed(self, url: str) -> None:
        host = urlparse(url).hostname or ""
        if host not in self.allowlist:
            raise HostNotAllowedError(f"Host not allowlisted: {host}")

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
        if self.offline:
            raise RuntimeError("HTTP disabled in offline mode")
        self.assert_allowed(url)
        method_u = method.upper()
        last_exc: Exception | None = None
        for attempt in range(1, maximum_attempts + 1):
            try:
                async with self._limiter.acquire():
                    response = await self._client.request(
                        method_u,
                        url,
                        params=params,
                        headers=headers,
                        auth=auth,
                    )
                if response.status_code in RETRYABLE_STATUS and attempt < maximum_attempts:
                    raise RetryableHttpError(response.status_code)
                return response
            except (RetryableHttpError, httpx.TransportError) as exc:
                last_exc = exc
                logger.info(
                    "http_retry",
                    extra={
                        "status_code": getattr(exc, "status_code", None),
                        "retry_count": attempt,
                    },
                )
                if attempt >= maximum_attempts:
                    raise
                import asyncio

                await asyncio.sleep(0.2 * (2 ** (attempt - 1)))
        assert last_exc is not None
        raise last_exc

    async def get(self, url: str, **kwargs: Any) -> httpx.Response:
        return await self.request("GET", url, **kwargs)

    async def aclose(self) -> None:
        await self._client.aclose()
