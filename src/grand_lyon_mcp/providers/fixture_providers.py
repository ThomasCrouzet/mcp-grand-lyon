"""Offline/fixture-backed providers for tests and GRAND_LYON_MCP_OFFLINE=true."""

from __future__ import annotations

import json
from datetime import datetime
from pathlib import Path
from typing import Any

from grand_lyon_mcp.domain.accessibility import AccessibilityIncident, AccessibilityStatus
from grand_lyon_mcp.domain.environment import EnvironmentIndicator, IndicatorValue
from grand_lyon_mcp.domain.facilities import Facility, FacilityCategory, OpenStatus
from grand_lyon_mcp.domain.geo import Point, haversine_m
from grand_lyon_mcp.domain.parking import ParkingOption, ParkingStatus, ParkingType
from grand_lyon_mcp.domain.places import EntityType, PlaceCandidate
from grand_lyon_mcp.domain.traffic import RoadEvent, RoadEventType, TrafficCondition
from grand_lyon_mcp.domain.transit import Departure, Severity, TransitAlert
from grand_lyon_mcp.domain.velov import VelovStation, VelovStationStatus
from grand_lyon_mcp.domain.waste import WasteCategory, WasteFacility
from grand_lyon_mcp.infrastructure.time import now_paris
from grand_lyon_mcp.providers.photon.parser import parse_photon_response
from grand_lyon_mcp.providers.siri.parser import parse_estimated_timetable, parse_situation_exchange


def _load_json(path: Path) -> Any:
    if not path.is_file():
        return None
    return json.loads(path.read_text(encoding="utf-8"))


class FixturePlaceProvider:
    def __init__(self, fixtures_dir: Path) -> None:
        self._dir = fixtures_dir

    async def search(
        self,
        query: str,
        *,
        near: Point | None = None,
        types: list[str] | None = None,
        limit: int = 5,
    ) -> list[PlaceCandidate]:
        data = _load_json(self._dir / "photon" / "part_dieu.json")
        if data is None:
            return []
        results = parse_photon_response(data, limit=limit)
        q = query.lower()
        filtered = [
            r for r in results if q in r.name.lower() or q in r.label.lower() or "part" in q
        ]
        if not filtered:
            filtered = results
        return filtered[:limit]

    async def reverse(self, point: Point) -> PlaceCandidate | None:
        return PlaceCandidate(
            id="photon:reverse",
            name="Lyon",
            label="Lyon",
            type=EntityType.POINT_OF_INTEREST,
            latitude=point.latitude,
            longitude=point.longitude,
            confidence=0.5,
        )


class FixtureTransitRealtime:
    def __init__(self, fixtures_dir: Path) -> None:
        self._dir = fixtures_dir

    async def get_departures(
        self,
        stop_id: str,
        *,
        line: str | None = None,
        direction: str | None = None,
        limit: int = 6,
    ) -> list[Departure]:
        data = _load_json(self._dir / "siri" / "estimated_timetable.json")
        if data is None:
            # datapusher-style departures
            dp = _load_json(self._dir / "datagrandlyon" / "tcl_departures.json")
            if dp is None:
                return []
            deps: list[Departure] = []
            for row in dp if isinstance(dp, list) else dp.get("values", []):
                if not isinstance(row, dict):
                    continue
                line_name = str(row.get("ligne") or row.get("line") or "?")
                if line:
                    from grand_lyon_mcp.domain.transit_line import line_matches

                    if not line_matches(line, line_name):
                        continue
                dest = str(row.get("direction") or row.get("destination") or "")
                if direction and direction.lower() not in dest.lower():
                    continue
                expected_raw = (
                    row.get("heurepassage") or row.get("expected") or row.get("delaipassage")
                )
                expected = None
                if isinstance(expected_raw, str) and "T" in expected_raw:
                    from dateutil.parser import isoparse

                    expected = isoparse(expected_raw)
                deps.append(
                    Departure(
                        line_id=f"tcl:{line_name}",
                        line_name=line_name,
                        destination=dest,
                        scheduled_at=expected,
                        expected_at=expected,
                        realtime=True,
                    )
                )
            return deps[:limit]
        deps = parse_estimated_timetable(data)
        if line:
            from grand_lyon_mcp.domain.transit_line import line_matches

            deps = [d for d in deps if line_matches(line, d.line_name, d.line_id)]
        if direction:
            deps = [d for d in deps if direction.lower() in d.destination.lower()]
        return deps[:limit]


