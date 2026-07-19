"""Transit domain models."""

from __future__ import annotations

from datetime import datetime
from enum import StrEnum

from pydantic import BaseModel, ConfigDict, Field


class Severity(StrEnum):
    INFO = "info"
    MINOR = "minor"
    MAJOR = "major"
    CRITICAL = "critical"
    UNKNOWN = "unknown"


class Departure(BaseModel):
    model_config = ConfigDict(extra="forbid")

    line_id: str
    line_name: str
    destination: str
    platform: str | None = None
    stop_ref: str | None = None  # SIRI StopPointRef / passage stop id when known
    scheduled_at: datetime | None = None
    expected_at: datetime | None = None
    delay_seconds: int | None = None
    realtime: bool = False
    cancelled: bool = False


class TransitAlert(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str
    severity: Severity = Severity.UNKNOWN
    title: str
    description: str = ""
    lines: list[str] = Field(default_factory=list)
    start_at: datetime | None = None
    end_at: datetime | None = None
