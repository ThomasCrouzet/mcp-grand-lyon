"""Tests de non-régression du durcissement sécurité."""

from __future__ import annotations

import logging
from pathlib import Path

import aiosqlite
import pytest

from grand_lyon_mcp.infrastructure.http import HttpClient
from grand_lyon_mcp.infrastructure.logging import get_logger
from grand_lyon_mcp.providers.gtfs import importer


@pytest.mark.asyncio
async def test_fts_query_with_quote_does_not_raise(app_container) -> None:
    """Un guillemet dans la requête ne doit pas produire une MATCH FTS5 malformée."""
    res = await app_container.entities.search_fts('Bellecour" OR x', limit=5)
    assert isinstance(res, list)


@pytest.mark.asyncio
async def test_gtfs_zip_size_guard(monkeypatch: pytest.MonkeyPatch, fixtures_dir: Path) -> None:
    """La borne anti zip-bomb rejette une archive trop volumineuse avant lecture."""
    monkeypatch.setattr(importer, "MAX_ZIP_BYTES", 1)
    conn = await aiosqlite.connect(":memory:")
    try:
        with pytest.raises(ValueError):
            await importer.import_gtfs_zip(conn, fixtures_dir / "gtfs" / "mini_gtfs.zip")
    finally:
        await conn.close()


def test_verify_tls_disabled_emits_warning() -> None:
    """Désactiver la vérification TLS doit émettre un WARNING explicite."""
    logger = get_logger("http")
    records: list[logging.LogRecord] = []

    class _Capture(logging.Handler):
        def emit(self, record: logging.LogRecord) -> None:
            records.append(record)

    handler = _Capture(level=logging.WARNING)
    logger.addHandler(handler)
    previous = logger.level
    logger.setLevel(logging.WARNING)
    try:
        HttpClient(offline=True, verify_tls=False)
    finally:
        logger.removeHandler(handler)
        logger.setLevel(previous)

    assert any("tls_verification_disabled" in r.getMessage() for r in records)
