"""Waste classification domain models."""

from __future__ import annotations

from enum import StrEnum

from pydantic import BaseModel, ConfigDict, Field


class WasteCategory(StrEnum):
    HOUSEHOLD_WASTE = "household_waste"
    PAPER = "paper"
    CARDBOARD = "cardboard"
    GLASS = "glass"
    METAL = "metal"
    TEXTILE = "textile"
    ELECTRICAL_EQUIPMENT = "electrical_equipment"
    PORTABLE_BATTERY = "portable_battery"
    VEHICLE_BATTERY = "vehicle_battery"
    LIGHT_BULB = "light_bulb"
    PAINT = "paint"
    CHEMICAL_PRODUCT = "chemical_product"
    USED_OIL = "used_oil"
    GREEN_WASTE = "green_waste"
    RUBBLE = "rubble"
    FURNITURE = "furniture"
    WOOD = "wood"
    MEDICATION = "medication"
    UNKNOWN = "unknown"


class WasteClassification(BaseModel):
    model_config = ConfigDict(extra="forbid")

    input: str
    category: WasteCategory
    hazardous: bool = False
    confidence: float = Field(ge=0.0, le=1.0, default=0.0)
    matched_rule: str | None = None
    candidates: list[WasteCategory] = Field(default_factory=list)


class WasteFacility(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str
    name: str
    latitude: float
    longitude: float
    accepted_categories: list[WasteCategory] = Field(default_factory=list)
    distance_m: float | None = None
    open_status: str = "unknown"
