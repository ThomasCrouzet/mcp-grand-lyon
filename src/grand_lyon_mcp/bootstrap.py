"""Application container / dependency wiring."""

from __future__ import annotations

from contextlib import AsyncExitStack
from dataclasses import dataclass
from pathlib import Path

import aiosqlite

from grand_lyon_mcp.domain.protocols import (
    AccessibilityProvider,
    EnvironmentProvider,
    FacilityProvider,
    ParkingProvider,
    PlaceProvider,
    TrafficProvider,
    TransitAlertProvider,
    TransitRealtimeProvider,
    VelovProvider,
    WasteProvider,
)
from grand_lyon_mcp.infrastructure.http import HttpClient
from grand_lyon_mcp.infrastructure.logging import setup_logging
from grand_lyon_mcp.providers.datagrandlyon.client import DataGrandLyonClient
from grand_lyon_mcp.providers.datagrandlyon.source_registry import SourceRegistry
from grand_lyon_mcp.providers.fixture_providers import (
    FixtureAccessibility,
    FixtureEnvironment,
    FixtureFacilities,
    FixtureParking,
    FixturePlaceProvider,
    FixtureTraffic,
    FixtureTransitAlerts,
    FixtureTransitRealtime,
    FixtureVelov,
    FixtureWaste,
)
from grand_lyon_mcp.providers.gtfs.repository import GtfsRepository
from grand_lyon_mcp.providers.routing.transitous import TransitousPlanner
from grand_lyon_mcp.services.accessibility_service import AccessibilityService
from grand_lyon_mcp.services.briefing_service import BriefingService
from grand_lyon_mcp.services.environment_service import EnvironmentService
from grand_lyon_mcp.services.facility_service import FacilityService
from grand_lyon_mcp.services.journey_service import JourneyService
from grand_lyon_mcp.services.mobility_status_service import MobilityStatusService
from grand_lyon_mcp.services.parking_service import ParkingService
from grand_lyon_mcp.services.place_service import PlaceService
from grand_lyon_mcp.services.transit_service import TransitService
from grand_lyon_mcp.services.velov_service import VelovService
from grand_lyon_mcp.services.waste_service import WasteService, WasteTaxonomy
from grand_lyon_mcp.settings import Settings, get_settings
from grand_lyon_mcp.storage.cache import HttpCache
from grand_lyon_mcp.storage.database import connect_and_migrate
from grand_lyon_mcp.storage.entity_repository import EntityRepository
from grand_lyon_mcp.storage.profile_repository import ProfileRepository
from grand_lyon_mcp.storage.source_health import SourceHealthStore
from grand_lyon_mcp.storage.velov_history import VelovHistoryStore


def default_fixtures_dir() -> Path:
    """Fixtures de démonstration offline empaquetées (fonctionne clone et wheel)."""
    from grand_lyon_mcp.resources import fixtures_dir

    return fixtures_dir()


@dataclass
class AppContainer:
    settings: Settings
    conn: aiosqlite.Connection
    http: HttpClient
    places: PlaceService
    transit: TransitService
    mobility: MobilityStatusService
    velov: VelovService
    parking: ParkingService
    accessibility: AccessibilityService
    facilities: FacilityService
    environment: EnvironmentService
    waste: WasteService
    journeys: JourneyService
    briefing: BriefingService
    source_registry: SourceRegistry
    profiles: ProfileRepository
    entities: EntityRepository
    gtfs: GtfsRepository
    velov_history: VelovHistoryStore
    dgl: DataGrandLyonClient

    async def aclose(self) -> None:
        try:
            await self.http.aclose()
        finally:
            await self.conn.close()


async def build_app(
    settings: Settings | None = None,
    *,
    fixtures_dir: Path | None = None,
    db_path: Path | None = None,
) -> AppContainer:
    settings = settings or get_settings()
    setup_logging(settings.log_level)

    async with AsyncExitStack() as resources:
        conn = await connect_and_migrate(db_path or settings.resolved_db_path())
        resources.push_async_callback(conn.close)
        http = HttpClient(
            connect_timeout=settings.http_connect_timeout_seconds,
            read_timeout=settings.http_read_timeout_seconds,
            max_parallel=settings.max_parallel_requests,
            verify_tls=settings.verify_tls,
            transitous_enabled=settings.transitous_enabled,
            offline=settings.offline,
        )
        resources.push_async_callback(http.aclose)
        app = await _build_services(settings, conn, http, fixtures_dir)
        # The complete container now owns both resources.
        resources.pop_all()
        return app