class FixtureTransitAlerts:
    def __init__(self, fixtures_dir: Path) -> None:
        self._dir = fixtures_dir

    async def get_alerts(self, *, lines: list[str] | None = None) -> list[TransitAlert]:
        data = _load_json(self._dir / "siri" / "situation_exchange.json")
        if data is None:
            data = _load_json(self._dir / "datagrandlyon" / "tcl_alerts.json")
            if isinstance(data, list):
                alerts = [
                    TransitAlert(
                        id=str(a.get("id") or i),
                        severity=Severity.MAJOR
                        if "majeur" in str(a.get("type", "")).lower()
                        else Severity.MINOR,
                        title=str(a.get("title") or a.get("titre") or "Alerte"),
                        description=str(a.get("description") or ""),
                        lines=list(a.get("lines") or []),
                    )
                    for i, a in enumerate(data)
                ]
            else:
                return []
        else:
            alerts = parse_situation_exchange(data)
        if lines:
            lines_l = {x.lower() for x in lines}
            alerts = [
                a
                for a in alerts
                if not a.lines
                or any(
                    line_ref.lower() in lines_l
                    or lines_l.intersection({x.lower() for x in a.lines})
                    for line_ref in a.lines
                )
                or any(
                    ln.lower() in " ".join(a.lines).lower() or ln.lower() in a.title.lower()
                    for ln in lines
                )
            ]
        return alerts


class FixtureAccessibility:
    def __init__(self, fixtures_dir: Path) -> None:
        self._dir = fixtures_dir

    async def get_incidents(self, *, lines: list[str] | None = None) -> list[AccessibilityIncident]:
        data = _load_json(self._dir / "datagrandlyon" / "tcl_accessibility.json")
        if not data:
            return []
        items = data if isinstance(data, list) else data.get("values", [])
        return [
            AccessibilityIncident(
                id=str(item.get("id") or i),
                location=str(item.get("location") or item.get("arret") or "unknown"),
                status=AccessibilityStatus.INACCESSIBLE
                if "indispo" in str(item.get("status", "")).lower()
                else AccessibilityStatus.PARTIALLY_ACCESSIBLE,
                description=str(item.get("description") or item.get("message") or ""),
            )
            for i, item in enumerate(items)
            if isinstance(item, dict)
        ]

    async def check_stop(self, stop_id: str) -> str:
        incidents = await self.get_incidents()
        for inc in incidents:
            if stop_id.lower() in inc.location.lower():
                return inc.status.value
        return AccessibilityStatus.UNKNOWN.value


class FixtureVelov:
    def __init__(self, fixtures_dir: Path) -> None:
        self._dir = fixtures_dir
        self._stations = self._load()

    def _load(self) -> list[VelovStation]:
        data = _load_json(self._dir / "datagrandlyon" / "velov.json")
        if not data:
            return []
        items = data if isinstance(data, list) else data.get("values", [])
        out: list[VelovStation] = []
        for item in items:
            if not isinstance(item, dict):
                continue
            lat = float(item.get("lat") or item.get("latitude") or 0)
            lon = float(item.get("lng") or item.get("lon") or item.get("longitude") or 0)
            out.append(
                VelovStation(
                    id=str(item.get("number") or item.get("id") or item.get("name")),
                    name=str(item.get("name") or "Vélo'v"),
                    latitude=lat,
                    longitude=lon,
                    bikes_available=int(item.get("available_bikes") or item.get("bikes") or 0),
                    docks_available=int(
                        item.get("available_bike_stands") or item.get("docks") or 0
                    ),
                    capacity=int(item.get("bike_stands") or item.get("capacity") or 0),
                    status=VelovStationStatus.OPEN
                    if str(item.get("status", "OPEN")).upper() == "OPEN"
                    else VelovStationStatus.CLOSED,
                    observed_at=now_paris(),
                )
            )
        return out

    async def stations_near(
        self, point: Point, *, radius_m: float = 1000, limit: int = 20
    ) -> list[VelovStation]:
        results: list[VelovStation] = []
        for s in self._stations:
            d = haversine_m(point, Point(s.latitude, s.longitude))
            if d <= radius_m:
                results.append(s.model_copy(update={"distance_m": d}))
        results.sort(key=lambda x: x.distance_m or 0)
        return results[:limit]


