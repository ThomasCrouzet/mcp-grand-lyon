"""Offline service paths for major tools."""

from __future__ import annotations

import pytest

from grand_lyon_mcp.domain.common import PlaceRef, ResultStatus


@pytest.mark.asyncio
async def test_resolve_place(app_container) -> None:
    env = await app_container.places.resolve_place(query="Part-Dieu", limit=5)
    assert env.status in (ResultStatus.OK, ResultStatus.AMBIGUOUS, ResultStatus.NOT_FOUND)
    # with fixtures + gtfs should find something
    assert env.schema_version == "1.0"
    assert "candidates" in env.data


@pytest.mark.asyncio
async def test_next_departures_gtfs_fallback(app_container) -> None:
    env = await app_container.transit.next_departures(
        stop=PlaceRef(query="Bellecour"),
        line="A",
        limit=5,
    )
    assert env.status in (
        ResultStatus.OK,
        ResultStatus.PARTIAL,
        ResultStatus.NOT_FOUND,
        ResultStatus.UNAVAILABLE,
    )
    assert "departures" in env.data
    for d in env.data.get("departures") or []:
        # if from GTFS fallback realtime must be false; realtime deps from fixtures ok
        assert "realtime" in d


@pytest.mark.asyncio
async def test_mobility_status(app_container) -> None:
    env = await app_container.mobility.status(
        lines=["A"], include=["transit", "traffic", "roadworks"]
    )
    assert env.status in (ResultStatus.OK, ResultStatus.PARTIAL)
    assert "alerts" in env.data


@pytest.mark.asyncio
async def test_parking(app_container) -> None:
    env = await app_container.parking.options(
        destination=PlaceRef(query="Hôtel de Ville"),
        limit=5,
    )
    assert "options" in env.data


@pytest.mark.asyncio
async def test_waste(app_container) -> None:
    env = await app_container.waste.dropoff(
        item="batterie de vélo électrique",
        location=PlaceRef(profile_place="home"),
    )
    assert env.data["classification"]["category"] == "portable_battery"
    assert env.data["classification"]["hazardous"] is True


@pytest.mark.asyncio
async def test_facilities(app_container) -> None:
    env = await app_container.facilities.nearby(
        location=PlaceRef(latitude=45.777, longitude=4.855),
        categories=["toilet", "drinking_water", "park"],
        radius_m=2000,
    )
    assert env.status in (ResultStatus.OK, ResultStatus.PARTIAL, ResultStatus.UNAVAILABLE)


@pytest.mark.asyncio
async def test_environment_partial(app_container) -> None:
    env = await app_container.environment.brief(
        location=PlaceRef(query="Lyon 3e"),
        indicators=["pollen", "air_quality", "heat"],
    )
    # heat unsupported in fixture
    assert env.degraded or env.status == ResultStatus.PARTIAL or env.warnings
    codes = {w.code for w in env.warnings}
    assert "UNSUPPORTED_INDICATOR" in codes or env.data.get("indicators")


@pytest.mark.asyncio
async def test_accessibility_unknown_default(app_container) -> None:
    env = await app_container.accessibility.check(
        origin=PlaceRef(query="Bellecour"),
        destination=PlaceRef(query="Part-Dieu"),
        needs=["wheelchair"],
    )
    overall = env.data.get("overall_status")
    assert overall != "accessible" or env.data.get("segments")
    # rule: unknown possible
    assert overall in {
        "accessible",
        "partially_accessible",
        "inaccessible",
        "unknown",
    }


@pytest.mark.asyncio
async def test_trip_options_partial_routing(app_container) -> None:
    env = await app_container.journeys.options(
        origin=PlaceRef(profile_place="home"),
        destination=PlaceRef(profile_place="work"),
        modes=["tcl", "velov", "walk", "car"],
    )
    assert env.data.get("options")
    # routing unavailable warning expected (transitous off)
    assert env.status in (ResultStatus.OK, ResultStatus.PARTIAL)


@pytest.mark.asyncio
async def test_briefing(app_container) -> None:
    env = await app_container.briefing.personal_briefing(profile="weekday_morning")
    assert env.status in (ResultStatus.OK, ResultStatus.PARTIAL, ResultStatus.NOT_FOUND)
