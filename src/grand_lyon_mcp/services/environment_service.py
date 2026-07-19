"""Environment brief — only resolved indicators."""

from __future__ import annotations

from datetime import datetime

from grand_lyon_mcp.domain.common import (
    Envelope,
    PlaceRef,
    ResultStatus,
    WarningCode,
    WarningItem,
    make_envelope,
)
from grand_lyon_mcp.domain.geo import Point
from grand_lyon_mcp.domain.protocols import EnvironmentProvider
from grand_lyon_mcp.infrastructure.time import now_paris
from grand_lyon_mcp.services.place_service import PlaceService


class EnvironmentService:
    def __init__(
        self, *, places: PlaceService, provider: EnvironmentProvider | None = None
    ) -> None:
        self._places = places
        self._provider = provider

    async def brief(
        self,
        *,
        location: PlaceRef,
        at: datetime | None = None,
        indicators: list[str] | None = None,
    ) -> Envelope:
        generated = now_paris()
        indicators = indicators or ["pollen", "air_quality", "heat"]
        env = await self._places.resolve_place(place=location, limit=1)
        cands = env.data.get("candidates") or []
        if cands:
            point = Point(cands[0]["latitude"], cands[0]["longitude"])
        elif location.latitude is not None:
            point = Point(location.latitude, location.longitude)  # type: ignore[arg-type]
        else:
            point = Point(45.75, 4.85)  # Lyon center fallback for area queries

        warnings: list[WarningItem] = []
        if self._provider is None:
            for ind in indicators:
                warnings.append(
                    WarningItem(
                        code=WarningCode.UNSUPPORTED_INDICATOR.value,
                        source=ind,
                        message=f"Indicateur {ind} non supporté (pas de source).",
                        retryable=False,
                    )
                )
            return make_envelope(
                status=ResultStatus.PARTIAL,
                generated_at=generated,
                summary="Aucun indicateur environnemental disponible.",
                data={"indicators": []},
                warnings=warnings,
                degraded=True,
            )

        values = await self._provider.indicators(point, indicators=indicators, at=at)
        out = []
        for v in values:
            if not v.supported:
                warnings.append(
                    WarningItem(
                        code=WarningCode.UNSUPPORTED_INDICATOR.value,
                        source=v.indicator.value,
                        message=f"Indicateur {v.indicator.value} sans source résolue.",
                        retryable=False,
                    )
                )
            else:
                out.append(v.model_dump(mode="json"))

        status = (
            ResultStatus.OK
            if out and not warnings
            else ResultStatus.PARTIAL
            if out
            else ResultStatus.UNAVAILABLE
        )
        return make_envelope(
            status=status,
            generated_at=generated,
            summary=f"{len(out)} indicateur(s) environnemental(aux).",
            data={"indicators": out},
            warnings=warnings,
            degraded=bool(warnings),
        )
