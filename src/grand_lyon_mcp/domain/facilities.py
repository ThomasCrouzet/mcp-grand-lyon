"""Urban facilities domain models."""

from __future__ import annotations

from enum import StrEnum

from pydantic import BaseModel, ConfigDict


class FacilityCategory(StrEnum):
    TOILET = "toilet"
    DRINKING_WATER = "drinking_water"
    PARK = "park"
    GARDEN = "garden"
    BIKE_PUMP = "bike_pump"
    VELOV_STATION = "velov_station"
    WASTE_FACILITY = "waste_facility"


class OpenStatus(StrEnum):
    OPEN = "open"
    CLOSED = "closed"
    UNKNOWN = "unknown"


class Facility(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str
    name: str
    category: FacilityCategory
    latitude: float
    longitude: float
    distance_m: float | None = None
    open_status: OpenStatus = OpenStatus.UNKNOWN
    opening_hours_raw: str | None = None
