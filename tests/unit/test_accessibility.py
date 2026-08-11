"""Accessibility aggregation rules and multi-evidence honesty."""

from __future__ import annotations

from typing import Any

import pytest

from grand_lyon_mcp.domain.accessibility import AccessibilityIncident, AccessibilityStatus
from grand_lyon_mcp.domain.common import PlaceRef, ResultStatus
from grand_lyon_mcp.services.accessibility_service import AccessibilityService, _aggregate


def test_unknown_not_accessible() -> None:
    assert _aggregate([AccessibilityStatus.UNKNOWN]) == AccessibilityStatus.UNKNOWN


def test_any_inaccessible() -> None:
    assert (
        _aggregate([AccessibilityStatus.ACCESSIBLE, AccessibilityStatus.INACCESSIBLE])
        == AccessibilityStatus.INACCESSIBLE
    )


def test_mixed_unknown_partial() -> None:
    assert (
        _aggregate([AccessibilityStatus.ACCESSIBLE, AccessibilityStatus.UNKNOWN])
        == AccessibilityStatus.PARTIALLY_ACCESSIBLE
    )


def test_all_accessible_only_if_proven() -> None:
    assert (
        _aggregate([AccessibilityStatus.ACCESSIBLE, AccessibilityStatus.ACCESSIBLE])
        == AccessibilityStatus.ACCESSIBLE
    )


class _FakePlaces:
    def __init__(self, name: str = "Bellecour", stop_id: str = "gtfs:stop:BEL1") -> None:
        self.name = name
        self.stop_id = stop_id

    async def resolve_place(self, **kwargs: Any) -> Any:
        from grand_lyon_mcp.domain.common import make_envelope
        from grand_lyon_mcp.infrastructure.time import now_paris

        return make_envelope(
            status=ResultStatus.OK,
            generated_at=now_paris(),
            summary="ok",
            data={
                "candidates": [
                    {
                        "id": self.stop_id,
                        "name": self.name,
                        "latitude": 45.7578,
                        "longitude": 4.8320,
                    }
                ]
            },
        )


class _IncidentProvider:
    def __init__(self, location: str = "Bellecour") -> None:
        self.location = location

    async def get_incidents(self, *, lines: list[str] | None = None) -> list[AccessibilityIncident]:
        return [
            AccessibilityIncident(
                id="i1",
                location=self.location,
                status=AccessibilityStatus.INACCESSIBLE,
                description="Ascenseur en panne",
            )
        ]

    async def check_stop(self, stop_id: str) -> str:
        return AccessibilityStatus.UNKNOWN.value


class _EmptyProvider:
    async def get_incidents(self, *, lines: list[str] | None = None) -> list[AccessibilityIncident]:
        return []

    async def check_stop(self, stop_id: str) -> str:
        return AccessibilityStatus.UNKNOWN.value


class _FakeGtfs:
    def __init__(self, wheelchair: int | None = None) -> None:
        self._wh = wheelchair

    async def get_wheelchair_boarding(self, stop_id: str) -> int | None:
        return self._wh


@pytest.mark.asyncio
async def test_absence_is_never_accessible() -> None:
    svc = AccessibilityService(places=_FakePlaces(), provider=_EmptyProvider(), gtfs=_FakeGtfs(0))
    env = await svc.check(origin=PlaceRef(query="Bellecour"), needs=["wheelchair"])
    assert env.data["overall_status"] != AccessibilityStatus.ACCESSIBLE.value
    segs = env.data["segments"]
    assert segs
    assert segs[0]["status"] != AccessibilityStatus.ACCESSIBLE.value
    # evidence present (declarative layer)
    assert "evidence" in segs[0]


@pytest.mark.asyncio
async def test_incident_reflected_on_stop() -> None:
    svc = AccessibilityService(
        places=_FakePlaces(name="Bellecour"),
        provider=_IncidentProvider(location="Bellecour"),
    )
    env = await svc.check(origin=PlaceRef(query="Bellecour"))
    assert env.data["overall_status"] == AccessibilityStatus.INACCESSIBLE.value
    assert env.data["incidents"]
    segs = env.data["segments"]
    assert segs[0]["status"] == AccessibilityStatus.INACCESSIBLE.value
    facts = [e["fact"] for e in segs[0]["evidence"]]
    assert "incident" in facts


@pytest.mark.asyncio
async def test_gtfs_wheelchair_declarative_not_accessible() -> None:
    """wheelchair_boarding=1 is declarative only, never silent full accessible."""
    svc = AccessibilityService(
        places=_FakePlaces(),
        provider=_EmptyProvider(),
        gtfs=_FakeGtfs(1),
    )
    env = await svc.check(origin=PlaceRef(query="Bellecour"))
    assert env.data["overall_status"] != AccessibilityStatus.ACCESSIBLE.value
    assert env.data["overall_status"] in {
        AccessibilityStatus.PARTIALLY_ACCESSIBLE.value,
        AccessibilityStatus.UNKNOWN.value,
    }
    evidence = env.data["segments"][0]["evidence"]
    assert any(e["source"] == "gtfs_wheelchair" for e in evidence)


@pytest.mark.asyncio
async def test_gtfs_wheelchair_2_inaccessible() -> None:
    svc = AccessibilityService(
        places=_FakePlaces(),
        provider=_EmptyProvider(),
        gtfs=_FakeGtfs(2),
    )
    env = await svc.check(origin=PlaceRef(query="Bellecour"))
    assert env.data["overall_status"] == AccessibilityStatus.INACCESSIBLE.value
