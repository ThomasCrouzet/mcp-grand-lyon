"""Live DataGrandLyon / SIRI providers (require credentials for protected tables)."""

from __future__ import annotations

from datetime import datetime
from typing import Any

from dateutil.parser import isoparse

from grand_lyon_mcp.domain.accessibility import AccessibilityIncident, AccessibilityStatus
from grand_lyon_mcp.domain.facilities import Facility, FacilityCategory, OpenStatus
from grand_lyon_mcp.domain.geo import Point, haversine_m
from grand_lyon_mcp.domain.parking import ParkingOption, ParkingStatus, ParkingType
from grand_lyon_mcp.domain.traffic import RoadEvent, RoadEventType, TrafficCondition
from grand_lyon_mcp.domain.transit import Departure, Severity, TransitAlert
from grand_lyon_mcp.domain.velov import VelovStation, VelovStationStatus
from grand_lyon_mcp.domain.waste import WasteCategory, WasteFacility
from grand_lyon_mcp.infrastructure.logging import get_logger
from grand_lyon_mcp.infrastructure.time import BUSINESS_TZ, now_paris
from grand_lyon_mcp.providers.datagrandlyon.client import DataGrandLyonClient
from grand_lyon_mcp.providers.datagrandlyon.datapusher import query_table
from grand_lyon_mcp.providers.siri.client import SiriClient

logger = get_logger("live")


def _parse_dt(value: Any) -> datetime | None:
    if value is None:
        return None
    if isinstance(value, datetime):
        return value if value.tzinfo else value.replace(tzinfo=BUSINESS_TZ)
    try:
        dt = isoparse(str(value))
        if dt.tzinfo is None:
            dt = dt.replace(tzinfo=BUSINESS_TZ)
        return dt
    except (ValueError, TypeError, OverflowError):
        return None


def _float(row: dict[str, Any], *keys: str) -> float | None:
    for k in keys:
        if k in row and row[k] is not None:
            try:
                return float(row[k])
            except (TypeError, ValueError):
                continue
    return None


def _int(row: dict[str, Any], *keys: str, default: int = 0) -> int:
    for k in keys:
        if k in row and row[k] is not None:
            try:
                return int(row[k])
            except (TypeError, ValueError):
                continue
    return default


def _first_present(row: dict[str, Any], *keys: str) -> Any:
    """Return first key value that is present and not None (keeps 0 / False)."""
    for k in keys:
        if k in row and row[k] is not None:
            return row[k]
    return None


def _facility_label(row: dict[str, Any]) -> str:
    """Human label for waste facilities (address may be a nested dict)."""
    for key in ("nom", "name", "title", "identifiant"):
        val = row.get(key)
        if isinstance(val, str) and val.strip():
            return val.strip()
    addr = row.get("address") or row.get("adresse")
    if isinstance(addr, dict):
        parts = [
            str(addr.get("streetAddress") or addr.get("street") or "").strip(),
            str(addr.get("postalCode") or "").strip(),
            str(addr.get("addressLocality") or addr.get("city") or "").strip(),
        ]
        label = " ".join(p for p in parts if p)
        if label:
            return label
    if isinstance(addr, str) and addr.strip():
        return addr.strip()
    return "Déchèterie"


def _line_matches(line: str, line_name: str, line_id: str = "") -> bool:
    """Strict line match — avoid substring false positives ('A' in 'ActIV...')."""
    from grand_lyon_mcp.domain.transit_line import line_matches

    return line_matches(line, line_name, line_id)


