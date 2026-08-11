"""MCP stdio server, only place that imports the MCP SDK."""

from __future__ import annotations

import json
from typing import Any

from mcp.server.fastmcp import FastMCP

from grand_lyon_mcp.adapters.mcp.tools import (
    PUBLIC_TOOL_NAMES,
    TOOL_DESCRIPTIONS,
    dispatch_tool,
    tool_input_models,
)
from grand_lyon_mcp.bootstrap import AppContainer, build_app
from grand_lyon_mcp.version import __version__

# Module-level container set at serve time
_app: AppContainer | None = None
_quiet_banner: bool = False


def get_app() -> AppContainer:
    if _app is None:
        raise RuntimeError("App container not initialized")
    return _app


def create_mcp_server(app: AppContainer) -> FastMCP:
    global _app
    _app = app
    mcp = FastMCP(
        name="grand-lyon-mcp",
        instructions=(
            "Serveur MCP local pour les services de la Métropole de Lyon "
            f"(v{__version__}). Outils métier en lecture seule."
        ),
    )

    models = tool_input_models()

    # Register each tool explicitly so list_tools is stable
    @mcp.tool(
        name="lyon_resolve_place",
        description=TOOL_DESCRIPTIONS["lyon_resolve_place"],
    )
    async def lyon_resolve_place(
        query: str | None = None,
        near: dict[str, Any] | None = None,
        types: list[str] | None = None,
        limit: int = 5,
    ) -> dict[str, Any]:
        args: dict[str, Any] = {"limit": limit}
        if query is not None:
            args["query"] = query
        if near is not None:
            args["near"] = near
        if types is not None:
            args["types"] = types
        return await dispatch_tool(get_app(), "lyon_resolve_place", args)

    @mcp.tool(
        name="lyon_next_departures",
        description=TOOL_DESCRIPTIONS["lyon_next_departures"],
    )
    async def lyon_next_departures(
        stop: dict[str, Any],
        line: str | None = None,
        direction: str | None = None,
        at: str | None = None,
        limit: int = 6,
    ) -> dict[str, Any]:
        args: dict[str, Any] = {"stop": stop, "limit": limit}
        if line is not None:
            args["line"] = line
        if direction is not None:
            args["direction"] = direction
        if at is not None:
            args["at"] = at
        return await dispatch_tool(get_app(), "lyon_next_departures", args)

    @mcp.tool(
        name="lyon_mobility_status",
        description=TOOL_DESCRIPTIONS["lyon_mobility_status"],
    )
    async def lyon_mobility_status(
        lines: list[str] | None = None,
        areas: list[dict[str, Any]] | None = None,
        include: list[str] | None = None,
    ) -> dict[str, Any]:
        args: dict[str, Any] = {}
        if lines is not None:
            args["lines"] = lines
        if areas is not None:
            args["areas"] = areas
        if include is not None:
            args["include"] = include
        return await dispatch_tool(get_app(), "lyon_mobility_status", args)

    @mcp.tool(
        name="lyon_trip_options",
        description=TOOL_DESCRIPTIONS["lyon_trip_options"],
    )
    async def lyon_trip_options(
        origin: dict[str, Any],
        destination: dict[str, Any],
        departure_at: str | None = None,
        arrival_before: str | None = None,
        modes: list[str] | None = None,
        preferences: dict[str, Any] | None = None,
    ) -> dict[str, Any]:
        args: dict[str, Any] = {"origin": origin, "destination": destination}
        if departure_at is not None:
            args["departure_at"] = departure_at
        if arrival_before is not None:
            args["arrival_before"] = arrival_before
        if modes is not None:
            args["modes"] = modes
        if preferences is not None:
            args["preferences"] = preferences
        return await dispatch_tool(get_app(), "lyon_trip_options", args)

    @mcp.tool(
        name="lyon_parking_options",
        description=TOOL_DESCRIPTIONS["lyon_parking_options"],
    )
    async def lyon_parking_options(
        destination: dict[str, Any],
        types: list[str] | None = None,
        radius_m: int = 1500,
        minimum_spaces: int = 0,
        limit: int = 10,
    ) -> dict[str, Any]:
        return await dispatch_tool(
            get_app(),
            "lyon_parking_options",
            {
                "destination": destination,
                "types": types,
                "radius_m": radius_m,
                "minimum_spaces": minimum_spaces,
                "limit": limit,
            },
        )

    @mcp.tool(
        name="lyon_accessibility_check",
        description=TOOL_DESCRIPTIONS["lyon_accessibility_check"],
    )
    async def lyon_accessibility_check(
        origin: dict[str, Any] | None = None,
        destination: dict[str, Any] | None = None,
        stops: list[dict[str, Any]] | None = None,
        needs: list[str] | None = None,
    ) -> dict[str, Any]:
        args: dict[str, Any] = {}
        if origin is not None:
            args["origin"] = origin
        if destination is not None:
            args["destination"] = destination
        if stops is not None:
            args["stops"] = stops
        if needs is not None:
            args["needs"] = needs
        return await dispatch_tool(get_app(), "lyon_accessibility_check", args)

    @mcp.tool(
        name="lyon_nearby_facilities",
        description=TOOL_DESCRIPTIONS["lyon_nearby_facilities"],
    )
    async def lyon_nearby_facilities(
        location: dict[str, Any],
        categories: list[str],
        radius_m: int = 1000,
        open_at: str | None = None,
        limit_per_category: int = 5,
    ) -> dict[str, Any]:
        args: dict[str, Any] = {
            "location": location,
            "categories": categories,
            "radius_m": radius_m,
            "limit_per_category": limit_per_category,
        }
        if open_at is not None:
            args["open_at"] = open_at
        return await dispatch_tool(get_app(), "lyon_nearby_facilities", args)

    @mcp.tool(
        name="lyon_environment_brief",
        description=TOOL_DESCRIPTIONS["lyon_environment_brief"],
    )
    async def lyon_environment_brief(
        location: dict[str, Any],
        at: str | None = None,
        indicators: list[str] | None = None,
    ) -> dict[str, Any]:
        args: dict[str, Any] = {"location": location}
        if at is not None:
            args["at"] = at
        if indicators is not None:
            args["indicators"] = indicators
        return await dispatch_tool(get_app(), "lyon_environment_brief", args)

    @mcp.tool(
        name="lyon_waste_dropoff",
        description=TOOL_DESCRIPTIONS["lyon_waste_dropoff"],
    )
    async def lyon_waste_dropoff(
        item: str,
        location: dict[str, Any] | None = None,
        transport: str = "car",
        open_at: str | None = None,
        radius_m: int = 15000,
        limit: int = 10,
    ) -> dict[str, Any]:
        args: dict[str, Any] = {
            "item": item,
            "transport": transport,
            "radius_m": radius_m,
            "limit": limit,
        }
        if location is not None:
            args["location"] = location
        if open_at is not None:
            args["open_at"] = open_at
        return await dispatch_tool(get_app(), "lyon_waste_dropoff", args)

    @mcp.tool(
        name="lyon_personal_briefing",
        description=TOOL_DESCRIPTIONS["lyon_personal_briefing"],
    )
    async def lyon_personal_briefing(
        profile: str,
        at: str | None = None,
        compare_with_previous: bool = True,
    ) -> dict[str, Any]:
        args: dict[str, Any] = {
            "profile": profile,
            "compare_with_previous": compare_with_previous,
        }
        if at is not None:
            args["at"] = at
        return await dispatch_tool(get_app(), "lyon_personal_briefing", args)

    # Silence unused in type checkers
    _ = models
    _ = PUBLIC_TOOL_NAMES
    return mcp


