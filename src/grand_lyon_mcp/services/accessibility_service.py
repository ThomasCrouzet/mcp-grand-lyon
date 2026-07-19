"""Accessibility check service — absence of data is never 'accessible'."""

from __future__ import annotations

from typing import Any

from grand_lyon_mcp.domain.accessibility import AccessibilityStatus
from grand_lyon_mcp.domain.common import Envelope, PlaceRef, ResultStatus, make_envelope
from grand_lyon_mcp.domain.geo import normalize_name
from grand_lyon_mcp.domain.protocols import AccessibilityProvider, WheelchairProvider
from grand_lyon_mcp.infrastructure.time import now_paris
from grand_lyon_mcp.services.place_service import PlaceService


class AccessibilityService:
    def __init__(
        self,
        *,
        places: PlaceService,
        provider: AccessibilityProvider | None = None,
        gtfs: WheelchairProvider | None = None,
    ) -> None:
        self._places = places
        self._provider = provider
        self._gtfs = gtfs

    async def check(
        self,
        *,
        origin: PlaceRef | None = None,
        destination: PlaceRef | None = None,
        stops: list[PlaceRef] | None = None,
        needs: list[str] | None = None,
    ) -> Envelope:
        generated = now_paris()
        unknowns: list[str] = []
        segments: list[dict[str, Any]] = []
        incidents: list[dict[str, Any]] = []

        refs = list(stops or [])
        if origin:
            refs.append(origin)
        if destination:
            refs.append(destination)

        if self._provider is None and self._gtfs is None:
            for ref in refs:
                label = ref.query or ref.place_id or ref.profile_place or "stop"
                segments.append(
                    {
                        "name": label,
                        "status": AccessibilityStatus.UNKNOWN.value,
                        "notes": "Source accessibilité non configurée",
                        "unknowns": ["no_provider"],
                        "evidence": [],
                    }
                )
                unknowns.append(str(label))
            return make_envelope(
                status=ResultStatus.PARTIAL,
                generated_at=generated,
                summary="Accessibilité inconnue (source absente).",
                data={
                    "overall_status": AccessibilityStatus.UNKNOWN.value,
                    "segments": segments,
                    "incidents": [],
                    "unknowns": unknowns,
                },
                degraded=True,
            )

        incidents_list = []
        if self._provider is not None:
            incidents_list = await self._provider.get_incidents()
        incidents = [i.model_dump(mode="json") for i in incidents_list]

        statuses: list[AccessibilityStatus] = []
        for ref in refs:
            env = await self._places.resolve_place(place=ref, limit=1)
            cands = env.data.get("candidates") or []
            name = cands[0]["name"] if cands else (ref.query or "unknown")
            stop_id = cands[0]["id"] if cands else ""
            lat = cands[0].get("latitude") if cands else None
            lon = cands[0].get("longitude") if cands else None

            evidence: list[dict[str, Any]] = []
            status = AccessibilityStatus.UNKNOWN
            notes_parts: list[str] = []
            seg_unknowns: list[str] = []

            # Layer 1: live accessibility incidents (highest confidence for inaccessible)
            matched_incidents = _match_incidents(
                incidents_list, name=name, stop_id=stop_id, lat=lat, lon=lon
            )
            for inc in matched_incidents:
                evidence.append(
                    {
                        "source": "tcl_accessibility_alerts",
                        "fact": "incident",
                        "confidence": "high",
                        "detail": inc.description or inc.location,
                        "status": AccessibilityStatus.INACCESSIBLE.value,
                    }
                )
            if matched_incidents:
                status = AccessibilityStatus.INACCESSIBLE
                notes_parts.append(
                    f"{len(matched_incidents)} incident(s) accessibilité déclaré(s)."
                )

            # Layer 2: provider check_stop (may be unknown)
            if self._provider is not None and status == AccessibilityStatus.UNKNOWN:
                raw = await self._provider.check_stop(stop_id or name)
                try:
                    st = AccessibilityStatus(raw)
                except ValueError:
                    st = AccessibilityStatus.UNKNOWN
                if st == AccessibilityStatus.INACCESSIBLE:
                    status = st
                    evidence.append(
                        {
                            "source": "tcl_accessibility_alerts",
                            "fact": "check_stop",
                            "confidence": "high",
                            "detail": raw,
                            "status": st.value,
                        }
                    )
                    notes_parts.append("Incident détecté via matching arrêt.")
                elif st != AccessibilityStatus.UNKNOWN:
                    # Never promote to accessible without positive structural evidence;
                    # treat non-inaccessible live signals as partial at best.
                    if st == AccessibilityStatus.ACCESSIBLE:
                        evidence.append(
                            {
                                "source": "provider",
                                "fact": "check_stop_accessible_claim",
                                "confidence": "low",
                                "detail": "claim ignored without structural proof",
                                "status": AccessibilityStatus.UNKNOWN.value,
                            }
                        )
                    else:
                        status = st
                        evidence.append(
                            {
                                "source": "provider",
                                "fact": "check_stop",
                                "confidence": "medium",
                                "detail": raw,
                                "status": st.value,
                            }
                        )

            # Layer 3: GTFS wheelchair_boarding — declarative only, never silent accessible
            wh = await _gtfs_wheelchair(self._gtfs, stop_id)
            if wh is not None:
                # GTFS: 0=no info, 1=some accessible, 2=not accessible
                if wh == 2:
                    if status != AccessibilityStatus.INACCESSIBLE:
                        status = AccessibilityStatus.INACCESSIBLE
                    evidence.append(
                        {
                            "source": "gtfs_wheelchair",
                            "fact": "wheelchair_boarding=2",
                            "confidence": "medium",
                            "detail": "GTFS déclare non accessible (déclaratif)",
                            "status": AccessibilityStatus.INACCESSIBLE.value,
                        }
                    )
                    notes_parts.append("GTFS : boarding fauteuil non prévu (déclaratif).")
                elif wh == 1:
                    evidence.append(
                        {
                            "source": "gtfs_wheelchair",
                            "fact": "wheelchair_boarding=1",
                            "confidence": "low",
                            "detail": (
                                "GTFS déclare un accès possible (déclaratif, non vérifié live)"
                            ),
                            "status": AccessibilityStatus.PARTIALLY_ACCESSIBLE.value,
                        }
                    )
                    # Do NOT set overall to ACCESSIBLE from GTFS alone
                    if status == AccessibilityStatus.UNKNOWN:
                        status = AccessibilityStatus.PARTIALLY_ACCESSIBLE
                        notes_parts.append(
                            "GTFS : accessibilité déclarative partielle — non confirmée live."
                        )
                    else:
                        notes_parts.append("GTFS wheelchair_boarding=1 (déclaratif).")
                else:
                    evidence.append(
                        {
                            "source": "gtfs_wheelchair",
                            "fact": "wheelchair_boarding=0",
                            "confidence": "low",
                            "detail": "pas d'info GTFS",
                            "status": AccessibilityStatus.UNKNOWN.value,
                        }
                    )

            if status == AccessibilityStatus.UNKNOWN:
                seg_unknowns.append("no_declared_data")
                unknowns.append(name)
                if not notes_parts:
                    notes_parts.append(
                        "Aucun incident déclaré ; accessibilité structurelle inconnue."
                    )

            statuses.append(status)
            segments.append(
                {
                    "name": name,
                    "status": status.value,
                    "notes": " ".join(notes_parts),
                    "unknowns": seg_unknowns,
                    "evidence": evidence,
                }
            )

        overall = _aggregate(statuses)
        summary = _summary_fr(overall, statuses, incidents)
        return make_envelope(
            status=ResultStatus.OK
            if overall != AccessibilityStatus.UNKNOWN
            else ResultStatus.PARTIAL,
            generated_at=generated,
            summary=summary,
            data={
                "overall_status": overall.value,
                "segments": segments,
                "incidents": incidents,
                "unknowns": unknowns,
            },
            degraded=overall == AccessibilityStatus.UNKNOWN,
        )


