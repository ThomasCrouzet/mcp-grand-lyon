"""PlaceRef validation."""

from __future__ import annotations

import pytest
from pydantic import ValidationError

from grand_lyon_mcp.domain.common import PlaceRef


def test_reject_multiple_modes() -> None:
    with pytest.raises(ValidationError):
        PlaceRef(query="x", latitude=1.0, longitude=2.0)


def test_reject_lat_without_lon() -> None:
    with pytest.raises(ValidationError):
        PlaceRef(latitude=45.0)


def test_reject_empty() -> None:
    with pytest.raises(ValidationError):
        PlaceRef()


def test_reject_bad_lat() -> None:
    with pytest.raises(ValidationError):
        PlaceRef(latitude=100.0, longitude=0.0)
