"""GTFS import + scheduled departures (offline)."""

from __future__ import annotations

import zipfile
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


@pytest.mark.asyncio
async def test_failed_import_preserves_existing_feed(tmp_path: Path, fixtures_dir: Path) -> None:
    """A malformed refresh must not replace the last usable GTFS feed."""
    conn = await connect_and_migrate(tmp_path / "g.db")
    try:
        source = fixtures_dir / "gtfs" / "mini_gtfs.zip"
        await import_gtfs_zip(conn, source)
        before = {
            table: (await (await conn.execute(f"SELECT count(*) FROM {table}")).fetchone())[0]
            for table in ("gtfs_routes", "gtfs_stops", "gtfs_trips", "gtfs_stop_times")
        }

        malformed = tmp_path / "malformed.zip"
        with zipfile.ZipFile(source) as input_zip, zipfile.ZipFile(malformed, "w") as output_zip:
            for info in input_zip.infolist():
                payload = input_zip.read(info.filename)
                if info.filename.endswith("stops.txt"):
                    payload = payload.replace(b"45.7578", b"not-a-latitude", 1)
                output_zip.writestr(info, payload)

        with pytest.raises(ValueError):
            await import_gtfs_zip(conn, malformed)

        after = {
            table: (await (await conn.execute(f"SELECT count(*) FROM {table}")).fetchone())[0]
            for table in before
        }
        assert after == before
        assert not conn.in_transaction
    finally:
        await conn.close()
