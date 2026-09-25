"""Single-flight, retry, cache, catalog, source registry, health."""

from __future__ import annotations

import asyncio
from pathlib import Path

import pytest

from grand_lyon_mcp.infrastructure.retry import RetryableHttpError, is_retryable, with_retry
from grand_lyon_mcp.infrastructure.singleflight import SingleFlight
from grand_lyon_mcp.providers.datagrandlyon.catalog import search_catalog
from grand_lyon_mcp.storage.cache import HttpCache, cache_key
from grand_lyon_mcp.storage.database import connect_and_migrate
from grand_lyon_mcp.storage.source_health import SourceHealthStore


@pytest.mark.asyncio
async def test_singleflight_coalesces() -> None:
    sf = SingleFlight()
    calls = 0

    async def work() -> int:
        nonlocal calls
        calls += 1
        await asyncio.sleep(0.05)
        return 42

    a, b = await asyncio.gather(sf.do("k", work), sf.do("k", work))
    assert a == b == 42
    assert calls == 1


def test_retryable() -> None:
    assert is_retryable(RetryableHttpError(503))
    assert not is_retryable(RetryableHttpError(400))

    class ConnectError(Exception):
        pass

    assert is_retryable(ConnectError("x"))
    assert not is_retryable(ValueError("no"))


def test_with_retry_success() -> None:
    n = {"c": 0}

    def flaky() -> str:
        n["c"] += 1
        if n["c"] < 2:
            raise RetryableHttpError(503)
        return "ok"

    assert with_retry(flaky, attempts=3) == "ok"


@pytest.mark.asyncio
async def test_http_cache_roundtrip(tmp_path: Path) -> None:
    conn = await connect_and_migrate(tmp_path / "c.db")
    cache = HttpCache(conn)
    key = cache_key("GET", "https://data.grandlyon.com/x", {"b": "1", "a": "2"}, "src")
    await cache.put(
        key,
        status_code=200,
        content_type="application/json",
        etag='"abc"',
        last_modified=None,
        payload='{"ok": true}',
        ttl_seconds=60,
        maximum_stale_seconds=120,
    )
    entry = await cache.get(key)
    assert entry is not None
    assert cache.is_fresh(entry)
    assert cache.is_within_stale(entry)
    assert cache.parse_json(entry)["ok"] is True
    await conn.close()


def test_search_catalog_exact_hint() -> None:
    entries = [
        {"name": "jcd_jcdecaux.jcdvelov", "title": "Vélo'v"},
        {"name": "other.table", "title": "Other"},
    ]
    found = search_catalog(entries, ["velov"], exact_table_hints=["jcd_jcdecaux.jcdvelov"])
    assert found
    assert "jcdvelov" in found[0]["name"]


@pytest.mark.asyncio
async def test_source_registry_resolve(tmp_path: Path) -> None:
    from grand_lyon_mcp.providers.datagrandlyon.source_registry import SourceRegistry

    conn = await connect_and_migrate(tmp_path / "s.db")
    reg = SourceRegistry(conn)
    await reg.upsert(
        "velov_realtime",
        {
            "provider": "datapusher",
            "enabled": True,
            "exact_table_hints": ["jcd_jcdecaux.jcdvelov"],
            "search_keywords": ["velov"],
        },
    )
    status = await reg.resolve_from_candidates(
        "velov_realtime",
        [{"name": "jcd_jcdecaux.jcdvelov"}],
        exact_hints=["jcd_jcdecaux.jcdvelov"],
    )
    assert status == "OK"
    row = await reg.get("velov_realtime")
    assert row is not None
    assert row["status"] == "OK"
    # ambiguous
    await reg.upsert("amb", {"enabled": True})
    st = await reg.resolve_from_candidates(
        "amb",
        [{"name": "a.t1"}, {"name": "a.t2"}],
        exact_hints=[],
    )
    assert st == "UNRESOLVED"
    await reg.upsert("off", {"enabled": False})
    assert await reg.resolve_from_candidates("off", [{"name": "x"}]) == "DISABLED"
    await conn.close()


@pytest.mark.asyncio
async def test_source_health(tmp_path: Path) -> None:
    conn = await connect_and_migrate(tmp_path / "h.db")
    store = SourceHealthStore(conn)
    await store.record_success("s1", latency_ms=12, schema_hash="abc")
    await store.record_failure("s1", error_code="TIMEOUT", latency_ms=100)
    row = await store.get("s1")
    assert row is not None
    assert row["consecutive_failures"] == 1
    await conn.close()


