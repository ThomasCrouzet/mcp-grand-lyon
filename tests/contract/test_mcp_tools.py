"""MCP contract: ten tools, no admin, envelope shape, unknown fields rejected."""

from __future__ import annotations

import pytest

from grand_lyon_mcp.adapters.mcp.tools import (
    ADMIN_TOOL_NAMES,
    PUBLIC_TOOL_NAMES,
    dispatch_tool,
    tool_input_models,
)


def test_exactly_ten_public_tools() -> None:
    assert len(PUBLIC_TOOL_NAMES) == 10
    assert all(n.startswith("lyon_") for n in PUBLIC_TOOL_NAMES)


def test_no_admin_in_public() -> None:
    public = set(PUBLIC_TOOL_NAMES)
    for admin in ADMIN_TOOL_NAMES:
        assert admin not in public


def test_all_tools_have_input_models() -> None:
    models = tool_input_models()
    assert set(models) == set(PUBLIC_TOOL_NAMES)


@pytest.mark.asyncio
async def test_dispatch_all_tools_envelope(app_container) -> None:
    """Drive real dispatch_tool for every public tool (offline)."""
    required_keys = {
        "schema_version",
        "request_id",
        "status",
        "generated_at",
        "summary",
        "data",
        "sources",
        "warnings",
        "degraded",
    }
    payloads = {
        "lyon_resolve_place": {"query": "Part-Dieu", "limit": 3},
        "lyon_next_departures": {"stop": {"query": "Bellecour"}, "line": "A", "limit": 3},
        "lyon_mobility_status": {"lines": ["A"], "include": ["transit", "traffic"]},
        "lyon_trip_options": {
            "origin": {"profile_place": "home"},
            "destination": {"profile_place": "work"},
            "modes": ["walk", "velov", "tcl"],
        },
        "lyon_parking_options": {
            "destination": {"query": "Hôtel de Ville"},
            "limit": 5,
        },
        "lyon_accessibility_check": {
            "origin": {"query": "Bellecour"},
            "destination": {"query": "Part-Dieu"},
            "needs": ["wheelchair"],
        },
        "lyon_nearby_facilities": {
            "location": {"latitude": 45.777, "longitude": 4.855},
            "categories": ["toilet", "park"],
            "radius_m": 2000,
        },
        "lyon_environment_brief": {
            "location": {"query": "Lyon"},
            "indicators": ["pollen", "heat"],
        },
        "lyon_waste_dropoff": {
            "item": "batterie de vélo électrique",
            "location": {"profile_place": "home"},
        },
        "lyon_personal_briefing": {"profile": "weekday_morning"},
    }
    for name in PUBLIC_TOOL_NAMES:
        result = await dispatch_tool(app_container, name, payloads[name])
        missing = required_keys - set(result)
        assert not missing, f"{name} missing {missing}"
        assert result["schema_version"] == "1.0"
        assert result["status"] in {
            "ok",
            "partial",
            "not_found",
            "ambiguous",
            "unavailable",
            "invalid_request",
        }
        # serializable
        import json

        json.dumps(result, default=str)


@pytest.mark.asyncio
async def test_reject_unknown_via_dispatch(app_container) -> None:
    result = await dispatch_tool(app_container, "lyon_resolve_place", {"query": "x", "evil": True})
    assert result["status"] == "invalid_request"


@pytest.mark.asyncio
async def test_parking_type_errors_are_invalid_requests(app_container) -> None:
    invalid = await dispatch_tool(
        app_container,
        "lyon_parking_options",
        {"destination": {"query": "Hôtel de Ville"}, "types": ["unsupported"]},
    )
    assert invalid["status"] == "invalid_request"
    valid = await dispatch_tool(
        app_container,
        "lyon_parking_options",
        {"destination": {"query": "Hôtel de Ville"}, "types": ["public_parking"]},
    )
    assert valid["status"] in {"ok", "partial"}
    assert valid["data"]["options"]
    assert all(option["type"] == "public_parking" for option in valid["data"]["options"])


@pytest.mark.asyncio
async def test_tool_input_schemas_stable(app_container) -> None:
    """Verrouille les arguments exposés (noms + required) contre une dérive d'API MCP."""
    from grand_lyon_mcp.adapters.mcp.server import create_mcp_server

    expected: dict[str, set[str]] = {
        "lyon_resolve_place": {"query", "near", "types", "limit"},
        "lyon_next_departures": {"stop", "line", "direction", "at", "limit"},
        "lyon_mobility_status": {"lines", "areas", "include"},
        "lyon_trip_options": {
            "origin",
            "destination",
            "departure_at",
            "arrival_before",
            "modes",
            "preferences",
        },
        "lyon_parking_options": {"destination", "types", "radius_m", "minimum_spaces", "limit"},
        "lyon_accessibility_check": {"origin", "destination", "stops", "needs"},
        "lyon_nearby_facilities": {
            "location",
            "categories",
            "radius_m",
            "open_at",
            "limit_per_category",
        },
        "lyon_environment_brief": {"location", "at", "indicators"},
        "lyon_waste_dropoff": {"item", "location", "transport", "open_at", "radius_m", "limit"},
        "lyon_personal_briefing": {"profile", "at", "compare_with_previous"},
    }
    mcp = create_mcp_server(app_container)
    tools = {t.name: t for t in await mcp.list_tools()}
    for name, args in expected.items():
        props = set(tools[name].inputSchema.get("properties", {}))
        assert props == args, f"{name}: dérive de schéma {props ^ args}"

    required = {t.name: set(t.inputSchema.get("required", [])) for t in tools.values()}
    assert "stop" in required["lyon_next_departures"]
    assert {"origin", "destination"} <= required["lyon_trip_options"]
    assert "item" in required["lyon_waste_dropoff"]
    assert "categories" in required["lyon_nearby_facilities"]
    assert "profile" in required["lyon_personal_briefing"]


@pytest.mark.asyncio
async def test_list_tools_via_fastmcp(app_container) -> None:
    """Use real FastMCP server list if available."""
    from grand_lyon_mcp.adapters.mcp.server import create_mcp_server
    from grand_lyon_mcp.adapters.mcp.tools import PUBLIC_TOOL_NAMES

    mcp = create_mcp_server(app_container)
    tools = await mcp.list_tools()
    names = {t.name for t in tools}
    assert set(PUBLIC_TOOL_NAMES) <= names or set(PUBLIC_TOOL_NAMES) == names
    for admin in (
        "admin_list_datasets",
        "admin_query_raw_dataset",
        "datagrandlyon_query",
    ):
        assert admin not in names
