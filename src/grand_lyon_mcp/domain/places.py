"""Place domain models."""

from __future__ import annotations

from enum import StrEnum

from pydantic import BaseModel, ConfigDict, Field


class EntityType(StrEnum):
    TRANSPORT_STOP = "transport_stop"
    TRANSPORT_STATION = "transport_station"
    VELOV_STATION = "velov_station"
    PARKING = "parking"
    PARK_AND_RIDE = "park_and_ride"
    TOILET = "toilet"
    DRINKING_WATER = "drinking_water"
    PARK = "park"
    GARDEN = "garden"
    BIKE_PUMP = "bike_pump"
    WASTE_FACILITY = "waste_facility"
    POINT_OF_INTEREST = "point_of_interest"


class PlaceCandidate(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str
    name: str
    label: str
    type: EntityType | str
    latitude: float
    longitude: float
    distance_m: float | None = None
    confidence: float = Field(ge=0.0, le=1.0, default=0.5)


class ResolvedPlace(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str
    name: str
    label: str
    type: EntityType | str
    latitude: float
    longitude: float
    confidence: float = 1.0
