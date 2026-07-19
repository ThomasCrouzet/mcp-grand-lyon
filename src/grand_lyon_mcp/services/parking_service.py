"""Parking options service — honest about availability provenance."""

from __future__ import annotations

from grand_lyon_mcp.domain.common import (
    Envelope,
    PlaceRef,
    ResultStatus,
    WarningCode,
    WarningItem,
    make_envelope,
)
from grand_lyon_mcp.domain.geo import Point
from grand_lyon_mcp.domain.parking import ParkingType
from grand_lyon_mcp.domain.protocols import ParkingProvider
from grand_lyon_mcp.infrastructure.time import now_paris
from grand_lyon_mcp.services.place_service import PlaceService


class ParkingService:
    def __init__(self, *, places: PlaceService, provider: ParkingProvider | None = None) -> None:
        self._places = places
        self._provider = provider

    async def options(
        self,
        *,
        destination: PlaceRef,
        types: list[str] | None = None,
        radius_m: float = 1500,
        minimum_spaces: int = 0,
        limit: int = 10,
    ) -> Envelope:
        generated = now_paris()
        warnings: list[WarningItem] = []
        env = await self._places.resolve_place(place=destination, limit=1)
        cands = env.data.get("candidates") or []
        if not cands and destination.latitude is not None and destination.longitude is not None:
            point = Point(destination.latitude, destination.longitude)
        elif cands:
            point = Point(cands[0]["latitude"], cands[0]["longitude"])
        else:
            return make_envelope(
                status=ResultStatus.NOT_FOUND,
                generated_at=generated,
                summary="Destination introuvable.",
                data={"options": []},
            )

        if self._provider is None:
            return make_envelope(
                status=ResultStatus.UNAVAILABLE,
                generated_at=generated,
                summary="Source parkings indisponible.",
                data={"options": []},
                degraded=True,
            )

        ptypes = None
        if types:
            ptypes = [ParkingType(t) for t in types]
        try:
            options = await self._provider.options_near(
                point,
                types=ptypes,
                radius_m=radius_m,
                minimum_spaces=minimum_spaces,
                limit=limit,
            )
        except Exception:
            return make_envelope(
                status=ResultStatus.UNAVAILABLE,
                generated_at=generated,
                summary="Parkings indisponibles.",
                data={"options": []},
                warnings=[
                    WarningItem(
                        code=WarningCode.SOURCE_UNAVAILABLE.value,
                        source="parking",
                        message="Échec récupération parkings.",
                        retryable=True,
                    )
                ],
                degraded=True,
            )

        has_live = any(o.available_spaces is not None for o in options)
        capacity_only = any(o.capacity is not None and o.available_spaces is None for o in options)
        if options and not has_live:
            warnings.append(
                WarningItem(
                    code=WarningCode.PARTIAL_RESULT.value,
                    source="parking_realtime",
                    message=(
                        "Aucune disponibilité temps réel validée ; "
                        "capacité seule affichée (places libres non inventées)."
                    ),
                    retryable=True,
                )
            )
        elif capacity_only and has_live:
            warnings.append(
                WarningItem(
                    code=WarningCode.PARTIAL_RESULT.value,
                    source="parking_realtime",
                    message=(
                        "Disponibilité temps réel partielle (certaines options sans places libres)."
                    ),
                    retryable=False,
                )
            )
        # surface unresolved live source if provider exposes the flag
        live_flag = getattr(self._provider, "live_dispo_resolved", None)
        if live_flag is False and not has_live:
            warnings.append(
                WarningItem(
                    code=WarningCode.SOURCE_UNRESOLVED.value,
                    source="parking_realtime",
                    message="Source de disponibilité parkings non résolue.",
                    retryable=True,
                )
            )

        status = ResultStatus.PARTIAL if warnings else ResultStatus.OK
        return make_envelope(
            status=status if options else ResultStatus.NOT_FOUND,
            generated_at=generated,
            summary=f"{len(options)} parking(s) trouvé(s).",
            data={"options": [o.model_dump(mode="json") for o in options]},
            warnings=warnings,
            degraded=bool(warnings),
        )
