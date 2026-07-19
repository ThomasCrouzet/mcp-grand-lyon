"""MCP tool input models and registration helpers (SDK types only here)."""

from __future__ import annotations

from datetime import datetime
from typing import Any

from pydantic import BaseModel, ConfigDict, Field

from grand_lyon_mcp.domain.common import PlaceRef
from grand_lyon_mcp.infrastructure.logging import get_logger

logger = get_logger("tools")

# Exactly the ten public tools
PUBLIC_TOOL_NAMES: tuple[str, ...] = (
    "lyon_resolve_place",
    "lyon_next_departures",
    "lyon_mobility_status",
    "lyon_trip_options",
    "lyon_parking_options",
    "lyon_accessibility_check",
    "lyon_nearby_facilities",
    "lyon_environment_brief",
    "lyon_waste_dropoff",
    "lyon_personal_briefing",
)

ADMIN_TOOL_NAMES: tuple[str, ...] = (
    "admin_list_datasets",
    "admin_query_raw_dataset",
    "admin_refresh_cache",
    "admin_clear_cache",
    "admin_source_health",
    "admin_dump_provider_response",
    "admin_reload_registry",
    "admin_record_fixture",
    "datagrandlyon_query",
)


class StrictModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class ResolvePlaceInput(StrictModel):
    query: str | None = Field(default=None, max_length=200)
    near: PlaceRef | None = None
    types: list[str] | None = Field(default=None, max_length=20)
    limit: int = Field(default=5, ge=1, le=20)


class NextDeparturesInput(StrictModel):
    stop: PlaceRef
    line: str | None = Field(default=None, max_length=32)
    direction: str | None = Field(default=None, max_length=100)
    at: datetime | None = None
    limit: int = Field(default=6, ge=1, le=20)


class MobilityStatusInput(StrictModel):
    lines: list[str] | None = Field(default=None, max_length=20)
    areas: list[PlaceRef] | None = Field(default=None, max_length=10)
    include: list[str] | None = Field(default=None, max_length=10)


class TripPreferences(StrictModel):
    max_walking_m: int = Field(default=800, ge=0, le=5000)
    minimum_velov_bikes: int = Field(default=3, ge=0, le=50)
    minimum_velov_docks: int = Field(default=3, ge=0, le=50)
    avoid_disruptions: bool = True
    wheelchair: bool = False


class TripOptionsInput(StrictModel):
    origin: PlaceRef
    destination: PlaceRef
    departure_at: datetime | None = None
    arrival_before: datetime | None = None
    modes: list[str] | None = Field(default=None, max_length=10)
    preferences: TripPreferences | None = None


class ParkingOptionsInput(StrictModel):
    destination: PlaceRef
    types: list[str] | None = Field(default=None, max_length=5)
    radius_m: int = Field(default=1500, ge=50, le=5000)
    minimum_spaces: int = Field(default=0, ge=0, le=500)
    limit: int = Field(default=10, ge=1, le=20)


class AccessibilityCheckInput(StrictModel):
    origin: PlaceRef | None = None
    destination: PlaceRef | None = None
    stops: list[PlaceRef] | None = Field(default=None, max_length=20)
    needs: list[str] | None = Field(default=None, max_length=10)


class NearbyFacilitiesInput(StrictModel):
    location: PlaceRef
    categories: list[str] = Field(max_length=20)
    radius_m: int = Field(default=1000, ge=50, le=5000)
    open_at: datetime | None = None
    limit_per_category: int = Field(default=5, ge=1, le=20)


class EnvironmentBriefInput(StrictModel):
    location: PlaceRef
    at: datetime | None = None
    indicators: list[str] | None = Field(default=None, max_length=10)


class WasteDropoffInput(StrictModel):
    item: str = Field(min_length=1, max_length=200)
    location: PlaceRef | None = None
    transport: str = Field(default="car", max_length=20)
    open_at: datetime | None = None
    radius_m: int = Field(default=15000, ge=100, le=50000)
    limit: int = Field(default=10, ge=1, le=20)


