"""Optional live departure smoke (requires credentials + network)."""

from __future__ import annotations

import os

import pytest

from grand_lyon_mcp.domain.common import PlaceRef

pytestmark = [
    pytest.mark.live,
    pytest.mark.skipif(
        os.environ.get("RUN_LIVE_TESTS") != "1",
        reason="Set RUN_LIVE_TESTS=1 to run live tests",
    ),
]


@pytest.mark.asyncio
async def test_live_next_departures_line_a_or_gtfs() -> None:
    from grand_lyon_mcp.bootstrap import build_app
    from grand_lyon_mcp.settings import get_settings

    settings = get_settings()
    if settings.offline or not settings.has_credentials():
        pytest.skip("live credentials required")
    app = await build_app(settings)
    try:
        env = await app.transit.next_departures(
            stop=PlaceRef(query="Bellecour"),
            line="A",
            limit=6,
        )
        deps = env.data.get("departures") or []
        if deps:
            # predominately line A when present
            a_count = sum(1 for d in deps if str(d.get("line_name", "")).upper() == "A")
            assert a_count >= 1 or env.status.value == "partial"
            for d in deps:
                if not d.get("realtime"):
                    # GTFS theoretical
                    assert d.get("realtime") is False
        else:
            assert env.status.value in {"partial", "not_found", "unavailable"}
    finally:
        await app.aclose()
