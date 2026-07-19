"""Additional transit service branches for honesty + coverage."""

from __future__ import annotations

from datetime import datetime
from typing import Any

import pytest

from grand_lyon_mcp.domain.common import PlaceRef, ResultStatus
from grand_lyon_mcp.domain.transit import Departure
from grand_lyon_mcp.infrastructure.time import BUSINESS_TZ
from grand_lyon_mcp.services.transit_service import TransitService


class _PlacesAmbiguous:
    async def resolve_place(self, **kwargs: Any) -> Any:
        from grand_lyon_mcp.domain.common import make_envelope
        from grand_lyon_mcp.infrastructure.time import now_paris

        return make_envelope(
            status=ResultStatus.AMBIGUOUS,
            generated_at=now_paris(),
            summary="ambiguous",
            data={
                "candidates": [
                    {
                        "id": "gtfs:stop:BEL1",
                        "name": "Bellecour",
                        "latitude": 45.7578,
                        "longitude": 4.8320,
                    },
                    {
                        "id": "gtfs:stop:BEL2",
                        "name": "Bellecour A. Poncet",
                        "latitude": 45.756,
                        "longitude": 4.833,
                    },
                ]
            },
        )


class _PlacesNotFound:
    async def resolve_place(self, **kwargs: Any) -> Any:
        from grand_lyon_mcp.domain.common import make_envelope
        from grand_lyon_mcp.infrastructure.time import now_paris

        return make_envelope(
            status=ResultStatus.NOT_FOUND,
            generated_at=now_paris(),
            summary="missing",
            data={"candidates": []},
        )


class _RtFail:
    async def get_departures(self, *a: Any, **k: Any) -> list[Departure]:
        raise RuntimeError("siri down")


class _StaticEmpty:
    async def search_stops(self, query: str, *, limit: int = 10) -> list:
        return []

    async def get_scheduled_departures(self, *a: Any, **k: Any) -> list[Departure]:
        return []


class _StaticOk:
    async def search_stops(self, query: str, *, limit: int = 10) -> list:
        return []

    async def get_scheduled_departures(self, *a: Any, **k: Any) -> list[Departure]:
        at = k.get("at") or datetime(2026, 7, 20, 8, 0, tzinfo=BUSINESS_TZ)
        return [
            Departure(
                line_id="tcl:A",
                line_name="A",
                destination="Vaulx",
                scheduled_at=at,
                expected_at=at,
                realtime=False,
            )
        ]


@pytest.mark.asyncio
async def test_stop_not_found() -> None:
    svc = TransitService(places=_PlacesNotFound(), realtime=None, static=None)  # type: ignore[arg-type]
    env = await svc.next_departures(stop=PlaceRef(query="nowhere"))
    assert env.status == ResultStatus.NOT_FOUND


@pytest.mark.asyncio
async def test_ambiguous_prefers_exact_name() -> None:
    svc = TransitService(
        places=_PlacesAmbiguous(),  # type: ignore[arg-type]
        realtime=None,
        static=_StaticOk(),  # type: ignore[arg-type]
    )
    env = await svc.next_departures(
        stop=PlaceRef(query="Bellecour"),
        line="A",
        at=datetime(2026, 7, 20, 8, 0, tzinfo=BUSINESS_TZ),
    )
    assert env.data["stop"]["name"] == "Bellecour"
    assert env.data["departures"]
    assert all(d["realtime"] is False for d in env.data["departures"])


@pytest.mark.asyncio
async def test_realtime_exception_falls_back_gtfs() -> None:
    svc = TransitService(
        places=_PlacesAmbiguous(),  # type: ignore[arg-type]
        realtime=_RtFail(),  # type: ignore[arg-type]
        static=_StaticOk(),  # type: ignore[arg-type]
    )
    env = await svc.next_departures(
        stop=PlaceRef(query="Bellecour"),
        line="A",
        at=datetime(2026, 7, 20, 8, 0, tzinfo=BUSINESS_TZ),
    )
    assert env.data["departures"]
    assert all(d["realtime"] is False for d in env.data["departures"])
    assert env.degraded


@pytest.mark.asyncio
async def test_no_departures_unavailable() -> None:
    svc = TransitService(
        places=_PlacesAmbiguous(),  # type: ignore[arg-type]
        realtime=_RtFail(),  # type: ignore[arg-type]
        static=_StaticEmpty(),  # type: ignore[arg-type]
    )
    env = await svc.next_departures(stop=PlaceRef(query="Bellecour"), line="A")
    assert env.status in (ResultStatus.UNAVAILABLE, ResultStatus.NOT_FOUND)
    assert env.data["departures"] == []
