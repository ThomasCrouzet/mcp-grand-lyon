"""Aggregate mobility / disruption status."""

from __future__ import annotations

from grand_lyon_mcp.domain.common import (
    Envelope,
    PlaceRef,
    ResultStatus,
    WarningCode,
    WarningItem,
    make_envelope,
)
from grand_lyon_mcp.domain.protocols import (
    AccessibilityProvider,
    TrafficProvider,
    TransitAlertProvider,
)
from grand_lyon_mcp.infrastructure.time import now_paris


class MobilityStatusService:
    def __init__(
        self,
        *,
        alerts: TransitAlertProvider | None = None,
        accessibility: AccessibilityProvider | None = None,
        traffic: TrafficProvider | None = None,
    ) -> None:
        self._alerts = alerts
        self._accessibility = accessibility
        self._traffic = traffic

    async def status(
        self,
        *,
        lines: list[str] | None = None,
        areas: list[PlaceRef] | None = None,
        include: list[str] | None = None,
    ) -> Envelope:
        generated = now_paris()
        include = include or ["transit", "accessibility", "traffic", "roadworks"]
        warnings: list[WarningItem] = []
        data: dict[str, object] = {
            "alerts": [],
            "line_statuses": [],
            "accessibility_incidents": [],
            "road_events": [],
            "traffic_conditions": [],
        }

        if "transit" in include and self._alerts is not None:
            try:
                alerts = await self._alerts.get_alerts(lines=lines)
                # dedupe by id
                seen: set[str] = set()
                unique = []
                for a in alerts:
                    if a.id in seen:
                        continue
                    seen.add(a.id)
                    unique.append(a)
                data["alerts"] = [a.model_dump(mode="json") for a in unique]
                data["line_statuses"] = [
                    {
                        "line": ln,
                        "alert_count": sum(1 for a in unique if ln in a.lines or not a.lines),
                    }
                    for ln in (lines or [])
                ]
            except Exception:
                warnings.append(
                    WarningItem(
                        code=WarningCode.SOURCE_UNAVAILABLE.value,
                        source="tcl_alerts",
                        message="Alertes TCL indisponibles.",
                        retryable=True,
                    )
                )

        if "accessibility" in include and self._accessibility is not None:
            try:
                incidents = await self._accessibility.get_incidents(lines=lines)
                data["accessibility_incidents"] = [i.model_dump(mode="json") for i in incidents]
            except Exception:
                warnings.append(
                    WarningItem(
                        code=WarningCode.SOURCE_UNAVAILABLE.value,
                        source="tcl_accessibility",
                        message="Accessibilité indisponible.",
                        retryable=True,
                    )
                )

        if self._traffic is not None:
            area_label = None
            if areas:
                area_label = areas[0].query or areas[0].profile_place
            if "traffic" in include:
                try:
                    conds = await self._traffic.conditions(area=area_label)
                    data["traffic_conditions"] = [c.model_dump(mode="json") for c in conds]
                except Exception:
                    warnings.append(
                        WarningItem(
                            code=WarningCode.SOURCE_UNAVAILABLE.value,
                            source="traffic",
                            message="Trafic indisponible.",
                            retryable=True,
                        )
                    )
            if "roadworks" in include:
                try:
                    events = await self._traffic.road_events(area=area_label)
                    data["road_events"] = [e.model_dump(mode="json") for e in events]
                except Exception:
                    warnings.append(
                        WarningItem(
                            code=WarningCode.SOURCE_UNAVAILABLE.value,
                            source="road_events",
                            message="Événements routiers indisponibles.",
                            retryable=True,
                        )
                    )

        status = ResultStatus.PARTIAL if warnings else ResultStatus.OK
        n_alerts = len(data["alerts"])  # type: ignore[arg-type]
        return make_envelope(
            status=status,
            generated_at=generated,
            summary=f"État mobilité : {n_alerts} alerte(s).",
            data=data,
            warnings=warnings,
            degraded=bool(warnings),
        )