class LiveVelovProvider:
    """Vélo'v realtime via DataPusher jcd_jcdecaux.jcdvelov."""

    def __init__(self, dgl: DataGrandLyonClient) -> None:
        self._dgl = dgl
        self._cache: list[VelovStation] | None = None

    async def _all(self) -> list[VelovStation]:
        if self._cache is not None:
            return self._cache
        rows = await query_table(
            self._dgl,
            service="rdata",
            schema_table="jcd_jcdecaux.jcdvelov",
            maxfeatures=200,
        )
        out: list[VelovStation] = []
        for row in rows:
            lat = _float(row, "lat", "latitude")
            lon = _float(row, "lng", "lon", "longitude")
            if lat is None or lon is None:
                continue
            status_raw = str(row.get("status") or row.get("availability") or "OPEN").upper()
            out.append(
                VelovStation(
                    id=str(row.get("number") or row.get("id") or row.get("name")),
                    name=str(row.get("name") or "Vélo'v"),
                    latitude=lat,
                    longitude=lon,
                    bikes_available=_int(row, "available_bikes", "bikes"),
                    docks_available=_int(row, "available_bike_stands", "docks"),
                    capacity=_int(row, "bike_stands", "capacity"),
                    status=VelovStationStatus.OPEN
                    if status_raw in {"OPEN", "DISPONIBLE", "GREEN", "1"}
                    else VelovStationStatus.CLOSED,
                    observed_at=now_paris(),
                )
            )
        self._cache = out
        return out

    async def stations_near(
        self, point: Point, *, radius_m: float = 1000, limit: int = 20
    ) -> list[VelovStation]:
        stations = await self._all()
        results: list[VelovStation] = []
        for s in stations:
            d = haversine_m(point, Point(s.latitude, s.longitude))
            if d <= radius_m:
                results.append(s.model_copy(update={"distance_m": d}))
        results.sort(key=lambda x: x.distance_m or 0)
        return results[:limit]


class LiveTransitRealtime:
    """TCL next departures: SIRI ET + DataPusher passages, filtered by stop + line.

    Honesty:
    - Never return network-wide hits labelled as the requested stop.
    - Strict line filter: empty means empty (GTFS fallback upstream), never unfiltered.
    - Stop match via StopPointRef / row ids / names, optional ~100 m spatial.
    """

    STOP_RADIUS_M = 100.0

    def __init__(
        self,
        dgl: DataGrandLyonClient,
        *,
        use_siri: bool = True,
        stop_resolver: Any | None = None,
    ) -> None:
        self._dgl = dgl
        self._siri = SiriClient(dgl) if use_siri else None
        # optional callable/async get_stop(stop_id) → object with lat/lon/name
        self._stop_resolver = stop_resolver

    async def _resolve_stop_meta(
        self, stop_id: str
    ) -> tuple[float | None, float | None, str | None]:
        if self._stop_resolver is None:
            return None, None, None
        try:
            getter = getattr(self._stop_resolver, "get_stop", None)
            if callable(getter):
                stop = await getter(stop_id)
                if stop is None:
                    return None, None, None
                lat = getattr(stop, "latitude", None)
                lon = getattr(stop, "longitude", None)
                name = getattr(stop, "name", None)
                return (
                    float(lat) if lat is not None else None,
                    float(lon) if lon is not None else None,
                    str(name) if name else None,
                )
        except Exception:
            return None, None, None
        return None, None, None

    async def get_departures(
        self,
        stop_id: str,
        *,
        line: str | None = None,
        direction: str | None = None,
        limit: int = 6,
    ) -> list[Departure]:
        from grand_lyon_mcp.domain.transit_line import (
            filter_departures_by_line,
            filter_departures_by_stop,
            stop_matches,
        )

        deps: list[Departure] = []
        stop_lat, stop_lon, stop_name = await self._resolve_stop_meta(stop_id)

        # 1) SIRI EstimatedTimetable — filter by stop then line (never unfiltered on empty)
        if self._siri is not None:
            try:
                siri_deps = await self._siri.estimated_timetable()
            except Exception:
                siri_deps = []
            filtered = filter_departures_by_stop(
                siri_deps,
                stop_id,
                stop_lat=stop_lat,
                stop_lon=stop_lon,
                radius_m=self.STOP_RADIUS_M,
            )
            # also try stop name as secondary identity if resolver provided
            if not filtered and stop_name:
                filtered = filter_departures_by_stop(
                    siri_deps,
                    stop_name,
                    stop_lat=stop_lat,
                    stop_lon=stop_lon,
                    radius_m=self.STOP_RADIUS_M,
                )
            if line:
                filtered = filter_departures_by_line(filtered, line)
                # do NOT re-introduce unfiltered lines when empty
            if direction:
                by_dir = [d for d in filtered if direction.lower() in (d.destination or "").lower()]
                if by_dir:
                    filtered = by_dir
            deps.extend(filtered[: limit * 2])

        # 2) DataPusher passages — only rows that can be tied to the stop
        if len(deps) < limit:
            try:
                rows = await query_table(
                    self._dgl,
                    service="rdata",
                    schema_table="tcl_sytral.tclpassagearret",
                    maxfeatures=80,
                )
            except Exception:
                rows = []
            for row in rows:
                line_name = str(row.get("ligne") or row.get("line") or "?")
                if line and not _line_matches(line, line_name):
                    continue
                dest = str(row.get("direction") or row.get("destination") or "")
                if direction and direction.lower() not in dest.lower():
                    continue
                row_stop = str(
                    row.get("id")
                    or row.get("arrets")
                    or row.get("nom")
                    or row.get("stop")
                    or row.get("stop_id")
                    or row.get("code_arret")
                    or ""
                )
                row_lat = _float(row, "lat", "latitude", "y")
                row_lon = _float(row, "lon", "lng", "longitude", "x")
                if not stop_matches(
                    stop_id,
                    stop_ref=row_stop or None,
                    stop_name=stop_name,
                    stop_lat=row_lat,
                    stop_lon=row_lon,
                    ref_lat=stop_lat,
                    ref_lon=stop_lon,
                    radius_m=self.STOP_RADIUS_M,
                ):
                    # Also try matching against resolved stop name
                    if not (
                        stop_name
                        and stop_matches(
                            stop_name,
                            stop_ref=row_stop or None,
                            stop_lat=row_lat,
                            stop_lon=row_lon,
                            ref_lat=stop_lat,
                            ref_lon=stop_lon,
                            radius_m=self.STOP_RADIUS_M,
                        )
                    ):
                        continue
                expected = _parse_dt(row.get("heurepassage") or row.get("expected"))
                delay_raw = row.get("delaipassage")
                delay = None
                if delay_raw is not None and str(delay_raw).strip().lower() not in {
                    "proche",
                    "approaching",
                    "",
                }:
                    try:
                        delay = int(float(str(delay_raw).replace(" min", "").strip()) * 60)
                    except (TypeError, ValueError):
                        delay = None
                deps.append(
                    Departure(
                        line_id=f"tcl:{line_name}",
                        line_name=line_name,
                        destination=dest,
                        stop_ref=row_stop or None,
                        scheduled_at=expected,
                        expected_at=expected,
                        delay_seconds=delay,
                        realtime=True,
                        cancelled=False,
                    )
                )
                if len(deps) >= limit * 2:
                    break

        # Dedupe by line+destination+expected
        seen: set[str] = set()
        unique: list[Departure] = []
        for d in deps:
            key = f"{d.line_name}|{d.destination}|{d.expected_at}"
            if key in seen:
                continue
            seen.add(key)
            unique.append(d)
            if len(unique) >= limit:
                break
        return unique


