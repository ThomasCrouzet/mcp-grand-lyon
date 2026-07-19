"""Transit service: strict line filter + GTFS fallback honesty."""

from __future__ import annotations

from datetime import datetime
from typing import Any

import pytest

from grand_lyon_mcp.domain.common import PlaceRef, ResultStatus, WarningCode
from grand_lyon_mcp.domain.transit import Departure
from grand_lyon_mcp.infrastructure.time import BUSINESS_TZ
from grand_lyon_mcp.services.transit_service import TransitService


class _FakePlaces:
    async def resolve_place(
        self,
        *,
        place: PlaceRef | None = None,
        query: str | None = None,
        limit: int = 5,
        **kwargs: Any,
    ) -> Any:
        from grand_lyon_mcp.domain.common import make_envelope
        from grand_lyon_mcp.infrastructure.time import now_paris

        return make_envelope(
            status=ResultStatus.OK,
            generated_at=now_paris(),
            summary="ok",
            data={
                "candidates": [
                    {
                        "id": "gtfs:stop:BEL1",
                        "name": "Bellecour",
                        "latitude": 45.7578,
                        "longitude": 4.8320,
                    }
                ]
            },
        )


class _WrongLineRealtime:
    """Returns only C12 even when line=A was requested (simulates loose provider)."""

    async def get_departures(
        self,
        stop_id: str,
        *,
        line: str | None = None,
        direction: str | None = None,
        limit: int = 6,
    ) -> list[Departure]:
        return [
            Departure(
                line_id="tcl:ActIV:Line::C12:SYTRAL",
                line_name="C12",
                destination="Sathonay",
                realtime=True,
            )
        ]


class _GtfsStatic:
    async def search_stops(self, query: str, *, limit: int = 10) -> list:
        return []

    async def get_scheduled_departures(
        self,
        stop_id: str,
        *,
        at: datetime,
        line: str | None = None,
        direction: str | None = None,
        limit: int = 6,
    ) -> list[Departure]:
        return [
            Departure(
                line_id="tcl:A",
                line_name="A",
                destination="Vaulx-en-Velin La Soie",
                scheduled_at=at,
                expected_at=at,
                realtime=True,  # provider bug: service must force False
            )
        ]


class _MatchingRealtime:
    async def get_departures(
        self,
        stop_id: str,
        *,
        line: str | None = None,
        direction: str | None = None,
        limit: int = 6,
    ) -> list[Departure]:
        return [
            Departure(
                line_id="tcl:ActIV:Line::A:SYTRAL",
                line_name="A",
                destination="Perrache",
                realtime=True,
            ),
            Departure(
                line_id="tcl:ActIV:Line::C12:SYTRAL",
                line_name="C12",
                destination="Sathonay",
                realtime=True,
            ),
        ]


@pytest.mark.asyncio
async def test_gtfs_fallback_when_strict_line_empty() -> None:
    svc = TransitService(places=_FakePlaces(), realtime=_WrongLineRealtime(), static=_GtfsStatic())  # type: ignore[arg-type]
    env = await svc.next_departures(
        stop=PlaceRef(query="Bellecour"),
        line="A",
        at=datetime(2026, 7, 20, 8, 0, tzinfo=BUSINESS_TZ),
        limit=5,
    )
    assert env.status == ResultStatus.PARTIAL
    deps = env.data["departures"]
    assert deps
    assert all(d["line_name"] == "A" for d in deps)
    assert all(d["realtime"] is False for d in deps)
    codes = {w.code for w in env.warnings}
    assert WarningCode.PARTIAL_RESULT.value in codes or WarningCode.STALE_DATA.value in codes


@pytest.mark.asyncio
async def test_realtime_line_a_dominates() -> None:
    svc = TransitService(
        places=_FakePlaces(),
        realtime=_MatchingRealtime(),
        static=_GtfsStatic(),  # type: ignore[arg-type]
    )
    env = await svc.next_departures(
        stop=PlaceRef(query="Bellecour"),
        line="A",
        limit=5,
    )
    deps = env.data["departures"]
    assert deps
    assert all(d["line_name"] == "A" for d in deps)
    assert all(d["realtime"] is True for d in deps)
    assert env.status == ResultStatus.OK


@pytest.mark.asyncio
async def test_gtfs_never_realtime_flag() -> None:
    svc = TransitService(places=_FakePlaces(), realtime=None, static=_GtfsStatic())  # type: ignore[arg-type]
    env = await svc.next_departures(
        stop=PlaceRef(query="Bellecour"),
        line="A",
        at=datetime(2026, 7, 20, 8, 0, tzinfo=BUSINESS_TZ),
    )
    for d in env.data["departures"]:
        assert d["realtime"] is False
