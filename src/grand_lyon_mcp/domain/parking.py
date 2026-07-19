"""Parking domain models."""

from __future__ import annotations

from datetime import datetime
from enum import StrEnum

from pydantic import BaseModel, ConfigDict, Field


class ParkingType(StrEnum):
    PUBLIC_PARKING = "public_parking"
    PARK_AND_RIDE = "park_and_ride"


class ParkingStatus(StrEnum):
    OPEN = "open"
    CLOSED = "closed"
    FULL = "full"
    UNKNOWN = "unknown"


class ParkingOption(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str
    name: str
    type: ParkingType
    latitude: float
    longitude: float
    capacity: int | None = None
    available_spaces: int | None = None
    occupied_spaces: int | None = None
    availability_ratio: float | None = Field(default=None, ge=0.0, le=1.0)
    status: ParkingStatus = ParkingStatus.UNKNOWN
    distance_m: float | None = None
    realtime: bool = False
    observed_at: datetime | None = None
