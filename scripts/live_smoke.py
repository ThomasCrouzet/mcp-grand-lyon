#!/usr/bin/env python3
"""Live smoke tests against DataGrandLyon / Photon (no secrets printed)."""

from __future__ import annotations

import asyncio
import json
import os
import sys
from pathlib import Path
from typing import Any

# Ensure package import when run as script
ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "src"))


def _mask_report(obj: Any) -> Any:
    """Ensure no credential-looking long secrets appear."""
    text = json.dumps(obj, ensure_ascii=False, default=str)
    for key in ("DATAGRANDLYON_USERNAME", "DATAGRANDLYON_PASSWORD"):
        val = os.environ.get(key, "")
        if val and len(val) >= 4:
            text = text.replace(val, "[REDACTED]")
    return json.loads(text)


async def probe_http() -> list[dict[str, Any]]:
    from grand_lyon_mcp.infrastructure.http import HttpClient
    from grand_lyon_mcp.providers.datagrandlyon.auth import basic_auth

    u = os.environ.get("DATAGRANDLYON_USERNAME", "")
    p = os.environ.get("DATAGRANDLYON_PASSWORD", "")
    http = HttpClient(offline=False, connect_timeout=12, read_timeout=45)
    auth = basic_auth(u, p) if u and p else None
    cases = [
        (
            "photon",
            "https://download.data.grandlyon.com/geocoding/photon-bal/api?q=Bellecour&limit=2",
            False,
        ),
        ("catalog_rdata", "https://data.grandlyon.com/fr/datapusher/ws/rdata/all.json", True),
        (
            "velov_public",
            "https://data.grandlyon.com/fr/datapusher/ws/rdata/jcd_jcdecaux.jcdvelov/all.json?maxfeatures=3&compact=false",
            False,
        ),
        (
            "parkings_v2",
            "https://data.grandlyon.com/fr/datapusher/ws/grandlyon/pvo_patrimoine_voirie.pvoparking/all.json?maxfeatures=2&compact=false",
            False,
        ),
        (
            "ogc_root",
            "https://data.grandlyon.com/geoserver/ogc/features/v1/collections?f=application/json",
            False,
        ),
    ]
    out: list[dict[str, Any]] = []
    try:
        for name, url, prefer_auth in cases:
            # try without auth
            r0 = await http.get(url)
            entry: dict[str, Any] = {
                "name": name,
                "noauth_status": r0.status_code,
                "noauth_bytes": len(r0.content),
            }
            if prefer_auth and auth is not None:
                r1 = await http.get(url, auth=auth)
                entry["basic_status"] = r1.status_code
                entry["basic_bytes"] = len(r1.content)
                if r1.status_code == 401:
                    try:
                        entry["basic_detail"] = r1.json().get("detail")
                    except Exception:
                        entry["basic_detail"] = r1.text[:80]
            elif r0.status_code == 200 and name == "velov_public":
                data = r0.json()
                vals = data.get("values") if isinstance(data, dict) else data
                entry["sample_names"] = [
                    (v.get("name") if isinstance(v, dict) else None) for v in (vals or [])[:3]
                ]
            out.append(entry)
    finally:
        await http.aclose()
    return out


