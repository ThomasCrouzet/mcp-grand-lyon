"""GTFS query repository."""

from __future__ import annotations

from datetime import UTC, datetime, timedelta

import aiosqlite

from grand_lyon_mcp.domain.geo import normalize_name
from grand_lyon_mcp.domain.places import EntityType, PlaceCandidate
from grand_lyon_mcp.domain.transit import Departure
from grand_lyon_mcp.domain.transit_line import line_matches
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
        at_utc = at_paris.astimezone(UTC)
        end_utc = at_utc + timedelta(hours=24)
        # Read all times for this stop. A SQL LIMIT before the calendar, time,
        # and strict line filters can discard the next valid departure.
        cursor = await self._conn.execute(
            """
            SELECT st.departure_time, st.arrival_time, t.trip_headsign,
                   r.route_id, r.route_short_name, r.route_long_name, t.service_id,
                   t.trip_id, st.stop_sequence
            FROM gtfs_stop_times st
            JOIN gtfs_trips t ON t.trip_id = st.trip_id
            JOIN gtfs_routes r ON r.route_id = t.route_id
            WHERE st.stop_id = ?
            ORDER BY t.trip_id, st.stop_sequence
        """,
            (sid,),
        )
        rows = await cursor.fetchall()
        await cursor.close()
        async with self._conn.execute("SELECT * FROM gtfs_calendar") as cursor:
            calendars = {r["service_id"]: r for r in await cursor.fetchall()}
        # HH:MM:SS permits up to 99 hours. Include four earlier service days
        # and tomorrow, then restrict results to the next 24 elapsed hours.
        first_day = at_paris.date() - timedelta(days=4)
        last_day = at_paris.date() + timedelta(days=1)
        async with self._conn.execute(
            "SELECT service_id, date, exception_type FROM gtfs_calendar_dates "
            "WHERE date BETWEEN ? AND ?",
            (first_day.strftime("%Y%m%d"), last_day.strftime("%Y%m%d")),
        ) as cursor:
            exceptions = {
                (r["service_id"], r["date"]): r["exception_type"] for r in await cursor.fetchall()
            }
        weekdays = ("monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday")

        departures: list[Departure] = []
        for offset in range(6):
            day = first_day + timedelta(days=offset)
            date_key = day.strftime("%Y%m%d")
            service_date = datetime(day.year, day.month, day.day, tzinfo=BUSINESS_TZ)
            for row in rows:
                service_id = row["service_id"]
                exception = exceptions.get((service_id, date_key))
                calendar = calendars.get(service_id)
                if exception != 1 and (
                    exception == 2
                    or calendar is None
                    or not calendar[weekdays[day.weekday()]]
                    or not calendar["start_date"] <= date_key <= calendar["end_date"]
                ):
                    continue
                name = row["route_short_name"] or row["route_long_name"] or row["route_id"]
                if line and not line_matches(line, str(name), str(row["route_id"])):
                    continue
                if (
                    direction
                    and direction.casefold() not in (row["trip_headsign"] or "").casefold()
                ):
                    continue
                try:
                    dep_at = parse_gtfs_time(row["departure_time"], service_date)
                except ValueError:
                    continue
                if not at_utc <= dep_at.astimezone(UTC) < end_utc:
                    continue
                departures.append(
                    Departure(
                        line_id=f"tcl:{row['route_id']}",
                        line_name=name,
                        destination=row["trip_headsign"] or "",
                        scheduled_at=dep_at,
                        expected_at=dep_at,
                        realtime=False,
                    )
                )
        departures.sort(key=lambda d: d.expected_at.astimezone(UTC) if d.expected_at else end_utc)
        return departures[:limit]
