"""Freshness helpers and GTFS never realtime."""

from __future__ import annotations

from datetime import UTC, datetime, timedelta

from grand_lyon_mcp.domain.provenance import theoretical_provenance
from grand_lyon_mcp.infrastructure.freshness import age_seconds, can_use_stale, is_stale


def test_age_and_stale() -> None:
    now = datetime(2026, 7, 19, 12, 0, tzinfo=UTC)
    past = now - timedelta(seconds=200)
    age = age_seconds(past, now)
    assert age == 200
    assert is_stale(age, 180)
    assert can_use_stale(150, 180)
    assert not can_use_stale(200, 180)


def test_theoretical_never_realtime() -> None:
    p = theoretical_provenance(
        provider="GTFS",
        source_id="gtfs",
        dataset="GTFS",
        attribution="SYTRAL",
        retrieved_at=datetime.now(UTC),
    )
    assert p.realtime is False
    assert p.stale is False
