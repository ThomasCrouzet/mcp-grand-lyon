"""Catalog download and inspection."""

from __future__ import annotations

from typing import Any

from grand_lyon_mcp.providers.datagrandlyon.client import DataGrandLyonClient

CATALOG_URLS = {
    "rdata": "https://data.grandlyon.com/fr/datapusher/ws/rdata/all.json",
    "grandlyon": "https://data.grandlyon.com/fr/datapusher/ws/grandlyon/all.json",
}


async def fetch_catalog(
    client: DataGrandLyonClient, service: str = "rdata"
) -> list[dict[str, Any]]:
    url = CATALOG_URLS.get(service)
    if not url:
        raise ValueError(f"Unknown catalog service: {service}")
    data = await client.get_json(url)
    if isinstance(data, list):
        return [x for x in data if isinstance(x, dict)]
    if isinstance(data, dict):
        for key in ("results", "values", "features", "records", "datasets"):
            if key in data and isinstance(data[key], list):
                return [x for x in data[key] if isinstance(x, dict)]
    return []


def search_catalog(
    entries: list[dict[str, Any]],
    keywords: list[str],
    *,
    exact_table_hints: list[str] | None = None,
) -> list[dict[str, Any]]:
    """Score catalog entries by keywords / exact table hints."""
    hints = {h.lower() for h in (exact_table_hints or [])}
    scored: list[tuple[float, dict[str, Any]]] = []
    for entry in entries:
        name = str(
            entry.get("name") or entry.get("id") or entry.get("table") or entry.get("layer") or ""
        ).lower()
        title = str(entry.get("title") or entry.get("label") or "").lower()
        blob = f"{name} {title}"
        score = 0.0
        for hint in hints:
            if hint in name or hint == name:
                score += 100.0
        for kw in keywords:
            if kw.lower() in blob:
                score += 10.0
        if score > 0:
            scored.append((score, entry))
    scored.sort(key=lambda x: x[0], reverse=True)
    return [e for _, e in scored]
