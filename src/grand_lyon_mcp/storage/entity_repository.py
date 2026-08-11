"""Entity index: FTS5 + RTree."""

from __future__ import annotations

import json
from typing import Any

import aiosqlite

from grand_lyon_mcp.domain.geo import Point, bounding_box, haversine_m, normalize_name
from grand_lyon_mcp.domain.places import EntityType, PlaceCandidate
from grand_lyon_mcp.infrastructure.time import now_utc


class EntityRepository:
    def __init__(self, conn: aiosqlite.Connection) -> None:
        self._conn = conn

    async def upsert(
        self,
        *,
        logical_id: str,
        source_id: str,
        provider_id: str,
        entity_type: str,
        name: str,
        latitude: float,
        longitude: float,
        properties: dict[str, Any] | None = None,
        aliases: list[str] | None = None,
    ) -> None:
        now = now_utc().isoformat()
        normalized = normalize_name(name)
        await self._conn.execute(
            """
            INSERT INTO entities (
                logical_id, source_id, provider_id, entity_type, name,
                normalized_name, latitude, longitude, properties_json, indexed_at
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            ON CONFLICT(logical_id) DO UPDATE SET
                source_id=excluded.source_id,
                provider_id=excluded.provider_id,
                entity_type=excluded.entity_type,
                name=excluded.name,
                normalized_name=excluded.normalized_name,
                latitude=excluded.latitude,
                longitude=excluded.longitude,
                properties_json=excluded.properties_json,
                indexed_at=excluded.indexed_at
            """,
            (
                logical_id,
                source_id,
                provider_id,
                entity_type,
                name,
                normalized,
                latitude,
                longitude,
                json.dumps(properties or {}, ensure_ascii=False),
                now,
            ),
        )
        # FTS
        await self._conn.execute("DELETE FROM entity_fts WHERE logical_id = ?", (logical_id,))
        alias_text = " ".join(aliases or [])
        await self._conn.execute(
            """
            INSERT INTO entity_fts (logical_id, name, aliases, normalized_name)
            VALUES (?, ?, ?, ?)
            """,
            (logical_id, name, alias_text, normalized),
        )
        # RTree (id as integer hash)
        rid = _stable_int_id(logical_id)
        await self._conn.execute("DELETE FROM entity_rtree WHERE id = ?", (rid,))
        await self._conn.execute(
            """
            INSERT INTO entity_rtree (id, minx, maxx, miny, maxy)
            VALUES (?, ?, ?, ?, ?)
            """,
            (rid, longitude, longitude, latitude, latitude),
        )
        await self._conn.execute(
            "INSERT OR REPLACE INTO entity_rtree_map (rtree_id, logical_id) VALUES (?, ?)",
            (rid, logical_id),
        )
        await self._conn.commit()

    async def get(self, logical_id: str) -> PlaceCandidate | None:
        cursor = await self._conn.execute(
            "SELECT * FROM entities WHERE logical_id = ?",
            (logical_id,),
        )
        row = await cursor.fetchone()
        if row is None:
            return None
        return _row_to_candidate(row)

    async def search_fts(self, query: str, *, limit: int = 10) -> list[PlaceCandidate]:
        # FTS5 MATCH: use prefix / plain tokens.
        # On retire les guillemets doubles de chaque token : sans cela une entrée
        # contenant `"` produirait une requête MATCH malformée (OperationalError) et
        # permettrait d'injecter la mini-syntaxe FTS5.
        tokens = [t.replace('"', "") for t in normalize_name(query).split()]
        tokens = [t for t in tokens if t]
        if not tokens:
            return []
        match = " ".join(f'"{t}"*' for t in tokens)
        cursor = await self._conn.execute(
            """
            SELECT e.* FROM entity_fts f
            JOIN entities e ON e.logical_id = f.logical_id
            WHERE entity_fts MATCH ?
            LIMIT ?
            """,
            (match, limit),
        )
        rows = await cursor.fetchall()
        if rows:
            return [_row_to_candidate(r) for r in rows]
        # fallback LIKE on normalized
        like = f"%{normalize_name(query).replace(' ', '%')}%"
        cursor = await self._conn.execute(
            """
            SELECT * FROM entities
            WHERE normalized_name LIKE ?
            LIMIT ?
            """,
            (like, limit),
        )
        rows = await cursor.fetchall()
        return [_row_to_candidate(r) for r in rows]

    async def search_near(
        self,
        point: Point,
        *,
        radius_m: float = 1000,
        entity_types: list[str] | None = None,
        limit: int = 20,
    ) -> list[PlaceCandidate]:
        min_lat, min_lon, max_lat, max_lon = bounding_box(point, radius_m)
        cursor = await self._conn.execute(
            """
            SELECT e.* FROM entity_rtree r
            JOIN entity_rtree_map m ON m.rtree_id = r.id
            JOIN entities e ON e.logical_id = m.logical_id
            WHERE r.minx <= ? AND r.maxx >= ? AND r.miny <= ? AND r.maxy >= ?
            """,
            (max_lon, min_lon, max_lat, min_lat),
        )
        rows = await cursor.fetchall()
        results: list[PlaceCandidate] = []
        for row in rows:
            if entity_types and row["entity_type"] not in entity_types:
                continue
            cand = _row_to_candidate(row)
            dist = haversine_m(point, Point(cand.latitude, cand.longitude))
            if dist <= radius_m:
                results.append(cand.model_copy(update={"distance_m": dist}))
        results.sort(key=lambda c: c.distance_m or 0.0)
        return results[:limit]


def _stable_int_id(logical_id: str) -> int:
    import hashlib

    h = hashlib.sha256(logical_id.encode()).hexdigest()[:15]
    return int(h, 16)


def _row_to_candidate(row: aiosqlite.Row) -> PlaceCandidate:
    etype: EntityType | str
    try:
        etype = EntityType(row["entity_type"])
    except ValueError:
        etype = row["entity_type"]
    return PlaceCandidate(
        id=row["logical_id"],
        name=row["name"],
        label=row["name"],
        type=etype,
        latitude=float(row["latitude"]),
        longitude=float(row["longitude"]),
        confidence=0.9,
    )