def _banner_stderr(app: AppContainer) -> None:
    """Human-readable status on stderr only (stdout is MCP protocol)."""
    import sys

    if _quiet_banner:
        return
    settings = app.settings
    mode = "offline (fixtures)" if settings.offline else "live"
    creds = "oui" if settings.has_credentials() else "non"
    lines = [
        f"grand-lyon-mcp v{__version__}, serveur MCP stdio prêt",
        f"  mode          : {mode}",
        f"  credentials   : {creds}",
        f"  outils        : {len(PUBLIC_TOOL_NAMES)} ({', '.join(PUBLIC_TOOL_NAMES[:3])}, …)",
        f"  db            : {settings.resolved_db_path()}",
        f"  log_level     : {settings.log_level}",
        "  stdout        : réservé au protocole MCP (ne pas y écrire)",
        "  logs          : stderr (JSON)",
        "  arrêt         : Ctrl+C ou fermeture du client MCP",
        "",
        "En attente d'un client MCP (Claude Desktop, Cursor, …) sur stdin/stdout…",
        "Pour un test local sans client :  make smoke   ou   grand-lyon-mcp smoke",
    ]
    if sys.stdin.isatty():
        lines.extend(
            [
                "",
                "⚠  stdin est un terminal interactif.",
                "   `make serve` ne « affiche » rien d'utile ici : c'est normal.",
                "   Le client MCP doit lancer ce binaire lui-même (voir: make client-config).",
                "   Test rapide des outils : make smoke",
            ]
        )
    sys.stderr.write("\n".join(lines) + "\n")
    sys.stderr.flush()


async def run_stdio() -> None:
    """Entry for `grand-lyon-mcp serve --transport stdio`."""
    app = await build_app()
    _banner_stderr(app)
    mcp = create_mcp_server(app)
    try:
        await mcp.run_stdio_async()
    finally:
        await app.aclose()


def list_public_tools_static() -> list[str]:
    return list(PUBLIC_TOOL_NAMES)


def dump_tool_meta() -> str:
    return json.dumps(
        {
            "tools": list(PUBLIC_TOOL_NAMES),
            "descriptions": TOOL_DESCRIPTIONS,
            "version": __version__,
        },
        ensure_ascii=False,
        indent=2,
    )
