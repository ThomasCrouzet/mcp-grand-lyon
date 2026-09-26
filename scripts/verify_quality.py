"""Run the offline quality gate sequentially and retain repeatable evidence."""

from __future__ import annotations

import argparse
import os
import subprocess
from pathlib import Path

from evidence import digest, source_identity, write_json


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--artifact-dir", required=True, type=Path)
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[1]
    output = args.artifact_dir.resolve()
    output.mkdir(parents=True, exist_ok=False)
    env = {
        k: v
        for k, v in os.environ.items()
        if not k.startswith(("GRAND_LYON_MCP_", "DATAGRANDLYON_", "TRANSITOUS_"))
    }
    env.update(
        {
            "GRAND_LYON_MCP_OFFLINE": "true",
            "GRAND_LYON_MCP_CONFIG_DIR": str(output / "config"),
            "GRAND_LYON_MCP_DATA_DIR": str(output / "data"),
            "GRAND_LYON_MCP_DB_PATH": str(output / "app.db"),
            "COVERAGE_FILE": str(output / ".coverage"),
        }
    )
    report = {
        "source": source_identity(root, output),
        "environment": {k: v for k, v in env.items() if k.startswith("GRAND_LYON_MCP_")},
        "fixtures_sha256": {
            str(p.relative_to(root)): digest(p)
            for p in sorted((root / "src/grand_lyon_mcp/_data").rglob("*"))
            if p.is_file() and "__pycache__" not in p.parts
        },
        "checks": [],
    }
    commands = [
        ["ruff", "format", "--check", "."],
        ["ruff", "check", "."],
        ["mypy", "src"],
        [
            "pytest",
            "-m",
            "not live",
            "--cov",
            "--cov-report=term-missing",
            f"--cov-report=xml:{output / 'coverage.xml'}",
            f"--junitxml={output / 'results.xml'}",
        ],
    ]
    try:
        for index, command in enumerate(commands):
            argv = ["uv", "run", "--offline", "--no-sync", *command]
            result = subprocess.run(
                argv, cwd=root, env=env, capture_output=True, text=True, timeout=300
            )
            log = output / f"{index}-{command[0]}.log"
            log.write_text(result.stdout + result.stderr, encoding="utf-8")
            print(result.stdout + result.stderr, end="", flush=True)
            report["checks"].append(
                {"command": argv, "returncode": result.returncode, "log": log.name}
            )
            if result.returncode:
                return result.returncode
        return 0
    finally:
        report["status"] = (
            "passed"
            if (
                len(report["checks"]) == len(commands)
                and all(c["returncode"] == 0 for c in report["checks"])
            )
            else "failed"
        )
        report["artifacts_sha256"] = {p.name: digest(p) for p in output.iterdir() if p.is_file()}
        write_json(output / "report.json", report)
        print(f"Evidence: {output / 'report.json'}")


if __name__ == "__main__":
    raise SystemExit(main())
