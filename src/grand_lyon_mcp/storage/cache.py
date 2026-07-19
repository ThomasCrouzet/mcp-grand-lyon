"""HTTP response cache in SQLite (no Authorization in keys)."""

from __future__ import annotations

import hashlib
import json
from datetime import datetime
from typing import Any
from urllib.parse import urlencode, urlparse, urlunparse

import aiosqlite

from grand_lyon_mcp.infrastructure.time import now_utc


def normalize_url(url: str, params: dict[str, Any] | None = None) -> str:
    parsed = urlparse(url)
    query_items: list[tuple[str, str]] = []
    if parsed.query:
        for part in parsed.query.split("&"):
            if not part:
                continue
            if "=" in part:
                k, v = part.split("=", 1)
                query_items.append((k, v))
            else:
                query_items.append((part, ""))
    if params:
        for k, v in params.items():
            query_items.append((str(k), str(v)))
    query_items.sort()
    query = urlencode(query_items)
    return urlunparse((parsed.scheme, parsed.netloc, parsed.path, "", query, ""))


def cache_key(method: str, url: str, params: dict[str, Any] | None, source_id: str) -> str:
    normalized = normalize_url(url, params)
    raw = f"{method.upper()}|{normalized}|{source_id}"
    return hashlib.sha256(raw.encode("utf-8")).hexdigest()


class HttpCache:
    def __init__(self, conn: aiosqlite.Connection) -> None:
        self._conn = conn

    async def get(self, key: str) -> dict[str, Any] | None:
        cursor = await self._conn.execute(
            """
            SELECT status_code, content_type, etag, last_modified, payload,
                   retrieved_at, expires_at, maximum_stale_at
            FROM http_cache WHERE cache_key = ?
            """,
            (key,),
        )
        row = await cursor.fetchone()
        if row is None:
            return None
        return {
            "status_code": row["status_code"],
            "content_type": row["content_type"],
            "etag": row["etag"],
            "last_modified": row["last_modified"],
            "payload": row["payload"],
            "retrieved_at": row["retrieved_at"],
            "expires_at": row["expires_at"],
            "maximum_stale_at": row["maximum_stale_at"],
        }

    async def put(
        self,
        key: str,
        *,
        status_code: int,
        content_type: str | None,
        etag: str | None,
        last_modified: str | None,
        payload: str | bytes,
        ttl_seconds: int,
        maximum_stale_seconds: int,
    ) -> None:
        now = now_utc()
        from datetime import timedelta

        expires = now + timedelta(seconds=ttl_seconds)
        max_stale = now + timedelta(seconds=maximum_stale_seconds)
        body = payload if isinstance(payload, str) else payload.decode("utf-8", errors="replace")
        await self._conn.execute(
            """
            INSERT INTO http_cache (
                cache_key, status_code, content_type, etag, last_modified,
                payload, retrieved_at, expires_at, maximum_stale_at
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
            ON CONFLICT(cache_key) DO UPDATE SET
                status_code=excluded.status_code,
                content_type=excluded.content_type,
                etag=excluded.etag,
                last_modified=excluded.last_modified,
                payload=excluded.payload,
                retrieved_at=excluded.retrieved_at,
                expires_at=excluded.expires_at,
                maximum_stale_at=excluded.maximum_stale_at
            """,
            (
                key,
                status_code,
                content_type,
                etag,
                last_modified,
                body,
                now.isoformat(),
                expires.isoformat(),
                max_stale.isoformat(),
            ),
        )
        await self._conn.commit()

    async def purge_expired(self) -> int:
        now = now_utc().isoformat()
        cursor = await self._conn.execute(
            "DELETE FROM http_cache WHERE maximum_stale_at < ?",
            (now,),
        )
        await self._conn.commit()
        return cursor.rowcount

    def is_fresh(self, entry: dict[str, Any], now: datetime | None = None) -> bool:
        now = now or now_utc()
        expires = datetime.fromisoformat(entry["expires_at"])
        if expires.tzinfo is None:
            from datetime import UTC

            expires = expires.replace(tzinfo=UTC)
        return now <= expires

    def is_within_stale(self, entry: dict[str, Any], now: datetime | None = None) -> bool:
        now = now or now_utc()
        max_stale = datetime.fromisoformat(entry["maximum_stale_at"])
        if max_stale.tzinfo is None:
            from datetime import UTC

            max_stale = max_stale.replace(tzinfo=UTC)
        return now <= max_stale

    def parse_json(self, entry: dict[str, Any]) -> Any:
        return json.loads(entry["payload"])
