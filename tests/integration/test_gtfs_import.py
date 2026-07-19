"""GTFS import + scheduled departures (offline)."""

from __future__ import annotations

from datetime import datetime
from pathlib import Path

import pytest

from grand_lyon_mcp.infrastructure.time import BUSINESS_TZ
from grand_lyon_mcp.providers.gtfs.importer import import_gtfs_zip
from grand_lyon_mcp.providers.gtfs.repository import GtfsRepository
from grand_lyon_mcp.storage.database import connect_and_migrate


@pytest.mark.asyncio
async def test_import_and_search(tmp_path: Path, fixtures_dir: Path) -> None:
    conn = await connect_and_migrate(tmp_path / "g.db")
    counts = await import_gtfs_zip(conn, fixtures_dir / "gtfs" / "mini_gtfs.zip")
    assert counts["stops"] >= 3
    repo = GtfsRepository(conn)
    stops = await repo.search_stops("Bellecour")
    assert stops
    assert "Bellecour" in stops[0].name
    at = datetime(2026, 7, 20, 8, 0, tzinfo=BUSINESS_TZ)
    deps = await repo.get_scheduled_departures(stops[0].id, at=at, line="A", limit=5)
    assert deps
    assert all(d.realtime is False for d in deps)
    await conn.close()
