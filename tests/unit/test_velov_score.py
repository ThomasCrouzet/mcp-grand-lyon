"""Vélo'v scoring."""

from __future__ import annotations

from grand_lyon_mcp.services.velov_service import score_station


def test_score_nearby_with_bikes() -> None:
    s = score_station(
        bikes=8,
        docks=5,
        capacity=20,
        distance_m=100,
        max_walking_m=800,
        age_seconds=30,
        max_age=300,
        reliability=0.8,
        sample_count=20,
    )
    assert s.score > 0.5


def test_score_far_no_history() -> None:
    s = score_station(
        bikes=1,
        docks=1,
        capacity=10,
        distance_m=900,
        max_walking_m=800,
        age_seconds=250,
        max_age=300,
        reliability=0.9,
        sample_count=2,  # insufficient sample → hist=0
    )
    assert s.historical_reliability_score == 0.0
