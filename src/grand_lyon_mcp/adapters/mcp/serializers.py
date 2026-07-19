"""Envelope serialization for MCP tool results."""

from __future__ import annotations

import json
from typing import Any

from grand_lyon_mcp.domain.common import Envelope


def envelope_to_dict(envelope: Envelope) -> dict[str, Any]:
    return envelope.model_dump(mode="json")


def envelope_to_json(envelope: Envelope) -> str:
    return json.dumps(envelope_to_dict(envelope), ensure_ascii=False, default=str)
