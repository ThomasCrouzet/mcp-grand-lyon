"""HTTP fixtures exercise the live mapping absent from offline process checks."""

from __future__ import annotations

import json

import httpx
import respx

from grand_lyon_mcp.domain.common import PlaceRef
from grand_lyon_mcp.domain.geo import Point
from grand_lyon_mcp.providers import live_providers
from grand_lyon_mcp.providers.live_providers import (
    LiveParking,
    LiveTransitRealtime,
    LiveVelovProvider,
)
from grand_lyon_mcp.services.parking_service import ParkingService
from grand_lyon_mcp.services.transit_service import TransitService


@respx.mock
async def test_live_parking_mixed_radius_and_refresh(app_container, fixtures_dir, monkeypatch):
    clock = 100.0
    monkeypatch.setattr(live_providers, "monotonic", lambda: clock)
    app_container.http.offline = False
    case = json.loads((fixtures_dir / "mobility_cases.json").read_text())["parking"]["values"]
    availability = {"features": [{"properties": {"nom": "Live", "available": 42}}]}

    def response(request):
        if "collections" in request.url.path:
            return httpx.Response(200, json=availability)
        if "tclparcrelaistr" in request.url.path:
            return httpx.Response(
                200,
                json=[dict(row, nom=row["name"]) for row in case if row["type"] == "parc relais"],
            )
        return httpx.Response(200, json=[row for row in case if row["type"] == "public"])

    respx.route().mock(side_effect=response)
    provider = LiveParking(app_container.dgl, cache_ttl_seconds=60)
    service = ParkingService(places=app_container.places, provider=provider)
    args = {"destination": PlaceRef(latitude=45.7675, longitude=4.8355), "radius_m": 100}
    first = await service.options(**args)
    options = {p["id"]: p for p in first.data["options"]}
    # Live public IDs use the name if no gid is present.
    assert {p["name"] for p in options.values()} == {
        "Live",
        "Full",
        "Capacity only",
        "Unknown",
        "P+R",
    }
    assert all(p["distance_m"] <= 100 for p in options.values())
    assert next(p for p in options.values() if p["name"] == "P+R")["status"] == "unknown"
    assert first.status.value == "partial"
    availability["features"][0]["properties"]["available"] = 3
    clock += 61
    second = await service.options(**args)
    assert next(p for p in second.data["options"] if p["name"] == "Live")["available_spaces"] == 3


@respx.mock
async def test_velov_refreshes_after_source_ttl(app_container, monkeypatch):
    clock = 100.0
    monkeypatch.setattr(live_providers, "monotonic", lambda: clock)
    app_container.http.offline = False
    row = {"id": "bike", "name": "Bike", "lat": 45.76, "lon": 4.85, "available_bikes": 5}
    respx.route().mock(side_effect=lambda request: httpx.Response(200, json=[row]))
    provider = LiveVelovProvider(app_container.dgl, cache_ttl_seconds=45)
    assert (await provider.stations_near(Point(45.76, 4.85)))[0].bikes_available == 5
    row["available_bikes"] = 0
    clock += 46
    assert (await provider.stations_near(Point(45.76, 4.85)))[0].bikes_available == 0


@respx.mock
async def test_wrong_direction_siri_uses_theoretical_fallback(app_container, fixtures_dir):
    from datetime import datetime

    app_container.http.offline = False
    payload = json.loads((fixtures_dir / "siri" / "estimated_timetable.json").read_text())
    payload = json.loads(json.dumps(payload).replace("Vaulx-en-Velin La Soie", "Perrache"))
    # SIRI uses the client URLs; DataPusher supplies no matching departures.
    respx.route().mock(
        side_effect=lambda request: httpx.Response(
            200, json=[] if "datapusher" in request.url.path else payload
        )
    )
    service = TransitService(
        places=app_container.places,
        static=app_container.gtfs,
        realtime=LiveTransitRealtime(app_container.dgl, stop_resolver=app_container.gtfs),
    )
    result = await service.next_departures(
        stop=PlaceRef(query="Bellecour"),
        line="A",
        direction="Vaulx",
        at=datetime.fromisoformat("2026-07-20T08:00:00+02:00"),
    )
    assert result.data["departures"]
    assert all(not d["realtime"] and "Vaulx" in d["destination"] for d in result.data["departures"])
