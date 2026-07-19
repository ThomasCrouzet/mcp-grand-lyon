"""GTFS query repository."""

from __future__ import annotations

from datetime import datetime

import aiosqlite

from grand_lyon_mcp.domain.geo import normalize_name
from grand_lyon_mcp.domain.places import EntityType, PlaceCandidate
from grand_lyon_mcp.domain.transit import Departure
from grand_lyon_mcp.infrastructure.time import BUSINESS_TZ, parse_gtfs_time


class GtfsRepository:
    def __init__(self, conn: aiosqlite.Connection) -> None:
        self._conn = conn

    async def search_stops(self, query: str, *, limit: int = 10) -> list[PlaceCandidate]:
        q = f"%{normalize_name(query).replace(' ', '%')}%"
        cursor = await self._conn.execute(
            """
            SELECT stop_id, stop_name, stop_lat, stop_lon FROM gtfs_stops
            WHERE lower(replace(replace(stop_name, '-', ' '), '''', ' ')) LIKE ?
               OR lower(stop_name) LIKE ?
            LIMIT ?
            """,
            (q, f"%{query.lower()}%", limit),
        )
        rows = await cursor.fetchall()
        return [
            PlaceCandidate(
                id=f"gtfs:stop:{r['stop_id']}",
                name=r["stop_name"],
                label=r["stop_name"],
                type=EntityType.TRANSPORT_STOP,
                latitude=float(r["stop_lat"]),
                longitude=float(r["stop_lon"]),
                confidence=0.85,
            )
            for r in rows
        ]

    async def get_stop(self, stop_id: str) -> PlaceCandidate | None:
        sid = stop_id.removeprefix("gtfs:stop:")
        cursor = await self._conn.execute(
            "SELECT stop_id, stop_name, stop_lat, stop_lon FROM gtfs_stops WHERE stop_id = ?",
            (sid,),
        )
        row = await cursor.fetchone()
        if row is None:
            return None
        return PlaceCandidate(
            id=f"gtfs:stop:{row['stop_id']}",
            name=row["stop_name"],
            label=row["stop_name"],
            type=EntityType.TRANSPORT_STOP,
            latitude=float(row["stop_lat"]),
            longitude=float(row["stop_lon"]),
            confidence=1.0,
        )

    async def get_wheelchair_boarding(self, stop_id: str) -> int | None:
        """GTFS wheelchair_boarding: 0=no info, 1=some access, 2=not accessible."""
        sid = stop_id.removeprefix("gtfs:stop:")
        cursor = await self._conn.execute(
            "SELECT wheelchair_boarding FROM gtfs_stops WHERE stop_id = ?",
            (sid,),
        )
        row = await cursor.fetchone()
        if row is None or row["wheelchair_boarding"] is None:
            return None
        try:
            return int(row["wheelchair_boarding"])
        except (TypeError, ValueError):
            return None

    async def get_scheduled_departures(
        self,
        stop_id: str,
        *,
        at: datetime,
        line: str | None = None,
        direction: str | None = None,
        limit: int = 6,
    ) -> list[Departure]:
        sid = stop_id.removeprefix("gtfs:stop:")
        at_paris = at.astimezone(BUSINESS_TZ) if at.tzinfo else at.replace(tzinfo=BUSINESS_TZ)
        service_date = at_paris
        # Join stop_times → trips → routes
        sql = """
            SELECT st.departure_time, st.arrival_time, t.trip_headsign,
                   r.route_id, r.route_short_name, r.route_long_name, t.direction_id
            FROM gtfs_stop_times st
            JOIN gtfs_trips t ON t.trip_id = st.trip_id
            JOIN gtfs_routes r ON r.route_id = t.route_id
            WHERE st.stop_id = ?
        """
        params: list[object] = [sid]
        if direction:
            sql += " AND (t.trip_headsign LIKE ?)"
            params.append(f"%{direction}%")
        sql += " ORDER BY st.departure_time LIMIT ?"
        params.append(limit * 20 if line else limit * 3)  # filter in python for time-of-day / line

        cursor = await self._conn.execute(sql, params)
        rows = await cursor.fetchall()
        from grand_lyon_mcp.domain.transit_line import line_matches

        departures: list[Departure] = []
        for row in rows:
            try:
                dep_at = parse_gtfs_time(row["departure_time"], service_date)
            except ValueError:
                continue
            if dep_at < at_paris:
                continue
            if (
                direction
                and row["trip_headsign"]
                and direction.lower() not in row["trip_headsign"].lower()
            ):
                continue
            name = row["route_short_name"] or row["route_long_name"] or row["route_id"]
            if line and not line_matches(line, str(name or ""), str(row["route_id"] or "")):
                continue
            departures.append(
                Departure(
                    line_id=f"tcl:{row['route_id']}",
                    line_name=name,
                    destination=row["trip_headsign"] or "",
                    scheduled_at=dep_at,
                    expected_at=dep_at,
                    delay_seconds=None,
                    realtime=False,  # GTFS theoretical never realtime
                    cancelled=False,
                )
            )
            if len(departures) >= limit:
                break
        return departures
