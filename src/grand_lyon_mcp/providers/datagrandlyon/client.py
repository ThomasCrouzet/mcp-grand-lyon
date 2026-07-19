"""Thin DataGrandLyon HTTP facade."""

from __future__ import annotations

from typing import Any

import httpx

from grand_lyon_mcp.infrastructure.http import HttpClient
from grand_lyon_mcp.providers.datagrandlyon.auth import basic_auth


class DataGrandLyonClient:
    def __init__(
        self,
        http: HttpClient,
        *,
        username: str = "",
        password: str = "",
    ) -> None:
        self.http = http
        self._auth: httpx.Auth | None = None
        if username and password:
            self._auth = basic_auth(username, password)

    @property
    def authenticated(self) -> bool:
        return self._auth is not None

    async def get_json(self, url: str, *, params: dict[str, Any] | None = None) -> Any:
        response = await self.http.get(url, params=params, auth=self._auth)
        response.raise_for_status()
        return response.json()
