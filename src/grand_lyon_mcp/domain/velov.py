"""Vélo'v domain models."""

from __future__ import annotations

from datetime import datetime
from enum import StrEnum

from pydantic import BaseModel, ConfigDict, Field


class VelovStationStatus(StrEnum):
    OPEN = "open"
    CLOSED = "closed"
    UNKNOWN = "unknown"


class VelovStation(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str
    name: str
    latitude: float
    longitude: float
    bikes_available: int = 0
    docks_available: int = 0
    capacity: int = 0
    status: VelovStationStatus = VelovStationStatus.UNKNOWN
    observed_at: datetime | None = None
    distance_m: float | None = None


class ReliabilityScore(BaseModel):
    model_config = ConfigDict(extra="forbid")

    score: float = Field(ge=0.0, le=1.0)
    sample_count: int = 0
    confidence: str = "low"
    probability_available: float = Field(ge=0.0, le=1.0, default=0.0)


class VelovScore(BaseModel):
    model_config = ConfigDict(extra="forbid")

    station_id: str
    score: float = Field(ge=0.0, le=1.0)
    availability_score: float = 0.0
    distance_score: float = 0.0
    freshness_score: float = 0.0
    historical_reliability_score: float = 0.0
