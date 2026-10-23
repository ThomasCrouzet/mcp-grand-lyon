"""Settings behavior through an isolated CLI process."""

from __future__ import annotations

import os
import subprocess
import sys
from pathlib import Path


def run_doctor(
    tmp_path: Path,
    settings_yaml: str,
    *,
    environment: dict[str, str] | None = None,
) -> subprocess.CompletedProcess[str]:
    config = tmp_path / "config"
    config.mkdir()
    (config / "settings.yaml").write_text(settings_yaml, encoding="utf-8")
    process_environment = {
        key: value
        for key, value in os.environ.items()
        if not key.startswith(("GRAND_LYON_MCP_", "DATAGRANDLYON_", "TRANSITOUS_"))
    }
    process_environment.update(
        {
            "GRAND_LYON_MCP_CONFIG_DIR": str(config),
            "GRAND_LYON_MCP_DATA_DIR": str(tmp_path / "data"),
            "GRAND_LYON_MCP_DB_PATH": str(tmp_path / "app.db"),
            "GRAND_LYON_MCP_OFFLINE": "true",
        }
    )
    process_environment.update(environment or {})
    return subprocess.run(
        [sys.executable, "-m", "grand_lyon_mcp", "doctor"],
        cwd=tmp_path,
        env=process_environment,
        capture_output=True,
        text=True,
        timeout=30,
        check=False,
    )


def test_quoted_yaml_false_disables_tls_verification(tmp_path: Path) -> None:
    result = run_doctor(
        tmp_path,
        'version: 1\nnetwork:\n  verify_tls: "false"\n',
    )

    assert result.returncode == 0, result.stderr
    assert "tls_verification_disabled" in result.stderr


def test_environment_has_priority_over_yaml(tmp_path: Path) -> None:
    result = run_doctor(
        tmp_path,
        "version: 1\nnetwork:\n  verify_tls: false\n",
        environment={"GRAND_LYON_MCP_VERIFY_TLS": "true"},
    )

    assert result.returncode == 0, result.stderr
    assert "tls_verification_disabled" not in result.stderr


def test_invalid_environment_request_limit_stops_startup(tmp_path: Path) -> None:
    result = run_doctor(
        tmp_path,
        "version: 1\nruntime:\n  max_parallel_requests: 6\n",
        environment={"GRAND_LYON_MCP_MAX_PARALLEL_REQUESTS": "0"},
    )

    assert result.returncode != 0
    assert "max_parallel_requests" in result.stderr


def test_invalid_yaml_timeout_stops_startup(tmp_path: Path) -> None:
    result = run_doctor(
        tmp_path,
        "version: 1\nnetwork:\n  connect_timeout_seconds: -1\n",
    )

    assert result.returncode != 0
    assert "http_connect_timeout_seconds" in result.stderr


def test_invalid_yaml_timezone_stops_startup(tmp_path: Path) -> None:
    result = run_doctor(
        tmp_path,
        "version: 1\nruntime:\n  timezone: Mars/Olympus_Mons\n",
    )

    assert result.returncode != 0
    assert "timezone" in result.stderr


def test_negative_journey_weight_stops_startup(tmp_path: Path) -> None:
    result = run_doctor(
        tmp_path,
        """version: 1
journey_scoring:
  duration: -0.10
  reliability: 0.45
  disruptions: 0.25
  walking: 0.15
  availability: 0.15
  user_preference: 0.10
""",
    )

    assert result.returncode != 0
    assert "duration" in result.stderr
