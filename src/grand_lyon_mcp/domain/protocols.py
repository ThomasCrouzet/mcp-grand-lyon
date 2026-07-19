"""Provider protocols (interfaces). Services depend on these, not concrete providers."""

from __future__ import annotations

from datetime import datetime
from typing import Any, Protocol, runtime_checkable

from grand_lyon_mcp.domain.accessibility import AccessibilityIncident
from grand_lyon_mcp.domain.environment import IndicatorValue
from grand_lyon_mcp.domain.facilities import Facility, FacilityCategory
from grand_lyon_mcp.domain.geo import Point
from grand_lyon_mcp.domain.journeys import TravelMode, TripOption
from grand_lyon_mcp.domain.parking import ParkingOption, ParkingType
from grand_lyon_mcp.domain.places import PlaceCandidate
from grand_lyon_mcp.domain.traffic import RoadEvent, TrafficCondition
from grand_lyon_mcp.domain.transit import Departure, TransitAlert
from grand_lyon_mcp.domain.velov import VelovStation
from grand_lyon_mcp.domain.waste import WasteCategory, WasteFacility


@runtime_checkable
class PlaceProvider(Protocol):
    async def search(
        self,
        query: str,
        *,
        near: Point | None = None,
        types: list[str] | None = None,
        limit: int = 5,
    ) -> list[PlaceCandidate]: ...

    async def reverse(self, point: Point) -> PlaceCandidate | None: ...


@runtime_checkable
class TransitStaticProvider(Protocol):
    async def search_stops(self, query: str, *, limit: int = 10) -> list[PlaceCandidate]: ...

    async def get_scheduled_departures(
        self,
        stop_id: str,
        *,
        at: datetime,
        line: str | None = None,
        direction: str | None = None,
        limit: int = 6,
    ) -> list[Departure]: ...


@runtime_checkable
class TransitRealtimeProvider(Protocol):
    async def get_departures(
        self,
        stop_id: str,
        *,
        line: str | None = None,
        direction: str | None = None,
        limit: int = 6,
    ) -> list[Departure]: ...


@runtime_checkable
class WheelchairProvider(Protocol):
    """Source d'accessibilité fauteuil d'un arrêt (GTFS wheelchair_boarding)."""

    async def get_wheelchair_boarding(self, stop_id: str) -> int | None: ...


@runtime_checkable
class TransitAlertProvider(Protocol):
    async def get_alerts(self, *, lines: list[str] | None = None) -> list[TransitAlert]: ...


@runtime_checkable
class AccessibilityProvider(Protocol):
    async def get_incidents(
        self, *, lines: list[str] | None = None
    ) -> list[AccessibilityIncident]: ...

    async def check_stop(self, stop_id: str) -> str: ...


@runtime_checkable
class VelovProvider(Protocol):
    async def stations_near(
        self, point: Point, *, radius_m: float = 1000, limit: int = 20
    ) -> list[VelovStation]: ...


@runtime_checkable
class ParkingProvider(Protocol):
    async def options_near(
        self,
        point: Point,
        *,
        types: list[ParkingType] | None = None,
        radius_m: float = 1500,
        minimum_spaces: int = 0,
        limit: int = 10,
    ) -> list[ParkingOption]: ...


@runtime_checkable
class TrafficProvider(Protocol):
    async def conditions(self, *, area: str | None = None) -> list[TrafficCondition]: ...

    async def road_events(self, *, area: str | None = None) -> list[RoadEvent]: ...


@runtime_checkable
class FacilityProvider(Protocol):
    async def nearby(
        self,
        point: Point,
        *,
        categories: list[FacilityCategory],
        radius_m: float = 1000,
        limit_per_category: int = 5,
    ) -> list[Facility]: ...


@runtime_checkable
class EnvironmentProvider(Protocol):
    async def indicators(
        self,
        point: Point,
        *,
        indicators: list[str],
        at: datetime | None = None,
    ) -> list[IndicatorValue]: ...


@runtime_checkable
class WasteProvider(Protocol):
    async def facilities_near(
        self,
        point: Point,
        *,
        category: WasteCategory,
        radius_m: float = 15000,
        limit: int = 10,
    ) -> list[WasteFacility]: ...


@runtime_checkable
class JourneyPlanner(Protocol):
    async def plan(
        self,
        origin: Point,
        destination: Point,
        *,
        departure_at: datetime,
        modes: list[TravelMode],
        preferences: dict[str, Any] | None = None,
    ) -> list[TripOption]: ...
