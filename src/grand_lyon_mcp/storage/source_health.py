"""Source health tracking."""

from __future__ import annotations

import aiosqlite

from grand_lyon_mcp.infrastructure.time import now_utc


class SourceHealthStore:
    def __init__(self, conn: aiosqlite.Connection) -> None:
        self._conn = conn

    async def record_success(
        self, source_id: str, *, latency_ms: int, schema_hash: str | None = None
    ) -> None:
        now = now_utc().isoformat()
        await self._conn.execute(
            """
            INSERT INTO source_health (
                source_id, last_success_at, consecutive_failures, last_status,
                last_latency_ms, last_error_code, last_schema_hash
            ) VALUES (?, ?, 0, 'ok', ?, NULL, ?)
            ON CONFLICT(source_id) DO UPDATE SET
                last_success_at=excluded.last_success_at,
                consecutive_failures=0,
                last_status='ok',
                last_latency_ms=excluded.last_latency_ms,
                last_error_code=NULL,
                last_schema_hash=COALESCE(excluded.last_schema_hash, source_health.last_schema_hash)
            """,
            (source_id, now, latency_ms, schema_hash),
        )
        await self._conn.commit()

    async def record_failure(
        self, source_id: str, *, error_code: str, latency_ms: int | None = None
    ) -> None:
        now = now_utc().isoformat()
        await self._conn.execute(
            """
            INSERT INTO source_health (
                source_id, last_failure_at, consecutive_failures, last_status,
                last_latency_ms, last_error_code
            ) VALUES (?, ?, 1, 'error', ?, ?)
            ON CONFLICT(source_id) DO UPDATE SET
                last_failure_at=excluded.last_failure_at,
                consecutive_failures=source_health.consecutive_failures + 1,
                last_status='error',
                last_latency_ms=excluded.last_latency_ms,
                last_error_code=excluded.last_error_code
            """,
            (source_id, now, latency_ms, error_code),
        )
        await self._conn.commit()

    async def get(self, source_id: str) -> dict[str, object] | None:
        cursor = await self._conn.execute(
            "SELECT * FROM source_health WHERE source_id = ?",
            (source_id,),
        )
        row = await cursor.fetchone()
        return dict(row) if row else None
