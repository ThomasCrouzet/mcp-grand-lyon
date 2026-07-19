"""Catalog hybrid 403 → known+ogc and doctor classify (real shipped helpers)."""

from __future__ import annotations

import pytest

from grand_lyon_mcp.providers.datagrandlyon.catalog_scan import (
    classify_catalog_http_status,
    hybrid_catalog_scan,
    probe_known_tables,
    should_use_known_ogc_fallback,
)


def test_classify_403_is_forbidden_not_auth_fail() -> None:
    c = classify_catalog_http_status(403)
    assert c["auth"] == "OK"
    assert c["catalog_list"] == "FORBIDDEN"
    assert "403" in c["detail"] or "permission" in c["detail"].lower()


def test_classify_401_is_auth_error() -> None:
    c = classify_catalog_http_status(401)
    assert c["auth"] == "ERROR"
    assert c["catalog_list"] == "ERROR"


def test_classify_200_ok() -> None:
    c = classify_catalog_http_status(200)
    assert c["auth"] == "OK"
    assert c["catalog_list"] == "OK"


def test_should_fallback_auto_when_not_usable() -> None:
    assert should_use_known_ogc_fallback("auto", False) is True
    assert should_use_known_ogc_fallback("auto", True) is False
    assert should_use_known_ogc_fallback("known", True) is True
    assert should_use_known_ogc_fallback("full", False) is False


@pytest.mark.asyncio
async def test_hybrid_auto_on_403_uses_known_and_ogc() -> None:
    async def fetch_forbidden(service: str) -> list:
        raise RuntimeError("403 Forbidden: pas la permission")

    known = {
        "velov_realtime": {
            "service": "rdata",
            "table_name": "jcd_jcdecaux.jcdvelov",
            "status": "OK",
        }
    }

    async def query_ok(*, service: str, schema_table: str, maxfeatures: int = 1) -> list:
        return [{"id": 1}]

    async def list_ogc() -> list:
        return [{"id": "parkings-dispo"}, {"id": "velov"}]

    result = await hybrid_catalog_scan(
        mode="auto",
        fetch_catalog_fn=fetch_forbidden,
        known=known,
        query_fn=query_ok,
        list_ogc_fn=list_ogc,
    )
    assert result["catalog_list"] == "FORBIDDEN"
    assert result["catalog_mode"] == "known_tables_fallback"
    assert result["sources"]["velov_realtime"]["status"] == "OK"
    assert result["ogc"]["status"] == "OK"
    assert result["ogc"]["collections"] == 2
    # auth not degraded to ERROR on 403
    assert result.get("auth") == "OK"


@pytest.mark.asyncio
async def test_hybrid_full_does_not_probe_known_on_403() -> None:
    async def fetch_forbidden(service: str) -> list:
        raise RuntimeError("403 Forbidden")

    called = {"q": 0}

    async def query_should_not(*, service: str, schema_table: str, maxfeatures: int = 1) -> list:
        called["q"] += 1
        return []

    result = await hybrid_catalog_scan(
        mode="full",
        fetch_catalog_fn=fetch_forbidden,
        known={"x": {"service": "rdata", "table_name": "a.b", "status": "OK"}},
        query_fn=query_should_not,
        list_ogc_fn=None,
    )
    assert result["catalog_list"] == "FORBIDDEN"
    assert called["q"] == 0


@pytest.mark.asyncio
async def test_probe_known_marks_403_unresolved() -> None:
    async def query_403(*, service: str, schema_table: str, maxfeatures: int = 1) -> list:
        raise RuntimeError("HTTP 403 Forbidden")

    out = await probe_known_tables(
        known={"s": {"service": "rdata", "table_name": "t.x", "status": "OK"}},
        query_fn=query_403,
    )
    assert out["s"]["status"] == "UNRESOLVED"
