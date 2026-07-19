"""Structured JSON logging to stderr only (stdout reserved for MCP)."""

from __future__ import annotations

import json
import logging
import sys
from datetime import UTC, datetime
from typing import Any

from grand_lyon_mcp.infrastructure.redaction import redact_mapping, redact_string


class JsonStderrFormatter(logging.Formatter):
    def __init__(self, extra_secrets: list[str] | None = None) -> None:
        super().__init__()
        self._extra_secrets = extra_secrets or None

    def format(self, record: logging.LogRecord) -> str:
        payload: dict[str, Any] = {
            "timestamp": datetime.now(UTC).isoformat(),
            "level": record.levelname,
            "event": record.getMessage(),
            "logger": record.name,
        }
        for key in (
            "request_id",
            "tool_name",
            "provider",
            "source_id",
            "duration_ms",
            "cache_status",
            "status_code",
            "retry_count",
            "result_status",
        ):
            if hasattr(record, key):
                payload[key] = getattr(record, key)
        if record.exc_info and record.exc_info[1] is not None:
            payload["error"] = redact_string(str(record.exc_info[1]), self._extra_secrets)
        return json.dumps(
            redact_mapping(payload, self._extra_secrets), ensure_ascii=False, default=str
        )


def setup_logging(level: str = "INFO", *, secrets: list[str] | None = None) -> None:
    """Configure root app logger to stderr with JSON formatter.

    ``secrets`` : valeurs littérales à masquer inconditionnellement (login/mot de passe).
    Si non fourni, on les récupère depuis les settings pour que toute exception qui
    contiendrait un secret brut soit redigée.
    """
    if secrets is None:
        try:
            from grand_lyon_mcp.settings import get_settings

            secrets = get_settings().secret_values()
        except Exception:
            # Le setup du logging ne doit jamais planter à cause des settings.
            secrets = None
    root = logging.getLogger("grand_lyon_mcp")
    root.handlers.clear()
    handler = logging.StreamHandler(sys.stderr)
    handler.setFormatter(JsonStderrFormatter(extra_secrets=secrets))
    root.addHandler(handler)
    root.setLevel(getattr(logging, level.upper(), logging.INFO))
    root.propagate = False


def get_logger(name: str) -> logging.Logger:
    return logging.getLogger(f"grand_lyon_mcp.{name}")
