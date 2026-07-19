"""Nearby facilities service."""

from __future__ import annotations

from datetime import datetime

from grand_lyon_mcp.domain.common import Envelope, PlaceRef, ResultStatus, make_envelope
from grand_lyon_mcp.domain.facilities import FacilityCategory
from grand_lyon_mcp.domain.geo import Point
from grand_lyon_mcp.domain.protocols import FacilityProvider
from grand_lyon_mcp.infrastructure.time import now_paris
from grand_lyon_mcp.services.place_service import PlaceService


class FacilityService:
    def __init__(self, *, places: PlaceService, provider: FacilityProvider | None = None) -> None:
        self._places = places
        self._provider = provider

    async def nearby(
        self,
        *,
        location: PlaceRef,
        categories: list[str],
        radius_m: float = 1000,
        open_at: datetime | None = None,
        limit_per_category: int = 5,
    ) -> Envelope:
        generated = now_paris()
        env = await self._places.resolve_place(place=location, limit=3)
        cands = env.data.get("candidates") or []
        if not cands and location.latitude is not None and location.longitude is not None:
            point = Point(location.latitude, location.longitude)
        elif cands:
            point = Point(cands[0]["latitude"], cands[0]["longitude"])
        elif location.query:
            # Last resort: try photon-style free query via place service already failed
            return make_envelope(
                status=ResultStatus.NOT_FOUND,
                generated_at=generated,
                summary="Lieu introuvable.",
                data={"facilities": []},
                warnings=env.warnings,
            )
        else:
            return make_envelope(
                status=ResultStatus.NOT_FOUND,
                generated_at=generated,
                summary="Lieu introuvable.",
                data={"facilities": []},
            )
        if self._provider is None:
            return make_envelope(
                status=ResultStatus.UNAVAILABLE,
                generated_at=generated,
                summary="Équipements indisponibles.",
                data={"facilities": []},
                degraded=True,
            )
        cats = [FacilityCategory(c) for c in categories]
        facilities = await self._provider.nearby(
            point, categories=cats, radius_m=radius_m, limit_per_category=limit_per_category
        )
        by_cat: dict[str, list[dict[str, object]]] = {}
        for f in facilities:
            by_cat.setdefault(f.category.value, []).append(f.model_dump(mode="json"))
        return make_envelope(
            status=ResultStatus.OK,
            generated_at=generated,
            summary=f"{len(facilities)} équipement(s) trouvé(s).",
            data={"facilities": by_cat, "items": [f.model_dump(mode="json") for f in facilities]},
        )
