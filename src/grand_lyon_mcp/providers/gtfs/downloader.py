"""Conditional GTFS download."""

from __future__ import annotations

import os
import tempfile
import zipfile
from pathlib import Path
from typing import Any

from grand_lyon_mcp.infrastructure.http import HttpClient, bounded_chunks
from grand_lyon_mcp.providers.gtfs.importer import MAX_ZIP_BYTES

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
    max_bytes: int = MAX_ZIP_BYTES,
) -> tuple[Path | None, str | None, bool]:
    """
    Download GTFS if changed.
    Returns (path, new_etag, downloaded).
    path is None if 304 / not modified.
    """
    headers: dict[str, str] = {}
    if etag:
        headers["If-None-Match"] = etag
    if not 0 < max_bytes <= MAX_ZIP_BYTES:
        raise ValueError("GTFS byte limit must be positive and cannot exceed the importer limit")
    temporary: Path | None = None
    try:
        async with http.stream("GET", url, headers=headers, auth=auth) as response:
            if response.status_code == 304:
                return None, etag, False
            response.raise_for_status()
            if response.status_code != 200:
                raise ValueError("GTFS download requires a complete HTTP 200 response")
            dest.parent.mkdir(parents=True, exist_ok=True)
            with tempfile.NamedTemporaryFile(
                dir=dest.parent, prefix=f".{dest.name}.", suffix=".partial", delete=False
            ) as output:
                temporary = Path(output.name)
                async for chunk in bounded_chunks(response, max_bytes):
                    output.write(chunk)
                output.flush()
                os.fsync(output.fileno())
            with zipfile.ZipFile(temporary) as archive:
                if not archive.infolist():
                    raise ValueError("GTFS archive is empty")
            new_etag = response.headers.get("ETag")
        os.replace(temporary, dest)
        temporary = None
        return dest, new_etag, True
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)
