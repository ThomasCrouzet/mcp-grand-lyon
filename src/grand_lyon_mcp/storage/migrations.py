"""Apply SQL migrations from package migrations/ directory."""

from __future__ import annotations

from importlib import resources
from pathlib import Path

import aiosqlite

from grand_lyon_mcp.infrastructure.logging import get_logger

logger = get_logger("migrations")


async def ensure_migrations_table(conn: aiosqlite.Connection) -> None:
    await conn.execute(
        """
        CREATE TABLE IF NOT EXISTS schema_migrations (
            version TEXT PRIMARY KEY,
            applied_at TEXT NOT NULL DEFAULT (datetime('now'))
        )
        """
    )
    await conn.commit()


def list_migration_files() -> list[tuple[str, str]]:
    """Return (version, sql) sorted by version name."""
    package = "grand_lyon_mcp.migrations"
    results: list[tuple[str, str]] = []
    try:
        root = resources.files(package)
    except TypeError:
        # fallback for editable installs
        root = Path(__file__).resolve().parent.parent / "migrations"
        for path in sorted(root.glob("*.sql")):
            results.append((path.stem, path.read_text(encoding="utf-8")))
        return results

    for item in sorted(root.iterdir(), key=lambda p: p.name):
        name = item.name
        if name.endswith(".sql"):
            results.append((name[:-4], item.read_text(encoding="utf-8")))
    return results


async def applied_versions(conn: aiosqlite.Connection) -> set[str]:
    await ensure_migrations_table(conn)
    cursor = await conn.execute("SELECT version FROM schema_migrations")
    rows = await cursor.fetchall()
    return {row[0] for row in rows}


async def migrate(conn: aiosqlite.Connection) -> list[str]:
    """Apply pending migrations. Returns list of applied version ids."""
    applied = await applied_versions(conn)
    newly: list[str] = []
    for version, sql in list_migration_files():
        if version in applied:
            continue
        logger.info("applying_migration", extra={"event": version})
        try:
            await conn.executescript("BEGIN IMMEDIATE;\n" + sql)
            await conn.execute(
                "INSERT INTO schema_migrations (version) VALUES (?)",
                (version,),
            )
            await conn.commit()
        except BaseException:
            await conn.rollback()
            raise
        newly.append(version)
    return newly


async def db_info(conn: aiosqlite.Connection) -> dict[str, object]:
    applied = sorted(await applied_versions(conn))
    # feature checks
    fts5 = False
    rtree = False
    try:
        await conn.execute("CREATE VIRTUAL TABLE IF NOT EXISTS _fts_probe USING fts5(x)")
        await conn.execute("DROP TABLE IF EXISTS _fts_probe")
        fts5 = True
    except Exception:
        fts5 = False
    try:
        await conn.execute(
            "CREATE VIRTUAL TABLE IF NOT EXISTS _rtree_probe "
            "USING rtree(id, minx, maxx, miny, maxy)"
        )
        await conn.execute("DROP TABLE IF EXISTS _rtree_probe")
        rtree = True
    except Exception:
        rtree = False
    return {
        "migrations_applied": applied,
        "migration_count": len(applied),
        "fts5": fts5,
        "rtree": rtree,
    }
