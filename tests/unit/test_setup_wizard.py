"""Setup wizard pure helpers."""

from __future__ import annotations

from pathlib import Path

import pytest

from grand_lyon_mcp.setup_wizard import (
    client_config_snippet,
    copy_config_examples,
    parse_env_file,
    run_setup,
    write_env_file,
)


def test_parse_and_write_env(tmp_path: Path, repo_root: Path) -> None:
    template = repo_root / ".env.example"
    dest = tmp_path / "secrets.env"
    write_env_file(
        dest,
        {
            "DATAGRANDLYON_USERNAME": "alice",
            "DATAGRANDLYON_PASSWORD": "s3cret",
            "GRAND_LYON_MCP_OFFLINE": "true",
            "GRAND_LYON_MCP_LOG_LEVEL": "DEBUG",
        },
        template=template,
    )
    vals = parse_env_file(dest)
    assert vals["DATAGRANDLYON_USERNAME"] == "alice"
    assert vals["DATAGRANDLYON_PASSWORD"] == "s3cret"
    assert vals["GRAND_LYON_MCP_OFFLINE"] == "true"
    snippet = client_config_snippet(command="/abs/bin")
    assert "s3cret" not in snippet
    assert '"mcpServers"' in snippet
    assert "/abs/bin" in snippet


def test_copy_config_examples(tmp_path: Path) -> None:
    written = copy_config_examples(tmp_path)
    assert "settings.yaml" in written or (tmp_path / "settings.yaml").exists()
    again = copy_config_examples(tmp_path, force=False)
    assert again == []


def test_non_interactive_live_without_creds_fails(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setenv("GRAND_LYON_MCP_CONFIG_DIR", str(tmp_path / "cfg"))
    monkeypatch.setenv("GRAND_LYON_MCP_DATA_DIR", str(tmp_path / "data"))
    monkeypatch.delenv("DATAGRANDLYON_USERNAME", raising=False)
    monkeypatch.delenv("DATAGRANDLYON_PASSWORD", raising=False)
    code = run_setup(
        non_interactive=True,
        offline=False,
        skip_install=True,
        skip_migrate=True,
    )
    assert code == 2


def test_non_interactive_live_with_creds(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("GRAND_LYON_MCP_CONFIG_DIR", str(tmp_path / "cfg"))
    monkeypatch.setenv("GRAND_LYON_MCP_DATA_DIR", str(tmp_path / "data"))
    monkeypatch.setenv("DATAGRANDLYON_USERNAME", "user@example.com")
    monkeypatch.setenv("DATAGRANDLYON_PASSWORD", "secret-pass")
    code = run_setup(
        non_interactive=True,
        offline=False,
        skip_install=True,
        skip_migrate=True,
        force_config=True,
    )
    assert code == 0
    secrets = tmp_path / "cfg" / "secrets.env"
    vals = parse_env_file(secrets)
    assert vals["DATAGRANDLYON_USERNAME"] == "user@example.com"
    assert vals["DATAGRANDLYON_PASSWORD"] == "secret-pass"
    assert vals["GRAND_LYON_MCP_OFFLINE"] == "false"
