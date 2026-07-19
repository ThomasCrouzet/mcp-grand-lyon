"""Traffic and road events domain models."""

from __future__ import annotations

from datetime import datetime
from enum import StrEnum

from pydantic import BaseModel, ConfigDict


class RoadEventType(StrEnum):
    INCIDENT = "incident"
    CLOSURE = "closure"
    ROADWORKS = "roadworks"
    OTHER = "other"


class TrafficCondition(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str
    area: str
    level: str  # free | slow | congested | unknown
    description: str = ""
    observed_at: datetime | None = None


class RoadEvent(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str
    type: RoadEventType = RoadEventType.OTHER
    title: str
    description: str = ""
    latitude: float | None = None
    longitude: float | None = None
    start_at: datetime | None = None
    end_at: datetime | None = None
