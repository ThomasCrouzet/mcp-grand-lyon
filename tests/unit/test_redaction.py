"""Secret redaction unit tests."""

from __future__ import annotations

from grand_lyon_mcp.infrastructure.redaction import REDACTED, redact_mapping, redact_string


def test_redact_authorization_header() -> None:
    s = "Authorization: Basic dXNlcjpwYXNz"
    out = redact_string(s)
    assert "dXNlcjpwYXNz" not in out
    assert REDACTED in out


def test_redact_password_field() -> None:
    s = "DATAGRANDLYON_PASSWORD=supersecret123"
    out = redact_string(s)
    assert "supersecret123" not in out
    assert REDACTED in out


def test_redact_extra_secrets() -> None:
    out = redact_string("user=alice token=xyz", extra_secrets=["alice"])
    assert "alice" not in out
    assert REDACTED in out


def test_redact_mapping_nested() -> None:
    data = {
        "Authorization": "Bearer tok",
        "nested": {"password": "p@ss", "ok": "value"},
    }
    out = redact_mapping(data)
    assert out["Authorization"] == REDACTED
    assert out["nested"]["password"] == REDACTED
    assert out["nested"]["ok"] == "value"
