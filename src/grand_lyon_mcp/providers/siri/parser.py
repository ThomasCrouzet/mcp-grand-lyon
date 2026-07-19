"""SIRI → domain models."""

from __future__ import annotations

from datetime import datetime
from typing import Any

from dateutil.parser import isoparse

from grand_lyon_mcp.domain.transit import Departure, Severity, TransitAlert
from grand_lyon_mcp.domain.transit_line import normalize_line_code
from grand_lyon_mcp.infrastructure.time import BUSINESS_TZ


def _text(value: Any) -> str:
    if value is None:
        return ""
    if isinstance(value, str):
        return value
    if isinstance(value, dict):
        return str(value.get("value") or value.get("#text") or next(iter(value.values()), ""))
    return str(value)


def _parse_dt(value: str | None) -> datetime | None:
    if not value:
        return None
    try:
        dt = isoparse(value)
        if dt.tzinfo is None:
            dt = dt.replace(tzinfo=BUSINESS_TZ)
        return dt
    except (ValueError, TypeError):
        return None


def parse_estimated_timetable(payload: dict[str, Any]) -> list[Departure]:
    departures: list[Departure] = []
    # Flexible walk of common SIRI Lite shapes
    for journey in _walk_key(payload, "EstimatedVehicleJourney"):
        line = _text(journey.get("LineRef") or journey.get("lineRef"))
        dest = _text(
            journey.get("DestinationName")
            or journey.get("destinationName")
            or journey.get("DirectionName")
        )
        calls = journey.get("EstimatedCalls") or journey.get("estimatedCalls") or {}
        call_list: list[Any]
        if isinstance(calls, dict):
            call_list = calls.get("EstimatedCall") or calls.get("estimatedCall") or []
        else:
            call_list = calls if isinstance(calls, list) else []
        if isinstance(call_list, dict):
            call_list = [call_list]
        for call in call_list:
            if not isinstance(call, dict):
                continue
            aimed = _parse_dt(call.get("AimedDepartureTime") or call.get("aimedDepartureTime"))
            expected = _parse_dt(
                call.get("ExpectedDepartureTime") or call.get("expectedDepartureTime")
            )
            delay = None
            if aimed and expected:
                delay = int((expected - aimed).total_seconds())
            status = _text(call.get("DepartureStatus") or "")
            line_name = _pretty_line_name(line)
            stop_ref = (
                _text(
                    call.get("StopPointRef")
                    or call.get("stopPointRef")
                    or call.get("StopPointName")
                    or call.get("stopPointName")
                )
                or None
            )
            departures.append(
                Departure(
                    line_id=f"tcl:{line}" if line else "tcl:unknown",
                    line_name=line_name or "?",
                    destination=dest or _text(call.get("DestinationDisplay")),
                    platform=_text(call.get("DeparturePlatformName") or None) or None,
                    stop_ref=stop_ref,
                    scheduled_at=aimed,
                    expected_at=expected or aimed,
                    delay_seconds=delay,
                    realtime=True,
                    cancelled=status.lower() in {"cancelled", "canceled"},
                )
            )
    return departures


def _pretty_line_name(line_ref: str) -> str:
    """Normalize SIRI LineRef like ActIV:Line::C12:SYTRAL → C12."""
    code = normalize_line_code(line_ref)
    return code or "?"


def parse_situation_exchange(payload: dict[str, Any]) -> list[TransitAlert]:
    alerts: list[TransitAlert] = []
    for sit in _walk_key(payload, "PtSituationElement") + _walk_key(payload, "Situation"):
        sid = _text(sit.get("SituationNumber") or sit.get("situationNumber") or sit.get("id"))
        title = _text(sit.get("Summary") or sit.get("summary") or sit.get("Title"))
        desc = _text(sit.get("Description") or sit.get("description"))
        sev_raw = _text(sit.get("Severity") or sit.get("severity") or "unknown").lower()
        severity = {
            "normal": Severity.INFO,
            "slight": Severity.MINOR,
            "verySlight": Severity.MINOR,
            "severe": Severity.MAJOR,
            "verySevere": Severity.CRITICAL,
            "noImpact": Severity.INFO,
        }.get(sev_raw, Severity.UNKNOWN)
        lines: list[str] = []
        affects = sit.get("Affects") or sit.get("affects") or {}
        for lr in _walk_key(affects, "LineRef"):
            if isinstance(lr, str):
                lines.append(lr)
            elif isinstance(lr, dict):
                lines.append(_text(lr))
        alerts.append(
            TransitAlert(
                id=sid or title[:40],
                severity=severity,
                title=title or "Alerte TCL",
                description=desc,
                lines=lines,
            )
        )
    return alerts


def _walk_key(obj: Any, key: str) -> list[dict[str, Any]]:
    found: list[dict[str, Any]] = []
    if isinstance(obj, dict):
        for k, v in obj.items():
            if k == key or k.lower() == key.lower():
                if isinstance(v, list):
                    found.extend([x for x in v if isinstance(x, dict)])
                elif isinstance(v, dict):
                    found.append(v)
            else:
                found.extend(_walk_key(v, key))
    elif isinstance(obj, list):
        for item in obj:
            found.extend(_walk_key(item, key))
    return found
