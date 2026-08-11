"""Deterministic journey scoring, no invented TCL duration."""

from __future__ import annotations

from typing import Any

import pytest

from grand_lyon_mcp.domain.common import PlaceRef, ResultStatus, WarningCode
from grand_lyon_mcp.domain.geo import Point
from grand_lyon_mcp.domain.journeys import TravelMode
from grand_lyon_mcp.domain.velov import VelovStation, VelovStationStatus
from grand_lyon_mcp.infrastructure.time import now_paris
from grand_lyon_mcp.services.journey_service import JourneyService
from grand_lyon_mcp.settings import JourneyScoringWeights


class _FakePlaces:
    async def resolve_place(self, **kwargs: Any) -> Any:
        from grand_lyon_mcp.domain.common import make_envelope

        place = kwargs.get("place")
        # origin vs dest via coordinates on PlaceRef
        lat = getattr(place, "latitude", None) or 45.76
        lon = getattr(place, "longitude", None) or 4.86
        return make_envelope(
            status=ResultStatus.OK,
            generated_at=now_paris(),
            summary="ok",
            data={"candidates": [{"id": "p", "name": "X", "latitude": lat, "longitude": lon}]},
        )


class _LiveVelov:
    async def stations_near(
        self, point: Point, *, radius_m: float = 1000, limit: int = 20
    ) -> list[VelovStation]:
        return [
            VelovStation(
                id="1",
                name="Station A",
                latitude=point.latitude + 0.001,
                longitude=point.longitude + 0.001,
                bikes_available=5,
                docks_available=3,
                capacity=20,
                status=VelovStationStatus.OPEN,
                distance_m=80,
                observed_at=now_paris(),
            )
        ]


def test_score_null_duration_penalized() -> None:
    js = JourneyService(places=_FakePlaces(), weights=JourneyScoringWeights())  # type: ignore[arg-type]
    with_dur = js._score(duration_s=600, reliability=0.8, disruptions=0.1, walking_m=200)
    no_dur = js._score(duration_s=None, reliability=0.8, disruptions=0.1, walking_m=200)
    assert no_dur < with_dur


@pytest.mark.asyncio
async def test_tcl_duration_null_without_router() -> None:
    js = JourneyService(
        places=_FakePlaces(),  # type: ignore[arg-type]
        planner=None,
        velov=None,
        parking=None,
    )
    env = await js.options(
        origin=PlaceRef(latitude=45.76, longitude=4.86),
        destination=PlaceRef(latitude=45.75, longitude=4.83),
        modes=["tcl", "walk"],
    )
    opts = env.data["options"]
    tcl = [o for o in opts if o["mode"] == TravelMode.TCL.value]
    assert tcl
    assert tcl[0]["estimated_duration_seconds"] is None
    assert (
        "non disponible" in tcl[0]["summary"].lower() or "non calcul" in tcl[0]["summary"].lower()
    )
    codes = {w.code for w in env.warnings}
    assert WarningCode.ROUTING_UNAVAILABLE.value in codes


@pytest.mark.asyncio
async def test_velov_live_preferred_over_durationless_tcl() -> None:
    js = JourneyService(
        places=_FakePlaces(),  # type: ignore[arg-type]
        planner=None,
        velov=_LiveVelov(),  # type: ignore[arg-type]
        parking=None,
    )
    env = await js.options(
        origin=PlaceRef(latitude=45.76, longitude=4.86),
        destination=PlaceRef(latitude=45.761, longitude=4.861),
        modes=["tcl", "velov"],
    )
    opts = env.data["options"]
    by_mode = {o["mode"]: o for o in opts}
    assert TravelMode.TCL.value in by_mode
    assert TravelMode.VELOV.value in by_mode
    assert by_mode[TravelMode.TCL.value]["estimated_duration_seconds"] is None
    assert by_mode[TravelMode.VELOV.value]["estimated_duration_seconds"] is not None
    # scoring must not prefer duration-less TCL over live velov unfairly
    assert by_mode[TravelMode.VELOV.value]["score"] >= by_mode[TravelMode.TCL.value]["score"]
