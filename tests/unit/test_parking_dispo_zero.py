"""Parking live dispo: available_spaces=0 must not be treated as missing."""

from __future__ import annotations

import pytest

from grand_lyon_mcp.providers.live_providers import LiveParking, _first_present


def test_first_present_keeps_zero() -> None:
    row = {"places_disponibles": 0, "available": 99}
    assert _first_present(row, "places_disponibles", "available") == 0
    row2 = {"available": 0}
    assert _first_present(row2, "places_disponibles", "available") == 0
    row3 = {"places_disponibles": None, "available": 5}
    assert _first_present(row3, "places_disponibles", "available") == 5
    assert _first_present({}, "places_disponibles", "available") is None


@pytest.mark.asyncio
async def test_load_live_dispo_keeps_zero_from_datapusher() -> None:
    class _FakeDgl:
        pass

    async def _fake_query(dgl, *, service, schema_table, maxfeatures=1):
        return [
            {"nom": "Parking Full", "places_disponibles": 0},
            {"nom": "Parking Open", "places_disponibles": 12},
        ]

    import grand_lyon_mcp.providers.live_providers as lp

    original = lp.query_table
    lp.query_table = _fake_query  # type: ignore[assignment]

    # also make OGC fail so we hit DataPusher path
    async def _fail_ogc(*a, **k):
        raise RuntimeError("no ogc")

    try:
        import grand_lyon_mcp.providers.datagrandlyon.ogc_features as ogc

        orig_fetch = ogc.fetch_items
        ogc.fetch_items = _fail_ogc  # type: ignore[assignment]
        try:
            parking = LiveParking(_FakeDgl())  # type: ignore[arg-type]
            mapping = await parking._load_live_dispo()
            assert mapping["parking full"] == 0
            assert mapping["parking open"] == 12
        finally:
            ogc.fetch_items = orig_fetch  # type: ignore[assignment]
    finally:
        lp.query_table = original  # type: ignore[assignment]


@pytest.mark.asyncio
async def test_load_live_dispo_keeps_zero_from_ogc() -> None:
    class _FakeDgl:
        pass

    async def _fake_items(dgl, collection, *, limit=50, bbox=None):
        return [
            {
                "properties": {
                    "nom": "Hôtel de Ville",
                    "places_disponibles": 0,
                }
            }
        ]

    import grand_lyon_mcp.providers.datagrandlyon.ogc_features as ogc

    orig = ogc.fetch_items
    ogc.fetch_items = _fake_items  # type: ignore[assignment]
    try:
        parking = LiveParking(_FakeDgl())  # type: ignore[arg-type]
        mapping = await parking._load_live_dispo()
        assert mapping["hôtel de ville"] == 0
    finally:
        ogc.fetch_items = orig  # type: ignore[assignment]
