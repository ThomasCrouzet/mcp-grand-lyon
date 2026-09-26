"""Observe real resource state which process exit alone cannot establish."""

from __future__ import annotations

import asyncio
import sqlite3
from pathlib import Path

import pytest
import yaml

from grand_lyon_mcp import bootstrap
from grand_lyon_mcp.settings import Settings
from grand_lyon_mcp.storage import database, migrations


@pytest.mark.parametrize("failure", ["sources", "profiles", "taxonomy", "http", "cancel"])
async def test_startup_releases_acquired_resources(tmp_path, monkeypatch, failure):
    connections = []
    clients = []
    original_connect = database.connect
    original_http = bootstrap.HttpClient

    async def capture_connection(path):
        conn = await original_connect(path)
        connections.append(conn)
        return conn

    def capture_http(**kwargs):
        if failure == "http":
            raise RuntimeError("HTTP initialization fixture")
        client = original_http(**kwargs)
        clients.append(client)
        return client

    async def cancel(*args, **kwargs):
        raise asyncio.CancelledError

    monkeypatch.setattr(database, "connect", capture_connection)
    monkeypatch.setattr(bootstrap, "HttpClient", capture_http)
    config = tmp_path / "config"
    config.mkdir()
    if failure in {"sources", "profiles", "taxonomy"}:
        name = "waste-taxonomy" if failure == "taxonomy" else failure
        (config / f"{name}.yaml").write_text("broken: [", encoding="utf-8")
    if failure == "cancel":
        monkeypatch.setattr(bootstrap.ProfileRepository, "load_from_yaml", cancel)
    settings = Settings(offline=True, config_dir=config, db_path=tmp_path / "app.db")
    try:
        error = (
            asyncio.CancelledError
            if failure == "cancel"
            else (RuntimeError if failure == "http" else yaml.YAMLError)
        )
        with pytest.raises(error):
            await asyncio.wait_for(bootstrap.build_app(settings), timeout=5)
        assert connections
        assert all(client._client.is_closed for client in clients)
        for conn in connections:
            with pytest.raises(ValueError, match=r"closed|active"):
                await conn.execute("SELECT 1")
            await asyncio.to_thread(conn._thread.join, 1)
            assert not conn._thread.is_alive()
    finally:
        for client in clients:
            await client.aclose()
        for conn in connections:
            await conn.close()


async def test_failed_migration_closes_connection(tmp_path, monkeypatch):
    connections = []
    original_connect = database.connect

    async def capture(path):
        conn = await original_connect(path)
        connections.append(conn)
        return conn

    monkeypatch.setattr(database, "connect", capture)
    monkeypatch.setattr(
        migrations, "list_migration_files", lambda: [("broken", "THIS IS NOT SQL;")]
    )
    try:
        with pytest.raises(sqlite3.OperationalError):
            await database.connect_and_migrate(tmp_path / "broken.db")
        with pytest.raises(ValueError, match=r"closed|active"):
            await connections[0].execute("SELECT 1")
    finally:
        for conn in connections:
            await conn.close()


async def test_http_close_failure_still_closes_database(app_container, monkeypatch):
    original_close = app_container.http.aclose

    async def fail_after_close():
        await original_close()
        raise RuntimeError("close fixture")

    with monkeypatch.context() as patch:
        patch.setattr(app_container.http, "aclose", fail_after_close)
        with pytest.raises(RuntimeError, match="close fixture"):
            await app_container.aclose()
        with pytest.raises(ValueError, match=r"closed|active"):
            await app_container.conn.execute("SELECT 1")


async def test_pragma_failure_closes_worker(tmp_path: Path, monkeypatch):
    connections = []
    original_connect = database.aiosqlite.connect

    def capture(*args, **kwargs):
        conn = original_connect(*args, **kwargs)
        connections.append(conn)
        return conn

    monkeypatch.setattr(database.aiosqlite, "connect", capture)
    path = tmp_path / "locked.db"
    with sqlite3.connect(path) as lock:
        lock.execute("CREATE TABLE fixture (id INTEGER)")
        lock.execute("BEGIN EXCLUSIVE")
        try:
            with pytest.raises(sqlite3.OperationalError):
                await database.connect(path)
            with pytest.raises(ValueError, match=r"closed|active"):
                await connections[0].execute("SELECT 1")
        finally:
            lock.rollback()
            for conn in connections:
                await conn.close()
