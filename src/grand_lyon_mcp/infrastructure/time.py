"""Timezone-aware time helpers (business TZ: Europe/Paris)."""

from __future__ import annotations

from datetime import UTC, datetime, timedelta
from zoneinfo import ZoneInfo

BUSINESS_TZ = ZoneInfo("Europe/Paris")


def now_utc() -> datetime:
    return datetime.now(UTC)


def now_paris() -> datetime:
    return datetime.now(BUSINESS_TZ)


def to_paris(dt: datetime) -> datetime:
    if dt.tzinfo is None:
        dt = dt.replace(tzinfo=UTC)
    return dt.astimezone(BUSINESS_TZ)


def to_utc(dt: datetime) -> datetime:
    if dt.tzinfo is None:
        dt = dt.replace(tzinfo=BUSINESS_TZ)
    return dt.astimezone(UTC)


def parse_gtfs_time(value: str, service_date: datetime) -> datetime:
    """Parse elapsed GTFS time from local noon minus twelve hours, including DST."""
    parts = value.strip().split(":")
    if len(parts) != 3:
        raise ValueError(f"Invalid GTFS time: {value}")
    hours, minutes, seconds = (int(parts[0]), int(parts[1]), int(parts[2]))
    if not (0 <= hours <= 99 and 0 <= minutes < 60 and 0 <= seconds < 60):
        raise ValueError(f"Invalid GTFS time: {value}")
    local = (
        service_date.astimezone(BUSINESS_TZ)
        if service_date.tzinfo
        else service_date.replace(tzinfo=BUSINESS_TZ)
    )
    noon = local.replace(hour=12, minute=0, second=0, microsecond=0)
    origin = noon.astimezone(UTC) - timedelta(hours=12)
    return (origin + timedelta(hours=hours, minutes=minutes, seconds=seconds)).astimezone(
        BUSINESS_TZ
    )
