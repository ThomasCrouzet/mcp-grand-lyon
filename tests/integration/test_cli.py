"""CLI smoke tests."""

from __future__ import annotations

from pathlib import Path

import pytest
from typer.testing import CliRunner

from grand_lyon_mcp.cli import app

runner = CliRunner()


def test_version() -> None:
    result = runner.invoke(app, ["version"])
    assert result.exit_code == 0
    assert result.stdout.strip()


def test_db_migrate_and_info(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("GRAND_LYON_MCP_DB_PATH", str(tmp_path / "cli.db"))
    monkeypatch.setenv("GRAND_LYON_MCP_DATA_DIR", str(tmp_path / "data"))
    monkeypatch.setenv("GRAND_LYON_MCP_CONFIG_DIR", str(tmp_path / "config"))
    monkeypatch.setenv("GRAND_LYON_MCP_OFFLINE", "true")
    r1 = runner.invoke(app, ["db", "migrate"])
    assert r1.exit_code == 0
    r2 = runner.invoke(app, ["db", "info"])
    assert r2.exit_code == 0
    assert "fts5" in r2.stdout.lower() or "migrations" in r2.stdout


def test_doctor_no_secrets(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("GRAND_LYON_MCP_DB_PATH", str(tmp_path / "cli.db"))
    monkeypatch.setenv("GRAND_LYON_MCP_DATA_DIR", str(tmp_path / "data"))
    monkeypatch.setenv("GRAND_LYON_MCP_CONFIG_DIR", str(tmp_path / "config"))
    monkeypatch.setenv("GRAND_LYON_MCP_OFFLINE", "true")
    monkeypatch.setenv("DATAGRANDLYON_USERNAME", "secret_user_xyz")
    monkeypatch.setenv("DATAGRANDLYON_PASSWORD", "secret_pass_xyz")
    result = runner.invoke(app, ["doctor"])
    assert result.exit_code == 0
    assert "secret_user_xyz" not in result.stdout
    assert "secret_pass_xyz" not in result.stdout
    assert "OK" in result.stdout or "WARNING" in result.stdout


def test_smoke_offline(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("GRAND_LYON_MCP_DB_PATH", str(tmp_path / "cli.db"))
    monkeypatch.setenv("GRAND_LYON_MCP_DATA_DIR", str(tmp_path / "data"))
    monkeypatch.setenv("GRAND_LYON_MCP_CONFIG_DIR", str(tmp_path / "config"))
    monkeypatch.setenv("GRAND_LYON_MCP_OFFLINE", "true")
    # seed profiles via packaged config templates
    import shutil

    from grand_lyon_mcp.resources import config_path

    cfg = tmp_path / "config"
    cfg.mkdir(parents=True, exist_ok=True)
    for name in ("sources.example.yaml", "profiles.example.yaml", "waste-taxonomy.yaml"):
        src = config_path(name)
        if src.is_file():
            dest = name.replace(".example", "") if "example" in name else name
            shutil.copy(src, cfg / dest)
    result = runner.invoke(app, ["smoke", "--tool", "lyon_resolve_place"])
    assert result.exit_code == 0, result.stdout + result.stderr
    assert "lyon_resolve_place" in result.stdout
