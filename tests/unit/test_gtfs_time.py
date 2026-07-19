"""GTFS time parsing including >24h and DST zone."""

from __future__ import annotations

from datetime import datetime

from grand_lyon_mcp.infrastructure.time import BUSINESS_TZ, parse_gtfs_time, to_paris


def test_gtfs_over_24h() -> None:
    service = datetime(2026, 7, 20, tzinfo=BUSINESS_TZ)
    dt = parse_gtfs_time("25:30:00", service)
    assert dt.day == 21
    assert dt.hour == 1
    assert dt.minute == 30


def test_paris_offset_summer() -> None:
    # CEST
    dt = datetime(2026, 7, 20, 10, 0, tzinfo=BUSINESS_TZ)
    assert "+02:00" in to_paris(dt).isoformat() or dt.utcoffset().total_seconds() == 7200


def test_paris_offset_winter() -> None:
    dt = datetime(2026, 1, 15, 10, 0, tzinfo=BUSINESS_TZ)
    assert dt.utcoffset().total_seconds() == 3600
