"""Photon geocoder client (Métropole de Lyon)."""

from __future__ import annotations

from grand_lyon_mcp.domain.geo import Point
from grand_lyon_mcp.domain.places import PlaceCandidate
from grand_lyon_mcp.infrastructure.http import HttpClient
from grand_lyon_mcp.providers.photon.parser import parse_photon_response

PHOTON_BASE = "https://download.data.grandlyon.com/geocoding/photon-bal/api"


class PhotonClient:
    def __init__(self, http: HttpClient) -> None:
        self._http = http

    async def search(
        self,
        query: str,
        *,
        near: Point | None = None,
        types: list[str] | None = None,
        limit: int = 5,
    ) -> list[PlaceCandidate]:
        # `types` fait partie du Protocol PlaceProvider ; Photon renvoie du géocodage
        # généraliste et le filtrage par type est appliqué en aval (scoring PlaceService).
        _ = types
        params: dict[str, str | int | float] = {"q": query, "limit": limit}
        if near is not None:
            params["lat"] = near.latitude
            params["lon"] = near.longitude
        response = await self._http.get(PHOTON_BASE, params=params)
        response.raise_for_status()
        return parse_photon_response(response.json(), limit=limit)

    async def reverse(self, point: Point) -> PlaceCandidate | None:
        params = {"lat": point.latitude, "lon": point.longitude, "limit": 1}
        # Photon reverse often uses lat/lon with empty q or reverse endpoint;
        # use search with reverse-style params when supported.
        response = await self._http.get(PHOTON_BASE, params=params)
        response.raise_for_status()
        results = parse_photon_response(response.json(), limit=1)
        return results[0] if results else None