class FixtureParking:
    def __init__(self, fixtures_dir: Path) -> None:
        self._dir = fixtures_dir

    async def options_near(
        self,
        point: Point,
        *,
        types: list[ParkingType] | None = None,
        radius_m: float = 1500,
        minimum_spaces: int = 0,
        limit: int = 10,
    ) -> list[ParkingOption]:
        data = _load_json(self._dir / "datagrandlyon" / "parkings.json")
        if not data:
            return []
        items = data if isinstance(data, list) else data.get("values", [])
        out: list[ParkingOption] = []
        allowed = set(types or [ParkingType.PUBLIC_PARKING, ParkingType.PARK_AND_RIDE])
        for item in items:
            if not isinstance(item, dict):
                continue
            ptype = (
                ParkingType.PARK_AND_RIDE
                if "relais" in str(item.get("type", "")).lower()
                or "p+r" in str(item.get("name", "")).lower()
                else ParkingType.PUBLIC_PARKING
            )
            if ptype not in allowed:
                continue
            lat = float(item.get("lat") or item.get("latitude") or 0)
            lon = float(item.get("lon") or item.get("longitude") or item.get("lng") or 0)
            avail = item.get("available") or item.get("places_disponibles")
            avail_i = int(avail) if avail is not None else None
            if avail_i is not None and avail_i < minimum_spaces:
                continue
            d = haversine_m(point, Point(lat, lon))
            if d > radius_m:
                continue
            cap = item.get("capacity") or item.get("capacite")
            cap_i = int(cap) if cap is not None else None
            ratio = None
            if cap_i and avail_i is not None and cap_i > 0:
                ratio = avail_i / cap_i
            out.append(
                ParkingOption(
                    id=str(item.get("id") or item.get("name")),
                    name=str(item.get("name") or "Parking"),
                    type=ptype,
                    latitude=lat,
                    longitude=lon,
                    capacity=cap_i,
                    available_spaces=avail_i,
                    availability_ratio=ratio,
                    status=ParkingStatus.OPEN if (avail_i or 0) > 0 else ParkingStatus.FULL,
                    distance_m=d,
                    realtime=True,
                    observed_at=now_paris(),
                )
            )
        out.sort(key=lambda p: p.distance_m or 0)
        return out[:limit]


class FixtureTraffic:
    def __init__(self, fixtures_dir: Path) -> None:
        self._dir = fixtures_dir

    async def conditions(self, *, area: str | None = None) -> list[TrafficCondition]:
        data = _load_json(self._dir / "datagrandlyon" / "traffic.json")
        if not data:
            return []
        items = data if isinstance(data, list) else data.get("values", [])
        return [
            TrafficCondition(
                id=str(item.get("id") or i),
                area=str(item.get("area") or item.get("secteur") or "Lyon"),
                level=str(item.get("level") or item.get("etat") or "unknown"),
                description=str(item.get("description") or ""),
            )
            for i, item in enumerate(items)
            if isinstance(item, dict)
        ]

    async def road_events(self, *, area: str | None = None) -> list[RoadEvent]:
        data = _load_json(self._dir / "datagrandlyon" / "road_events.json")
        if not data:
            return []
        items = data if isinstance(data, list) else data.get("values", [])
        return [
            RoadEvent(
                id=str(item.get("id") or i),
                type=RoadEventType.ROADWORKS
                if "chantier" in str(item.get("type", "")).lower()
                else RoadEventType.INCIDENT,
                title=str(item.get("title") or item.get("nom") or "Événement"),
                description=str(item.get("description") or ""),
                latitude=float(item["lat"]) if item.get("lat") is not None else None,
                longitude=float(item["lon"]) if item.get("lon") is not None else None,
            )
            for i, item in enumerate(items)
            if isinstance(item, dict)
        ]


