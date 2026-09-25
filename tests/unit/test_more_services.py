"""Extra service edge cases for coverage."""

from __future__ import annotations

from datetime import datetime

import pytest

from grand_lyon_mcp.domain.common import PlaceRef, ResultStatus
from grand_lyon_mcp.domain.geo import Point
from grand_lyon_mcp.infrastructure.time import BUSINESS_TZ
from grand_lyon_mcp.providers.gtfs.repository import GtfsRepository
from grand_lyon_mcp.services.velov_service import VelovService


@pytest.mark.asyncio
async def test_gtfs_get_stop(app_container) -> None:
    repo = GtfsRepository(app_container.conn)
    stop = await repo.get_stop("gtfs:stop:BEL1")
    assert stop is not None


@pytest.mark.asyncio
async def test_place_coords_and_profile(app_container) -> None:
    env = await app_container.places.resolve_place(
        place=PlaceRef(latitude=45.76, longitude=4.86),
        limit=1,
    )
    assert env.status == ResultStatus.OK
    env2 = await app_container.places.resolve_place(place=PlaceRef(profile_place="home"), limit=1)
    assert env2.status in (ResultStatus.OK, ResultStatus.NOT_FOUND)
    env3 = await app_container.places.resolve_place(place=PlaceRef(place_id="missing:id"), limit=1)
    assert env3.status == ResultStatus.NOT_FOUND


@pytest.mark.asyncio
async def test_velov_and_snapshot_path(app_container) -> None:
    env = await app_container.velov.stations_near_ref(
        PlaceRef(latitude=45.76, longitude=4.86),
        radius_m=3000,
        minimum_bikes=1,
        limit=5,
    )
    assert env.status in (ResultStatus.OK, ResultStatus.PARTIAL, ResultStatus.UNAVAILABLE)
    # provider none path
    bare = VelovService(places=app_container.places, provider=None)
    env2 = await bare.stations_near_ref(PlaceRef(latitude=45.76, longitude=4.86))
    assert env2.status == ResultStatus.UNAVAILABLE


@pytest.mark.asyncio
async def test_journey_without_velov_parking(app_container) -> None:
    from grand_lyon_mcp.services.journey_service import JourneyService

    js = JourneyService(places=app_container.places, planner=None, velov=None, parking=None)
    env = await js.options(
        origin=PlaceRef(latitude=45.76, longitude=4.86),
        destination=PlaceRef(latitude=45.75, longitude=4.83),
        modes=["walk", "car", "tcl"],
    )
    assert env.data.get("options")


@pytest.mark.asyncio
async def test_transitous_disabled() -> None:
    from grand_lyon_mcp.infrastructure.http import HttpClient
    from grand_lyon_mcp.providers.routing.transitous import TransitousPlanner

    http = HttpClient(offline=True)
    planner = TransitousPlanner(http, enabled=False)
    opts = await planner.plan(
        Point(45.76, 4.86),
        Point(45.75, 4.83),
        departure_at=datetime.now(BUSINESS_TZ),
        modes=[],
    )
    assert opts == []
    await http.aclose()


@pytest.mark.asyncio
async def test_facility_parking_env_edges(app_container) -> None:
    env = await app_container.facilities.nearby(
        location=PlaceRef(query="nowhere-xyz-unknown-place-zz"),
        categories=["toilet"],
    )
    assert env.status in (ResultStatus.NOT_FOUND, ResultStatus.OK, ResultStatus.UNAVAILABLE)

    env2 = await app_container.parking.options(
        destination=PlaceRef(latitude=45.767, longitude=4.835),
        types=["public_parking", "park_and_ride"],
        minimum_spaces=1,
    )
    assert "options" in env2.data

    from grand_lyon_mcp.services.environment_service import EnvironmentService

    bare = EnvironmentService(places=app_container.places, provider=None)
    env3 = await bare.brief(location=PlaceRef(query="Lyon"), indicators=["heat"])
    assert env3.degraded


@pytest.mark.asyncio
async def test_accessibility_provider_none(app_container) -> None:
    from grand_lyon_mcp.services.accessibility_service import AccessibilityService

    bare = AccessibilityService(places=app_container.places, provider=None)
    env = await bare.check(origin=PlaceRef(query="Bellecour"))
    assert env.data["overall_status"] == "unknown"


@pytest.mark.asyncio
async def test_dispatch_invalid_tool(app_container) -> None:
    from grand_lyon_mcp.adapters.mcp.tools import dispatch_tool

    r = await dispatch_tool(app_container, "not_a_tool", {})
    assert r["status"] == "invalid_request"


@pytest.mark.asyncio
async def test_mobility_empty_provider() -> None:
    from grand_lyon_mcp.services.mobility_status_service import MobilityStatusService

    svc = MobilityStatusService()
    env = await svc.status(include=["transit"])
    assert env.status in (ResultStatus.OK, ResultStatus.PARTIAL)


def test_serializers() -> None:
    from grand_lyon_mcp.adapters.mcp.serializers import envelope_to_dict, envelope_to_json
    from grand_lyon_mcp.domain.common import ResultStatus, make_envelope
    from grand_lyon_mcp.infrastructure.time import now_paris

    env = make_envelope(status=ResultStatus.OK, generated_at=now_paris(), summary="ok")
    d = envelope_to_dict(env)
    assert d["status"] == "ok"
    assert "request_id" in envelope_to_json(env)


@pytest.mark.asyncio
async def test_http_retry_success(respx_mock) -> None:
    import httpx

    from grand_lyon_mcp.infrastructure.http import HttpClient

    route = respx_mock.get("https://data.grandlyon.com/ok").mock(
        side_effect=[
            httpx.Response(503),
            httpx.Response(200, json={"ok": True}),
        ]
    )
    client = HttpClient(offline=False, connect_timeout=1, read_timeout=1)
    resp = await client.get("https://data.grandlyon.com/ok")
    assert resp.status_code == 200
    assert route.call_count == 2
    await client.aclose()
