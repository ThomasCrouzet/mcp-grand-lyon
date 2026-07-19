"""Environment indicators: unresolved does not block others; no invention."""

from __future__ import annotations

from datetime import datetime
from typing import Any

import pytest

from grand_lyon_mcp.domain.common import PlaceRef, ResultStatus, WarningCode
from grand_lyon_mcp.domain.environment import EnvironmentIndicator, IndicatorValue
from grand_lyon_mcp.domain.geo import Point
from grand_lyon_mcp.services.environment_service import EnvironmentService


class _FakePlaces:
    async def resolve_place(self, **kwargs: Any) -> Any:
        from grand_lyon_mcp.domain.common import make_envelope
        from grand_lyon_mcp.infrastructure.time import now_paris

        return make_envelope(
            status=ResultStatus.OK,
            generated_at=now_paris(),
            summary="ok",
            data={
                "candidates": [{"id": "lyon", "name": "Lyon", "latitude": 45.75, "longitude": 4.85}]
            },
        )


class _PartialEnvProvider:
    async def indicators(
        self,
        point: Point,
        *,
        indicators: list[str],
        at: datetime | None = None,
    ) -> list[IndicatorValue]:
        out: list[IndicatorValue] = []
        for ind in indicators:
            try:
                ei = EnvironmentIndicator(ind)
            except ValueError:
                out.append(
                    IndicatorValue(
                        indicator=EnvironmentIndicator.POLLEN, supported=False, value=None
                    )
                )
                continue
            if ei == EnvironmentIndicator.AIR_QUALITY:
                out.append(
                    IndicatorValue(
                        indicator=ei,
                        value=42,
                        unit="AQI",
                        zone="Lyon",
                        supported=True,
                        source_id="env_air",
                    )
                )
            else:
                out.append(IndicatorValue(indicator=ei, supported=False, value=None))
        return out


class _AllUnsupported:
    async def indicators(
        self,
        point: Point,
        *,
        indicators: list[str],
        at: datetime | None = None,
    ) -> list[IndicatorValue]:
        return [
            IndicatorValue(indicator=EnvironmentIndicator(i), supported=False, value=None)
            if i in {e.value for e in EnvironmentIndicator}
            else IndicatorValue(indicator=EnvironmentIndicator.POLLEN, supported=False, value=None)
            for i in indicators
        ]


@pytest.mark.asyncio
async def test_unresolved_does_not_block_others() -> None:
    svc = EnvironmentService(places=_FakePlaces(), provider=_PartialEnvProvider())  # type: ignore[arg-type]
    env = await svc.brief(
        location=PlaceRef(query="Lyon"),
        indicators=["pollen", "air_quality", "heat"],
    )
    assert env.status == ResultStatus.PARTIAL
    inds = env.data["indicators"]
    assert len(inds) == 1
    assert inds[0]["indicator"] == "air_quality"
    assert inds[0]["value"] == 42
    codes = {w.code for w in env.warnings}
    assert WarningCode.UNSUPPORTED_INDICATOR.value in codes
    # pollen and heat warned, air still present
    assert len([w for w in env.warnings if w.code == WarningCode.UNSUPPORTED_INDICATOR.value]) >= 2


@pytest.mark.asyncio
async def test_no_provider_all_unsupported() -> None:
    svc = EnvironmentService(places=_FakePlaces(), provider=None)
    env = await svc.brief(
        location=PlaceRef(latitude=45.75, longitude=4.85),
        indicators=["pollen", "air_quality", "heat"],
    )
    assert env.data["indicators"] == []
    assert all(w.code == WarningCode.UNSUPPORTED_INDICATOR.value for w in env.warnings)


@pytest.mark.asyncio
async def test_all_unsupported_no_fabricated_values() -> None:
    svc = EnvironmentService(places=_FakePlaces(), provider=_AllUnsupported())  # type: ignore[arg-type]
    env = await svc.brief(
        location=PlaceRef(latitude=45.75, longitude=4.85),
        indicators=["pollen", "air_quality", "heat"],
    )
    assert env.data["indicators"] == []
    assert env.degraded