class LiveTransitAlerts:
    def __init__(self, dgl: DataGrandLyonClient) -> None:
        self._dgl = dgl
        self._siri = SiriClient(dgl)

    async def get_alerts(self, *, lines: list[str] | None = None) -> list[TransitAlert]:
        alerts: list[TransitAlert] = []
        try:
            rows = await query_table(
                self._dgl,
                service="rdata",
                schema_table="tcl_sytral.tclalertetrafic_2",
                maxfeatures=50,
            )
        except Exception:
            rows = []
        for i, row in enumerate(rows):
            sev_raw = str(row.get("niveauseverite") or row.get("severity") or "").lower()
            severity = {
                "1": Severity.INFO,
                "2": Severity.MINOR,
                "3": Severity.MAJOR,
                "4": Severity.CRITICAL,
                "info": Severity.INFO,
                "mineur": Severity.MINOR,
                "majeur": Severity.MAJOR,
                "critique": Severity.CRITICAL,
            }.get(sev_raw, Severity.UNKNOWN)
            line_field = str(row.get("ligne_cli") or row.get("ligne_com") or "")
            line_list = [x.strip() for x in line_field.replace(";", ",").split(",") if x.strip()]
            alerts.append(
                TransitAlert(
                    id=str(row.get("n") or row.get("id") or i),
                    severity=severity,
                    title=str(row.get("titre") or row.get("title") or "Alerte TCL"),
                    description=str(row.get("message") or row.get("cause") or ""),
                    lines=line_list,
                    start_at=_parse_dt(row.get("debut")),
                    end_at=_parse_dt(row.get("fin")),
                )
            )
        if not alerts:
            try:
                alerts = await self._siri.situations()
            except Exception:
                alerts = []
        if lines:
            lines_l = {x.lower() for x in lines}
            alerts = [
                a
                for a in alerts
                if not a.lines
                or any(ln.lower() in lines_l for ln in a.lines)
                or any(ln.lower() in a.title.lower() for ln in lines)
            ]
        return alerts


