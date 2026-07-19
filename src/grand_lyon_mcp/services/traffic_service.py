"""Traffic service (thin wrapper)."""

from __future__ import annotations

from grand_lyon_mcp.domain.protocols import TrafficProvider


class TrafficService:
    def __init__(self, provider: TrafficProvider | None = None) -> None:
        self._provider = provider

    @property
    def provider(self) -> TrafficProvider | None:
        return self._provider
