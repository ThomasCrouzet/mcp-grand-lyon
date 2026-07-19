"""GTFS ZIP import into SQLite."""

from __future__ import annotations

import csv
import hashlib
import io
import zipfile
from pathlib import Path

import aiosqlite

from grand_lyon_mcp.infrastructure.time import now_utc

GTFS_FILES = (
    "agency.txt",
    "routes.txt",
    "trips.txt",
    "stops.txt",
    "stop_times.txt",
    "calendar.txt",
    "calendar_dates.txt",
    "transfers.txt",
)

# Garde-fous anti zip-bomb. La source officielle est de confiance, mais `--from-file`
# accepte un ZIP arbitraire : on borne la taille compressée du ZIP et la taille
# décompressée de chaque membre lu avant de le charger en mémoire.
MAX_ZIP_BYTES = 300 * 1024 * 1024  # 300 Mo compressés
MAX_MEMBER_BYTES = 1024 * 1024 * 1024  # 1 Go décompressé par fichier


def _read_csv_from_zip(zf: zipfile.ZipFile, name: str) -> list[dict[str, str]]:
    # GTFS zips may nest files
    members = [n for n in zf.namelist() if n.endswith(name) or n.endswith("/" + name)]
    if not members:
        # try exact
        if name not in zf.namelist():
            return []
        members = [name]
    info = zf.getinfo(members[0])
    if info.file_size > MAX_MEMBER_BYTES:
        raise ValueError(
            f"Membre GTFS trop volumineux ({info.file_size} octets décompressés) : {members[0]}"
        )
    with zf.open(members[0]) as raw:
        text = io.TextIOWrapper(raw, encoding="utf-8-sig", newline="")
        reader = csv.DictReader(text)
        return [{k: (v or "").strip() for k, v in row.items() if k} for row in reader]