class PersonalBriefingInput(StrictModel):
    profile: str = Field(min_length=1, max_length=64)
    at: datetime | None = None
    compare_with_previous: bool = True


TOOL_DESCRIPTIONS: dict[str, str] = {
    "lyon_resolve_place": (
        "Résout une adresse, un arrêt TCL, une station Vélo'v, un quartier ou un lieu "
        "personnel dans la Métropole de Lyon. Utilisez cet outil avant les autres lorsqu'un "
        "lieu est ambigu. Ne calcule pas d'itinéraire."
    ),
    "lyon_next_departures": (
        "Retourne les prochains passages TCL à un arrêt ou une station. "
        "Pour les horaires immédiats uniquement. Pour un trajet complet, "
        "utilisez lyon_trip_options."
    ),
    "lyon_mobility_status": (
        "État des transports et de la circulation : alertes TCL, accessibilité, trafic, "
        "chantiers. Pas pour les prochains passages d'un arrêt précis."
    ),
    "lyon_trip_options": (
        "Compare des modes de déplacement (TCL, Vélo'v, P+R, voiture, marche) entre deux lieux. "
        "Ne remplace pas lyon_next_departures pour un seul arrêt."
    ),
    "lyon_parking_options": (
        "Parkings publics et parcs relais près d'une destination, "
        "avec places disponibles si connues."
    ),
    "lyon_accessibility_check": (
        "Vérifie l'accessibilité (fauteuil, sans marche) d'arrêts ou d'un trajet. "
        "L'absence de donnée n'est jamais assimilée à accessible."
    ),
    "lyon_nearby_facilities": (
        "Équipements urbains à proximité : toilettes, fontaines, parcs, "
        "pompes à vélo, stations Vélo'v."
    ),
    "lyon_environment_brief": (
        "Indicateurs environnementaux disponibles (pollen, qualité de l'air, "
        "chaleur) pour une zone. Les indicateurs sans source sont signalés "
        "sans bloquer les autres."
    ),
    "lyon_waste_dropoff": (
        "Classe un objet à jeter (taxonomie déterministe) et propose des "
        "déchèteries / points de collecte."
    ),
    "lyon_personal_briefing": (
        "Briefing personnel selon un profil local (trajet habituel, alertes, Vélo'v). "
        "N'envoie aucune notification : le déclenchement et la livraison sont à la "
        "charge du client MCP appelant."
    ),
}


def tool_input_models() -> dict[str, type[BaseModel]]:
    return {
        "lyon_resolve_place": ResolvePlaceInput,
        "lyon_next_departures": NextDeparturesInput,
        "lyon_mobility_status": MobilityStatusInput,
        "lyon_trip_options": TripOptionsInput,
        "lyon_parking_options": ParkingOptionsInput,
        "lyon_accessibility_check": AccessibilityCheckInput,
        "lyon_nearby_facilities": NearbyFacilitiesInput,
        "lyon_environment_brief": EnvironmentBriefInput,
        "lyon_waste_dropoff": WasteDropoffInput,
        "lyon_personal_briefing": PersonalBriefingInput,
    }


