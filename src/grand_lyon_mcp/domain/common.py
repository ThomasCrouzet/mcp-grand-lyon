"""Shared domain models: place refs, envelopes, warnings, status."""

from __future__ import annotations

from datetime import datetime
from enum import StrEnum
from typing import Any, Literal
from uuid import uuid4

from pydantic import BaseModel, ConfigDict, Field, model_validator


class ResultStatus(StrEnum):
    OK = "ok"
    PARTIAL = "partial"
    NOT_FOUND = "not_found"
    AMBIGUOUS = "ambiguous"
    UNAVAILABLE = "unavailable"
    INVALID_REQUEST = "invalid_request"


class WarningCode(StrEnum):
    AUTH_FAILED = "AUTH_FAILED"
    SOURCE_TIMEOUT = "SOURCE_TIMEOUT"
    SOURCE_UNAVAILABLE = "SOURCE_UNAVAILABLE"
    SOURCE_SCHEMA_CHANGED = "SOURCE_SCHEMA_CHANGED"
    SOURCE_UNRESOLVED = "SOURCE_UNRESOLVED"
    STALE_DATA = "STALE_DATA"
    AMBIGUOUS_PLACE = "AMBIGUOUS_PLACE"
    PLACE_NOT_FOUND = "PLACE_NOT_FOUND"
    ROUTING_UNAVAILABLE = "ROUTING_UNAVAILABLE"
    UNSUPPORTED_INDICATOR = "UNSUPPORTED_INDICATOR"
    INVALID_REQUEST = "INVALID_REQUEST"
    PARTIAL_RESULT = "PARTIAL_RESULT"


class PlaceRef(BaseModel):
    """Exactly one of place_id, query, coordinates, or profile_place."""

    model_config = ConfigDict(extra="forbid")

    place_id: str | None = Field(default=None, max_length=200)
    query: str | None = Field(default=None, max_length=200)
    latitude: float | None = None
    longitude: float | None = None
    profile_place: str | None = Field(default=None, max_length=64)

    @model_validator(mode="after")
    def exactly_one_mode(self) -> PlaceRef:
        has_id = self.place_id is not None and self.place_id.strip() != ""
        has_query = self.query is not None and self.query.strip() != ""
        has_coords = self.latitude is not None or self.longitude is not None
        has_profile = self.profile_place is not None and self.profile_place.strip() != ""
        modes = sum([has_id, has_query, has_coords, has_profile])
        if modes == 0:
            raise ValueError("Exactly one place mode is required")
        if modes > 1:
            raise ValueError("Only one place mode is allowed at a time")
        if has_coords:
            if self.latitude is None or self.longitude is None:
                raise ValueError("latitude and longitude must both be provided")
            if not (-90.0 <= self.latitude <= 90.0):
                raise ValueError("latitude out of range")
            if not (-180.0 <= self.longitude <= 180.0):
                raise ValueError("longitude out of range")
        if has_query and self.query is not None and not self.query.strip():
            raise ValueError("query must not be empty")
        if has_id and self.place_id is not None and not self.place_id.strip():
            raise ValueError("place_id must not be empty")
        return self


class SourceProvenance(BaseModel):
    model_config = ConfigDict(extra="forbid")

    provider: str
    source_id: str
    dataset: str
    attribution: str
    license: str = "unknown"
    observed_at: datetime | None = None
    retrieved_at: datetime | None = None
    age_seconds: int | None = None
    realtime: bool = False
    stale: bool = False


class WarningItem(BaseModel):
    model_config = ConfigDict(extra="forbid")

    code: str
    source: str | None = None
    message: str
    retryable: bool = False


class Envelope(BaseModel):
    """Common MCP tool response envelope."""

    model_config = ConfigDict(extra="forbid")

    schema_version: Literal["1.0"] = "1.0"
    request_id: str = Field(default_factory=lambda: str(uuid4()))
    status: ResultStatus
    generated_at: datetime
    summary: str
    data: dict[str, Any] = Field(default_factory=dict)
    sources: list[SourceProvenance] = Field(default_factory=list)
    warnings: list[WarningItem] = Field(default_factory=list)
    degraded: bool = False


def make_envelope(
    *,
    status: ResultStatus,
    generated_at: datetime,
    summary: str,
    data: dict[str, Any] | None = None,
    sources: list[SourceProvenance] | None = None,
    warnings: list[WarningItem] | None = None,
    degraded: bool = False,
    request_id: str | None = None,
) -> Envelope:
    return Envelope(
        request_id=request_id or str(uuid4()),
        status=status,
        generated_at=generated_at,
        summary=summary,
        data=data or {},
        sources=sources or [],
        warnings=warnings or [],
        degraded=degraded,
    )
