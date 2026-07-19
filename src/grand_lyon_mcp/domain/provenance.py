"""Provenance helpers."""

from __future__ import annotations

from datetime import datetime

from grand_lyon_mcp.domain.common import SourceProvenance
from grand_lyon_mcp.infrastructure.freshness import age_seconds, is_stale


def build_provenance(
    *,
    provider: str,
    source_id: str,
    dataset: str,
    attribution: str,
    license_name: str = "unknown",
    observed_at: datetime | None,
    retrieved_at: datetime,
    realtime: bool,
    maximum_stale_seconds: int,
) -> SourceProvenance:
    age = age_seconds(observed_at or retrieved_at, retrieved_at)
    stale = is_stale(age, maximum_stale_seconds) if realtime else False
    return SourceProvenance(
        provider=provider,
        source_id=source_id,
        dataset=dataset,
        attribution=attribution,
        license=license_name,
        observed_at=observed_at,
        retrieved_at=retrieved_at,
        age_seconds=age,
        realtime=realtime,
        stale=stale,
    )


def theoretical_provenance(
    *,
    provider: str,
    source_id: str,
    dataset: str,
    attribution: str,
    retrieved_at: datetime,
    license_name: str = "unknown",
) -> SourceProvenance:
    """GTFS / static fallback — never marked realtime."""
    return SourceProvenance(
        provider=provider,
        source_id=source_id,
        dataset=dataset,
        attribution=attribution,
        license=license_name,
        observed_at=None,
        retrieved_at=retrieved_at,
        age_seconds=0,
        realtime=False,
        stale=False,
    )