async def app_tool_tests() -> dict[str, Any]:
    from grand_lyon_mcp.adapters.mcp.tools import PUBLIC_TOOL_NAMES, dispatch_tool
    from grand_lyon_mcp.bootstrap import build_app
    from grand_lyon_mcp.domain.common import PlaceRef
    from grand_lyon_mcp.settings import Settings

    settings = Settings(
        offline=False,
        datagrandlyon_username=os.environ.get("DATAGRANDLYON_USERNAME", ""),
        datagrandlyon_password=os.environ.get("DATAGRANDLYON_PASSWORD", ""),
        log_level="WARNING",
        transitous_enabled=False,
    )
    app = await build_app(settings)
    result: dict[str, Any] = {
        "settings": {
            "offline": settings.offline,
            "has_credentials": settings.has_credentials(),
            "db": str(settings.resolved_db_path()),
        },
        "services": {},
        "tools": [],
    }
    try:
        # Services
        place = await app.places.resolve_place(query="Part-Dieu", limit=3)
        result["services"]["resolve_place"] = {
            "status": place.status.value,
            "summary": place.summary,
            "n": len(place.data.get("candidates") or []),
            "top": (place.data.get("candidates") or [])[:2],
        }

        deps = await app.transit.next_departures(
            stop=PlaceRef(query="Bellecour"), line="A", limit=4
        )
        result["services"]["next_departures"] = {
            "status": deps.status.value,
            "summary": deps.summary,
            "n": len(deps.data.get("departures") or []),
            "realtime_flags": [d.get("realtime") for d in (deps.data.get("departures") or [])[:4]],
        }

        mob = await app.mobility.status(
            lines=["A", "C3"],
            include=["transit", "traffic", "roadworks", "accessibility"],
        )
        result["services"]["mobility"] = {
            "status": mob.status.value,
            "summary": mob.summary,
            "alerts": len(mob.data.get("alerts") or []),
            "road_events": len(mob.data.get("road_events") or []),
            "traffic": len(mob.data.get("traffic_conditions") or []),
        }

        velov = await app.velov.stations_near_ref(
            PlaceRef(latitude=45.7606, longitude=4.8618),
            radius_m=1200,
            minimum_bikes=1,
            limit=5,
        )
        result["services"]["velov"] = {
            "status": velov.status.value,
            "summary": velov.summary,
            "n": len(velov.data.get("stations") or []),
            "sample": (velov.data.get("stations") or [])[:2],
        }

        park = await app.parking.options(
            destination=PlaceRef(latitude=45.7675, longitude=4.8355),
            radius_m=2000,
            limit=5,
        )
        result["services"]["parking"] = {
            "status": park.status.value,
            "summary": park.summary,
            "n": len(park.data.get("options") or []),
        }

        fac = await app.facilities.nearby(
            location=PlaceRef(latitude=45.777, longitude=4.855),
            categories=["toilet", "drinking_water", "park"],
            radius_m=1500,
        )
        result["services"]["facilities"] = {
            "status": fac.status.value,
            "summary": fac.summary,
        }

        waste = await app.waste.dropoff(
            item="batterie de vélo électrique",
            location=PlaceRef(latitude=45.76, longitude=4.85),
        )
        result["services"]["waste"] = {
            "status": waste.status.value,
            "summary": waste.summary,
            "category": (waste.data.get("classification") or {}).get("category"),
            "hazardous": (waste.data.get("classification") or {}).get("hazardous"),
        }

        trips = await app.journeys.options(
            origin=PlaceRef(latitude=45.7606, longitude=4.8618),
            destination=PlaceRef(latitude=45.7578, longitude=4.8320),
            modes=["walk", "velov", "tcl", "car"],
        )
        result["services"]["trips"] = {
            "status": trips.status.value,
            "summary": trips.summary,
            "recommended": trips.data.get("recommended_mode"),
            "options": [
                {
                    "mode": o.get("mode"),
                    "score": o.get("score"),
                    "summary": o.get("summary"),
                    "duration_s": o.get("estimated_duration_seconds"),
                }
                for o in (trips.data.get("options") or [])
            ],
        }

        # MCP dispatch envelope for all tools
        payloads: dict[str, dict[str, Any]] = {
            "lyon_resolve_place": {"query": "Bellecour", "limit": 3},
            "lyon_next_departures": {"stop": {"query": "Part-Dieu"}, "limit": 3},
            "lyon_mobility_status": {"lines": ["A"], "include": ["transit", "traffic"]},
            "lyon_trip_options": {
                "origin": {"latitude": 45.7606, "longitude": 4.8618},
                "destination": {"latitude": 45.7578, "longitude": 4.8320},
                "modes": ["walk", "velov", "tcl"],
            },
            "lyon_parking_options": {
                "destination": {"latitude": 45.767, "longitude": 4.835},
                "limit": 5,
            },
            "lyon_accessibility_check": {
                "origin": {"query": "Bellecour"},
                "destination": {"query": "Part-Dieu"},
            },
            "lyon_nearby_facilities": {
                "location": {"latitude": 45.777, "longitude": 4.855},
                "categories": ["toilet", "park"],
                "radius_m": 1200,
            },
            "lyon_environment_brief": {
                "location": {"query": "Lyon"},
                "indicators": ["pollen", "air_quality", "heat"],
            },
            "lyon_waste_dropoff": {
                "item": "pot de peinture",
                "location": {"latitude": 45.75, "longitude": 4.84},
            },
            "lyon_personal_briefing": {"profile": "weekday_morning"},
        }
        for name in PUBLIC_TOOL_NAMES:
            r = await dispatch_tool(app, name, payloads[name])
            result["tools"].append(
                {
                    "tool": name,
                    "status": r.get("status"),
                    "summary": r.get("summary"),
                    "degraded": r.get("degraded"),
                    "warnings": len(r.get("warnings") or []),
                    "has_data": bool(r.get("data")),
                }
            )
    finally:
        await app.aclose()
    return result