async def import_gtfs_zip(conn: aiosqlite.Connection, zip_path: Path) -> dict[str, int]:
    size = zip_path.stat().st_size
    if size > MAX_ZIP_BYTES:
        raise ValueError(f"Archive GTFS trop volumineuse ({size} octets compressés)")
    data = zip_path.read_bytes()
    file_hash = hashlib.sha256(data).hexdigest()
    counts: dict[str, int] = {}
    with zipfile.ZipFile(io.BytesIO(data)) as zf:
        # clear previous
        for table in (
            "gtfs_transfers",
            "gtfs_stop_times",
            "gtfs_trips",
            "gtfs_stops",
            "gtfs_routes",
            "gtfs_agencies",
            "gtfs_calendar",
            "gtfs_calendar_dates",
        ):
            await conn.execute(f"DELETE FROM {table}")

        agencies = _read_csv_from_zip(zf, "agency.txt")
        for row in agencies:
            await conn.execute(
                """
                INSERT OR REPLACE INTO gtfs_agencies
                (agency_id, agency_name, agency_url, agency_timezone)
                VALUES (?, ?, ?, ?)
                """,
                (
                    row.get("agency_id") or "default",
                    row.get("agency_name") or "",
                    row.get("agency_url"),
                    row.get("agency_timezone"),
                ),
            )
        counts["agencies"] = len(agencies)

        routes = _read_csv_from_zip(zf, "routes.txt")
        for row in routes:
            await conn.execute(
                """
                INSERT OR REPLACE INTO gtfs_routes
                (route_id, agency_id, route_short_name, route_long_name, route_type, route_color)
                VALUES (?, ?, ?, ?, ?, ?)
                """,
                (
                    row["route_id"],
                    row.get("agency_id"),
                    row.get("route_short_name"),
                    row.get("route_long_name"),
                    int(row["route_type"]) if row.get("route_type") else None,
                    row.get("route_color"),
                ),
            )
        counts["routes"] = len(routes)

        stops = _read_csv_from_zip(zf, "stops.txt")
        for row in stops:
            await conn.execute(
                """
                INSERT OR REPLACE INTO gtfs_stops
                (stop_id, stop_code, stop_name, stop_lat, stop_lon,
                 location_type, parent_station, wheelchair_boarding)
                VALUES (?, ?, ?, ?, ?, ?, ?, ?)
                """,
                (
                    row["stop_id"],
                    row.get("stop_code"),
                    row.get("stop_name") or "",
                    float(row.get("stop_lat") or 0),
                    float(row.get("stop_lon") or 0),
                    int(row["location_type"]) if row.get("location_type") else 0,
                    row.get("parent_station") or None,
                    int(row["wheelchair_boarding"]) if row.get("wheelchair_boarding") else None,
                ),
            )
        counts["stops"] = len(stops)

        trips = _read_csv_from_zip(zf, "trips.txt")
        for row in trips:
            await conn.execute(
                """
                INSERT OR REPLACE INTO gtfs_trips
                (trip_id, route_id, service_id, trip_headsign, direction_id, shape_id)
                VALUES (?, ?, ?, ?, ?, ?)
                """,
                (
                    row["trip_id"],
                    row["route_id"],
                    row["service_id"],
                    row.get("trip_headsign"),
                    int(row["direction_id"]) if row.get("direction_id") else None,
                    row.get("shape_id"),
                ),
            )
        counts["trips"] = len(trips)

        stop_times = _read_csv_from_zip(zf, "stop_times.txt")
        for row in stop_times:
            await conn.execute(
                """
                INSERT OR REPLACE INTO gtfs_stop_times
                (trip_id, arrival_time, departure_time, stop_id, stop_sequence)
                VALUES (?, ?, ?, ?, ?)
                """,
                (
                    row["trip_id"],
                    row.get("arrival_time") or row.get("departure_time") or "00:00:00",
                    row.get("departure_time") or row.get("arrival_time") or "00:00:00",
                    row["stop_id"],
                    int(row["stop_sequence"]),
                ),
            )
        counts["stop_times"] = len(stop_times)

        calendar = _read_csv_from_zip(zf, "calendar.txt")
        for row in calendar:
            await conn.execute(
                """
                INSERT OR REPLACE INTO gtfs_calendar
                (service_id, monday, tuesday, wednesday, thursday, friday,
                 saturday, sunday, start_date, end_date)
                VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                """,
                (
                    row["service_id"],
                    int(row.get("monday") or 0),
                    int(row.get("tuesday") or 0),
                    int(row.get("wednesday") or 0),
                    int(row.get("thursday") or 0),
                    int(row.get("friday") or 0),
                    int(row.get("saturday") or 0),
                    int(row.get("sunday") or 0),
                    row.get("start_date") or "19700101",
                    row.get("end_date") or "20991231",
                ),
            )
        counts["calendar"] = len(calendar)

        cal_dates = _read_csv_from_zip(zf, "calendar_dates.txt")
        for row in cal_dates:
            await conn.execute(
                """
                INSERT OR REPLACE INTO gtfs_calendar_dates
                (service_id, date, exception_type)
                VALUES (?, ?, ?)
                """,
                (row["service_id"], row["date"], int(row.get("exception_type") or 1)),
            )
        counts["calendar_dates"] = len(cal_dates)

        transfers = _read_csv_from_zip(zf, "transfers.txt")
        for row in transfers:
            await conn.execute(
                """
                INSERT OR REPLACE INTO gtfs_transfers
                (from_stop_id, to_stop_id, transfer_type, min_transfer_time)
                VALUES (?, ?, ?, ?)
                """,
                (
                    row["from_stop_id"],
                    row["to_stop_id"],
                    int(row["transfer_type"]) if row.get("transfer_type") else None,
                    int(row["min_transfer_time"]) if row.get("min_transfer_time") else None,
                ),
            )
        counts["transfers"] = len(transfers)

    await conn.execute(
        """
        INSERT INTO gtfs_imports (source_url, imported_at, file_hash, status)
        VALUES (?, ?, ?, ?)
        """,
        (str(zip_path), now_utc().isoformat(), file_hash, "ok"),
    )
    await conn.commit()
    return counts
