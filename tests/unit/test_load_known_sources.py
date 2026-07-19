"""Known source YAML loader (pure config path)."""

from __future__ import annotations

from pathlib import Path

import yaml

from grand_lyon_mcp.providers.live_providers import load_known_source_tables


def test_load_from_explicit_yaml(tmp_path: Path) -> None:
    path = tmp_path / "sources.known.yaml"
    path.write_text(
        yaml.dump(
            {
                "version": 1,
                "tables": {
                    "demo": {
                        "service": "rdata",
                        "table_name": "demo.table",
                        "status": "OK",
                    }
                },
            }
        ),
        encoding="utf-8",
    )
    tables = load_known_source_tables(path)
    assert tables["demo"]["table_name"] == "demo.table"
    assert tables["demo"]["service"] == "rdata"


def test_skips_invalid_entries(tmp_path: Path) -> None:
    path = tmp_path / "bad.yaml"
    path.write_text(
        yaml.dump(
            {
                "tables": {
                    "ok": {"service": "rdata", "table_name": "a.b"},
                    "no_table": {"service": "rdata"},
                    "bad_meta": "not-a-dict",
                }
            }
        ),
        encoding="utf-8",
    )
    tables = load_known_source_tables(path)
    assert "ok" in tables
    assert "no_table" not in tables
