"""Vélo'v historical snapshots for reliability scoring."""

from __future__ import annotations

from datetime import datetime

import aiosqlite

from grand_lyon_mcp.domain.velov import ReliabilityScore, VelovStation
from grand_lyon_mcp.infrastructure.time import now_utc


class VelovHistoryStore:
    def __init__(self, conn: aiosqlite.Connection) -> None:
        self._conn = conn

    async def upsert_station(self, station: VelovStation) -> None:
        await self._conn.execute(
            """
            INSERT INTO velov_stations (
                station_id, name, latitude, longitude, capacity, updated_at
            ) VALUES (?, ?, ?, ?, ?, ?)
            ON CONFLICT(station_id) DO UPDATE SET
                name=excluded.name,
                latitude=excluded.latitude,
                longitude=excluded.longitude,
                capacity=excluded.capacity,
                updated_at=excluded.updated_at
            """,
            (
                station.id,
                station.name,
                station.latitude,
                station.longitude,
                station.capacity,
                now_utc().isoformat(),
            ),
        )
        await self._conn.commit()

    async def record_snapshot(self, station: VelovStation, *, slot: str | None = None) -> None:
        observed = (station.observed_at or now_utc()).isoformat()
        hour_slot = slot or datetime.fromisoformat(observed).strftime("%H")
        await self._conn.execute(
            """
            INSERT INTO velov_snapshots (
                station_id, observed_at, bikes_available, docks_available, hour_slot
            ) VALUES (?, ?, ?, ?, ?)
            """,
            (
                station.id,
                observed,
                station.bikes_available,
                station.docks_available,
                hour_slot,
            ),
        )
        await self.upsert_station(station)
        await self._conn.commit()

    async def reliability(
        self,
        station_id: str,
        *,
        hour_slot: str | None = None,
        minimum_bikes: int = 1,
    ) -> ReliabilityScore:
        if hour_slot:
            cursor = await self._conn.execute(
                """
                SELECT bikes_available FROM velov_snapshots
                WHERE station_id = ? AND hour_slot = ?
                """,
                (station_id, hour_slot),
            )
        else:
            cursor = await self._conn.execute(
                "SELECT bikes_available FROM velov_snapshots WHERE station_id = ?",
                (station_id,),
            )
        rows = list(await cursor.fetchall())
        n = len(rows)
        if n == 0:
            return ReliabilityScore(
                score=0.0, sample_count=0, confidence="low", probability_available=0.0
            )
        available_count = sum(1 for r in rows if r[0] >= minimum_bikes)
        prob = available_count / n
        if n < 5:
            confidence = "low"
        elif n < 20:
            confidence = "medium"
        else:
            confidence = "high"
        return ReliabilityScore(
            score=prob,
            sample_count=n,
            confidence=confidence,
            probability_available=prob,
        )
