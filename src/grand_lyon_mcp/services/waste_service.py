"""Waste classification (deterministic taxonomy) + dropoff search."""

from __future__ import annotations

import re
from datetime import datetime
from pathlib import Path
from typing import Any

import yaml

from grand_lyon_mcp.domain.common import Envelope, PlaceRef, ResultStatus, make_envelope
from grand_lyon_mcp.domain.geo import Point
from grand_lyon_mcp.domain.protocols import WasteProvider
from grand_lyon_mcp.domain.waste import WasteCategory, WasteClassification
from grand_lyon_mcp.infrastructure.time import now_paris
from grand_lyon_mcp.services.place_service import PlaceService


class WasteTaxonomy:
    def __init__(self, rules: list[dict[str, Any]]) -> None:
        # higher priority first
        self._rules = sorted(rules, key=lambda r: int(r.get("priority") or 0), reverse=True)

    @classmethod
    def from_yaml(cls, path: Path) -> WasteTaxonomy:
        if not path.is_file():
            return cls(default_rules())
        data = yaml.safe_load(path.read_text(encoding="utf-8")) or {}
        return cls(list(data.get("rules") or default_rules()))

    @classmethod
    def default(cls) -> WasteTaxonomy:
        return cls(default_rules())

    def classify(self, item: str) -> WasteClassification:
        text = item.lower().strip()
        matches: list[tuple[float, dict[str, Any]]] = []
        for rule in self._rules:
            aliases = [a.lower() for a in rule.get("aliases") or []]
            patterns = rule.get("patterns") or []
            hit = False
            for a in aliases:
                if a in text:
                    hit = True
                    break
            if not hit:
                for p in patterns:
                    if re.search(p, text, re.I):
                        hit = True
                        break
            if hit:
                matches.append((float(rule.get("confidence") or 0.8), rule))
        if not matches:
            return WasteClassification(
                input=item,
                category=WasteCategory.UNKNOWN,
                hazardous=False,
                confidence=0.0,
                matched_rule=None,
                candidates=[],
            )
        matches.sort(key=lambda x: x[0], reverse=True)
        best_conf, best = matches[0]
        category = WasteCategory(best["category"])
        candidates = []
        if len(matches) > 1 and abs(matches[0][0] - matches[1][0]) < 0.1:
            candidates = [WasteCategory(m[1]["category"]) for m in matches[:3]]
        return WasteClassification(
            input=item,
            category=category,
            hazardous=bool(best.get("hazardous")),
            confidence=best_conf,
            matched_rule=str(best.get("id") or best.get("category")),
            candidates=candidates,
        )


def default_rules() -> list[dict[str, Any]]:
    return [
        {
            "id": "battery_electric_bike",
            "category": "portable_battery",
            "priority": 100,
            "confidence": 0.93,
            "hazardous": True,
            "aliases": ["batterie de vélo", "batterie velo", "batterie de vélo électrique"],
            "patterns": [r"batterie.*(v[eé]lo|bike)", r"(v[eé]lo|bike).*batterie"],
        },
        {
            "id": "vehicle_battery",
            "category": "vehicle_battery",
            "priority": 90,
            "confidence": 0.9,
            "hazardous": True,
            "aliases": ["batterie voiture", "batterie auto"],
            "patterns": [r"batterie.*(voiture|auto|camion)"],
        },
        {
            "id": "portable_battery",
            "category": "portable_battery",
            "priority": 80,
            "confidence": 0.85,
            "hazardous": True,
            "aliases": ["pile", "batterie", "accu"],
            "patterns": [r"\bpiles?\b", r"\bbatteries?\b"],
        },
        {
            "id": "glass",
            "category": "glass",
            "priority": 50,
            "confidence": 0.85,
            "hazardous": False,
            "aliases": ["verre", "bouteille en verre"],
            "patterns": [r"\bverre\b", r"bouteille"],
        },
        {
            "id": "paper",
            "category": "paper",
            "priority": 40,
            "confidence": 0.8,
            "hazardous": False,
            "aliases": ["papier", "journal", "magazine"],
            "patterns": [r"\bpapier\b"],
        },
        {
            "id": "cardboard",
            "category": "cardboard",
            "priority": 45,
            "confidence": 0.85,
            "hazardous": False,
            "aliases": ["carton", "cardboard"],
            "patterns": [r"\bcarton\b"],
        },
        {
            "id": "electrical",
            "category": "electrical_equipment",
            "priority": 70,
            "confidence": 0.88,
            "hazardous": True,
            "aliases": ["électroménager", "ordinateur", "téléphone", "deee"],
            "patterns": [r"ordinateur|téléphone|electrom[eé]nager|deee"],
        },
        {
            "id": "medication",
            "category": "medication",
            "priority": 75,
            "confidence": 0.9,
            "hazardous": True,
            "aliases": ["médicament", "medicament", "médicaments"],
            "patterns": [r"m[eé]dicament"],
        },
        {
            "id": "paint",
            "category": "paint",
            "priority": 70,
            "confidence": 0.9,
            "hazardous": True,
            "aliases": ["peinture", "pot de peinture"],
            "patterns": [r"peinture"],
        },
        {
            "id": "green",
            "category": "green_waste",
            "priority": 40,
            "confidence": 0.8,
            "hazardous": False,
            "aliases": ["déchets verts", "tonte", "branchages"],
            "patterns": [r"d[eé]chets?\s+verts?|tonte|branch"],
        },
    ]


class WasteService:
    def __init__(
        self,
        *,
        places: PlaceService,
        taxonomy: WasteTaxonomy,
        provider: WasteProvider | None = None,
    ) -> None:
        self._places = places
        self._taxonomy = taxonomy
        self._provider = provider

    async def dropoff(
        self,
        *,
        item: str,
        location: PlaceRef | None = None,
        transport: str = "car",
        open_at: datetime | None = None,
        radius_m: float = 15000,
        limit: int = 10,
    ) -> Envelope:
        generated = now_paris()
        classification = self._taxonomy.classify(item)
        status = ResultStatus.OK
        if classification.candidates:
            status = ResultStatus.AMBIGUOUS
        if classification.category == WasteCategory.UNKNOWN:
            status = ResultStatus.NOT_FOUND

        facilities: list[dict[str, object]] = []
        if self._provider and location is not None:
            env = await self._places.resolve_place(place=location, limit=1)
            cands = env.data.get("candidates") or []
            if cands:
                point = Point(cands[0]["latitude"], cands[0]["longitude"])
            elif location.latitude is not None:
                point = Point(location.latitude, location.longitude)  # type: ignore[arg-type]
            else:
                point = Point(45.75, 4.85)
            facs = await self._provider.facilities_near(
                point, category=classification.category, radius_m=radius_m, limit=limit
            )
            facilities = [f.model_dump(mode="json") for f in facs]

        summary = f"« {item} » → {classification.category.value}" + (
            " (dangereux)" if classification.hazardous else ""
        )
        return make_envelope(
            status=status,
            generated_at=generated,
            summary=summary,
            data={
                "classification": classification.model_dump(mode="json"),
                "facilities": facilities,
                "transport": transport,
                "open_at": open_at.isoformat() if open_at else None,
            },
        )
