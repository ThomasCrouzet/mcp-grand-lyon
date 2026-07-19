"""SIRI Lite client."""

from __future__ import annotations

from typing import Any

from grand_lyon_mcp.domain.transit import Departure, TransitAlert
from grand_lyon_mcp.providers.datagrandlyon.client import DataGrandLyonClient
from grand_lyon_mcp.providers.siri.parser import parse_estimated_timetable, parse_situation_exchange

SIRI_ET = "https://data.grandlyon.com/siri-lite/2.0/estimated-timetables.json"
SIRI_SX = "https://data.grandlyon.com/siri-lite/2.0/situation-exchange.json"
SIRI_VM = "https://data.grandlyon.com/siri-lite/2.0/vehicle-monitoring.json"


class SiriClient:
    def __init__(self, dgl: DataGrandLyonClient) -> None:
        self._dgl = dgl

    async def estimated_timetable(self) -> list[Departure]:
        data = await self._dgl.get_json(SIRI_ET)
        if not isinstance(data, dict):
            return []
        return parse_estimated_timetable(data)

    async def situations(self) -> list[TransitAlert]:
        data = await self._dgl.get_json(SIRI_SX)
        if not isinstance(data, dict):
            return []
        return parse_situation_exchange(data)

    async def vehicle_monitoring(self) -> Any:
        return await self._dgl.get_json(SIRI_VM)
