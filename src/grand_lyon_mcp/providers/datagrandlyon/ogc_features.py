"""OGC API Features client."""

from __future__ import annotations

from typing import Any

from grand_lyon_mcp.providers.datagrandlyon.client import DataGrandLyonClient

OGC_BASE = "https://data.grandlyon.com/geoserver/ogc/features/v1"


async def list_collections(client: DataGrandLyonClient) -> list[dict[str, Any]]:
    data = await client.get_json(f"{OGC_BASE}/collections")
    if isinstance(data, dict) and isinstance(data.get("collections"), list):
        return [x for x in data["collections"] if isinstance(x, dict)]
    return []


async def fetch_items(
    client: DataGrandLyonClient,
    collection: str,
    *,
    limit: int = 50,
    bbox: tuple[float, float, float, float] | None = None,
) -> list[dict[str, Any]]:
    params: dict[str, Any] = {"limit": max(1, min(limit, 200)), "f": "application/geo+json"}
    if bbox:
        params["bbox"] = ",".join(str(x) for x in bbox)
    data = await client.get_json(f"{OGC_BASE}/collections/{collection}/items", params=params)
    if isinstance(data, dict) and isinstance(data.get("features"), list):
        return [x for x in data["features"] if isinstance(x, dict)]
    return []