class LiveAccessibility:
    def __init__(self, dgl: DataGrandLyonClient) -> None:
        self._dgl = dgl

    async def get_incidents(self, *, lines: list[str] | None = None) -> list[AccessibilityIncident]:
        try:
            rows = await query_table(
                self._dgl,
                service="rdata",
                schema_table="tcl_sytral.tclalerteaccessibilite",
                maxfeatures=50,
            )
        except Exception:
            return []
        out: list[AccessibilityIncident] = []
        for i, row in enumerate(rows):
            loc = str(
                row.get("code_station")
                or row.get("code_lieu")
                or row.get("equipement")
                or "unknown"
            )
            out.append(
                AccessibilityIncident(
                    id=str(row.get("id") or row.get("gid") or i),
                    location=loc,
                    status=AccessibilityStatus.INACCESSIBLE,
                    description=str(
                        row.get("consequence") or row.get("cause") or row.get("equipement") or ""
                    ),
                    start_at=_parse_dt(row.get("debut_indispo")),
                    end_at=_parse_dt(row.get("fin_indispo")),
                )
            )
        return out

    async def check_stop(self, stop_id: str) -> str:
        incidents = await self.get_incidents()
        for inc in incidents:
            if stop_id.lower() in inc.location.lower() or inc.location.lower() in stop_id.lower():
                return AccessibilityStatus.INACCESSIBLE.value
        return AccessibilityStatus.UNKNOWN.value


# OGC collection candidate for live parking availability (validated when reachable).
PARKING_AVAILABILITY_OGC = "parkings-de-la-metropole-de-lyon-disponibilites-temps-reel-v2"


