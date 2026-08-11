"""Hybrid catalog discovery helpers (403-safe).

Pure/status helpers + async scan steps testable offline without full CLI.
"""

from __future__ import annotations

from collections.abc import Awaitable, Callable
from typing import Any


def classify_catalog_http_status(status_code: int) -> dict[str, str]:
    """Map HTTP status of catalog list to doctor-style auth/catalog_list rows.

    403 is a métier permission limit, not an authentication failure.
    """
    if status_code == 200:
        return {"auth": "OK", "catalog_list": "OK", "detail": "full list accessible"}
    if status_code == 401:
        return {"auth": "ERROR", "catalog_list": "ERROR", "detail": "auth failed (401)"}
    if status_code == 403:
        return {
            "auth": "OK",
            "catalog_list": "FORBIDDEN",
            "detail": "403: pas la permission (limite métier, use known+ogc)",
        }
    return {
        "auth": "UNKNOWN",
        "catalog_list": "ERROR",
        "detail": f"HTTP {status_code}",
    }


def should_use_known_ogc_fallback(mode: str, catalog_usable: bool) -> bool:
    """Whether auto/known/ogc paths should probe known tables / OGC."""
    mode_l = mode.lower().strip()
    if mode_l in {"known", "ogc"}:
        return True
    return bool(mode_l == "auto" and not catalog_usable)


async def probe_known_tables(
    *,
    known: dict[str, dict[str, str]],
    query_fn: Callable[..., Awaitable[list[Any]]],
    set_status_fn: Callable[..., Awaitable[Any]] | None = None,
) -> dict[str, dict[str, Any]]:
    """Bounded probe of known tables (maxfeatures=1 semantics via query_fn)."""
    results: dict[str, dict[str, Any]] = {}
    for sid, meta in known.items():
        try:
            rows = await query_fn(
                service=meta["service"],
                schema_table=meta["table_name"],
                maxfeatures=1,
            )
            if set_status_fn is not None:
                await set_status_fn(sid, "OK", table_name=meta["table_name"])
            results[sid] = {
                "status": "OK",
                "table": meta["table_name"],
                "sample_rows": len(rows),
            }
        except Exception as exc:
            err = str(exc)
            st = "UNRESOLVED" if ("403" in err or "Forbidden" in err) else "ERROR"
            results[sid] = {
                "status": st,
                "table": meta["table_name"],
                "error": err,
            }
    return results


async def hybrid_catalog_scan(
    *,
    mode: str = "auto",
    fetch_catalog_fn: Callable[[str], Awaitable[list[Any]]] | None = None,
    known: dict[str, dict[str, str]] | None = None,
    query_fn: Callable[..., Awaitable[list[Any]]] | None = None,
    list_ogc_fn: Callable[[], Awaitable[list[Any]]] | None = None,
) -> dict[str, Any]:
    """Run hybrid scan logic offline-testable with injected callables.

    ``auto``: try full catalog; on failure/403 → known + ogc.
    """
    mode_l = mode.lower().strip()
    results: dict[str, Any] = {"catalog_mode": mode_l, "auth": "OK"}
    catalog_usable = False
    catalog_forbidden = False

    if mode_l in {"auto", "full"} and fetch_catalog_fn is not None:
        for service in ("rdata", "grandlyon"):
            try:
                entries = await fetch_catalog_fn(service)
                results[service] = {"count": len(entries), "status": "OK"}
                catalog_usable = True
            except Exception as exc:
                err = str(exc)
                st = "ERROR"
                if "403" in err or "Forbidden" in err or "pas la permission" in err.lower():
                    st = "FORBIDDEN"
                    catalog_forbidden = True
                results[service] = {"status": st, "error": err}
        if catalog_usable:
            results["catalog_list"] = "OK"
        elif catalog_forbidden:
            results["catalog_list"] = "FORBIDDEN"
            results["note"] = "403 catalogue n'est pas un échec d'auth, bascule known+ogc"

    if should_use_known_ogc_fallback(mode_l, catalog_usable):
        probe_known = (
            known is not None
            and query_fn is not None
            and (mode_l == "known" or (mode_l == "auto" and not catalog_usable))
        )
        if probe_known:
            results["catalog_mode"] = "known" if mode_l == "known" else "known_tables_fallback"
            results["sources"] = await probe_known_tables(known=known, query_fn=query_fn)  # type: ignore[arg-type]
        if list_ogc_fn is not None and (
            mode_l == "ogc" or (mode_l == "auto" and not catalog_usable)
        ):
            try:
                cols = await list_ogc_fn()
                results["ogc"] = {"status": "OK", "collections": len(cols)}
            except Exception as exc:
                results["ogc"] = {"status": "ERROR", "error": str(exc)}

    return results
