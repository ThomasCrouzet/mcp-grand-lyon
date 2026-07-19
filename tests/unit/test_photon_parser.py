"""Photon parser."""

from __future__ import annotations

import json
from pathlib import Path

from grand_lyon_mcp.providers.photon.parser import parse_photon_response


def test_parse_photon(fixtures_dir: Path) -> None:
    data = json.loads((fixtures_dir / "photon" / "part_dieu.json").read_text())
    results = parse_photon_response(data)
    assert results
    assert "Part-Dieu" in results[0].name or "Part" in results[0].name
    assert results[0].id.startswith("photon:")