class FixtureFacilities:
    def __init__(self, fixtures_dir: Path) -> None:
        self._dir = fixtures_dir

    async def nearby(
        self,
        point: Point,
        *,
        categories: list[FacilityCategory],
        radius_m: float = 1000,
        limit_per_category: int = 5,
    ) -> list[Facility]:
        data = _load_json(self._dir / "datagrandlyon" / "facilities.json")
        if not data:
            return []
        items = data if isinstance(data, list) else data.get("values", [])
        out: list[Facility] = []
        counts: dict[str, int] = {}
        for item in items:
            if not isinstance(item, dict):
                continue
            cat_raw = str(item.get("category") or item.get("type") or "toilet")
            try:
                cat = FacilityCategory(cat_raw)
            except ValueError:
                mapping = {
                    "toilettes": FacilityCategory.TOILET,
                    "toilet": FacilityCategory.TOILET,
                    "fontaine": FacilityCategory.DRINKING_WATER,
                    "drinking_water": FacilityCategory.DRINKING_WATER,
                    "parc": FacilityCategory.PARK,
                    "park": FacilityCategory.PARK,
                }
                cat = mapping.get(cat_raw.lower(), FacilityCategory.TOILET)
            if cat not in categories:
                continue
            if counts.get(cat.value, 0) >= limit_per_category:
                continue
            lat = float(item.get("lat") or item.get("latitude") or 0)
            lon = float(item.get("lon") or item.get("longitude") or 0)
            d = haversine_m(point, Point(lat, lon))
            if d > radius_m:
                continue
            out.append(
                Facility(
                    id=str(item.get("id") or item.get("name")),
                    name=str(item.get("name") or cat.value),
                    category=cat,
                    latitude=lat,
                    longitude=lon,
                    distance_m=d,
                    open_status=OpenStatus.UNKNOWN,
                    opening_hours_raw=item.get("hours"),
                )
            )
            counts[cat.value] = counts.get(cat.value, 0) + 1
        out.sort(key=lambda f: f.distance_m or 0)
        return out


class FixtureEnvironment:
    def __init__(self, fixtures_dir: Path) -> None:
        self._dir = fixtures_dir

    async def indicators(
        self,
        point: Point,
        *,
        indicators: list[str],
        at: datetime | None = None,
    ) -> list[IndicatorValue]:
        data = _load_json(self._dir / "datagrandlyon" / "environment.json")
        supported = set()
        values: dict[str, Any] = {}
        if isinstance(data, dict):
            supported = set(data.get("supported") or data.keys())
            values = data
        results: list[IndicatorValue] = []
        for ind in indicators:
            try:
                ei = EnvironmentIndicator(ind)
            except ValueError:
                results.append(
                    IndicatorValue(
                        indicator=EnvironmentIndicator.POLLEN, supported=False, value=None
                    )
                )
                continue
            if data is None or (ei.value not in supported and ei.value not in values):
                results.append(IndicatorValue(indicator=ei, supported=False, value=None))
                continue
            block = values.get(ei.value) if isinstance(values.get(ei.value), dict) else {}
            results.append(
                IndicatorValue(
                    indicator=ei,
                    value=block.get("value") if block else values.get(ei.value),
                    unit=block.get("unit") if block else None,
                    zone=block.get("zone") if block else "Lyon",
                    observed_at=at or now_paris(),
                    source_id=f"env_{ei.value}",
                    supported=True,
                )
            )
        return results


class FixtureWaste:
    def __init__(self, fixtures_dir: Path) -> None:
        self._dir = fixtures_dir

    async def facilities_near(
        self,
        point: Point,
        *,
        category: WasteCategory,
        radius_m: float = 15000,
        limit: int = 10,
    ) -> list[WasteFacility]:
        data = _load_json(self._dir / "datagrandlyon" / "waste_facilities.json")
        if not data:
            return []
        items = data if isinstance(data, list) else data.get("values", [])
        out: list[WasteFacility] = []
        for item in items:
            if not isinstance(item, dict):
                continue
            # accept string category values from fixtures
            accepted: list[WasteCategory] = []
            for c in item.get("categories", []):
                try:
                    accepted.append(WasteCategory(str(c)))
                except ValueError:
                    continue
            if accepted and category not in accepted and category != WasteCategory.UNKNOWN:
                continue
            lat = float(item.get("lat") or item.get("latitude") or 0)
            lon = float(item.get("lon") or item.get("longitude") or 0)
            d = haversine_m(point, Point(lat, lon))
            if d > radius_m:
                continue
            out.append(
                WasteFacility(
                    id=str(item.get("id") or item.get("name")),
                    name=str(item.get("name") or "Déchèterie"),
                    latitude=lat,
                    longitude=lon,
                    accepted_categories=accepted,
                    distance_m=d,
                    open_status=str(item.get("open_status") or "unknown"),
                )
            )
        out.sort(key=lambda f: f.distance_m or 0)
        return out[:limit]