async def live_velov_parse() -> dict[str, Any]:
    """Direct live Vélo'v table (public) + normalize a few stations."""
    from grand_lyon_mcp.infrastructure.http import HttpClient
    from grand_lyon_mcp.providers.datagrandlyon.client import DataGrandLyonClient
    from grand_lyon_mcp.providers.datagrandlyon.datapusher import query_table

    http = HttpClient(offline=False)
    dgl = DataGrandLyonClient(http, username="", password="")
    try:
        rows = await query_table(
            dgl,
            service="rdata",
            schema_table="jcd_jcdecaux.jcdvelov",
            maxfeatures=5,
        )
        open_with_bikes = [
            r
            for r in rows
            if str(r.get("status", "")).upper() == "OPEN"
            and int(r.get("available_bikes") or 0) >= 3
        ]
        return {
            "rows": len(rows),
            "open_with_ge3_bikes": len(open_with_bikes),
            "examples": [
                {
                    "name": r.get("name"),
                    "bikes": r.get("available_bikes"),
                    "docks": r.get("available_bike_stands"),
                    "lat": r.get("lat"),
                    "lng": r.get("lng"),
                }
                for r in open_with_bikes[:5]
            ],
        }
    finally:
        await http.aclose()


async def main() -> int:
    print("=== LIVE HTTP probe ===")
    http_report = await probe_http()
    for row in http_report:
        print(json.dumps(row, ensure_ascii=False))

    print("\n=== LIVE Vélo'v public table ===")
    velov = await live_velov_parse()
    print(json.dumps(velov, ensure_ascii=False, indent=2))

    print("\n=== APP services + MCP tools ===")
    app_report = await app_tool_tests()
    for k, v in app_report.get("services", {}).items():
        print(f"SERVICE {k}: {json.dumps(v, ensure_ascii=False, default=str)[:240]}")
    print()
    for t in app_report.get("tools", []):
        print(
            f"TOOL {t['tool']:28} status={t['status']:12} "
            f"degraded={t['degraded']} warnings={t['warnings']} | {t['summary']}"
        )

    out_dir = Path(os.environ.get("LIVE_OUT", "/tmp/grand-lyon-live-tests"))
    out_dir.mkdir(parents=True, exist_ok=True)
    payload = _mask_report(
        {
            "http": http_report,
            "velov_live": velov,
            "app": app_report,
            "credential_note": (
                "If catalog basic_status=401 with detail about invalid username/password, "
                "the DataGrandLyon *data platform* password is wrong (often confused with "
                "GrandLyon Connect password). Reset via data.grandlyon.com password page."
            ),
        }
    )
    out = out_dir / "live_report.json"
    out.write_text(json.dumps(payload, ensure_ascii=False, indent=2, default=str), encoding="utf-8")
    print(f"\nWrote {out}")

    # Exit code: soft: credentials may fail but public + app path should work
    tools_ok = all(
        t.get("status")
        in {"ok", "partial", "ambiguous", "not_found", "unavailable", "invalid_request"}
        for t in app_report.get("tools", [])
    )
    return 0 if tools_ok else 1


if __name__ == "__main__":
    raise SystemExit(asyncio.run(main()))
