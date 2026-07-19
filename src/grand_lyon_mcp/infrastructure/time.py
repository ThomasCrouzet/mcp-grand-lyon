"""Timezone-aware time helpers (business TZ: Europe/Paris)."""

from __future__ import annotations

from datetime import UTC, datetime
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
    """Parse GTFS HH:MM:SS which may exceed 24:00:00 into aware datetime."""
    parts = value.strip().split(":")
    if len(parts) != 3:
        raise ValueError(f"Invalid GTFS time: {value}")
    hours, minutes, seconds = (int(parts[0]), int(parts[1]), int(parts[2]))
    days, hours = divmod(hours, 24)
    base = service_date.astimezone(BUSINESS_TZ).replace(hour=0, minute=0, second=0, microsecond=0)
    from datetime import timedelta

    return base + timedelta(days=days, hours=hours, minutes=minutes, seconds=seconds)