@pytest.mark.asyncio
async def test_entity_near_and_get(tmp_path: Path) -> None:
    from grand_lyon_mcp.domain.geo import Point
    from grand_lyon_mcp.storage.entity_repository import EntityRepository

    conn = await connect_and_migrate(tmp_path / "e.db")
    repo = EntityRepository(conn)
    await repo.upsert(
        logical_id="lyon:test:1",
        source_id="t",
        provider_id="1",
        entity_type="toilet",
        name="Toilettes Test",
        latitude=45.75,
        longitude=4.85,
        aliases=["wc test"],
    )
    got = await repo.get("lyon:test:1")
    assert got is not None
    near = await repo.search_near(Point(45.75, 4.85), radius_m=500, limit=5)
    assert any(c.id == "lyon:test:1" for c in near)
    fts = await repo.search_fts("Toilettes", limit=5)
    assert fts
    await conn.close()


@pytest.mark.asyncio
async def test_velov_history_reliability(tmp_path: Path) -> None:
    from grand_lyon_mcp.domain.velov import VelovStation
    from grand_lyon_mcp.storage.velov_history import VelovHistoryStore

    conn = await connect_and_migrate(tmp_path / "v.db")
    store = VelovHistoryStore(conn)
    for i in range(10):
        await store.record_snapshot(
            VelovStation(
                id="s1",
                name="S",
                latitude=45.0,
                longitude=4.0,
                bikes_available=5 if i % 2 == 0 else 0,
                docks_available=5,
                capacity=10,
            ),
            slot="08",
        )
    rel = await store.reliability("s1", hour_slot="08", minimum_bikes=1)
    assert rel.sample_count == 10
    assert rel.confidence in {"medium", "high"}
    await conn.close()


def test_domain_errors() -> None:
    from grand_lyon_mcp.domain.errors import (
        AmbiguousPlaceError,
        InvalidRequestError,
        PlaceNotFoundError,
        SourceUnavailableError,
        SourceUnresolvedError,
    )

    assert InvalidRequestError("x").code == "INVALID_REQUEST"
    assert PlaceNotFoundError().code == "PLACE_NOT_FOUND"
    assert AmbiguousPlaceError().code == "AMBIGUOUS_PLACE"
    assert SourceUnavailableError("e", source="s").source == "s"
    assert SourceUnresolvedError("e", source="s").source == "s"


def test_build_provenance() -> None:
    from datetime import UTC, datetime

    from grand_lyon_mcp.domain.provenance import build_provenance

    now = datetime.now(UTC)
    p = build_provenance(
        provider="DGL",
        source_id="v",
        dataset="d",
        attribution="a",
        observed_at=now,
        retrieved_at=now,
        realtime=True,
        maximum_stale_seconds=300,
    )
    assert p.realtime is True
    assert p.stale is False


def test_infra_geo_wgs84() -> None:
    from grand_lyon_mcp.infrastructure.geo import to_wgs84

    lon, _lat = to_wgs84(4.85, 45.75, "EPSG:4326")
    assert lon == 4.85


def test_apply_settings_yaml(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    from grand_lyon_mcp.settings import Settings, apply_settings_yaml

    # Env must not shadow YAML offline for this unit test
    monkeypatch.delenv("GRAND_LYON_MCP_OFFLINE", raising=False)
    monkeypatch.delenv("DATAGRANDLYON_USERNAME", raising=False)
    monkeypatch.delenv("DATAGRANDLYON_PASSWORD", raising=False)

    cfg = tmp_path / "settings.yaml"
    cfg.write_text(
        "version: 1\nruntime:\n  offline: true\n  timezone: Europe/Paris\n"
        "places:\n  default_radius_m: 500\n"
        "journey_scoring:\n  duration: 0.35\n  reliability: 0.25\n  disruptions: 0.15\n"
        "  walking: 0.10\n  availability: 0.10\n  user_preference: 0.05\n",
        encoding="utf-8",
    )
    s = apply_settings_yaml(Settings(), config_dir=tmp_path)
    assert s.offline is True
    assert s.default_radius_m == 500


def test_waste_taxonomy_from_yaml() -> None:
    from grand_lyon_mcp.resources import config_path
    from grand_lyon_mcp.services.waste_service import WasteTaxonomy

    tax = WasteTaxonomy.from_yaml(config_path("waste-taxonomy.yaml"))
    c = tax.classify("peinture glycéro")
    assert c.category.value == "paint"


@pytest.mark.asyncio
async def test_datapusher_table_url() -> None:
    from grand_lyon_mcp.providers.datagrandlyon.datapusher import extract_records, table_url

    assert "rdata" in table_url("rdata", "schema.table")
    assert extract_records([{"a": 1}]) == [{"a": 1}]
    assert extract_records("nope") == []
