"""Environment indicators domain models."""

from __future__ import annotations

from datetime import datetime
from enum import StrEnum

from pydantic import BaseModel, ConfigDict


class EnvironmentIndicator(StrEnum):
    POLLEN = "pollen"
    AIR_QUALITY = "air_quality"
    HEAT = "heat"


class IndicatorValue(BaseModel):
    model_config = ConfigDict(extra="forbid")

    indicator: EnvironmentIndicator
    value: float | str | None = None
    unit: str | None = None
    zone: str | None = None
    observed_at: datetime | None = None
    source_id: str | None = None
    supported: bool = True
