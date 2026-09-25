"""Parking honesty: no invented spaces; P+R real distance; live/partial warnings."""

from __future__ import annotations

from typing import Any

import pytest

from grand_lyon_mcp.domain.common import PlaceRef, ResultStatus, WarningCode
from grand_lyon_mcp.domain.geo import Point, haversine_m
from grand_lyon_mcp.domain.parking import ParkingOption, ParkingStatus, ParkingType
from grand_lyon_mcp.services.parking_service import ParkingService


class _FakePlaces:
    async def resolve_place(self, **kwargs: Any) -> Any:
        from grand_lyon_mcp.domain.common import make_envelope
        from grand_lyon_mcp.infrastructure.time import now_paris

        return make_envelope(
            status=ResultStatus.OK,
            generated_at=now_paris(),
            summary="ok",
            data={
                "candidates": [
                    {
                        "id": "poi:hdv",
                        "name": "Hôtel de Ville",
                        "latitude": 45.7675,
                        "longitude": 4.8355,
                    }
                ]
            },
        )


class _CapacityOnlyProvider:
    """Public parking with capacity only, must not invent available_spaces."""

    live_dispo_resolved = False

    async def options_near(
        self,
        point: Point,
        *,
        types: list[ParkingType] | None = None,
        radius_m: float = 1500,
        minimum_spaces: int = 0,
        limit: int = 10,
    ) -> list[ParkingOption]:
        return [
            ParkingOption(
                id="p1",
                name="Parking Capacité",
                type=ParkingType.PUBLIC_PARKING,
                latitude=45.768,
                longitude=4.836,
                capacity=400,
                available_spaces=None,
                status=ParkingStatus.UNKNOWN,
                distance_m=haversine_m(point, Point(45.768, 4.836)),
                realtime=False,
            )
        ]


class _LiveDispoProvider:
    live_dispo_resolved = True

    async def options_near(
        self,
        point: Point,
        *,
        types: list[ParkingType] | None = None,
        radius_m: float = 1500,
        minimum_spaces: int = 0,
        limit: int = 10,
    ) -> list[ParkingOption]:
        return [
            ParkingOption(
                id="p-live",
                name="Parking Live",
                type=ParkingType.PUBLIC_PARKING,
                latitude=45.768,
                longitude=4.836,
                capacity=400,
                available_spaces=42,
                status=ParkingStatus.OPEN,
                distance_m=haversine_m(point, Point(45.768, 4.836)),
                realtime=True,
            ),
            ParkingOption(
                id="pr1",
                name="P+R Near",
                type=ParkingType.PARK_AND_RIDE,
                latitude=45.77,
                longitude=4.84,
                capacity=800,
                available_spaces=120,
                status=ParkingStatus.OPEN,
                distance_m=haversine_m(point, Point(45.77, 4.84)),
                realtime=True,
            ),
            ParkingOption(
                id="pr2",
                name="P+R Far",
                type=ParkingType.PARK_AND_RIDE,
                latitude=45.80,
                longitude=4.90,
                capacity=500,
                available_spaces=10,
                status=ParkingStatus.OPEN,
                distance_m=haversine_m(point, Point(45.80, 4.90)),
                realtime=True,
            ),
        ]


@pytest.mark.asyncio
async def test_capacity_only_no_invented_spaces_partial() -> None:
    svc = ParkingService(places=_FakePlaces(), provider=_CapacityOnlyProvider())  # type: ignore[arg-type]
    env = await svc.options(destination=PlaceRef(query="Hôtel de Ville"), limit=5)
    assert env.status == ResultStatus.PARTIAL
    opts = env.data["options"]
    assert opts
    assert all(o["available_spaces"] is None for o in opts)
    assert all(o["realtime"] is False for o in opts)
    codes = {w.code for w in env.warnings}
    assert WarningCode.PARTIAL_RESULT.value in codes or WarningCode.SOURCE_UNRESOLVED.value in codes


@pytest.mark.asyncio
async def test_live_dispo_present() -> None:
    svc = ParkingService(places=_FakePlaces(), provider=_LiveDispoProvider())  # type: ignore[arg-type]
    env = await svc.options(destination=PlaceRef(query="Hôtel de Ville"), limit=5)
    opts = env.data["options"]
    assert any(o.get("available_spaces") is not None for o in opts)
    live = [o for o in opts if o.get("available_spaces") is not None]
    assert live[0]["realtime"] is True
