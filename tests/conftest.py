"""Shared pytest fixtures — offline by default."""

from __future__ import annotations

import os
from pathlib import Path

import pytest

# Ensure no accidental live network from env during unit/integration tests
os.environ.setdefault("GRAND_LYON_MCP_OFFLINE", "true")
os.environ.pop("DATAGRANDLYON_PASSWORD", None)


@pytest.fixture
def fixtures_dir() -> Path:
    from grand_lyon_mcp.resources import fixtures_dir as packaged_fixtures_dir

    return packaged_fixtures_dir()


@pytest.fixture
def repo_root() -> Path:
    return Path(__file__).resolve().parents[1]


@pytest.fixture
async def app_container(tmp_path: Path, fixtures_dir: Path):
    from grand_lyon_mcp.bootstrap import build_app
    from grand_lyon_mcp.settings import Settings

    settings = Settings(
        offline=True,
        db_path=tmp_path / "test.db",
        data_dir=tmp_path / "data",
        config_dir=tmp_path / "config",
        log_level="WARNING",
    )
    (tmp_path / "config").mkdir(parents=True, exist_ok=True)
    # copy example sources/profiles from packaged config
    import shutil

    from grand_lyon_mcp.resources import config_path

    for name in ("sources.example.yaml", "profiles.example.yaml", "waste-taxonomy.yaml"):
        src = config_path(name)
        if src.is_file():
            dest_name = name.replace(".example", "") if "example" in name else name
            shutil.copy(src, tmp_path / "config" / dest_name)

    container = await build_app(settings, fixtures_dir=fixtures_dir, db_path=tmp_path / "test.db")
    # import mini GTFS
    from grand_lyon_mcp.providers.gtfs.importer import import_gtfs_zip

    await import_gtfs_zip(container.conn, fixtures_dir / "gtfs" / "mini_gtfs.zip")
    cursor = await container.conn.execute(
        "SELECT stop_id, stop_name, stop_lat, stop_lon FROM gtfs_stops"
    )
    for r in await cursor.fetchall():
        await container.entities.upsert(
            logical_id=f"gtfs:stop:{r['stop_id']}",
            source_id="gtfs_tcl",
            provider_id=r["stop_id"],
            entity_type="transport_stop",
            name=r["stop_name"],
            latitude=float(r["stop_lat"]),
            longitude=float(r["stop_lon"]),
        )
    yield container
    await container.aclose()
