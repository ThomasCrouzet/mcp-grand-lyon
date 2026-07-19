"""Vélo'v stations and scoring."""

from __future__ import annotations

from grand_lyon_mcp.domain.common import (
    Envelope,
    PlaceRef,
    ResultStatus,
    WarningItem,
    make_envelope,
)
from grand_lyon_mcp.domain.geo import Point
from grand_lyon_mcp.domain.protocols import VelovProvider
from grand_lyon_mcp.domain.velov import VelovScore
from grand_lyon_mcp.infrastructure.time import now_paris
from grand_lyon_mcp.services.place_service import PlaceService
from grand_lyon_mcp.storage.velov_history import VelovHistoryStore


def score_station(
    *,
    bikes: int,
    docks: int,
    capacity: int,
    distance_m: float,
    max_walking_m: float,
    age_seconds: int,
    max_age: int,
    reliability: float,
    sample_count: int,
    weights: dict[str, float] | None = None,
) -> VelovScore:
    w = weights or {
        "availability": 0.4,
        "distance": 0.3,
        "freshness": 0.15,
        "historical": 0.15,
    }
    cap = max(capacity, 1)
    availability_score = min(1.0, bikes / max(1, min(5, cap)))
    distance_score = max(0.0, 1.0 - (distance_m / max(max_walking_m, 1)))
    freshness_score = max(0.0, 1.0 - (age_seconds / max(max_age, 1)))
    hist = reliability if sample_count >= 5 else 0.0
    total = (
        w["availability"] * availability_score
        + w["distance"] * distance_score
        + w["freshness"] * freshness_score
        + w["historical"] * hist
    )
    return VelovScore(
        station_id="",
        score=min(1.0, max(0.0, total)),
        availability_score=availability_score,
        distance_score=distance_score,
        freshness_score=freshness_score,
        historical_reliability_score=hist,
    )


class VelovService:
    def __init__(
        self,
        *,
        places: PlaceService,
        provider: VelovProvider | None = None,
        history: VelovHistoryStore | None = None,
    ) -> None:
        self._places = places
        self._provider = provider
        self._history = history

    async def stations_near_ref(
        self,
        location: PlaceRef,
        *,
        radius_m: float = 1000,
        minimum_bikes: int = 0,
        limit: int = 10,
    ) -> Envelope:
        generated = now_paris()
        point, _, st = await self._places.resolve_point(location)
        if point is None and location.query:
            env = await self._places.resolve_place(place=location, limit=1)
            cands = env.data.get("candidates") or []
            if cands:
                point = Point(cands[0]["latitude"], cands[0]["longitude"])
                st = ResultStatus.OK
        if point is None or st == ResultStatus.NOT_FOUND:
            return make_envelope(
                status=ResultStatus.NOT_FOUND,
                generated_at=generated,
                summary="Lieu introuvable pour Vélo'v.",
                data={"stations": []},
            )
        if self._provider is None:
            return make_envelope(
                status=ResultStatus.UNAVAILABLE,
                generated_at=generated,
                summary="Source Vélo'v indisponible.",
                data={"stations": []},
                degraded=True,
                warnings=[
                    WarningItem(
                        code="SOURCE_UNAVAILABLE",
                        source="velov_realtime",
                        message="Provider Vélo'v non configuré.",
                        retryable=True,
                    )
                ],
            )
        stations = await self._provider.stations_near(point, radius_m=radius_m, limit=limit * 2)
        if minimum_bikes:
            stations = [s for s in stations if s.bikes_available >= minimum_bikes]
        stations = stations[:limit]
        if self._history:
            for s in stations:
                await self._history.record_snapshot(s)
        return make_envelope(
            status=ResultStatus.OK,
            generated_at=generated,
            summary=f"{len(stations)} station(s) Vélo'v.",
            data={"stations": [s.model_dump(mode="json") for s in stations]},
        )