async def dispatch_tool(app: Any, name: str, arguments: dict[str, Any]) -> dict[str, Any]:
    """Validate args and call the matching service. Returns envelope dict."""
    from grand_lyon_mcp.adapters.mcp.serializers import envelope_to_dict
    from grand_lyon_mcp.domain.common import ResultStatus, make_envelope
    from grand_lyon_mcp.infrastructure.time import now_paris

    models = tool_input_models()
    if name not in models:
        env = make_envelope(
            status=ResultStatus.INVALID_REQUEST,
            generated_at=now_paris(),
            summary=f"Outil inconnu: {name}",
            data={},
        )
        return envelope_to_dict(env)

    try:
        payload = models[name].model_validate(arguments)
    except Exception as exc:
        env = make_envelope(
            status=ResultStatus.INVALID_REQUEST,
            generated_at=now_paris(),
            summary=f"Requête invalide: {exc}",
            data={},
        )
        return envelope_to_dict(env)

    try:
        if name == "lyon_resolve_place":
            p = payload
            assert isinstance(p, ResolvePlaceInput)
            place = PlaceRef(query=p.query) if p.query else None
            result = await app.places.resolve_place(
                query=p.query, place=place, near=p.near, types=p.types, limit=p.limit
            )
        elif name == "lyon_next_departures":
            p = payload
            assert isinstance(p, NextDeparturesInput)
            result = await app.transit.next_departures(
                stop=p.stop, line=p.line, direction=p.direction, at=p.at, limit=p.limit
            )
        elif name == "lyon_mobility_status":
            p = payload
            assert isinstance(p, MobilityStatusInput)
            result = await app.mobility.status(lines=p.lines, areas=p.areas, include=p.include)
        elif name == "lyon_trip_options":
            p = payload
            assert isinstance(p, TripOptionsInput)
            prefs = p.preferences.model_dump() if p.preferences else None
            result = await app.journeys.options(
                origin=p.origin,
                destination=p.destination,
                departure_at=p.departure_at,
                arrival_before=p.arrival_before,
                modes=p.modes,
                preferences=prefs,
            )
        elif name == "lyon_parking_options":
            p = payload
            assert isinstance(p, ParkingOptionsInput)
            result = await app.parking.options(
                destination=p.destination,
                types=p.types,
                radius_m=p.radius_m,
                minimum_spaces=p.minimum_spaces,
                limit=p.limit,
            )
        elif name == "lyon_accessibility_check":
            p = payload
            assert isinstance(p, AccessibilityCheckInput)
            result = await app.accessibility.check(
                origin=p.origin, destination=p.destination, stops=p.stops, needs=p.needs
            )
        elif name == "lyon_nearby_facilities":
            p = payload
            assert isinstance(p, NearbyFacilitiesInput)
            result = await app.facilities.nearby(
                location=p.location,
                categories=p.categories,
                radius_m=p.radius_m,
                open_at=p.open_at,
                limit_per_category=p.limit_per_category,
            )
        elif name == "lyon_environment_brief":
            p = payload
            assert isinstance(p, EnvironmentBriefInput)
            result = await app.environment.brief(
                location=p.location, at=p.at, indicators=p.indicators
            )
        elif name == "lyon_waste_dropoff":
            p = payload
            assert isinstance(p, WasteDropoffInput)
            result = await app.waste.dropoff(
                item=p.item,
                location=p.location,
                transport=p.transport,
                open_at=p.open_at,
                radius_m=p.radius_m,
                limit=p.limit,
            )
        elif name == "lyon_personal_briefing":
            p = payload
            assert isinstance(p, PersonalBriefingInput)
            result = await app.briefing.personal_briefing(
                profile=p.profile, at=p.at, compare_with_previous=p.compare_with_previous
            )
        else:
            result = make_envelope(
                status=ResultStatus.INVALID_REQUEST,
                generated_at=now_paris(),
                summary=f"Outil non implémenté: {name}",
                data={},
            )
        return envelope_to_dict(result)
    except Exception as exc:
        from grand_lyon_mcp.infrastructure.redaction import redact_string

        env = make_envelope(
            status=ResultStatus.UNAVAILABLE,
            generated_at=now_paris(),
            summary="Erreur interne sécurisée.",
            data={},
            warnings=[],
            degraded=True,
        )
        # Trace redigée (aucun secret) : l'erreur reste sur stderr, la sortie MCP est neutre.
        logger.warning(
            "tool_error",
            extra={
                "tool_name": name,
                "result_status": "unavailable",
                "error": redact_string(str(exc)),
            },
        )
        return envelope_to_dict(env)
