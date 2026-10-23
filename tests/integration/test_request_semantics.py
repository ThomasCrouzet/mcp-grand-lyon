"""Observable request constraints must change public tool results."""

from __future__ import annotations

from datetime import datetime

import pytest

from grand_lyon_mcp.adapters.mcp.tools import dispatch_tool
from grand_lyon_mcp.domain.common import PlaceRef, ResultStatus, WarningCode
from grand_lyon_mcp.domain.geo import Point
from grand_lyon_mcp.domain.journeys import TravelMode, TripOption
from grand_lyon_mcp.domain.transit import Severity, TransitAlert
from grand_lyon_mcp.infrastructure.time import BUSINESS_TZ
from grand_lyon_mcp.services.journey_service import JourneyService


@pytest.mark.asyncio
async def test_trip_rejects_unknown_and_empty_modes(app_container) -> None:
    base = {
        "origin": {"latitude": 45.7606, "longitude": 4.8618},
        "destination": {"latitude": 45.7578, "longitude": 4.8320},
    }

    unknown = await dispatch_tool(
        app_container, "lyon_trip_options", {**base, "modes": ["teleport"]}
    )
    empty = await dispatch_tool(app_container, "lyon_trip_options", {**base, "modes": []})

    assert unknown["status"] == ResultStatus.INVALID_REQUEST.value
    assert empty["status"] == ResultStatus.INVALID_REQUEST.value


@pytest.mark.asyncio
async def test_trip_honors_arrival_deadline(app_container) -> None:
    departure = datetime(2026, 7, 20, 8, 0, tzinfo=BUSINESS_TZ)
    deadline = datetime(2026, 7, 20, 8, 0, 1, tzinfo=BUSINESS_TZ)

    result = await dispatch_tool(
        app_container,
        "lyon_trip_options",
        {
            "origin": {"latitude": 45.7606, "longitude": 4.8618},
            "destination": {"latitude": 45.7578, "longitude": 4.8320},
            "departure_at": departure.isoformat(),
            "arrival_before": deadline.isoformat(),
            "modes": ["walk", "car"],
        },
    )

    assert result["status"] == ResultStatus.NOT_FOUND.value
    assert result["data"]["recommended_mode"] is None
    assert result["data"]["options"] == []


@pytest.mark.asyncio
async def test_wheelchair_trip_requires_verified_options(app_container) -> None:
    result = await dispatch_tool(
        app_container,
        "lyon_trip_options",
        {
            "origin": {"latitude": 45.7606, "longitude": 4.8618},
            "destination": {"latitude": 45.7578, "longitude": 4.8320},
            "modes": ["walk", "velov", "car", "tcl"],
            "preferences": {"wheelchair": True},
        },
    )

    assert result["status"] == ResultStatus.UNAVAILABLE.value
    assert result["data"]["recommended_mode"] is None
    assert result["data"]["options"] == []
    assert WarningCode.PARTIAL_RESULT.value in {w["code"] for w in result["warnings"]}


class _PlannerWithAlerts:
    async def plan(
        self,
        origin: Point,
        destination: Point,
        *,
        departure_at: datetime,
        modes: list[TravelMode],
        preferences: dict | None = None,
    ) -> list[TripOption]:
        alert = TransitAlert(
            id="incident",
            severity=Severity.MAJOR,
            title="Line interrupted",
        )
        return [
            TripOption(
                mode=TravelMode.TCL,
                score=0.9,
                estimated_duration_seconds=500,
                accessibility="accessible",
                alerts=[alert],
                summary="Disrupted route",
            ),
            TripOption(
                mode=TravelMode.TCL,
                score=0.7,
                estimated_duration_seconds=600,
                accessibility="accessible",
                summary="Clear route",
            ),
        ]


@pytest.mark.asyncio
async def test_trip_filters_known_disruptions_and_keeps_verified_access(app_container) -> None:
    service = JourneyService(
        places=app_container.places,
        planner=_PlannerWithAlerts(),  # type: ignore[arg-type]
    )

    result = await service.options(
        origin=PlaceRef(latitude=45.7606, longitude=4.8618),
        destination=PlaceRef(latitude=45.7578, longitude=4.8320),
        modes=["tcl"],
        preferences={"avoid_disruptions": True, "wheelchair": True},
    )

    assert [option["summary"] for option in result.data["options"]] == ["Clear route"]
    assert result.data["options"][0]["accessibility"] == "accessible"


@pytest.mark.asyncio
async def test_facility_open_at_filters_closed_and_marks_open(app_container) -> None:
    location = {"latitude": 45.777, "longitude": 4.855}
    closed = await dispatch_tool(
        app_container,
        "lyon_nearby_facilities",
        {
            "location": location,
            "categories": ["toilet"],
            "open_at": "2026-07-20T07:00:00+02:00",
        },
    )
    opened = await dispatch_tool(
        app_container,
        "lyon_nearby_facilities",
        {
            "location": location,
            "categories": ["toilet"],
            "open_at": "2026-07-20T09:00:00+02:00",
        },
    )

    assert closed["data"]["items"] == []
    assert opened["data"]["items"]
    assert opened["data"]["items"][0]["open_status"] == "open"


@pytest.mark.asyncio
async def test_accessibility_rejects_unknown_needs_and_missing_places(app_container) -> None:
    unknown = await dispatch_tool(
        app_container,
        "lyon_accessibility_check",
        {"origin": {"query": "Bellecour"}, "needs": ["hearing"]},
    )
    missing = await dispatch_tool(app_container, "lyon_accessibility_check", {})

    assert unknown["status"] == ResultStatus.INVALID_REQUEST.value
    assert missing["status"] == ResultStatus.INVALID_REQUEST.value


@pytest.mark.asyncio
async def test_waste_constraints_do_not_claim_unknown_results(app_container) -> None:
    location = {"latitude": 45.76, "longitude": 4.85}
    unsupported_transport = await dispatch_tool(
        app_container,
        "lyon_waste_dropoff",
        {"item": "pile", "location": location, "transport": "walk"},
    )
    unknown_hours = await dispatch_tool(
        app_container,
        "lyon_waste_dropoff",
        {
            "item": "pile",
            "location": location,
            "open_at": "2026-07-20T09:00:00+02:00",
        },
    )
    unresolved = await dispatch_tool(
        app_container,
        "lyon_waste_dropoff",
        {"item": "pile", "location": {"place_id": "missing:waste-place"}},
    )

    assert unsupported_transport["status"] == ResultStatus.INVALID_REQUEST.value
    assert unknown_hours["status"] == ResultStatus.PARTIAL.value
    assert unknown_hours["data"]["facilities"] == []
    assert unresolved["status"] == ResultStatus.NOT_FOUND.value
    assert unresolved["data"]["facilities"] == []
