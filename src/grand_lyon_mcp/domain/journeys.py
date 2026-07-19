"""Journey / trip options domain models."""

from __future__ import annotations

from datetime import datetime
from enum import StrEnum

from pydantic import BaseModel, ConfigDict, Field

from grand_lyon_mcp.domain.transit import TransitAlert


class TravelMode(StrEnum):
    TCL = "tcl"
    VELOV = "velov"
    PARK_AND_RIDE = "park_and_ride"
    CAR = "car"
    WALK = "walk"


class TripOption(BaseModel):
    model_config = ConfigDict(extra="forbid")

    mode: TravelMode
    score: float = Field(ge=0.0, le=1.0)
    estimated_duration_seconds: int | None = None
    departure_at: datetime | None = None
    arrival_at: datetime | None = None
    walking_distance_m: int | None = None
    transfers: int = 0
    availability: str | None = None
    reliability: float | None = None
    accessibility: str | None = None
    alerts: list[TransitAlert] = Field(default_factory=list)
    realtime: bool = False
    summary: str = ""
    sources: list[str] = Field(default_factory=list)
