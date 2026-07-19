"""Conditional GTFS download."""

from __future__ import annotations

from pathlib import Path
from typing import Any

from grand_lyon_mcp.infrastructure.http import HttpClient

DEFAULT_GTFS_URL = (
    "https://download.data.grandlyon.com/files/rdata/tcl_sytral.tcltheorique/GTFS_TCL.ZIP"
)


async def download_gtfs(
    http: HttpClient,
    dest: Path,
    *,
    url: str = DEFAULT_GTFS_URL,
    etag: str | None = None,
    auth: Any | None = None,
) -> tuple[Path | None, str | None, bool]:
    """
    Download GTFS if changed.
    Returns (path, new_etag, downloaded).
    path is None if 304 / not modified.
    """
    headers: dict[str, str] = {}
    if etag:
        headers["If-None-Match"] = etag
    response = await http.get(url, headers=headers, auth=auth)
    if response.status_code == 304:
        return None, etag, False
    response.raise_for_status()
    dest.parent.mkdir(parents=True, exist_ok=True)
    dest.write_bytes(response.content)
    new_etag = response.headers.get("ETag")
    return dest, new_etag, True
