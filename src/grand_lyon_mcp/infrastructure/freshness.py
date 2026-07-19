"""Freshness / stale helpers."""

from __future__ import annotations

from datetime import datetime


def age_seconds(observed_or_retrieved: datetime, now: datetime) -> int:
    delta = now - observed_or_retrieved
    return max(0, int(delta.total_seconds()))


def is_stale(age: int, maximum_stale_seconds: int) -> bool:
    return age > maximum_stale_seconds


def can_use_stale(age: int, maximum_stale_seconds: int) -> bool:
    """Stale-on-error: usable only while still under maximum_stale_seconds."""
    return age <= maximum_stale_seconds
