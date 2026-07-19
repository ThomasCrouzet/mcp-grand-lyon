"""HTTP allowlist and cache key (no auth in key)."""

from __future__ import annotations

import httpx
import pytest
import respx

from grand_lyon_mcp.infrastructure.http import HostNotAllowedError, HttpClient
from grand_lyon_mcp.storage.cache import cache_key


def test_cache_key_ignores_auth_conceptually() -> None:
    # Authorization is never part of cache_key inputs
    k1 = cache_key("GET", "https://data.grandlyon.com/x", {"a": "1"}, "src")
    k2 = cache_key("GET", "https://data.grandlyon.com/x", {"a": "1"}, "src")
    assert k1 == k2
    k3 = cache_key("GET", "https://data.grandlyon.com/x", {"a": "2"}, "src")
    assert k1 != k3


@pytest.mark.asyncio
async def test_host_allowlist() -> None:
    client = HttpClient(offline=False)
    with pytest.raises(HostNotAllowedError):
        client.assert_allowed("https://evil.example.com/api")
    client.assert_allowed("https://data.grandlyon.com/fr/x")
    await client.aclose()


@pytest.mark.asyncio
async def test_offline_blocks_http() -> None:
    client = HttpClient(offline=True)
    with pytest.raises(RuntimeError):
        await client.get("https://data.grandlyon.com/fr/x")
    await client.aclose()


@pytest.mark.asyncio
@respx.mock
async def test_allowlist_enforced_on_redirect() -> None:
    """Un 302 depuis un host allowlisté vers un host arbitraire doit être bloqué (anti-SSRF)."""
    respx.get("https://data.grandlyon.com/redirect").mock(
        return_value=httpx.Response(302, headers={"Location": "https://169.254.169.254/latest"})
    )
    client = HttpClient(offline=False)
    with pytest.raises(HostNotAllowedError):
        await client.get("https://data.grandlyon.com/redirect")
    await client.aclose()
