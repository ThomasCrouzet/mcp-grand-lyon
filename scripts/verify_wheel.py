"""Verify an installed wheel through CLI processes and the real MCP stdio boundary."""

from __future__ import annotations

import argparse
import asyncio
import json
import os
import shutil
import sqlite3
import subprocess
import sys
import traceback
import zipfile
from contextlib import asynccontextmanager
from pathlib import Path

from evidence import digest, source_identity, write_json

ROOT = Path(__file__).resolve().parents[1]
TOOLS = {
    "lyon_resolve_place",
    "lyon_next_departures",
    "lyon_mobility_status",
    "lyon_trip_options",
    "lyon_parking_options",
    "lyon_accessibility_check",
    "lyon_nearby_facilities",
    "lyon_environment_brief",
    "lyon_waste_dropoff",
    "lyon_personal_briefing",
}


class Verification:
    def __init__(self, output: Path):
        self.output = output
        self.report = {"source": source_identity(ROOT, output), "checks": [], "commands": []}
        self.binary = output / "venv" / "bin" / "grand-lyon-mcp"
        self.env = {
            key: value
            for key, value in os.environ.items()
            if not key.startswith(("GRAND_LYON_MCP_", "DATAGRANDLYON_", "TRANSITOUS_", "PYTHON"))
            and key not in {"VIRTUAL_ENV", "UV_PROJECT_ENVIRONMENT"}
        }
        self.env.update({"GRAND_LYON_MCP_OFFLINE": "true", "NO_COLOR": "1"})
        self.fixtures = output / "fixtures"

    def command(self, args, *, cwd=None, env=None, success=True, timeout=180):
        environment = {
            key: value
            for key, value in (env or self.env).items()
            if key.startswith("GRAND_LYON_MCP_")
        }
        try:
            result = subprocess.run(
                list(map(str, args)),
                cwd=cwd or self.output,
                env=env or self.env,
                capture_output=True,
                text=True,
                timeout=timeout,
            )
        except subprocess.TimeoutExpired as exc:
            self.report["commands"].append(
                {
                    "argv": list(map(str, args)),
                    "cwd": str(cwd or self.output),
                    "timeout_seconds": timeout,
                    "environment": environment,
                    "stdout": (exc.stdout or b"").decode(errors="replace"),
                    "stderr": (exc.stderr or b"").decode(errors="replace"),
                }
            )
            raise
        self.report["commands"].append(
            {
                "argv": list(map(str, args)),
                "cwd": str(cwd or self.output),
                "returncode": result.returncode,
                "environment": environment,
                "stdout": result.stdout,
                "stderr": result.stderr,
            }
        )
        if success:
            assert result.returncode == 0, result.stderr
        return result

    def install(self, wheel):
        if wheel is None:
            self.command(["uv", "build", "--out-dir", self.output / "dist"], cwd=ROOT)
            (wheel,) = (self.output / "dist").glob("*.whl")
        self.report["wheel"] = {"path": str(wheel), "sha256": digest(wheel)}
        requirements = self.command(
            [
                "uv",
                "export",
                "--locked",
                "--no-dev",
                "--no-emit-project",
                "--no-hashes",
            ],
            cwd=ROOT,
        ).stdout
        requirements_path = self.output / "requirements.txt"
        requirements_path.write_text(requirements, encoding="utf-8")
        self.command(["uv", "venv", "--python", sys.executable, self.output / "venv"])
        python = self.binary.with_name("python")
        self.command(["uv", "pip", "install", "--python", python, "-r", requirements_path, wheel])
        result = self.command(
            [
                python,
                "-c",
                (
                    "import json,sys,grand_lyon_mcp; "
                    "from grand_lyon_mcp.resources import fixtures_dir; "
                    "print(json.dumps({'package':grand_lyon_mcp.__file__,"
                    "'fixtures':str(fixtures_dir()),'python':sys.version}))"
                ),
            ]
        )
        installed = json.loads(result.stdout)
        assert Path(installed["package"]).is_relative_to(self.output / "venv")
        self.report["installed"] = installed
        shutil.copytree(installed["fixtures"], self.fixtures)
        self.report["fixtures_sha256"] = {
            str(path.relative_to(self.fixtures)): digest(path)
            for path in sorted(self.fixtures.rglob("*"))
            if path.is_file()
        }
        self.cases = json.loads((self.fixtures / "mobility_cases.json").read_text())
        with zipfile.ZipFile(self.output / "dated.zip", "w") as archive:
            for path in sorted((self.fixtures / "gtfs" / "dated").glob("*.txt")):
                archive.writestr(
                    zipfile.ZipInfo(path.name, (2026, 7, 20, 0, 0, 0)), path.read_bytes()
                )

    def environment(self, name):
        folder = self.output / name
        folder.mkdir()
        config = folder / "config"
        config.mkdir()
        env = dict(
            self.env,
            GRAND_LYON_MCP_CONFIG_DIR=str(config),
            GRAND_LYON_MCP_DATA_DIR=str(folder / "data"),
            GRAND_LYON_MCP_DB_PATH=str(folder / "app.db"),
        )
        return folder, env

    @asynccontextmanager
    async def server(self, folder, env):
        transcript = (folder / "protocol.jsonl").open("w", encoding="utf-8")
        stderr = (folder / "stderr.log").open("w", encoding="utf-8")
        process = await asyncio.create_subprocess_exec(
            str(self.binary),
            "serve",
            "--transport",
            "stdio",
            "--quiet",
            cwd=folder,
            env=env,
            stdin=asyncio.subprocess.PIPE,
            stdout=asyncio.subprocess.PIPE,
            stderr=stderr,
        )
        write_json(
            folder / "environment.json",
            {key: value for key, value in env.items() if key.startswith("GRAND_LYON_MCP_")},
        )
        process_record = {
            "argv": [str(self.binary), "serve", "--transport", "stdio", "--quiet"],
            "cwd": str(folder),
            "transcript": str(folder / "protocol.jsonl"),
            "forced_termination": False,
        }
        self.report["commands"].append(process_record)
        index = 0

        async def request(method, params=None, notification=False):
            nonlocal index
            index += 1
            message = {"jsonrpc": "2.0", "method": method}
            if params is not None:
                message["params"] = params
            if not notification:
                message["id"] = index
            transcript.write(json.dumps({"direction": "client", "message": message}) + "\n")
            transcript.flush()
            process.stdin.write((json.dumps(message) + "\n").encode())
            await process.stdin.drain()
            if notification:
                return None
            while True:
                raw = await asyncio.wait_for(process.stdout.readline(), timeout=15)
                assert raw, "Server closed stdout before its response"
                transcript.write(
                    json.dumps(
                        {
                            "direction": "server",
                            "raw": raw.decode(errors="replace"),
                        }
                    )
                    + "\n"
                )
                transcript.flush()
                reply = json.loads(raw)
                assert reply.get("jsonrpc") == "2.0"
                if reply.get("id") == index:
                    assert "error" not in reply, reply
                    return reply["result"]

        try:
            initialized = await request(
                "initialize",
                {
                    "protocolVersion": "2024-11-05",
                    "capabilities": {},
                    "clientInfo": {"name": "wheel-verification", "version": "1.0"},
                },
            )
            assert initialized["serverInfo"]["name"] == "grand-lyon-mcp"
            await request("notifications/initialized", notification=True)
            tools = await request("tools/list")
            assert len(tools["tools"]) == 10
            assert {tool["name"] for tool in tools["tools"]} == TOOLS

            async def call(name, arguments):
                result = await request("tools/call", {"name": name, "arguments": arguments})
                assert not result.get("isError", False), result
                content = result.get("structuredContent")
                if content is None:
                    content = json.loads(result["content"][0]["text"])
                assert content["schema_version"] == "1.0"
                return content

            yield call
            process.stdin.close()
            assert await asyncio.wait_for(process.wait(), timeout=10) == 0
            assert await process.stdout.read() == b"", "Unexpected output after EOF"
        finally:
            if process.returncode is None:
                process_record["forced_termination"] = True
                process.kill()
                await process.wait()
            process_record["returncode"] = process.returncode
            transcript.close()
            stderr.close()

    async def packaged_mobility(self):
        folder, env = self.environment("packaged-mobility")
        self.command([self.binary, "version"], env=env)
        self.command(
            [self.binary, "sync", "gtfs", "--from-file", self.fixtures / "gtfs" / "mini_gtfs.zip"],
            env=env,
        )
        async with self.server(folder, env) as call:
            place = await call("lyon_resolve_place", {"query": "Bellecour"})
            assert place["status"] in {"ok", "ambiguous"} and place["data"]["candidates"]
            assert any(c["name"] == "Bellecour" for c in place["data"]["candidates"])
            departures = await call(
                "lyon_next_departures",
                {
                    "stop": {"query": "Bellecour"},
                    "line": "A",
                    "at": "2026-07-20T08:00:00+02:00",
                },
            )
            assert departures["status"] == "ok"
            assert all(
                d["line_name"] == "A" and d["realtime"] for d in departures["data"]["departures"]
            )
            assert [d["expected_at"] for d in departures["data"]["departures"]] == [
                "2026-07-20T08:05:00+02:00"
            ]
            parking = await call(
                "lyon_parking_options",
                {
                    "destination": {"latitude": 45.7675, "longitude": 4.8355},
                    "radius_m": 500,
                },
            )
            assert parking["status"] == "ok"
            assert [p["id"] for p in parking["data"]["options"]] == ["p1"]
            assert parking["data"]["options"][0]["available_spaces"] == 42

    async def transit(self, case):
        folder, env = self.environment(case["name"])
        fixtures = folder / "fixtures"
        (fixtures / "siri").mkdir(parents=True)
        if case["realtime"] == "wrong-line":
            source = (self.fixtures / "siri" / "estimated_timetable.json").read_text()
            (fixtures / "siri" / "estimated_timetable.json").write_text(
                source.replace('"LineRef": "A"', '"LineRef": "C12"')
            )
        elif case["realtime"] == "invalid":
            (fixtures / "siri" / "estimated_timetable.json").write_text("{")
        env["GRAND_LYON_MCP_FIXTURES_DIR"] = str(fixtures)
        self.command(
            [self.binary, "sync", "gtfs", "--from-file", self.output / "dated.zip"], env=env
        )
        if case.get("static_unavailable"):
            with sqlite3.connect(env["GRAND_LYON_MCP_DB_PATH"]) as conn:
                conn.execute("DROP TABLE gtfs_stop_times")
        async with self.server(folder, env) as call:
            result = await call(
                "lyon_next_departures",
                {
                    "stop": {"place_id": "gtfs:stop:BEL1"},
                    "line": "A",
                    "direction": "Vaulx",
                    "at": case["at"],
                    "limit": max(1, len(case["expected"])),
                },
            )
            departures = result["data"]["departures"]
            assert [d["expected_at"] for d in departures] == case["expected"], result
            assert all(d["line_name"] == "A" and not d["realtime"] for d in departures)
            if departures:
                assert result["status"] == "partial" and result["degraded"]
                assert result["warnings"] and result["sources"]
                assert all(s["provider"] == "GTFS" and not s["realtime"] for s in result["sources"])
            elif case.get("static_unavailable"):
                assert result["status"] == "unavailable"
                assert {w["source"] for w in result["warnings"]} >= {"tcl_departures", "gtfs"}
            else:
                assert result["status"] == "not_found"

    async def parking(self):
        folder, env = self.environment("mixed-parking")
        fixtures = folder / "fixtures" / "datagrandlyon"
        fixtures.mkdir(parents=True)
        write_json(fixtures / "parkings.json", self.cases["parking"])
        env["GRAND_LYON_MCP_FIXTURES_DIR"] = str(fixtures.parent)
        async with self.server(folder, env) as call:
            args = {"destination": {"latitude": 45.7675, "longitude": 4.8355}, "radius_m": 100}
            result = await call("lyon_parking_options", args)
            options = {p["id"]: p for p in result["data"]["options"]}
            assert set(options) == {"live", "zero", "capacity", "unknown", "pr"}, result
            assert all(p["distance_m"] <= 100 for p in options.values())
            assert options["zero"]["available_spaces"] == 0
            assert options["zero"]["status"] == "full"
            for name in ("capacity", "unknown", "pr"):
                assert options[name]["available_spaces"] is None
                assert options[name]["status"] == "unknown"
                assert options[name]["realtime"] is False
            assert options["capacity"]["capacity"] == 800
            assert result["status"] == "partial" and result["degraded"]
            assert "PARTIAL_RESULT" in {w["code"] for w in result["warnings"]}
            filtered = await call("lyon_parking_options", dict(args, minimum_spaces=1))
            assert {p["id"] for p in filtered["data"]["options"]} == {
                "live",
                "capacity",
                "unknown",
                "pr",
            }
            typed = await call("lyon_parking_options", dict(args, types=["park_and_ride"]))
            assert [p["id"] for p in typed["data"]["options"]] == ["pr"]

    async def startup_failure(self, name):
        folder, env = self.environment(name)
        if name == "invalid-config":
            (folder / "config" / "profiles.yaml").write_text("profiles: [")
        else:
            with sqlite3.connect(env["GRAND_LYON_MCP_DB_PATH"]) as conn:
                conn.execute("CREATE TABLE schema_migrations (wrong_column TEXT)")
        result = await asyncio.to_thread(
            self.command,
            [self.binary, "serve", "--quiet"],
            env=env,
            success=False,
            timeout=10,
        )
        assert result.returncode != 0 and result.stdout == ""
        assert "Error" in result.stderr
        with sqlite3.connect(env["GRAND_LYON_MCP_DB_PATH"], timeout=1) as conn:
            conn.execute("BEGIN EXCLUSIVE")
            conn.execute("CREATE TABLE cleanup_probe (id INTEGER)")

    async def run(self):
        checks = [("packaged-mobility", self.packaged_mobility), ("mixed-parking", self.parking)]
        checks += [(c["name"], lambda c=c: self.transit(c)) for c in self.cases["transit"]]
        checks += [
            (n, lambda n=n: self.startup_failure(n))
            for n in ("invalid-config", "invalid-migration")
        ]
        for name, check in checks:
            try:
                await asyncio.wait_for(check(), timeout=45)
            except Exception:
                self.report["checks"].append(
                    {"name": name, "status": "failed", "error": traceback.format_exc()}
                )
            else:
                self.report["checks"].append({"name": name, "status": "passed"})
            print(f"{name}: {self.report['checks'][-1]['status']}", flush=True)
        assert all(c["status"] == "passed" for c in self.report["checks"]), "Verification failed"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--artifact-dir", type=Path, required=True)
    parser.add_argument("--wheel", type=Path)
    args = parser.parse_args()
    output = args.artifact_dir.resolve()
    output.mkdir(parents=True, exist_ok=False)
    verification = Verification(output)
    try:
        verification.install(args.wheel.resolve() if args.wheel else None)
        asyncio.run(verification.run())
        verification.report["status"] = "passed"
    except Exception:
        verification.report["status"] = "failed"
        verification.report["error"] = traceback.format_exc()
        raise
    finally:
        verification.report["artifacts_sha256"] = {
            str(p.relative_to(output)): digest(p)
            for p in sorted(output.rglob("*"))
            if p.is_file() and not p.is_relative_to(output / "venv")
        }
        write_json(output / "report.json", verification.report)
        print(f"Evidence: {output / 'report.json'}")


if __name__ == "__main__":
    main()
