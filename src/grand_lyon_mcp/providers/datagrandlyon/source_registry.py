"""Logical source registry resolution (no silent random table pick)."""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any

import aiosqlite
import yaml

from grand_lyon_mcp.infrastructure.time import now_utc


class SourceRegistry:
    def __init__(self, conn: aiosqlite.Connection) -> None:
        self._conn = conn

    async def load_config(self, path: Path) -> None:
        if not path.is_file():
            return
        with path.open(encoding="utf-8") as fh:
            data = yaml.safe_load(fh) or {}
        sources = data.get("sources") or {}
        for source_id, cfg in sources.items():
            await self.upsert(source_id, cfg)

    async def upsert(self, source_id: str, cfg: dict[str, Any]) -> None:
        status = cfg.get("status") or ("DISABLED" if not cfg.get("enabled", True) else "UNRESOLVED")
        await self._conn.execute(
            """
            INSERT INTO source_registry (
                source_id, provider, enabled, required, status, service,
                schema_name, table_name, collection, resolved_url, parser,
                realtime, ttl_seconds, maximum_stale_seconds, authentication,
                attribution, license, metadata_json, updated_at
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            ON CONFLICT(source_id) DO UPDATE SET
                provider=excluded.provider,
                enabled=excluded.enabled,
                required=excluded.required,
                status=excluded.status,
                service=excluded.service,
                schema_name=excluded.schema_name,
                table_name=excluded.table_name,
                collection=excluded.collection,
                resolved_url=excluded.resolved_url,
                parser=excluded.parser,
                realtime=excluded.realtime,
                ttl_seconds=excluded.ttl_seconds,
                maximum_stale_seconds=excluded.maximum_stale_seconds,
                authentication=excluded.authentication,
                attribution=excluded.attribution,
                license=excluded.license,
                metadata_json=excluded.metadata_json,
                updated_at=excluded.updated_at
            """,
            (
                source_id,
                str(cfg.get("provider") or "datapusher"),
                1 if cfg.get("enabled", True) else 0,
                1 if cfg.get("required", False) else 0,
                status,
                cfg.get("service"),
                cfg.get("schema_name"),
                cfg.get("table_name")
                or (
                    (cfg.get("exact_table_hints") or [None])[0]
                    if cfg.get("exact_table_hints")
                    else None
                ),
                cfg.get("collection"),
                cfg.get("resolved_url"),
                cfg.get("parser"),
                1 if cfg.get("realtime") else 0,
                int(cfg.get("ttl_seconds") or 60),
                int(cfg.get("maximum_stale_seconds") or 300),
                cfg.get("authentication"),
                cfg.get("attribution"),
                cfg.get("license") or "unknown",
                json.dumps(cfg, ensure_ascii=False, default=str),
                now_utc().isoformat(),
            ),
        )
        await self._conn.commit()

    async def set_status(
        self,
        source_id: str,
        status: str,
        *,
        table_name: str | None = None,
        resolved_url: str | None = None,
    ) -> None:
        await self._conn.execute(
            """
            UPDATE source_registry
            SET status = ?,
                table_name = COALESCE(?, table_name),
                resolved_url = COALESCE(?, resolved_url),
                updated_at = ?
            WHERE source_id = ?
            """,
            (status, table_name, resolved_url, now_utc().isoformat(), source_id),
        )
        await self._conn.commit()

    async def get(self, source_id: str) -> dict[str, Any] | None:
        cursor = await self._conn.execute(
            "SELECT * FROM source_registry WHERE source_id = ?",
            (source_id,),
        )
        row = await cursor.fetchone()
        return dict(row) if row else None

    async def list_all(self) -> list[dict[str, Any]]:
        cursor = await self._conn.execute("SELECT * FROM source_registry ORDER BY source_id")
        rows = await cursor.fetchall()
        return [dict(r) for r in rows]

    async def resolve_from_candidates(
        self,
        source_id: str,
        candidates: list[dict[str, Any]],
        *,
        exact_hints: list[str] | None = None,
    ) -> str:
        """
        Resolve a single table. Returns OK, UNRESOLVED, or DISABLED.
        Never picks randomly among ambiguous candidates.
        """
        row = await self.get(source_id)
        if row and not row["enabled"]:
            await self.set_status(source_id, "DISABLED")
            return "DISABLED"
        hints = [h.lower() for h in (exact_hints or [])]
        exact_matches = []
        for c in candidates:
            name = str(c.get("name") or c.get("id") or "").lower()
            for h in hints:
                if name == h or name.endswith("." + h) or h in name:
                    exact_matches.append(c)
                    break
        if len(exact_matches) == 1:
            name = str(exact_matches[0].get("name") or exact_matches[0].get("id"))
            await self.set_status(source_id, "OK", table_name=name)
            return "OK"
        if len(candidates) == 1 and candidates[0]:
            name = str(candidates[0].get("name") or candidates[0].get("id"))
            await self.set_status(source_id, "OK", table_name=name)
            return "OK"
        await self.set_status(source_id, "UNRESOLVED")
        return "UNRESOLVED"
