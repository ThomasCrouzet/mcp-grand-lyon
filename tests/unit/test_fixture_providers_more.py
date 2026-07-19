"""Exercise fixture providers thoroughly."""

from __future__ import annotations

from pathlib import Path

import pytest

from grand_lyon_mcp.domain.facilities import FacilityCategory
from grand_lyon_mcp.domain.geo import Point
from grand_lyon_mcp.domain.parking import ParkingType
from grand_lyon_mcp.domain.waste import WasteCategory
from grand_lyon_mcp.providers.fixture_providers import (
    FixtureAccessibility,
    FixtureEnvironment,
    FixtureFacilities,
    FixtureParking,
    FixturePlaceProvider,
    FixtureTraffic,
    FixtureTransitAlerts,
    FixtureTransitRealtime,
    FixtureVelov,
    FixtureWaste,
)


@pytest.mark.asyncio
async def test_all_fixtures(fixtures_dir: Path) -> None:
    p = Point(45.76, 4.85)
    places = FixturePlaceProvider(fixtures_dir)
    assert await places.search("Part")
    assert await places.reverse(p)

    rt = FixtureTransitRealtime(fixtures_dir)
    deps = await rt.get_departures("x", line="A", limit=5)
    assert deps

    al = FixtureTransitAlerts(fixtures_dir)
    assert await al.get_alerts(lines=["A"])

    acc = FixtureAccessibility(fixtures_dir)
    assert await acc.get_incidents()
    assert await acc.check_stop("Bellecour")

    velov = FixtureVelov(fixtures_dir)
    assert await velov.stations_near(p, radius_m=5000)

    park = FixtureParking(fixtures_dir)
    assert await park.options_near(p, types=[ParkingType.PUBLIC_PARKING], radius_m=10000)

    tr = FixtureTraffic(fixtures_dir)
    assert await tr.conditions()
    assert await tr.road_events()

    fac = FixtureFacilities(fixtures_dir)
    assert await fac.nearby(
        Point(45.777, 4.855),
        categories=[FacilityCategory.TOILET, FacilityCategory.PARK],
        radius_m=3000,
    )

    env = FixtureEnvironment(fixtures_dir)
    vals = await env.indicators(p, indicators=["pollen", "heat", "nope"])
    assert vals

    waste = FixtureWaste(fixtures_dir)
    assert await waste.facilities_near(p, category=WasteCategory.PORTABLE_BATTERY, radius_m=50000)
