"""Trip options comparison with deterministic scoring."""

from __future__ import annotations

from datetime import datetime
from typing import Any

from grand_lyon_mcp.domain.common import (
    Envelope,
    PlaceRef,
    ResultStatus,
    WarningCode,
    WarningItem,
    make_envelope,
)
from grand_lyon_mcp.domain.geo import Point, haversine_m
from grand_lyon_mcp.domain.journeys import TravelMode, TripOption
from grand_lyon_mcp.domain.protocols import JourneyPlanner, ParkingProvider, VelovProvider
from grand_lyon_mcp.infrastructure.time import now_paris
from grand_lyon_mcp.services.place_service import PlaceService
from grand_lyon_mcp.settings import JourneyScoringWeights


class JourneyService:
    def __init__(
        self,
        *,
        places: PlaceService,
        planner: JourneyPlanner | None = None,
        velov: VelovProvider | None = None,
        parking: ParkingProvider | None = None,
        weights: JourneyScoringWeights | None = None,
    ) -> None:
        self._places = places
        self._planner = planner
        self._velov = velov
        self._parking = parking
        self._weights = weights or JourneyScoringWeights()

    async def options(
        self,
        *,
        origin: PlaceRef,
        destination: PlaceRef,
        departure_at: datetime | None = None,
        arrival_before: datetime | None = None,
        modes: list[str] | None = None,
        preferences: dict[str, Any] | None = None,
    ) -> Envelope:
        generated = now_paris()
        departure_at = departure_at or generated
        modes = modes or ["tcl", "velov", "park_and_ride", "car", "walk"]
        preferences = preferences or {}
        warnings: list[WarningItem] = []

        o_env = await self._places.resolve_place(place=origin, limit=1)
        d_env = await self._places.resolve_place(place=destination, limit=1)
        o_cands = o_env.data.get("candidates") or []
        d_cands = d_env.data.get("candidates") or []

        def _point(ref: PlaceRef, cands: list[dict[str, Any]]) -> Point | None:
            if cands:
                return Point(cands[0]["latitude"], cands[0]["longitude"])
            if ref.latitude is not None and ref.longitude is not None:
                return Point(ref.latitude, ref.longitude)
            return None

        o_pt = _point(origin, o_cands)
        d_pt = _point(destination, d_cands)
        if o_pt is None or d_pt is None:
            return make_envelope(
                status=ResultStatus.NOT_FOUND,
                generated_at=generated,
                summary="Origine ou destination introuvable.",
                data={"recommended_mode": None, "options": []},
            )

        trip_options: list[TripOption] = []
        distance_m = haversine_m(o_pt, d_pt)

        # walk
        if "walk" in modes:
            walk_s = int(distance_m / 1.4)  # ~5 km/h
            trip_options.append(
                TripOption(
                    mode=TravelMode.WALK,
                    score=self._score(
                        duration_s=walk_s,
                        reliability=0.95,
                        disruptions=0,
                        walking_m=int(distance_m),
                    ),
                    estimated_duration_seconds=walk_s,
                    walking_distance_m=int(distance_m),
                    summary=f"Marche ~{int(distance_m)} m",
                    realtime=False,
                    sources=["haversine"],
                )
            )

        # car rough estimate
        if "car" in modes:
            car_s = int(distance_m / 8.0)  # ~30 km/h urban
            trip_options.append(
                TripOption(
                    mode=TravelMode.CAR,
                    score=self._score(
                        duration_s=car_s, reliability=0.7, disruptions=0.2, walking_m=0
                    ),
                    estimated_duration_seconds=car_s,
                    summary=f"Voiture estimée ({int(distance_m)} m à vol d'oiseau)",
                    realtime=False,
                    sources=["estimate"],
                )
            )

        # velov
        if "velov" in modes and self._velov is not None:
            try:
                min_bikes = int(preferences.get("minimum_velov_bikes") or 1)
                min_docks = int(preferences.get("minimum_velov_docks") or 1)
                max_walk = float(preferences.get("max_walking_m") or 800)
                near_o = await self._velov.stations_near(o_pt, radius_m=max_walk, limit=5)
                near_d = await self._velov.stations_near(d_pt, radius_m=max_walk, limit=5)
                near_o = [s for s in near_o if s.bikes_available >= min_bikes]
                near_d = [s for s in near_d if s.docks_available >= min_docks]
                if near_o and near_d:
                    bike_dist = haversine_m(
                        Point(near_o[0].latitude, near_o[0].longitude),
                        Point(near_d[0].latitude, near_d[0].longitude),
                    )
                    bike_s = int(bike_dist / 4.0)  # ~15 km/h
                    walk = int((near_o[0].distance_m or 0) + (near_d[0].distance_m or 0))
                    trip_options.append(
                        TripOption(
                            mode=TravelMode.VELOV,
                            score=self._score(
                                duration_s=bike_s + walk,
                                reliability=0.75,
                                disruptions=0,
                                walking_m=walk,
                                availability=0.8,
                            ),
                            estimated_duration_seconds=bike_s + walk,
                            walking_distance_m=walk,
                            availability=(
                                f"{near_o[0].bikes_available} bikes / "
                                f"{near_d[0].docks_available} docks"
                            ),
                            summary=f"Vélo'v {near_o[0].name} → {near_d[0].name}",
                            realtime=True,
                            sources=["velov_realtime"],
                        )
                    )
                else:
                    warnings.append(
                        WarningItem(
                            code=WarningCode.PARTIAL_RESULT.value,
                            source="velov_realtime",
                            message="Pas de couple stations Vélo'v satisfaisant les seuils.",
                            retryable=False,
                        )
                    )
            except Exception:
                warnings.append(
                    WarningItem(
                        code=WarningCode.SOURCE_UNAVAILABLE.value,
                        source="velov_realtime",
                        message="Vélo'v indisponible pour le trajet.",
                        retryable=True,
                    )
                )

        # park and ride
        if "park_and_ride" in modes and self._parking is not None:
            try:
                from grand_lyon_mcp.domain.parking import ParkingType

                pr = await self._parking.options_near(
                    d_pt, types=[ParkingType.PARK_AND_RIDE], radius_m=3000, limit=3
                )
                if pr:
                    trip_options.append(
                        TripOption(
                            mode=TravelMode.PARK_AND_RIDE,
                            score=self._score(
                                duration_s=int(distance_m / 9.0),
                                reliability=0.7,
                                disruptions=0.1,
                                walking_m=int(pr[0].distance_m or 200),
                                availability=0.7 if (pr[0].available_spaces or 0) > 5 else 0.4,
                            ),
                            estimated_duration_seconds=int(distance_m / 9.0),
                            summary=f"P+R {pr[0].name}",
                            realtime=pr[0].realtime,
                            sources=["parking"],
                        )
                    )
            except Exception:
                warnings.append(
                    WarningItem(
                        code=WarningCode.SOURCE_UNAVAILABLE.value,
                        source="parking",
                        message="P+R indisponible.",
                        retryable=True,
                    )
                )

        # TCL via optional planner (Transitous behind flag; default off if unvalidated)
        if "tcl" in modes:
            planned: list[TripOption] = []
            if self._planner is not None:
                try:
                    planned = await self._planner.plan(
                        o_pt,
                        d_pt,
                        departure_at=departure_at,
                        modes=[TravelMode.TCL],
                        preferences=preferences,
                    )
                except Exception:
                    planned = []
            if planned:
                # Ensure planner options that lack duration are not preferred unfairly
                for opt in planned:
                    if opt.estimated_duration_seconds is None:
                        trip_options.append(
                            opt.model_copy(
                                update={
                                    "score": self._score(
                                        duration_s=None,
                                        reliability=0.75,
                                        disruptions=0.1,
                                        walking_m=opt.walking_distance_m or 400,
                                    ),
                                    "summary": (
                                        opt.summary or "TCL (durée non disponible — non calculée)"
                                    ),
                                }
                            )
                        )
                    else:
                        trip_options.append(opt)
            else:
                # Lightweight honest TCL: no invented door-to-door duration
                warnings.append(
                    WarningItem(
                        code=WarningCode.ROUTING_UNAVAILABLE.value,
                        source="journey_planner",
                        message="Aucune durée porte-à-porte TCL fiable (routeur indisponible).",
                        retryable=True,
                    )
                )
                trip_options.append(
                    TripOption(
                        mode=TravelMode.TCL,
                        # Penalize missing duration vs live Vélo'v / calculated modes
                        score=self._score(
                            duration_s=None,
                            reliability=0.75,
                            disruptions=0.1,
                            walking_m=400,
                            availability=0.4,
                        ),
                        estimated_duration_seconds=None,  # never invent
                        summary="TCL (durée non disponible — routeur indisponible)",
                        realtime=False,
                        sources=[],
                    )
                )

        trip_options.sort(key=lambda t: t.score, reverse=True)
        recommended = trip_options[0].mode.value if trip_options else None
        status = ResultStatus.PARTIAL if warnings else ResultStatus.OK
        return make_envelope(
            status=status,
            generated_at=generated,
            summary=f"Option recommandée : {recommended}." if recommended else "Aucune option.",
            data={
                "recommended_mode": recommended,
                "options": [t.model_dump(mode="json") for t in trip_options],
            },
            warnings=warnings,
            degraded=bool(warnings),
        )

    def _score(
        self,
        *,
        duration_s: int | None,
        reliability: float,
        disruptions: float,
        walking_m: int,
        availability: float = 0.5,
        user_preference: float = 0.5,
    ) -> float:
        w = self._weights
        # Missing duration: strong penalty so live Vélo'v / calculated modes win fairly
        # normalize duration: 1 at 0s, ~0 at 90 min when known
        duration_score = 0.15 if duration_s is None else max(0.0, 1.0 - (duration_s / 5400.0))
        walk_score = max(0.0, 1.0 - (walking_m / 2000.0))
        disruption_score = max(0.0, 1.0 - disruptions)
        total = (
            w.duration * duration_score
            + w.reliability * reliability
            + w.disruptions * disruption_score
            + w.walking * walk_score
            + w.availability * availability
            + w.user_preference * user_preference
        )
        return min(1.0, max(0.0, total))