class LiveParking:
    """Public parkings (capacity + optional live dispo) and P+R (live spaces).

    Honesty rules:
    - Capacity alone never invents ``available_spaces``.
    - P+R without real coordinates are not placed at the destination (no fake zero distance).
    - Live availability is only set when a validated live field is present.
    """

    def __init__(
        self,
        dgl: DataGrandLyonClient,
        *,
        entity_lookup: Any | None = None,
    ) -> None:
        self._dgl = dgl
        self._entities = entity_lookup
        self._live_dispo_by_name: dict[str, int] | None = None
        self._live_dispo_ok: bool | None = None

    async def _load_live_dispo(self) -> dict[str, int]:
        """Best-effort live availability map name → free spaces (OGC then DataPusher)."""
        if self._live_dispo_by_name is not None:
            return self._live_dispo_by_name
        mapping: dict[str, int] = {}
        try:
            from grand_lyon_mcp.providers.datagrandlyon.ogc_features import fetch_items

            features = await fetch_items(self._dgl, PARKING_AVAILABILITY_OGC, limit=100)
            for feat in features:
                props = feat.get("properties") if isinstance(feat, dict) else None
                if not isinstance(props, dict):
                    continue
                name = str(
                    props.get("nom")
                    or props.get("name")
                    or props.get("idparking")
                    or props.get("id")
                    or ""
                ).strip()
                avail = _first_present(
                    props, "places_disponibles", "available", "nb_places_disponibles"
                )
                if not name or avail is None:
                    continue
                try:
                    mapping[name.lower()] = int(avail)
                except (TypeError, ValueError):
                    continue
            if mapping:
                self._live_dispo_ok = True
                self._live_dispo_by_name = mapping
                return mapping
        except Exception as exc:
            logger.debug(
                "live_parking_dispo_ogc_failed", extra={"provider": "parking"}, exc_info=exc
            )
        for service, table in (
            ("grandlyon", "pvo_patrimoine_voirie.pvoparkingdispo"),
            ("rdata", "pvo_patrimoine_voirie.pvoparkingdispo"),
        ):
            try:
                rows = await query_table(
                    self._dgl, service=service, schema_table=table, maxfeatures=100
                )
            except Exception:
                continue
            for row in rows:
                name = str(row.get("nom") or row.get("name") or row.get("id") or "").strip()
                avail = _first_present(
                    row, "places_disponibles", "available", "nb_places_disponibles"
                )
                if not name or avail is None:
                    continue
                try:
                    mapping[name.lower()] = int(avail)
                except (TypeError, ValueError):
                    continue
            if mapping:
                break
        self._live_dispo_ok = bool(mapping)
        self._live_dispo_by_name = mapping
        return mapping

    async def _geocode_pr(self, name: str) -> tuple[float, float] | None:
        """Resolve P+R coordinates from entity cache only (no silent dest fallback)."""
        if self._entities is None:
            return None
        try:
            hits = await self._entities.search_fts(name, limit=5)
        except Exception:
            return None
        for h in hits:
            return float(h.latitude), float(h.longitude)
        return None

    async def options_near(
        self,
        point: Point,
        *,
        types: list[ParkingType] | None = None,
        radius_m: float = 1500,
        minimum_spaces: int = 0,
        limit: int = 10,
    ) -> list[ParkingOption]:
        allowed = set(types or [ParkingType.PUBLIC_PARKING, ParkingType.PARK_AND_RIDE])
        out: list[ParkingOption] = []
        live_map = await self._load_live_dispo()

        if ParkingType.PUBLIC_PARKING in allowed:
            try:
                rows = await query_table(
                    self._dgl,
                    service="grandlyon",
                    schema_table="pvo_patrimoine_voirie.pvoparking",
                    maxfeatures=100,
                )
            except Exception:
                rows = []
            for row in rows:
                lat = _float(row, "lat", "latitude", "y")
                lon = _float(row, "lon", "lng", "longitude", "x")
                if lat is None or lon is None:
                    continue
                d = haversine_m(point, Point(lat, lon))
                if d > radius_m:
                    continue
                cap = _int(row, "capacite", "capacity", default=0) or None
                name = str(row.get("nom") or row.get("name") or "Parking")
                avail: int | None = None
                realtime = False
                live_val = live_map.get(name.lower())
                if live_val is not None:
                    avail = live_val
                    realtime = True
                else:
                    for key in (
                        "places_disponibles",
                        "nb_places_disponibles",
                        "available",
                        "dispo",
                    ):
                        if key in row and row[key] is not None:
                            try:
                                avail = int(row[key])
                                realtime = True
                                break
                            except (TypeError, ValueError):
                                continue
                out.append(
                    ParkingOption(
                        id=str(row.get("gid") or row.get("nom") or row.get("name")),
                        name=name,
                        type=ParkingType.PUBLIC_PARKING,
                        latitude=lat,
                        longitude=lon,
                        capacity=cap,
                        available_spaces=avail,
                        status=ParkingStatus.OPEN
                        if (avail is not None and avail > 0)
                        else ParkingStatus.UNKNOWN
                        if avail is None
                        else ParkingStatus.FULL,
                        distance_m=d,
                        realtime=realtime,
                        observed_at=now_paris(),
                    )
                )

        if ParkingType.PARK_AND_RIDE in allowed:
            try:
                rows = await query_table(
                    self._dgl,
                    service="rdata",
                    schema_table="tcl_sytral.tclparcrelaistr",
                    maxfeatures=50,
                )
            except Exception:
                rows = []
            for row in rows:
                lat = _float(row, "lat", "latitude")
                lon = _float(row, "lon", "lng", "longitude")
                name = str(row.get("nom") or "P+R")
                avail_raw = _int(row, "nb_tot_place_dispo", "available", default=-1)
                avail = avail_raw if avail_raw >= 0 else None
                if avail is not None and minimum_spaces and avail < minimum_spaces:
                    continue
                if lat is None or lon is None:
                    geo = await self._geocode_pr(name)
                    if geo is None:
                        continue  # no fake dest coords
                    lat, lon = geo
                d = haversine_m(point, Point(lat, lon))
                if d > radius_m:
                    continue
                out.append(
                    ParkingOption(
                        id=str(row.get("id") or row.get("gid") or row.get("nom")),
                        name=name,
                        type=ParkingType.PARK_AND_RIDE,
                        latitude=lat,
                        longitude=lon,
                        capacity=_int(row, "capacite", default=0) or None,
                        available_spaces=avail,
                        status=ParkingStatus.OPEN
                        if (avail is None or avail > 0)
                        else ParkingStatus.FULL,
                        distance_m=d,
                        realtime=avail is not None,
                        observed_at=now_paris(),
                    )
                )

        filtered: list[ParkingOption] = []
        for p in out:
            if (
                p.available_spaces is not None
                and minimum_spaces
                and p.available_spaces < minimum_spaces
            ):
                continue
            filtered.append(p)
        filtered.sort(
            key=lambda p: (
                0 if p.available_spaces is not None else 1,
                p.distance_m if p.distance_m is not None else 1e12,
            )
        )
        return filtered[:limit]

    @property
    def live_dispo_resolved(self) -> bool | None:
        return self._live_dispo_ok


