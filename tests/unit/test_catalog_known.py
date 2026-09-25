"""Known sources config + hybrid catalog helpers."""

from __future__ import annotations

from pathlib import Path

from grand_lyon_mcp.providers.live_providers import (
    KNOWN_SOURCE_TABLES,
    load_known_source_tables,
)
from grand_lyon_mcp.resources import config_path


def test_known_tables_loaded_from_yaml() -> None:
    yaml_path = config_path("sources.known.yaml")
    assert yaml_path.is_file(), "sources.known.yaml must be packaged in _data/config/"
    tables = load_known_source_tables(yaml_path)
    assert "velov_realtime" in tables
    assert tables["velov_realtime"]["table_name"] == "jcd_jcdecaux.jcdvelov"
    assert "tcl_departures" in tables
    # module-level export populated
    assert "velov_realtime" in KNOWN_SOURCE_TABLES


def test_known_tables_fallback_when_missing(tmp_path: Path) -> None:
    tables = load_known_source_tables(tmp_path / "missing.yaml")
    assert "velov_realtime" in tables
    assert tables["velov_realtime"]["service"] == "rdata"
