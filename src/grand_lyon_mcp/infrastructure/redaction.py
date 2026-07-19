"""Secret redaction for logs and error messages."""

from __future__ import annotations

import re
from typing import Any

REDACTED = "[REDACTED]"

# Keys / header names that must never appear in cleartext values
_SENSITIVE_KEYS = frozenset(
    {
        "authorization",
        "proxy-authorization",
        "cookie",
        "set-cookie",
        "datagrandlyon_username",
        "datagrandlyon_password",
        "password",
        "secret",
        "token",
        "api_key",
        "apikey",
    }
)

_SENSITIVE_PATTERNS = [
    # Full Authorization / Proxy-Authorization header values (incl. "Basic xxx")
    re.compile(r"(?i)(authorization\s*[:=]\s*)(.+)"),
    re.compile(r"(?i)(proxy-authorization\s*[:=]\s*)(.+)"),
    re.compile(r"(?i)(cookie\s*[:=]\s*)([^\n]+)"),
    re.compile(r"(?i)(set-cookie\s*[:=]\s*)([^\n]+)"),
    re.compile(r"(?i)(datagrandlyon_username\s*[:=]\s*)(\S+)"),
    re.compile(r"(?i)(datagrandlyon_password\s*[:=]\s*)(\S+)"),
    re.compile(r"(?i)(password\s*[:=]\s*)(\S+)"),
    re.compile(r"(?i)(secret\s*[:=]\s*)(\S+)"),
    re.compile(r"(?i)(token\s*[:=]\s*)(\S+)"),
    re.compile(r"(?i)\b(basic\s+)([a-zA-Z0-9+/=]{8,})"),
]


def redact_string(value: str, extra_secrets: list[str] | None = None) -> str:
    """Replace sensitive substrings with [REDACTED]."""
    out = value
    for pattern in _SENSITIVE_PATTERNS:
        out = pattern.sub(rf"\1{REDACTED}", out)
    if extra_secrets:
        for secret in extra_secrets:
            if secret and len(secret) >= 2:
                out = out.replace(secret, REDACTED)
    return out


def redact_mapping(data: dict[str, Any], extra_secrets: list[str] | None = None) -> dict[str, Any]:
    """Deep-copy-ish redaction of dict values for sensitive keys."""
    result: dict[str, Any] = {}
    for key, value in data.items():
        if key.lower() in _SENSITIVE_KEYS:
            result[key] = REDACTED
        elif isinstance(value, dict):
            result[key] = redact_mapping(value, extra_secrets)
        elif isinstance(value, str):
            result[key] = redact_string(value, extra_secrets)
        elif isinstance(value, list):
            result[key] = [
                redact_mapping(v, extra_secrets)
                if isinstance(v, dict)
                else redact_string(v, extra_secrets)
                if isinstance(v, str)
                else v
                for v in value
            ]
        else:
            result[key] = value
    return result


def is_sensitive_key(key: str) -> bool:
    return key.lower() in _SENSITIVE_KEYS
