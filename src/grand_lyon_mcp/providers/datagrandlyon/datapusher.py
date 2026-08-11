"""DataPusher query adapter (internal only, never exposed as MCP tool)."""

from __future__ import annotations

from typing import Any
from urllib.parse import quote

from grand_lyon_mcp.providers.datagrandlyon.client import DataGrandLyonClient

BASE = "https://data.grandlyon.com/fr/datapusher/ws"


def table_url(service: str, schema_table: str) -> str:
    # schema_table like "jcd_jcdecaux.jcdvelov"
    return f"{BASE}/{service}/{quote(schema_table, safe='.')}/all.json"


async def query_table(
    client: DataGrandLyonClient,
    *,
    service: str,
    schema_table: str,
    maxfeatures: int = 50,
    start: int = 0,
    filters: dict[str, str] | None = None,
) -> list[dict[str, Any]]:
    """Bounded query, filters must come from internal allowlists only."""
    params: dict[str, Any] = {
        "compact": "false",
        "maxfeatures": max(1, min(maxfeatures, 200)),
    }
    # Some DataPusher tables return HTTP 500 when start=0 is explicit;
    # only send start for non-zero offsets.
    if start > 0:
        params["start"] = start
    if filters:
        params.update(filters)
    url = table_url(service, schema_table)
    data = await client.get_json(url, params=params)
    return extract_records(data)


def extract_records(data: Any) -> list[dict[str, Any]]:
    if isinstance(data, list):
        return [x for x in data if isinstance(x, dict)]
    if not isinstance(data, dict):
        return []
    for key in ("values", "results", "features", "records"):
        if key in data and isinstance(data[key], list):
            items = data[key]
            out: list[dict[str, Any]] = []
            for item in items:
                if isinstance(item, dict):
                    if "properties" in item and isinstance(item["properties"], dict):
                        merged = dict(item["properties"])
                        if "geometry" in item:
                            merged["_geometry"] = item["geometry"]
                        out.append(merged)
                    else:
                        out.append(item)
            return out
    return []
