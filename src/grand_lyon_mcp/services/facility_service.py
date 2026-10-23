"""Nearby facilities service."""

from __future__ import annotations

import re
from datetime import datetime

from grand_lyon_mcp.domain.common import (
    Envelope,
    PlaceRef,
    ResultStatus,
    WarningCode,
    WarningItem,
    make_envelope,
)
from grand_lyon_mcp.domain.facilities import Facility, FacilityCategory, OpenStatus
from grand_lyon_mcp.domain.geo import Point
from grand_lyon_mcp.domain.protocols import FacilityProvider
from grand_lyon_mcp.infrastructure.time import BUSINESS_TZ, now_paris
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
        if open_at is not None and open_at.utcoffset() is None:
            return make_envelope(
                status=ResultStatus.INVALID_REQUEST,
                generated_at=generated,
                summary="open_at must include a UTC offset.",
                data={"facilities": {}, "items": []},
            )
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
        try:
            cats = [FacilityCategory(c) for c in categories]
        except ValueError:
            return make_envelope(
                status=ResultStatus.INVALID_REQUEST,
                generated_at=generated,
                summary="One or more facility categories are unsupported.",
                data={"facilities": {}, "items": []},
            )
        facilities = await self._provider.nearby(
            point, categories=cats, radius_m=radius_m, limit_per_category=limit_per_category
        )
        warnings: list[WarningItem] = []
        if open_at is not None:
            verified: list[Facility] = []
            unknown_count = 0
            for facility in facilities:
                status = _opening_status_at(facility, open_at)
                if status == OpenStatus.OPEN:
                    verified.append(facility.model_copy(update={"open_status": OpenStatus.OPEN}))
                elif status == OpenStatus.UNKNOWN:
                    unknown_count += 1
            facilities = verified
            if unknown_count:
                warnings.append(
                    WarningItem(
                        code=WarningCode.PARTIAL_RESULT.value,
                        source="facility_opening_hours",
                        message=(
                            f"Omitted {unknown_count} facilities with unverified opening hours."
                        ),
                        retryable=False,
                    )
                )
        by_cat: dict[str, list[dict[str, object]]] = {}
        for f in facilities:
            by_cat.setdefault(f.category.value, []).append(f.model_dump(mode="json"))
        return make_envelope(
            status=ResultStatus.PARTIAL if warnings else ResultStatus.OK,
            generated_at=generated,
            summary=f"{len(facilities)} équipement(s) trouvé(s).",
            data={
                "facilities": by_cat,
                "items": [f.model_dump(mode="json") for f in facilities],
                "open_at": open_at.isoformat() if open_at else None,
            },
            warnings=warnings,
            degraded=bool(warnings),
        )


_SIMPLE_HOURS = re.compile(
    r"^\s*(?P<start_hour>[01]?\d|2[0-3]):(?P<start_minute>[0-5]\d)\s*"
    r"-\s*(?P<end_hour>[01]?\d|2[0-3]):(?P<end_minute>[0-5]\d)\s*$"
)


def _opening_status_at(facility: Facility, at: datetime) -> OpenStatus:
    raw = facility.opening_hours_raw
    if not raw:
        return OpenStatus.UNKNOWN
    match = _SIMPLE_HOURS.fullmatch(raw)
    if match is None:
        return OpenStatus.UNKNOWN
    local = at.astimezone(BUSINESS_TZ)
    current = local.hour * 60 + local.minute
    start = int(match["start_hour"]) * 60 + int(match["start_minute"])
    end = int(match["end_hour"]) * 60 + int(match["end_minute"])
    is_open = start <= current < end if start <= end else current >= start or current < end
    return OpenStatus.OPEN if is_open else OpenStatus.CLOSED
