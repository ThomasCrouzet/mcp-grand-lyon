"""User places, commute and briefing profiles."""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any

import aiosqlite
import yaml

from grand_lyon_mcp.infrastructure.time import now_utc


class ProfileRepository:
    def __init__(self, conn: aiosqlite.Connection) -> None:
        self._conn = conn

    async def load_from_yaml(self, path: Path) -> None:
        if not path.is_file():
            return
        with path.open(encoding="utf-8") as fh:
            data = yaml.safe_load(fh) or {}
        places = data.get("places") or {}
        for key, place in places.items():
            await self.upsert_place(
                key,
                label=str(place.get("label") or key),
                address=place.get("address"),
                latitude=place.get("latitude"),
                longitude=place.get("longitude"),
            )
        for key, commute in (data.get("commutes") or {}).items():
            await self.upsert_commute(key, commute)
        for key, briefing in (data.get("briefings") or {}).items():
            await self.upsert_briefing(key, briefing)

    async def upsert_place(
        self,
        place_key: str,
        *,
        label: str,
        address: str | None,
        latitude: float | None,
        longitude: float | None,
    ) -> None:
        await self._conn.execute(
            """
            INSERT INTO user_places (place_key, label, address, latitude, longitude, updated_at)
            VALUES (?, ?, ?, ?, ?, ?)
            ON CONFLICT(place_key) DO UPDATE SET
                label=excluded.label,
                address=excluded.address,
                latitude=excluded.latitude,
                longitude=excluded.longitude,
                updated_at=excluded.updated_at
            """,
            (place_key, label, address, latitude, longitude, now_utc().isoformat()),
        )
        await self._conn.commit()

    async def get_place(self, place_key: str) -> dict[str, Any] | None:
        cursor = await self._conn.execute(
            "SELECT * FROM user_places WHERE place_key = ?",
            (place_key,),
        )
        row = await cursor.fetchone()
        return dict(row) if row else None

    async def upsert_commute(self, key: str, data: dict[str, Any]) -> None:
        await self._conn.execute(
            """
            INSERT INTO commute_profiles (profile_key, config_json, updated_at)
            VALUES (?, ?, ?)
            ON CONFLICT(profile_key) DO UPDATE SET
                config_json=excluded.config_json,
                updated_at=excluded.updated_at
            """,
            (key, json.dumps(data, ensure_ascii=False), now_utc().isoformat()),
        )
        await self._conn.commit()

    async def get_commute(self, key: str) -> dict[str, Any] | None:
        cursor = await self._conn.execute(
            "SELECT config_json FROM commute_profiles WHERE profile_key = ?",
            (key,),
        )
        row = await cursor.fetchone()
        if row is None:
            return None
        data = json.loads(row[0])
        return data if isinstance(data, dict) else None

    async def upsert_briefing(self, key: str, data: dict[str, Any]) -> None:
        await self._conn.execute(
            """
            INSERT INTO briefing_profiles (profile_key, config_json, updated_at)
            VALUES (?, ?, ?)
            ON CONFLICT(profile_key) DO UPDATE SET
                config_json=excluded.config_json,
                updated_at=excluded.updated_at
            """,
            (key, json.dumps(data, ensure_ascii=False), now_utc().isoformat()),
        )
        await self._conn.commit()

    async def get_briefing_profile(self, key: str) -> dict[str, Any] | None:
        cursor = await self._conn.execute(
            "SELECT config_json FROM briefing_profiles WHERE profile_key = ?",
            (key,),
        )
        row = await cursor.fetchone()
        if row is None:
            return None
        data = json.loads(row[0])
        return data if isinstance(data, dict) else None

    async def save_briefing_snapshot(self, profile_key: str, payload: dict[str, Any]) -> None:
        await self._conn.execute(
            """
            INSERT INTO briefing_snapshots (profile_key, captured_at, payload_json)
            VALUES (?, ?, ?)
            """,
            (profile_key, now_utc().isoformat(), json.dumps(payload, ensure_ascii=False)),
        )
        await self._conn.commit()

    async def last_briefing_snapshot(self, profile_key: str) -> dict[str, Any] | None:
        cursor = await self._conn.execute(
            """
            SELECT payload_json FROM briefing_snapshots
            WHERE profile_key = ?
            ORDER BY id DESC LIMIT 1
            """,
            (profile_key,),
        )
        row = await cursor.fetchone()
        if row is None:
            return None
        data = json.loads(row[0])
        return data if isinstance(data, dict) else None
