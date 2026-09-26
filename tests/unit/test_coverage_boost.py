"""Small tests to cover remaining high-value branches."""

from __future__ import annotations

from datetime import UTC, datetime, timedelta
from pathlib import Path

import httpx
import pytest
import respx

from grand_lyon_mcp.domain.common import PlaceRef
from grand_lyon_mcp.domain.geo import Point
from grand_lyon_mcp.domain.journeys import TravelMode
from grand_lyon_mcp.infrastructure.http import HttpClient
from grand_lyon_mcp.infrastructure.redaction import is_sensitive_key, redact_mapping
from grand_lyon_mcp.infrastructure.time import BUSINESS_TZ, to_paris, to_utc
from grand_lyon_mcp.providers.datagrandlyon.catalog import fetch_catalog
from grand_lyon_mcp.providers.datagrandlyon.client import DataGrandLyonClient
from grand_lyon_mcp.providers.datagrandlyon.datapusher import query_table
from grand_lyon_mcp.providers.routing.transitous import TransitousPlanner
from grand_lyon_mcp.storage.cache import HttpCache, normalize_url
from grand_lyon_mcp.storage.database import connect_and_migrate


def test_redact_mapping_list_and_key() -> None:
    assert is_sensitive_key("Password")
    out = redact_mapping(
        {
            "items": [{"token": "abc", "ok": 1}, "password=secret"],
            "plain": 3,
        },
        extra_secrets=["abc"],
    )
    assert out["items"][0]["token"] == "[REDACTED]"
    assert "secret" not in str(out)
    assert out["plain"] == 3


def test_naive_datetime_conversions() -> None:
    naive = datetime(2026, 1, 1, 12, 0, 0)
    assert to_paris(naive).tzinfo is not None
    assert to_utc(naive).tzinfo is not None


@pytest.mark.asyncio
@respx.mock
async def test_catalog_and_datapusher_query() -> None:
    respx.get("https://data.grandlyon.com/fr/datapusher/ws/rdata/all.json").mock(
        return_value=httpx.Response(200, json={"results": [{"name": "a.b"}]})
    )
    respx.get("https://data.grandlyon.com/fr/datapusher/ws/rdata/schema.table/all.json").mock(
        return_value=httpx.Response(200, json={"values": [{"id": 1}]})
    )
    http = HttpClient(offline=False)
    dgl = DataGrandLyonClient(http, username="u", password="p")
    assert dgl.authenticated
    entries = await fetch_catalog(dgl, "rdata")
    assert entries
    rows = await query_table(dgl, service="rdata", schema_table="schema.table", maxfeatures=2)
    assert rows
    await http.aclose()


@pytest.mark.asyncio
@respx.mock
async def test_transitous_enabled_path() -> None:
    respx.get("https://api.transitous.org/api/v5/plan").mock(
        return_value=httpx.Response(
            200, json={"itineraries": [{"duration": 1000, "summary": "metro"}]}
        )
    )
    http = HttpClient(offline=False, transitous_enabled=True)
    planner = TransitousPlanner(http, enabled=True)
    opts = await planner.plan(
        Point(45.76, 4.86),
        Point(45.75, 4.83),
        departure_at=datetime.now(BUSINESS_TZ),
        modes=[TravelMode.TCL],
    )
    assert opts
    assert opts[0].sources == ["transitous"]
    await http.aclose()


@pytest.mark.asyncio
async def test_cache_purge_and_normalize(tmp_path: Path) -> None:
    assert "a=1" in normalize_url("https://example.com/x?b=2", {"a": 1})
    conn = await connect_and_migrate(tmp_path / "p.db")
    cache = HttpCache(conn)
    await cache.put(
        "oldkey",
        status_code=200,
        content_type="text/plain",
        etag=None,
        last_modified=None,
        payload="x",
        ttl_seconds=60,
        maximum_stale_seconds=120,
    )
    # Seed a usable entry before forcing expiry; writes now prune expired entries.
    await conn.execute(
        "UPDATE http_cache SET maximum_stale_at = ? WHERE cache_key = ?",
        ((datetime.now(UTC) - timedelta(hours=1)).isoformat(), "oldkey"),
    )
    await conn.commit()
    n = await cache.purge_expired()
    assert n >= 1
    await conn.close()


@pytest.mark.asyncio
async def test_singleflight_exception() -> None:
    from grand_lyon_mcp.infrastructure.singleflight import SingleFlight

    sf = SingleFlight()

    async def boom() -> int:
        raise RuntimeError("fail")

    with pytest.raises(RuntimeError):
        await sf.do("e", boom)


@pytest.mark.asyncio
async def test_parking_unavailable_provider(app_container) -> None:
    from grand_lyon_mcp.services.parking_service import ParkingService

    svc = ParkingService(places=app_container.places, provider=None)
    env = await svc.options(destination=PlaceRef(latitude=45.76, longitude=4.85))
    assert env.status.value == "unavailable"


@pytest.mark.asyncio
async def test_facility_unavailable(app_container) -> None:
    from grand_lyon_mcp.services.facility_service import FacilityService

    svc = FacilityService(places=app_container.places, provider=None)
    env = await svc.nearby(
        location=PlaceRef(latitude=45.76, longitude=4.85),
        categories=["toilet"],
    )
    assert env.status.value == "unavailable"


@pytest.mark.asyncio
async def test_briefing_missing_profile(app_container) -> None:
    env = await app_container.briefing.personal_briefing(profile="does_not_exist")
    assert env.status.value == "not_found"


def test_infra_geo_pyproj() -> None:
    from grand_lyon_mcp.infrastructure.geo import to_wgs84

    # Web mercator-ish of Lyon-ish
    lon, lat = to_wgs84(539958.0, 5741471.0, "EPSG:3857")
    assert 4.0 < lon < 5.5
    assert 45.0 < lat < 46.5


def test_logging_exc_and_extra(capsys) -> None:
    import logging

    from grand_lyon_mcp.infrastructure.logging import setup_logging

    setup_logging("INFO")
    log = logging.getLogger("grand_lyon_mcp.testexc")
    try:
        raise ValueError("password=supersecret")
    except ValueError:
        log.exception("boom")
    err = capsys.readouterr().err
    assert "supersecret" not in err or "[REDACTED]" in err


@pytest.mark.asyncio
async def test_journey_with_parking_and_velov(app_container) -> None:
    env = await app_container.journeys.options(
        origin=PlaceRef(profile_place="home"),
        destination=PlaceRef(profile_place="work"),
        modes=["velov", "park_and_ride", "walk"],
        preferences={"minimum_velov_bikes": 1, "minimum_velov_docks": 1, "max_walking_m": 5000},
    )
    modes = {o["mode"] for o in env.data.get("options") or []}
    assert "walk" in modes
