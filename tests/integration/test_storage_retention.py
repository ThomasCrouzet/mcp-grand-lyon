"""Real SQLite retention checks; these stores have no public MCP endpoint."""

from __future__ import annotations

import asyncio
from datetime import UTC, datetime, timedelta

from grand_lyon_mcp.storage import cache as cache_module
from grand_lyon_mcp.storage import source_health
from grand_lyon_mcp.storage.cache import HttpCache, cache_key
from grand_lyon_mcp.storage.database import connect_and_migrate
from grand_lyon_mcp.storage.source_health import SourceHealthStore


async def put(cache, key, payload="{}", ttl=30, stale=180):
    await cache.put(
        key,
        status_code=200,
        content_type="application/json",
        etag=None,
        last_modified=None,
        payload=payload,
        ttl_seconds=ttl,
        maximum_stale_seconds=stale,
    )


async def test_expiry_and_source_specific_stale_warnings(tmp_path, monkeypatch):
    clock = datetime(2026, 7, 20, 6, tzinfo=UTC)
    monkeypatch.setattr(cache_module, "now_utc", lambda: clock)
    conn = await connect_and_migrate(tmp_path / "cache.db")
    try:
        cache = HttpCache(conn)
        transit = cache_key("GET", "https://data.grandlyon.com/x", None, "tcl_departures")
        parking = cache_key("GET", "https://data.grandlyon.com/x", None, "parking_realtime")
        await put(cache, transit, ttl=30, stale=180)
        await put(cache, parking, ttl=60, stale=300)
        clock += timedelta(seconds=45)
        entry, warnings = await cache.get_usable(transit, allow_stale=True)
        assert entry and [w.code for w in warnings] == ["STALE_DATA"]
        assert await cache.get_usable(transit) == (None, [])
        entry, warnings = await cache.get_usable(parking, allow_stale=True)
        assert entry and warnings == []
        clock += timedelta(seconds=135)
        assert await cache.get_usable(transit, allow_stale=True) == (None, [])
        await cache.prune()
        assert await cache.get(transit) is None
        assert await cache.get(parking) is not None
        clock += timedelta(seconds=120)
        await cache.prune()
        assert await cache.get(parking) is None
    finally:
        await conn.close()


async def test_count_bytes_concurrency_and_reopen(tmp_path, monkeypatch):
    clock = datetime(2026, 7, 20, 6, tzinfo=UTC)
    monkeypatch.setattr(cache_module, "now_utc", lambda: clock)
    path = tmp_path / "cache.db"
    conn = await connect_and_migrate(path)
    try:
        cache = HttpCache(conn, max_entries=3, max_bytes=12)
        await put(cache, "old", "é" * 4)
        clock += timedelta(seconds=1)
        await put(cache, "new", "é" * 4)
        assert await cache.get("old") is None  # UTF-8 bytes, not characters
        assert await cache.get("new") is not None
        await put(cache, "new", "x" * 13)
        assert await cache.get("new") is None  # no old value after oversized replacement
        await asyncio.gather(*(put(cache, f"key-{i}") for i in range(12)))
        async with conn.execute(
            "SELECT count(*), sum(length(CAST(payload AS BLOB))) FROM http_cache"
        ) as cur:
            count, size = await cur.fetchone()
        assert count == 3 and size <= 12
    finally:
        await conn.close()
    conn = await connect_and_migrate(path)
    try:
        cache = HttpCache(conn, max_entries=2, max_bytes=12)
        await cache.prune()
        async with conn.execute("SELECT count(*) FROM http_cache") as cur:
            assert (await cur.fetchone())[0] == 2
    finally:
        await conn.close()


async def test_health_uses_latest_event_and_bounds_count(tmp_path, monkeypatch):
    clock = datetime(2026, 7, 20, 6, tzinfo=UTC)
    monkeypatch.setattr(source_health, "now_utc", lambda: clock)
    conn = await connect_and_migrate(tmp_path / "health.db")
    try:
        store = SourceHealthStore(conn, max_entries=2, retention_seconds=60)
        await store.record_success("old", latency_ms=1)
        clock += timedelta(seconds=30)
        await store.record_success("active", latency_ms=2, schema_hash="fixture")
        clock += timedelta(seconds=30)
        await store.record_failure("active", error_code="TIMEOUT")
        await store.record_success("new", latency_ms=3)
        assert await store.get("old") is None
        active = await store.get("active")
        assert active["consecutive_failures"] == 1
        assert active["last_schema_hash"] == "fixture"
        clock += timedelta(seconds=1)
        await store.record_success("newest", latency_ms=4)
        async with conn.execute("SELECT count(*) FROM source_health") as cur:
            assert (await cur.fetchone())[0] == 2
        clock += timedelta(seconds=60)
        await store.prune()
        assert await store.get("newest") is None
    finally:
        await conn.close()
