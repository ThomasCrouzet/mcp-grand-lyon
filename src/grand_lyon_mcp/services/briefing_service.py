"""Personal briefing aggregation."""

from __future__ import annotations

from datetime import datetime

from grand_lyon_mcp.domain.briefings import BriefingChange
from grand_lyon_mcp.domain.common import (
    Envelope,
    PlaceRef,
    ResultStatus,
    WarningItem,
    make_envelope,
)
from grand_lyon_mcp.infrastructure.time import now_paris
from grand_lyon_mcp.services.mobility_status_service import MobilityStatusService
from grand_lyon_mcp.services.place_service import PlaceService
from grand_lyon_mcp.services.transit_service import TransitService
from grand_lyon_mcp.services.velov_service import VelovService
from grand_lyon_mcp.storage.profile_repository import ProfileRepository


class BriefingService:
    def __init__(
        self,
        *,
        profiles: ProfileRepository,
        places: PlaceService,
        transit: TransitService | None = None,
        mobility: MobilityStatusService | None = None,
        velov: VelovService | None = None,
    ) -> None:
        self._profiles = profiles
        self._places = places
        self._transit = transit
        self._mobility = mobility
        self._velov = velov

    async def personal_briefing(
        self,
        *,
        profile: str,
        at: datetime | None = None,
        compare_with_previous: bool = True,
    ) -> Envelope:
        generated = now_paris()
        at = at or generated
        cfg = await self._profiles.get_briefing_profile(profile)
        if cfg is None:
            # try load defaults from empty: not found
            return make_envelope(
                status=ResultStatus.NOT_FOUND,
                generated_at=generated,
                summary=f"Profil de briefing « {profile} » introuvable.",
                data={},
            )

        include = list(cfg.get("include") or ["transit", "velov", "traffic"])
        commute_key = cfg.get("commute")
        facts: dict[str, object] = {"profile": profile, "include": include}
        warnings: list[WarningItem] = []

        if commute_key:
            commute = await self._profiles.get_commute(str(commute_key))
            facts["commute"] = commute
            if commute and self._transit and "transit" in include:
                origin = PlaceRef(profile_place=str(commute.get("origin") or "home"))
                # use work-area stop query if available: resolve home and get departures near
                try:
                    home = await self._profiles.get_place(str(commute.get("origin") or "home"))
                    if home and home.get("latitude") is not None:
                        dep_env = await self._transit.next_departures(
                            stop=PlaceRef(
                                latitude=float(home["latitude"]),
                                longitude=float(home["longitude"]),
                            ),
                            limit=3,
                        )
                        # better: use a stop query from preferred lines later
                        facts["departures"] = dep_env.data
                        warnings.extend(dep_env.warnings)
                    else:
                        # try Bellecour as demo fallback only if no coords: actually skip
                        pass
                except Exception:
                    warnings.append(
                        WarningItem(
                            code="PARTIAL_RESULT",
                            source="transit",
                            message="Départs non inclus dans le briefing.",
                            retryable=True,
                        )
                    )

            if commute and self._velov and "velov" in include:
                origin = PlaceRef(profile_place=str(commute.get("origin") or "home"))
                try:
                    v_env = await self._velov.stations_near_ref(
                        origin,
                        minimum_bikes=int(commute.get("minimum_velov_bikes") or 0),
                        limit=3,
                    )
                    facts["velov"] = v_env.data
                    warnings.extend(v_env.warnings)
                except Exception:
                    warnings.append(
                        WarningItem(
                            code="PARTIAL_RESULT",
                            source="velov",
                            message="Disponibilité Vélo'v non incluse dans le briefing.",
                            retryable=True,
                        )
                    )

        if self._mobility and (
            "transit" in include or "traffic" in include or "accessibility" in include
        ):
            try:
                m_env = await self._mobility.status(
                    include=[
                        x
                        for x in include
                        if x in ("transit", "traffic", "accessibility", "roadworks")
                    ]
                    or ["transit"]
                )
                facts["mobility"] = m_env.data
                warnings.extend(m_env.warnings)
            except Exception:
                warnings.append(
                    WarningItem(
                        code="PARTIAL_RESULT",
                        source="mobility",
                        message="État mobilité/trafic non inclus dans le briefing.",
                        retryable=True,
                    )
                )

        changes: list[BriefingChange] = []
        if compare_with_previous:
            prev = await self._profiles.last_briefing_snapshot(profile)
            if prev:
                for key in ("departures", "velov", "mobility"):
                    if key in facts and key in prev and facts[key] != prev.get(key):
                        changes.append(
                            BriefingChange(key=key, previous="(previous)", current="(updated)")
                        )

        snapshot = {"facts": facts, "at": at.isoformat()}
        await self._profiles.save_briefing_snapshot(profile, snapshot)

        summary_parts = [f"Briefing {profile}"]
        if facts.get("mobility"):
            mobility = facts["mobility"]
            alerts: list[object] = []
            if isinstance(mobility, dict):
                raw = mobility.get("alerts") or []
                if isinstance(raw, list):
                    alerts = raw
            summary_parts.append(f"{len(alerts)} alerte(s)")
        summary = ": ".join(summary_parts) + "."

        return make_envelope(
            status=ResultStatus.PARTIAL if warnings else ResultStatus.OK,
            generated_at=generated,
            summary=summary,
            data={
                "profile": profile,
                "at": at.isoformat(),
                "summary": summary,
                "facts": facts,
                "changes": [c.model_dump(mode="json") for c in changes],
            },
            warnings=warnings,
            degraded=bool(warnings),
        )
