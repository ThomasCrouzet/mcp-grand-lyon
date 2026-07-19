"""SQLite connection helpers."""

from __future__ import annotations

from pathlib import Path

import aiosqlite

from grand_lyon_mcp.storage.migrations import migrate


async def connect(db_path: Path) -> aiosqlite.Connection:
    db_path.parent.mkdir(parents=True, exist_ok=True)
    conn = await aiosqlite.connect(db_path)
    conn.row_factory = aiosqlite.Row
    await conn.execute("PRAGMA foreign_keys = ON")
    await conn.execute("PRAGMA journal_mode = WAL")
    return conn


async def connect_and_migrate(db_path: Path) -> aiosqlite.Connection:
    conn = await connect(db_path)
    await migrate(conn)
    return conn
