"""Tolerant SIRI Lite models."""

from __future__ import annotations

from typing import Any

from pydantic import BaseModel, ConfigDict, Field


class SiriModel(BaseModel):
    model_config = ConfigDict(extra="allow")


class EstimatedCall(SiriModel):
    StopPointRef: str | None = None
    ExpectedDepartureTime: str | None = None
    AimedDepartureTime: str | None = None
    DestinationDisplay: str | None = None
    DepartureStatus: str | None = None


class EstimatedVehicleJourney(SiriModel):
    LineRef: str | None = None
    DirectionRef: str | None = None
    DestinationName: str | None = None
    EstimatedCalls: dict[str, Any] | list[Any] | None = None


class Situation(SiriModel):
    SituationNumber: str | None = None
    Summary: str | dict[str, Any] | None = None
    Description: str | dict[str, Any] | None = None
    Severity: str | None = None
    Affects: dict[str, Any] | None = None


class VehicleActivity(SiriModel):
    VehicleRef: str | None = None
    ProgressBetweenStops: dict[str, Any] | None = None
    MonitoredVehicleJourney: dict[str, Any] | None = Field(default=None)
