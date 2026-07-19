"""Waste taxonomy classification."""

from __future__ import annotations

from grand_lyon_mcp.domain.waste import WasteCategory
from grand_lyon_mcp.services.waste_service import WasteTaxonomy


def test_electric_bike_battery() -> None:
    tax = WasteTaxonomy.default()
    c = tax.classify("batterie de vélo électrique")
    assert c.category == WasteCategory.PORTABLE_BATTERY
    assert c.hazardous is True
    assert c.confidence >= 0.9


def test_unknown_item() -> None:
    tax = WasteTaxonomy.default()
    c = tax.classify("objet complètement inconnu xyzzy")
    assert c.category == WasteCategory.UNKNOWN


def test_cardboard() -> None:
    tax = WasteTaxonomy.default()
    c = tax.classify("gros carton déménagement")
    assert c.category == WasteCategory.CARDBOARD
