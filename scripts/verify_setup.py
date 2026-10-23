"""Verify setup and client configuration through an installed wheel."""

from __future__ import annotations

import argparse
import json
import stat
import traceback
from pathlib import Path

from evidence import digest, write_json
from verify_wheel import Verification


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--artifact-dir", required=True, type=Path)
    parser.add_argument("--wheel", type=Path)
    args = parser.parse_args()
    output = args.artifact_dir.resolve()
    output.mkdir(parents=True, exist_ok=False)
    verification = Verification(output)
    try:
        verification.install(args.wheel.resolve() if args.wheel else None)
        folder = output / "installed-setup"
        config = folder / "config"
        data = folder / "data"
        database = folder / "custom.db"
        folder.mkdir()
        env = dict(
            verification.env,
            GRAND_LYON_MCP_CONFIG_DIR=str(config),
            GRAND_LYON_MCP_DATA_DIR=str(data),
            GRAND_LYON_MCP_DB_PATH=str(database),
        )
        before = set((output / "venv").rglob(".env"))
        verification.command(
            [verification.binary, "setup", "--yes", "--offline", "--force-config"],
            cwd=folder,
            env=env,
        )
        after = set((output / "venv").rglob(".env"))
        assert after == before, "Setup wrote an environment file inside the installation"
        assert database.is_file()
        assert (config / "settings.yaml").is_file()
        assert (config / "mcp-client.snippet.json").is_file()
        secrets = config / "secrets.env"
        assert stat.S_IMODE(secrets.stat().st_mode) == 0o600
        generated = json.loads((config / "mcp-client.snippet.json").read_text())
        direct = json.loads(
            verification.command(
                [verification.binary, "client-config", "--bin"], cwd=folder, env=env
            ).stdout
        )
        for config_block in (generated, direct):
            server = config_block["mcpServers"]["grand-lyon"]
            command = Path(server["command"])
            assert command.is_absolute() and command.is_file()
            assert command.samefile(verification.binary)
            assert server["args"] == ["serve", "--transport", "stdio"]
            assert server["env"] == {
                "GRAND_LYON_MCP_CONFIG_DIR": str(config),
                "GRAND_LYON_MCP_DATA_DIR": str(data),
                "GRAND_LYON_MCP_DB_PATH": str(database),
                "GRAND_LYON_MCP_OFFLINE": "true",
            }
        verification.report["checks"].append(
            {"name": "installed-setup-and-client-config", "status": "passed"}
        )
        verification.report["status"] = "passed"
        return 0
    except Exception:
        verification.report["checks"].append(
            {
                "name": "installed-setup-and-client-config",
                "status": "failed",
                "error": traceback.format_exc(),
            }
        )
        verification.report["status"] = "failed"
        raise
    finally:
        verification.report["artifacts_sha256"] = {
            str(path.relative_to(output)): digest(path)
            for path in sorted(output.rglob("*"))
            if path.is_file() and not path.is_relative_to(output / "venv")
        }
        write_json(output / "report.json", verification.report)
        print(f"Evidence: {output / 'report.json'}")


if __name__ == "__main__":
    raise SystemExit(main())
