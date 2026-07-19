"""Accessibility domain models."""

from __future__ import annotations

from datetime import datetime
from enum import StrEnum

from pydantic import BaseModel, ConfigDict, Field


class AccessibilityStatus(StrEnum):
    ACCESSIBLE = "accessible"
    PARTIALLY_ACCESSIBLE = "partially_accessible"
    INACCESSIBLE = "inaccessible"
    UNKNOWN = "unknown"


class AccessibilityIncident(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str
    location: str
    status: AccessibilityStatus = AccessibilityStatus.UNKNOWN
    description: str = ""
    start_at: datetime | None = None
    end_at: datetime | None = None


class AccessibilitySegment(BaseModel):
    model_config = ConfigDict(extra="forbid")

    name: str
    status: AccessibilityStatus = AccessibilityStatus.UNKNOWN
    notes: str = ""
    unknowns: list[str] = Field(default_factory=list)
