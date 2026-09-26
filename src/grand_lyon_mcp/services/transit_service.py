"""Transit departures with realtime → GTFS fallback."""

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
from grand_lyon_mcp.domain.protocols import TransitRealtimeProvider, TransitStaticProvider
from grand_lyon_mcp.domain.provenance import theoretical_provenance
from grand_lyon_mcp.domain.transit import Departure
from grand_lyon_mcp.domain.transit_line import filter_departures_by_line, line_matches
from grand_lyon_mcp.infrastructure.time import now_paris
from grand_lyon_mcp.services.place_service import PlaceService


class TransitService:
    def __init__(
        self,
        *,
        places: PlaceService,
        realtime: TransitRealtimeProvider | None = None,
        static: TransitStaticProvider | None = None,
    ) -> None:
        self._places = places
        self._realtime = realtime
        self._static = static

    async def next_departures(
        self,
        *,
        stop: PlaceRef,
        line: str | None = None,
        direction: str | None = None,
        at: datetime | None = None,
        limit: int = 6,
    ) -> Envelope:
        generated = now_paris()
        at = at or generated
        warnings: list[WarningItem] = []
        sources = []

        resolved = await self._places.resolve_place(place=stop, limit=5)
        if resolved.status in (ResultStatus.NOT_FOUND, ResultStatus.INVALID_REQUEST):
            return make_envelope(
                status=resolved.status,
                generated_at=generated,
                summary="Arrêt introuvable.",
                data={"stop": {}, "departures": []},
                warnings=resolved.warnings,
            )
        cands = resolved.data.get("candidates") or []
        if not cands:
            return make_envelope(
                status=ResultStatus.NOT_FOUND,
                generated_at=generated,
                summary="Arrêt introuvable.",
                data={"stop": {}, "departures": []},
            )
        if resolved.status == ResultStatus.AMBIGUOUS and cands:
            # Prefer exact name match; otherwise top candidate + warning
            from grand_lyon_mcp.domain.geo import normalize_name

            q = stop.query or ""
            nq = normalize_name(q) if q else ""
            exact = [c for c in cands if nq and normalize_name(str(c.get("name") or "")) == nq]
            if exact:
                cands = exact + [c for c in cands if c not in exact]
            else:
                warnings.append(
                    WarningItem(
                        code=WarningCode.AMBIGUOUS_PLACE.value,
                        message="Plusieurs arrêts correspondent ; le plus probable est utilisé.",
                        retryable=False,
                    )
                )
            warnings.extend(resolved.warnings)
        stop_info = cands[0]
        stop_id = stop_info["id"]

        # Fallback chain: passages (stop+line) → SIRI (line+direction) → GTFS theoretical
        departures: list[Departure] = []
        used_realtime = False
        used_gtfs = False
        realtime_failed = False
        line_filter_dropped = False

        if self._realtime is not None:
            try:
                raw = await self._realtime.get_departures(
                    stop_id, line=line, direction=direction, limit=limit
                )
                # Re-apply strict filter: providers may return partial unfiltered sets
                if line:
                    strict = filter_departures_by_line(raw, line)
                    if raw and not strict:
                        line_filter_dropped = True
                    departures = strict
                else:
                    departures = list(raw)
                if direction:
                    departures = [
                        d for d in departures if direction.casefold() in d.destination.casefold()
                    ]
                used_realtime = bool(departures)
            except Exception:
                realtime_failed = True
                warnings.append(
                    WarningItem(
                        code=WarningCode.SOURCE_UNAVAILABLE.value,
                        source="tcl_departures",
                        message="Temps réel indisponible, bascule GTFS si possible.",
                        retryable=True,
                    )
                )

        if line_filter_dropped:
            warnings.append(
                WarningItem(
                    code=WarningCode.PARTIAL_RESULT.value,
                    source="tcl_departures",
                    message=(
                        f"Aucun départ temps réel strictement filtré pour la ligne {line} ; "
                        "repli GTFS théorique si disponible."
                    ),
                    retryable=False,
                )
            )

        # GTFS fallback when no realtime matches, or realtime failed
        if not departures and self._static is not None:
            try:
                departures = await self._static.get_scheduled_departures(
                    stop_id, at=at, line=line, direction=direction, limit=limit
                )
                # ensure never marked realtime
                departures = [d.model_copy(update={"realtime": False}) for d in departures]
                used_gtfs = bool(departures)
                sources.append(
                    theoretical_provenance(
                        provider="GTFS",
                        source_id="gtfs_tcl",
                        dataset="GTFS_TCL",
                        attribution="SYTRAL Mobilités",
                        retrieved_at=generated,
                    )
                )
                if used_gtfs:
                    code = (
                        WarningCode.STALE_DATA.value
                        if not realtime_failed and not line_filter_dropped
                        else WarningCode.PARTIAL_RESULT.value
                    )
                    if not any(w.code == code for w in warnings):
                        warnings.append(
                            WarningItem(
                                code=code,
                                source="gtfs_tcl",
                                message=(
                                    "Horaires théoriques GTFS (pas de temps réel pour ce filtre)."
                                ),
                                retryable=False,
                            )
                        )
            except Exception:
                warnings.append(
                    WarningItem(
                        code=WarningCode.SOURCE_UNAVAILABLE.value,
                        source="gtfs",
                        message="Horaires théoriques indisponibles.",
                        retryable=True,
                    )
                )

        if used_realtime:
            from grand_lyon_mcp.domain.common import SourceProvenance

            sources.append(
                SourceProvenance(
                    provider="DataGrandLyon",
                    source_id="tcl_departures",
                    dataset="prochains_passages",
                    attribution="SYTRAL Mobilités",
                    license="unknown",
                    observed_at=generated,
                    retrieved_at=generated,
                    age_seconds=0,
                    realtime=True,
                    stale=False,
                )
            )

        # Final honesty: GTFS-sourced never realtime; re-filter if line set
        if line:
            departures = [d for d in departures if line_matches(line, d.line_name, d.line_id)]
        if direction:
            departures = [d for d in departures if direction.casefold() in d.destination.casefold()]
        departures = departures[:limit]
        if used_gtfs:
            departures = [d.model_copy(update={"realtime": False}) for d in departures]

        if not departures:
            status = ResultStatus.UNAVAILABLE if warnings else ResultStatus.NOT_FOUND
            return make_envelope(
                status=status,
                generated_at=generated,
                summary="Aucun départ trouvé.",
                data={"stop": stop_info, "departures": []},
                sources=sources,
                warnings=warnings,
                degraded=True,
            )

        status = ResultStatus.PARTIAL if warnings else ResultStatus.OK
        if used_realtime:
            rt_label = "temps réel"
        elif used_gtfs:
            rt_label = "théoriques (GTFS)"
        else:
            rt_label = ""
        return make_envelope(
            status=status,
            generated_at=generated,
            summary=f"{len(departures)} départ(s) {rt_label} pour {stop_info.get('name')}.",
            data={
                "stop": stop_info,
                "departures": [d.model_dump(mode="json") for d in departures],
            },
            sources=sources,
            warnings=warnings,
            degraded=bool(warnings),
        )
