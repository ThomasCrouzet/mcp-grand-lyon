"""DataGrandLyon HTTP Basic auth (https only)."""

from __future__ import annotations

import httpx


def basic_auth(username: str, password: str) -> httpx.BasicAuth:
    """Return httpx BasicAuth, never embed credentials in URLs."""
    return httpx.BasicAuth(username, password)
