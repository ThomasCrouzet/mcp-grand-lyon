"""DataPusher record extraction."""

from __future__ import annotations

from grand_lyon_mcp.providers.datagrandlyon.datapusher import extract_records


def test_extract_values() -> None:
    data = {"values": [{"a": 1}, {"a": 2}]}
    assert len(extract_records(data)) == 2


def test_extract_features() -> None:
    data = {
        "features": [
            {"properties": {"name": "x"}, "geometry": {"type": "Point"}},
        ]
    }
    recs = extract_records(data)
    assert recs[0]["name"] == "x"
    assert "_geometry" in recs[0]
