"""Optional Transitous journey planner."""

from __future__ import annotations

from datetime import datetime
from typing import Any

from grand_lyon_mcp.domain.geo import Point
from grand_lyon_mcp.domain.journeys import TravelMode, TripOption
from grand_lyon_mcp.infrastructure.http import HttpClient


class TransitousPlanner:
    def __init__(
        self,
        http: HttpClient,
        *,
        base_url: str = "https://api.transitous.org/api/",
        enabled: bool = False,
    ) -> None:
        self._http = http
        self._base = base_url.rstrip("/") + "/"
        self.enabled = enabled

    async def plan(
        self,
        origin: Point,
        destination: Point,
        *,
        departure_at: datetime,
        modes: list[TravelMode],
        preferences: dict[str, Any] | None = None,
    ) -> list[TripOption]:
        if not self.enabled:
            return []
        params = {
            "fromPlace": f"{origin.latitude},{origin.longitude}",
            "toPlace": f"{destination.latitude},{destination.longitude}",
            "time": departure_at.isoformat(),
        }
        try:
            response = await self._http.get(self._base + "v5/plan", params=params)
            response.raise_for_status()
            data = response.json()
        except Exception:
            return []
        options: list[TripOption] = []
        itineraries: list[Any] = []
        if isinstance(data, dict):
            raw = data.get("itineraries") or data.get("plans") or []
            if isinstance(raw, list):
                itineraries = raw
        for it in itineraries[:5]:
            if not isinstance(it, dict):
                continue
            duration = int(it.get("duration") or it.get("durationSeconds") or 0)
            options.append(
                TripOption(
                    mode=TravelMode.TCL,
                    score=0.7,
                    estimated_duration_seconds=duration,
                    summary=str(it.get("summary") or "Itinéraire Transitous"),
                    realtime=False,
                    sources=["transitous"],
                )
            )
        return options