class LiveTraffic:
    def __init__(self, dgl: DataGrandLyonClient) -> None:
        self._dgl = dgl

    async def conditions(self, *, area: str | None = None) -> list[TrafficCondition]:
        # traffic realtime table not exposed via datapusher under expected name
        return []

    async def road_events(self, *, area: str | None = None) -> list[RoadEvent]:
        try:
            rows = await query_table(
                self._dgl,
                service="grandlyon",
                schema_table="pvo_patrimoine_voirie.pvochantierperturbant",
                maxfeatures=50,
            )
        except Exception:
            return []
        out: list[RoadEvent] = []
        for i, row in enumerate(rows):
            title = str(row.get("nom") or row.get("title") or "Chantier")
            if (
                area
                and area.lower() not in title.lower()
                and area.lower() not in str(row.get("commune1") or "").lower()
            ):
                # keep if no area filter match on commune
                if area.lower() not in str(row.get("descripchantierinternet") or "").lower():
                    pass  # still include — area filter soft
            out.append(
                RoadEvent(
                    id=str(row.get("gid") or i),
                    type=RoadEventType.ROADWORKS,
                    title=title,
                    description=str(row.get("descripchantierinternet") or ""),
                    start_at=_parse_dt(row.get("debutchantier")),
                    end_at=_parse_dt(row.get("finchantier")),
                )
            )
        return out


class LiveFacilities:
    def __init__(self, dgl: DataGrandLyonClient) -> None:
        self._dgl = dgl

    async def nearby(
        self,
        point: Point,
        *,
        categories: list[FacilityCategory],
        radius_m: float = 1000,
        limit_per_category: int = 5,
    ) -> list[Facility]:
        mapping: dict[FacilityCategory, tuple[str, str, str]] = {
            FacilityCategory.TOILET: (
                "grandlyon",
                "adr_voie_lieu.adrtoilettepublique_latest",
                "adresse",
            ),
            FacilityCategory.DRINKING_WATER: (
                "grandlyon",
                "adr_voie_lieu.adrbornefontaine_latest",
                "identifiant",
            ),
            FacilityCategory.BIKE_PUMP: (
                "grandlyon",
                "pvo_patrimoine_voirie.pvostationvelovpompe",
                "nom",
            ),
        }
        defaults = {
            FacilityCategory.TOILET: "Toilettes",
            FacilityCategory.DRINKING_WATER: "Fontaine",
            FacilityCategory.BIKE_PUMP: "Pompe à vélo",
        }
        out: list[Facility] = []
        counts: dict[str, int] = {}
        for cat in categories:
            if cat not in mapping:
                continue
            service, table, name_key = mapping[cat]
            try:
                rows = await query_table(
                    self._dgl, service=service, schema_table=table, maxfeatures=100
                )
            except Exception:
                continue
            for row in rows:
                lat = _float(row, "lat", "latitude")
                lon = _float(row, "lon", "lng", "longitude")
                if lat is None or lon is None:
                    continue
                d = haversine_m(point, Point(lat, lon))
                if d > radius_m:
                    continue
                if counts.get(cat.value, 0) >= limit_per_category:
                    break
                raw_name = row.get(name_key) or row.get("adresse") or row.get("nom")
                name = str(raw_name).strip() if raw_name else defaults.get(cat, cat.value)
                if name in {"None", cat.value, ""}:
                    name = defaults.get(cat, cat.value)
                out.append(
                    Facility(
                        id=str(row.get("gid") or raw_name or f"{cat.value}-{len(out)}"),
                        name=name,
                        category=cat,
                        latitude=lat,
                        longitude=lon,
                        distance_m=d,
                        open_status=OpenStatus.UNKNOWN,
                        opening_hours_raw=str(
                            row.get("openinghoursspecification") or row.get("horaires") or ""
                        )
                        or None,
                    )
                )
                counts[cat.value] = counts.get(cat.value, 0) + 1
        out.sort(key=lambda f: f.distance_m or 0)
        return out


