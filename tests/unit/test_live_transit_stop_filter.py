"""Live transit: stop filter + no unfiltered line fallback (shipped helpers)."""

from __future__ import annotations

from datetime import datetime

import pytest

from grand_lyon_mcp.domain.transit import Departure
from grand_lyon_mcp.domain.transit_line import (
    filter_departures_by_line,
    filter_departures_by_stop,
    stop_matches,
)
from grand_lyon_mcp.infrastructure.time import BUSINESS_TZ
from grand_lyon_mcp.providers.siri.parser import parse_estimated_timetable


def _dep(
    *,
    line: str,
    stop_ref: str | None,
    dest: str = "X",
) -> Departure:
    return Departure(
        line_id=f"tcl:{line}",
        line_name=line,
        destination=dest,
        stop_ref=stop_ref,
        realtime=True,
        expected_at=datetime(2026, 7, 20, 8, 0, tzinfo=BUSINESS_TZ),
    )


def test_stop_matches_by_ref_token() -> None:
    assert stop_matches("gtfs:stop:BEL1", stop_ref="BEL1")
    assert stop_matches("Bellecour", stop_ref="BELLECOUR")
    assert not stop_matches("gtfs:stop:BEL1", stop_ref="PARTDIEU")
    assert not stop_matches("Bellecour", stop_ref=None)  # no identity → fail


def test_stop_matches_spatial_100m() -> None:
    # Bellecour-ish
    assert stop_matches(
        "gtfs:stop:BEL1",
        stop_lat=45.7579,
        stop_lon=4.8321,
        ref_lat=45.7578,
        ref_lon=4.8320,
        radius_m=100.0,
    )
    # far away
    assert not stop_matches(
        "gtfs:stop:BEL1",
        stop_lat=45.80,
        stop_lon=4.90,
        ref_lat=45.7578,
        ref_lon=4.8320,
        radius_m=100.0,
    )


def test_filter_by_stop_drops_wrong_stop() -> None:
    deps = [
        _dep(line="A", stop_ref="Bellecour"),
        _dep(line="A", stop_ref="Part-Dieu"),
        _dep(line="C12", stop_ref=None),  # unknown stop, drop
    ]
    kept = filter_departures_by_stop(deps, "Bellecour")
    assert len(kept) == 1
    assert kept[0].stop_ref == "Bellecour"
    # wrong-stop must not pass through
    assert all(
        (d.stop_ref or "").lower().find("part") < 0 or "bellecour" in (d.stop_ref or "").lower()
        for d in kept
    )


def test_strict_line_empty_does_not_restore_unfiltered() -> None:
    """Mirrors LiveTransitRealtime: empty by_line stays empty."""
    deps = [
        _dep(line="C12", stop_ref="Bellecour"),
        _dep(line="C12", stop_ref="Bellecour", dest="Sathonay"),
    ]
    by_line = filter_departures_by_line(deps, "A")
    # shipped live path must use by_line as-is when empty: never fall back to deps
    filtered = by_line  # correct
    wrong = by_line if by_line else deps  # old buggy pattern
    assert filtered == []
    assert wrong  # documents the bug we removed
    assert filtered != wrong


def test_siri_parser_preserves_stop_ref() -> None:
    payload = {
        "Siri": {
            "ServiceDelivery": {
                "EstimatedTimetableDelivery": {
                    "EstimatedJourneyVersionFrame": {
                        "EstimatedVehicleJourney": [
                            {
                                "LineRef": "ActIV:Line::A:SYTRAL",
                                "DestinationName": "Perrache",
                                "EstimatedCalls": {
                                    "EstimatedCall": [
                                        {
                                            "StopPointRef": "Part-Dieu",
                                            "AimedDepartureTime": "2026-07-20T08:12:00+02:00",
                                            "ExpectedDepartureTime": "2026-07-20T08:12:00+02:00",
                                        },
                                        {
                                            "StopPointRef": "Bellecour",
                                            "AimedDepartureTime": "2026-07-20T08:15:00+02:00",
                                            "ExpectedDepartureTime": "2026-07-20T08:15:00+02:00",
                                        },
                                    ]
                                },
                            }
                        ]
                    }
                }
            }
        }
    }
    deps = parse_estimated_timetable(payload)
    assert {d.stop_ref for d in deps} == {"Part-Dieu", "Bellecour"}
    only_bellecour = filter_departures_by_stop(deps, "Bellecour")
    assert len(only_bellecour) == 1
    assert only_bellecour[0].stop_ref == "Bellecour"


@pytest.mark.asyncio
async def test_live_transit_provider_filters_stop_and_line() -> None:
    """Drive LiveTransitRealtime.get_departures with mocked SIRI, real shipped class."""

    class _FakeSiri:
        async def estimated_timetable(self) -> list[Departure]:
            return [
                _dep(line="A", stop_ref="Part-Dieu", dest="Perrache"),
                _dep(line="A", stop_ref="Bellecour", dest="Perrache"),
                _dep(line="C12", stop_ref="Bellecour", dest="Sathonay"),
            ]

    class _FakeDgl:
        pass

    from grand_lyon_mcp.providers.live_providers import LiveTransitRealtime

    prov = LiveTransitRealtime(_FakeDgl(), use_siri=True)  # type: ignore[arg-type]
    prov._siri = _FakeSiri()  # type: ignore[assignment]

    # stop Bellecour + line A → only matching call
    out = await prov.get_departures("Bellecour", line="A", limit=10)
    assert len(out) == 1
    assert out[0].line_name == "A"
    assert out[0].stop_ref == "Bellecour"

    # wrong stop must yield empty (GTFS can win upstream)
    out2 = await prov.get_departures("Vaulx-en-Velin", line="A", limit=10)
    assert out2 == []

    # line filter empty stays empty (no unfiltered C12)
    out3 = await prov.get_departures("Bellecour", line="Z99", limit=10)
    assert out3 == []
