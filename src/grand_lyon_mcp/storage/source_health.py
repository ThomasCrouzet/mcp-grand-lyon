"""Source health tracking."""

from __future__ import annotations

from datetime import timedelta

import aiosqlite

from grand_lyon_mcp.infrastructure.time import now_utc


class SourceHealthStore:
    def __init__(
        self,
        conn: aiosqlite.Connection,
        *,
        max_entries: int = 256,
        retention_seconds: int = 30 * 86400,
    ) -> None:
        if max_entries < 0 or retention_seconds < 0:
            raise ValueError("Health retention limits must not be negative")
        self._conn = conn
        self._max_entries = max_entries
        self._retention_seconds = retention_seconds

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
        await self.prune()

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
        await self.prune()

    async def prune(self) -> int:
        cutoff = (now_utc() - timedelta(seconds=self._retention_seconds)).isoformat()
        expired = await self._conn.execute(
            """DELETE FROM source_health
            WHERE MAX(COALESCE(last_success_at, ''), COALESCE(last_failure_at, '')) <= ?""",
            (cutoff,),
        )
        evicted = await self._conn.execute(
            """DELETE FROM source_health WHERE source_id IN (
                SELECT source_id FROM source_health
                ORDER BY MAX(COALESCE(last_success_at, ''), COALESCE(last_failure_at, '')) DESC,
                    rowid DESC
                LIMIT -1 OFFSET ?
            )""",
            (self._max_entries,),
        )
        await self._conn.commit()
        count = expired.rowcount + evicted.rowcount
        await expired.close()
        await evicted.close()
        return count

    async def get(self, source_id: str) -> dict[str, object] | None:
        cursor = await self._conn.execute(
            "SELECT * FROM source_health WHERE source_id = ?",
            (source_id,),
        )
        row = await cursor.fetchone()
        await cursor.close()
        return dict(row) if row else None
