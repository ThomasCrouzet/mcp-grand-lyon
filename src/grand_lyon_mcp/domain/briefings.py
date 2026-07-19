"""Personal briefing domain models."""

from __future__ import annotations

from datetime import datetime
from typing import Any

from pydantic import BaseModel, ConfigDict, Field


class BriefingChange(BaseModel):
    model_config = ConfigDict(extra="forbid")

    key: str
    previous: str | None = None
    current: str | None = None


class BriefingResult(BaseModel):
    model_config = ConfigDict(extra="forbid")

    profile: str
    at: datetime
    summary: str
    facts: dict[str, Any] = Field(default_factory=dict)
    changes: list[BriefingChange] = Field(default_factory=list)