async def _build_services(
    settings: Settings,
    conn: aiosqlite.Connection,
    http: HttpClient,
    fixtures_dir: Path | None,
) -> AppContainer:
    await HttpCache(conn).prune()
    await SourceHealthStore(conn).prune()
    dgl = DataGrandLyonClient(
        http,
        username=settings.datagrandlyon_username,
        password=settings.datagrandlyon_password,
    )

    entities = EntityRepository(conn)
    profiles = ProfileRepository(conn)
    gtfs = GtfsRepository(conn)
    velov_history = VelovHistoryStore(conn)
    source_registry = SourceRegistry(conn)

    # Load example configs if present in package config or user config
    config_dir = settings.resolved_config_dir()
    await source_registry.load_config(config_dir / "sources.yaml")
    await profiles.load_from_yaml(config_dir / "profiles.yaml")

    # Also load packaged examples into registry if empty
    from grand_lyon_mcp.resources import config_dir as _packaged_config_dir

    packaged_config = _packaged_config_dir()
    if (
        not (await source_registry.list_all())
        and (packaged_config / "sources.example.yaml").is_file()
    ):
        await source_registry.load_config(packaged_config / "sources.example.yaml")
    if (packaged_config / "profiles.example.yaml").is_file():
        # only if no user profiles
        place = await profiles.get_place("home")
        if place is None:
            await profiles.load_from_yaml(packaged_config / "profiles.example.yaml")

    fx = fixtures_dir or settings.fixtures_dir or default_fixtures_dir()
    use_fixtures = settings.offline or not settings.has_credentials()

    # Variables typées par leur Protocol : mypy vérifie la conformité des familles
    # Fixture* et Live* au point de câblage (plus de `type: ignore[arg-type]`).
    photon: PlaceProvider
    realtime: TransitRealtimeProvider
    alerts: TransitAlertProvider
    accessibility_p: AccessibilityProvider
    velov_p: VelovProvider
    parking_p: ParkingProvider
    traffic_p: TrafficProvider
    facilities_p: FacilityProvider
    env_p: EnvironmentProvider | None
    waste_p: WasteProvider

    if use_fixtures:
        photon = FixturePlaceProvider(fx)
        realtime = FixtureTransitRealtime(fx)
        alerts = FixtureTransitAlerts(fx)
        accessibility_p = FixtureAccessibility(fx)
        velov_p = FixtureVelov(fx)
        parking_p = FixtureParking(fx)
        traffic_p = FixtureTraffic(fx)
        facilities_p = FixtureFacilities(fx)
        env_p = FixtureEnvironment(fx)
        waste_p = FixtureWaste(fx)
    else:
        from grand_lyon_mcp.providers.live_providers import (
            KNOWN_SOURCE_TABLES,
            LiveAccessibility,
            LiveFacilities,
            LiveParking,
            LiveTraffic,
            LiveTransitAlerts,
            LiveTransitRealtime,
            LiveVelovProvider,
            LiveWaste,
        )
        from grand_lyon_mcp.providers.photon.client import PhotonClient

        photon = PhotonClient(http)
        realtime = LiveTransitRealtime(dgl, stop_resolver=gtfs)
        alerts = LiveTransitAlerts(dgl)
        accessibility_p = LiveAccessibility(dgl)
        velov_source = await source_registry.get("velov_realtime")
        parking_source = await source_registry.get("parking_realtime")
        velov_p = LiveVelovProvider(
            dgl, cache_ttl_seconds=float(velov_source["ttl_seconds"]) if velov_source else 45
        )
        parking_p = LiveParking(
            dgl,
            entity_lookup=entities,
            cache_ttl_seconds=float(parking_source["ttl_seconds"]) if parking_source else 60,
        )
        traffic_p = LiveTraffic(dgl)
        facilities_p = LiveFacilities(dgl)
        # Pas de source live d'indicateurs environnementaux : provider=None →
        # EnvironmentService dégrade honnêtement (PARTIAL + UNSUPPORTED_INDICATOR)
        # plutôt que de servir des valeurs de fixture comme réelles.
        env_p = None
        waste_p = LiveWaste(dgl)

        # When catalog list is 403, seed known tables as OK from live discovery
        for sid, meta in KNOWN_SOURCE_TABLES.items():
            row = await source_registry.get(sid)
            if row is None:
                await source_registry.upsert(
                    sid,
                    {
                        "provider": "datapusher",
                        "enabled": True,
                        "service": meta["service"],
                        "table_name": meta["table_name"],
                        "status": meta["status"],
                        "realtime": True,
                        "attribution": "Métropole de Lyon / SYTRAL",
                    },
                )
            else:
                await source_registry.set_status(
                    sid,
                    meta["status"],
                    table_name=meta["table_name"],
                )

    places = PlaceService(
        entities=entities,
        profiles=profiles,
        photon=photon,
        gtfs=gtfs,
        fuzzy_threshold=settings.fuzzy_score_threshold,
    )
    transit = TransitService(places=places, realtime=realtime, static=gtfs)
    mobility = MobilityStatusService(
        alerts=alerts,
        accessibility=accessibility_p,
        traffic=traffic_p,
    )
    velov = VelovService(places=places, provider=velov_p, history=velov_history)
    parking = ParkingService(places=places, provider=parking_p)
    accessibility = AccessibilityService(places=places, provider=accessibility_p, gtfs=gtfs)
    facilities = FacilityService(places=places, provider=facilities_p)
    environment = EnvironmentService(places=places, provider=env_p)

    taxonomy_path = config_dir / "waste-taxonomy.yaml"
    if not taxonomy_path.is_file():
        taxonomy_path = packaged_config / "waste-taxonomy.yaml"
    taxonomy = (
        WasteTaxonomy.from_yaml(taxonomy_path)
        if taxonomy_path.is_file()
        else WasteTaxonomy.default()
    )
    waste = WasteService(places=places, taxonomy=taxonomy, provider=waste_p)

    planner = TransitousPlanner(
        http,
        base_url=settings.transitous_base_url,
        enabled=settings.transitous_enabled and not settings.offline,
    )
    journeys = JourneyService(
        places=places,
        planner=planner,
        velov=velov_p,
        parking=parking_p,
        weights=settings.journey_scoring,
    )
    briefing = BriefingService(
        profiles=profiles,
        places=places,
        transit=transit,
        mobility=mobility,
        velov=velov,
    )

    return AppContainer(
        settings=settings,
        conn=conn,
        http=http,
        places=places,
        transit=transit,
        mobility=mobility,
        velov=velov,
        parking=parking,
        accessibility=accessibility,
        facilities=facilities,
        environment=environment,
        waste=waste,
        journeys=journeys,
        briefing=briefing,
        source_registry=source_registry,
        profiles=profiles,
        entities=entities,
        gtfs=gtfs,
        velov_history=velov_history,
        dgl=dgl,
    )
