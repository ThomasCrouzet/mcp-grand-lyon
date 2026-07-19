"""Place resolution service."""

from __future__ import annotations

from rapidfuzz import fuzz

from grand_lyon_mcp.domain.common import (
    Envelope,
    PlaceRef,
    ResultStatus,
    WarningCode,
    WarningItem,
    make_envelope,
)
from grand_lyon_mcp.domain.geo import Point, normalize_name
from grand_lyon_mcp.domain.places import PlaceCandidate
from grand_lyon_mcp.domain.protocols import PlaceProvider, TransitStaticProvider
from grand_lyon_mcp.infrastructure.time import now_paris
from grand_lyon_mcp.storage.entity_repository import EntityRepository
from grand_lyon_mcp.storage.profile_repository import ProfileRepository


class PlaceService:
    def __init__(
        self,
        *,
        entities: EntityRepository,
        profiles: ProfileRepository,
        photon: PlaceProvider | None = None,
        gtfs: TransitStaticProvider | None = None,
        fuzzy_threshold: int = 72,
    ) -> None:
        self._entities = entities
        self._profiles = profiles
        self._photon = photon
        self._gtfs = gtfs
        self._fuzzy_threshold = fuzzy_threshold

    async def resolve_point(
        self, ref: PlaceRef
    ) -> tuple[Point | None, list[PlaceCandidate], ResultStatus]:
        if ref.latitude is not None and ref.longitude is not None:
            return Point(ref.latitude, ref.longitude), [], ResultStatus.OK
        if ref.profile_place:
            place = await self._profiles.get_place(ref.profile_place)
            if place and place.get("latitude") is not None and place.get("longitude") is not None:
                return (
                    Point(float(place["latitude"]), float(place["longitude"])),
                    [],
                    ResultStatus.OK,
                )
            return None, [], ResultStatus.NOT_FOUND
        if ref.place_id:
            ent = await self._entities.get(ref.place_id)
            if ent:
                return Point(ent.latitude, ent.longitude), [ent], ResultStatus.OK
            if self._gtfs and ref.place_id.startswith("gtfs:"):
                from grand_lyon_mcp.providers.gtfs.repository import GtfsRepository

                if isinstance(self._gtfs, GtfsRepository):
                    s = await self._gtfs.get_stop(ref.place_id)
                    if s:
                        return Point(s.latitude, s.longitude), [s], ResultStatus.OK
            return None, [], ResultStatus.NOT_FOUND
        # query path handled by resolve_place
        return None, [], ResultStatus.INVALID_REQUEST

    async def resolve_place(
        self,
        *,
        query: str | None = None,
        place: PlaceRef | None = None,
        near: PlaceRef | None = None,
        types: list[str] | None = None,
        limit: int = 5,
    ) -> Envelope:
        generated = now_paris()
        warnings: list[WarningItem] = []
        near_point: Point | None = None
        if near is not None:
            near_point, _, st = await self.resolve_point(near)
            if st != ResultStatus.OK:
                warnings.append(
                    WarningItem(
                        code=WarningCode.PLACE_NOT_FOUND.value,
                        message="near place could not be resolved",
                        retryable=False,
                    )
                )

        if place is not None and place.query is None:
            point, cands, status = await self.resolve_point(place)
            if status == ResultStatus.OK and point is not None:
                if not cands:
                    cands = [
                        PlaceCandidate(
                            id=place.place_id or place.profile_place or "coord",
                            name=place.profile_place or "Point",
                            label=place.profile_place or f"{point.latitude},{point.longitude}",
                            type="point_of_interest",
                            latitude=point.latitude,
                            longitude=point.longitude,
                            confidence=1.0,
                        )
                    ]
                return make_envelope(
                    status=ResultStatus.OK,
                    generated_at=generated,
                    summary=f"{len(cands)} lieu(x) résolu(s).",
                    data={"candidates": [c.model_dump(mode="json") for c in cands[:limit]]},
                    warnings=warnings,
                )
            if status == ResultStatus.NOT_FOUND:
                return make_envelope(
                    status=ResultStatus.NOT_FOUND,
                    generated_at=generated,
                    summary="Lieu introuvable.",
                    data={"candidates": []},
                    warnings=warnings,
                )

        q = query or (place.query if place else None)
        if not q:
            return make_envelope(
                status=ResultStatus.INVALID_REQUEST,
                generated_at=generated,
                summary="Requête de lieu invalide.",
                data={"candidates": []},
            )

        candidates: list[PlaceCandidate] = []

        # 1) FTS local
        candidates.extend(await self._entities.search_fts(q, limit=limit * 2))

        # 2) GTFS stops
        if self._gtfs:
            try:
                candidates.extend(await self._gtfs.search_stops(q, limit=limit))
            except Exception:
                warnings.append(
                    WarningItem(
                        code=WarningCode.SOURCE_UNAVAILABLE.value,
                        source="gtfs",
                        message="Recherche GTFS indisponible.",
                        retryable=True,
                    )
                )

        # 3) fuzzy on collected names
        if len(candidates) < limit:
            # already have FTS; fuzzy re-rank
            pass

        # 4) Photon if weak / non-exact local matches (GTFS often floods partial stop names)
        nq_pre = normalize_name(q)
        exact_local = [c for c in candidates if normalize_name(c.name) == nq_pre]
        # Always ask Photon when no exact local hit — GTFS partial matches are often noise
        # (e.g. many "… Tête d'Or …" stops vs the actual park POI).
        if self._photon is not None and not exact_local:
            try:
                remote = await self._photon.search(q, near=near_point, limit=limit)
                if types:
                    remote = [
                        c
                        for c in remote
                        if str(c.type) in types
                        or (hasattr(c.type, "value") and c.type.value in types)
                    ]
                candidates.extend(remote)
            except Exception:
                warnings.append(
                    WarningItem(
                        code=WarningCode.SOURCE_UNAVAILABLE.value,
                        source="photon",
                        message="Géocodage Photon indisponible.",
                        retryable=True,
                    )
                )

        # dedupe by id
        seen: set[str] = set()
        unique: list[PlaceCandidate] = []
        for c in candidates:
            if c.id in seen:
                continue
            seen.add(c.id)
            if (
                types
                and str(c.type) not in types
                and (c.type.value if hasattr(c.type, "value") else None) not in types
            ):
                continue
            score = fuzz.token_set_ratio(normalize_name(q), normalize_name(c.name)) / 100.0
            unique.append(c.model_copy(update={"confidence": max(c.confidence, score)}))
        unique.sort(key=lambda c: c.confidence, reverse=True)

        # Prefer exact normalized name matches (e.g. "Bellecour" vs "Bellecour A. Poncet")
        nq = normalize_name(q)
        exact = [c for c in unique if normalize_name(c.name) == nq]
        if exact:
            rest = [c for c in unique if normalize_name(c.name) != nq]
            unique = [
                c.model_copy(update={"confidence": max(c.confidence, 0.99)}) for c in exact
            ] + rest

        unique = unique[:limit]

        if not unique:
            return make_envelope(
                status=ResultStatus.NOT_FOUND,
                generated_at=generated,
                summary=f"Aucun lieu trouvé pour « {q} ».",
                data={"candidates": []},
                warnings=warnings,
                degraded=bool(warnings),
            )

        # ambiguity only when no clear exact-name winner and top scores are tied
        status = ResultStatus.OK
        if exact:
            # Multiple platforms with the same official name → ok (list them)
            status = ResultStatus.OK
        elif len(unique) > 1 and abs(unique[0].confidence - unique[1].confidence) < 0.05:
            status = ResultStatus.AMBIGUOUS
            warnings.append(
                WarningItem(
                    code=WarningCode.AMBIGUOUS_PLACE.value,
                    message="Plusieurs lieux plausibles.",
                    retryable=False,
                )
            )

        return make_envelope(
            status=status,
            generated_at=generated,
            summary=f"{len(unique)} candidat(s) pour « {q} ».",
            data={"candidates": [c.model_dump(mode="json") for c in unique]},
            warnings=warnings,
            degraded=bool(warnings),
        )
