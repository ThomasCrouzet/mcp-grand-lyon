"""SIRI parser."""

from __future__ import annotations

import json
from pathlib import Path

from grand_lyon_mcp.providers.siri.parser import parse_estimated_timetable, parse_situation_exchange


def test_parse_et(fixtures_dir: Path) -> None:
    data = json.loads((fixtures_dir / "siri" / "estimated_timetable.json").read_text())
    deps = parse_estimated_timetable(data)
    assert deps
    assert deps[0].realtime is True
    assert deps[0].line_name == "A"


def test_parse_sx(fixtures_dir: Path) -> None:
    data = json.loads((fixtures_dir / "siri" / "situation_exchange.json").read_text())
    alerts = parse_situation_exchange(data)
    assert alerts
    assert "A" in alerts[0].title or alerts[0].title
