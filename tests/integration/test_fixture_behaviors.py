"""Verify observable offline fixture and briefing behavior."""

from __future__ import annotations

from datetime import datetime
from zoneinfo import ZoneInfo

import pytest

from grand_lyon_mcp.domain.common import PlaceRef, ResultStatus


@pytest.mark.asyncio
async def test_unknown_fixture_place_is_not_found(app_container) -> None:
    result = await app_container.places.resolve_place(query="qzxqzx unknown place", limit=5)

    assert result.status == ResultStatus.NOT_FOUND
    assert result.data["candidates"] == []


@pytest.mark.asyncio
async def test_fixture_realtime_departures_match_the_resolved_stop(app_container) -> None:
    request_time = datetime(2026, 7, 20, 8, 0, tzinfo=ZoneInfo("Europe/Paris"))

    bellecour = await app_container.transit.next_departures(
        stop=PlaceRef(query="Bellecour"),
        line="A",
        at=request_time,
        limit=5,
    )
    part_dieu = await app_container.transit.next_departures(
        stop=PlaceRef(query="Part-Dieu"),
        line="A",
        at=request_time,
        limit=5,
    )

    assert [item["expected_at"] for item in bellecour.data["departures"]] == [
        "2026-07-20T08:05:00+02:00"
    ]
    assert all(item["realtime"] for item in bellecour.data["departures"])
    assert [item["expected_at"] for item in part_dieu.data["departures"]] == [
        "2026-07-20T08:12:00+02:00"
    ]
    assert all(not item["realtime"] for item in part_dieu.data["departures"])


@pytest.mark.asyncio
async def test_briefing_compares_nested_snapshot_facts(app_container) -> None:
    await app_container.profiles.save_briefing_snapshot(
        "weekday_morning",
        {
            "facts": {"mobility": {"alerts": [{"id": "previous-alert"}]}},
            "at": "2026-07-19T08:00:00+02:00",
        },
    )

    result = await app_container.briefing.personal_briefing(
        profile="weekday_morning",
        at=datetime(2026, 7, 20, 8, 0, tzinfo=ZoneInfo("Europe/Paris")),
    )

    assert {change["key"] for change in result.data["changes"]} == {"mobility"}


@pytest.mark.asyncio
async def test_equal_briefing_facts_do_not_report_changes(app_container) -> None:
    first = await app_container.briefing.personal_briefing(profile="weekday_morning")
    second = await app_container.briefing.personal_briefing(profile="weekday_morning")

    assert first.data["facts"] == second.data["facts"]
    assert second.data["changes"] == []
