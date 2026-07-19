"""Edge cases for transit line normalization."""

from __future__ import annotations

from grand_lyon_mcp.domain.transit_line import line_matches, line_tokens, normalize_line_code


def test_normalize_empty_and_noise() -> None:
    assert normalize_line_code(None) == ""
    assert normalize_line_code("") == ""
    assert normalize_line_code("  ") == ""
    assert normalize_line_code("bus C12") == "C12"
    assert normalize_line_code("line A") == "A"
    assert normalize_line_code("tramway t3") == "T3"


def test_line_tokens_strips_noise() -> None:
    tokens = line_tokens("ActIV:Line::C12:SYTRAL")
    assert "c12" in tokens
    assert "activ" not in tokens
    assert "sytral" not in tokens
    assert line_tokens(None) == set()
    assert line_tokens("") == set()


def test_match_id_only() -> None:
    assert line_matches("T1", line_name="", line_id="tcl:T1")
    assert not line_matches("T1", line_name="T2", line_id="tcl:T2")