class LiveWaste:
    def __init__(self, dgl: DataGrandLyonClient) -> None:
        self._dgl = dgl

    async def facilities_near(
        self,
        point: Point,
        *,
        category: WasteCategory,
        radius_m: float = 15000,
        limit: int = 10,
    ) -> list[WasteFacility]:
        try:
            rows = await query_table(
                self._dgl,
                service="grandlyon",
                schema_table="gip_proprete.gipdecheterie_3_0_0",
                maxfeatures=50,
            )
        except Exception:
            return []
        out: list[WasteFacility] = []
        for row in rows:
            lat = _float(row, "lat", "latitude")
            lon = _float(row, "lon", "lng", "longitude")
            if lat is None or lon is None:
                continue
            d = haversine_m(point, Point(lat, lon))
            if d > radius_m:
                continue
            allowed_raw = str(row.get("allowed_waste") or "")
            # déchèteries acceptent la plupart des flux spéciaux
            accepted: list[WasteCategory] = [category] if category != WasteCategory.UNKNOWN else []
            if category == WasteCategory.HOUSEHOLD_WASTE and "ordure" not in allowed_raw.lower():
                # still list — municipal facilities
                pass
            out.append(
                WasteFacility(
                    id=str(row.get("identifiant") or row.get("gid") or row.get("adresse")),
                    name=_facility_label(row),
                    latitude=lat,
                    longitude=lon,
                    accepted_categories=list(accepted) if accepted else [category],
                    distance_m=d,
                    open_status="unknown",
                )
            )
        out.sort(key=lambda f: f.distance_m or 0)
        return out[:limit]


def load_known_source_tables(
    path: object | None = None,
) -> dict[str, dict[str, str]]:
    """Load known tables from versioned config (not Python magic).

    Search order: explicit path → config empaquetée (``_data/config/sources.known.yaml``)
    → cwd-relative → empty (then fallback hardcoded baseline below).
    """
    from pathlib import Path

    import yaml

    from grand_lyon_mcp.resources import config_path

    candidates: list[Path] = []
    if path is not None:
        candidates.append(Path(str(path)))
    # config empaquetée (fonctionne clone et wheel)
    candidates.append(config_path("sources.known.yaml"))
    # cwd-relative (ops installs)
    candidates.append(Path("config/sources.known.yaml"))

    for cand in candidates:
        if not cand.is_file():
            continue
        try:
            data = yaml.safe_load(cand.read_text(encoding="utf-8")) or {}
        except Exception:
            continue
        tables = data.get("tables") if isinstance(data, dict) else None
        if not isinstance(tables, dict):
            continue
        out: dict[str, dict[str, str]] = {}
        for sid, meta in tables.items():
            if not isinstance(meta, dict):
                continue
            service = str(meta.get("service") or "rdata")
            table_name = str(meta.get("table_name") or "")
            if not table_name:
                continue
            out[str(sid)] = {
                "service": service,
                "table_name": table_name,
                "status": str(meta.get("status") or "OK"),
            }
        if out:
            return out
    return dict(_KNOWN_SOURCE_TABLES_FALLBACK)


# Hardcoded baseline if YAML missing (kept for offline bootstrap)
_KNOWN_SOURCE_TABLES_FALLBACK: dict[str, dict[str, str]] = {
    "velov_realtime": {
        "service": "rdata",
        "table_name": "jcd_jcdecaux.jcdvelov",
        "status": "OK",
    },
    "tcl_departures": {
        "service": "rdata",
        "table_name": "tcl_sytral.tclpassagearret",
        "status": "OK",
    },
    "tcl_alerts": {
        "service": "rdata",
        "table_name": "tcl_sytral.tclalertetrafic_2",
        "status": "OK",
    },
    "tcl_accessibility_alerts": {
        "service": "rdata",
        "table_name": "tcl_sytral.tclalerteaccessibilite",
        "status": "OK",
    },
    "parking_realtime": {
        "service": "grandlyon",
        "table_name": "pvo_patrimoine_voirie.pvoparking",
        "status": "OK",
    },
    "park_and_ride": {
        "service": "rdata",
        "table_name": "tcl_sytral.tclparcrelaistr",
        "status": "OK",
    },
    "road_events": {
        "service": "grandlyon",
        "table_name": "pvo_patrimoine_voirie.pvochantierperturbant",
        "status": "OK",
    },
    "toilets": {
        "service": "grandlyon",
        "table_name": "adr_voie_lieu.adrtoilettepublique_latest",
        "status": "OK",
    },
}

# Known resolved sources when full catalog list is forbidden (403)
KNOWN_SOURCE_TABLES: dict[str, dict[str, str]] = load_known_source_tables()
