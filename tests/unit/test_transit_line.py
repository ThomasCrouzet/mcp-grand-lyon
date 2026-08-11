"""Strict transit line normalization and matching."""

from __future__ import annotations

import json
from pathlib import Path

from grand_lyon_mcp.domain.transit import Departure
from grand_lyon_mcp.domain.transit_line import (
    filter_departures_by_line,
    line_matches,
    normalize_line_code,
)
from grand_lyon_mcp.providers.siri.parser import parse_estimated_timetable


def test_normalize_siri_lineref() -> None:
    assert normalize_line_code("ActIV:Line::C12:SYTRAL") == "C12"
    assert normalize_line_code("ActIV:Line::A:SYTRAL") == "A"
    assert normalize_line_code("A") == "A"
    assert normalize_line_code("métro a") == "A"
    assert normalize_line_code("ligne T1") == "T1"
    assert normalize_line_code("tram t2") == "T2"


def test_line_matches_strict_not_substring() -> None:
    # Naive '"a" in "ActIV..."' would be true: we must reject
    assert not line_matches("A", line_name="C12", line_id="ActIV:Line::C12:SYTRAL")
    assert line_matches("A", line_name="A", line_id="ActIV:Line::A:SYTRAL")
    assert line_matches("a", line_name="A", line_id="tcl:A")
    assert line_matches("C12", line_name="C12", line_id="ActIV:Line::C12:SYTRAL")
    assert line_matches("métro a", line_name="A")
    assert not line_matches("B", line_name="A", line_id="tcl:A")


def test_empty_line_matches_all() -> None:
    assert line_matches(None, line_name="C12")
    assert line_matches("", line_name="A")


def test_filter_departures_by_line() -> None:
    deps = [
        Departure(
            line_id="tcl:ActIV:Line::C12:SYTRAL",
            line_name="C12",
            destination="Perrache",
            realtime=True,
        ),
        Departure(
            line_id="tcl:ActIV:Line::A:SYTRAL",
            line_name="A",
            destination="Vaulx",
            realtime=True,
        ),
    ]
    only_a = filter_departures_by_line(deps, "A")
    assert len(only_a) == 1
    assert only_a[0].line_name == "A"
    only_c12 = filter_departures_by_line(deps, "C12")
    assert len(only_c12) == 1
    assert only_c12[0].line_name == "C12"


def test_siri_fixture_pretty_line_and_strict_filter(fixtures_dir: Path) -> None:
    data = json.loads((fixtures_dir / "siri" / "estimated_timetable.json").read_text())
    deps = parse_estimated_timetable(data)
    assert deps
    # fixture includes short "A" and ActIV-style refs
    names = {d.line_name for d in deps}
    assert "A" in names or any(line_matches("A", d.line_name, d.line_id) for d in deps)

    by_a = filter_departures_by_line(deps, "A")
    assert by_a
    assert all(line_matches("A", d.line_name, d.line_id) for d in by_a)
    # filtering for a line not present yields empty (strict)
    by_z = filter_departures_by_line(deps, "Z99")
    assert by_z == []


def test_siri_activ_lineref_fixture(fixtures_dir: Path) -> None:
    """SIRI payload with ActIV LineRef must not match letter noise in provider name."""
    payload = {
        "Siri": {
            "ServiceDelivery": {
                "EstimatedTimetableDelivery": {
                    "EstimatedJourneyVersionFrame": {
                        "EstimatedVehicleJourney": [
                            {
                                "LineRef": "ActIV:Line::C12:SYTRAL",
                                "DestinationName": "Sathonay",
                                "EstimatedCalls": {
                                    "EstimatedCall": [
                                        {
                                            "AimedDepartureTime": "2026-07-20T08:10:00+02:00",
                                            "ExpectedDepartureTime": "2026-07-20T08:10:00+02:00",
                                        }
                                    ]
                                },
                            },
                            {
                                "LineRef": "ActIV:Line::A:SYTRAL",
                                "DestinationName": "Perrache",
                                "EstimatedCalls": {
                                    "EstimatedCall": [
                                        {
                                            "AimedDepartureTime": "2026-07-20T08:12:00+02:00",
                                            "ExpectedDepartureTime": "2026-07-20T08:12:00+02:00",
                                        }
                                    ]
                                },
                            },
                        ]
                    }
                }
            }
        }
    }
    deps = parse_estimated_timetable(payload)
    assert {d.line_name for d in deps} == {"C12", "A"}
    # "A" must not match C12 journey (substring false positive on ActIV)
    only_a = filter_departures_by_line(deps, "A")
    assert len(only_a) == 1
    assert only_a[0].line_name == "A"
    only_c = filter_departures_by_line(deps, "c12")
    assert len(only_c) == 1
