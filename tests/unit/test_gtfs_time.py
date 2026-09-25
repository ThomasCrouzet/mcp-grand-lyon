"""GTFS time parsing including >24h and DST zone."""

from __future__ import annotations

from datetime import datetime

from grand_lyon_mcp.infrastructure.time import BUSINESS_TZ, parse_gtfs_time


def test_gtfs_over_24h() -> None:
    service = datetime(2026, 7, 20, tzinfo=BUSINESS_TZ)
    dt = parse_gtfs_time("25:30:00", service)
    assert dt.day == 21
    assert dt.hour == 1
    assert dt.minute == 30