def _match_incidents(
    incidents: list[Any],
    *,
    name: str,
    stop_id: str,
    lat: float | None,
    lon: float | None,
) -> list[Any]:
    """Match incidents by normalized name / stop id (coords soft if present)."""
    n_name = normalize_name(name) if name else ""
    n_sid = (stop_id or "").lower()
    matched = []
    for inc in incidents:
        loc = str(getattr(inc, "location", "") or "")
        n_loc = normalize_name(loc)
        if n_name and (n_name in n_loc or n_loc in n_name):
            matched.append(inc)
            continue
        if n_sid and (n_sid in loc.lower() or loc.lower() in n_sid):
            matched.append(inc)
            continue
    return matched


async def _gtfs_wheelchair(gtfs: WheelchairProvider | None, stop_id: str) -> int | None:
    if gtfs is None or not stop_id:
        return None
    try:
        raw_wh = await gtfs.get_wheelchair_boarding(stop_id)
        return int(raw_wh) if raw_wh is not None else None
    except Exception:
        return None


def _summary_fr(
    overall: AccessibilityStatus,
    statuses: list[AccessibilityStatus],
    incidents: list[dict[str, Any]],
) -> str:
    if overall == AccessibilityStatus.INACCESSIBLE:
        return "Accessibilité : inaccessible (incident ou déclaration négative)."
    if overall == AccessibilityStatus.UNKNOWN:
        if not incidents:
            return "Aucun incident déclaré ; accessibilité structurelle inconnue."
        return "Accessibilité inconnue (données insuffisantes)."
    if overall == AccessibilityStatus.PARTIALLY_ACCESSIBLE:
        return "Accessibilité partielle (preuves déclaratives ou mixtes)."
    # ACCESSIBLE only if aggregation says so with real evidence
    return f"Accessibilité : {overall.value}."


def _aggregate(statuses: list[AccessibilityStatus]) -> AccessibilityStatus:
    if not statuses:
        return AccessibilityStatus.UNKNOWN
    if any(s == AccessibilityStatus.INACCESSIBLE for s in statuses):
        return AccessibilityStatus.INACCESSIBLE
    if any(s == AccessibilityStatus.UNKNOWN for s in statuses):
        if all(s == AccessibilityStatus.UNKNOWN for s in statuses):
            return AccessibilityStatus.UNKNOWN
        return AccessibilityStatus.PARTIALLY_ACCESSIBLE
    if any(s == AccessibilityStatus.PARTIALLY_ACCESSIBLE for s in statuses):
        return AccessibilityStatus.PARTIALLY_ACCESSIBLE
    return AccessibilityStatus.ACCESSIBLE
