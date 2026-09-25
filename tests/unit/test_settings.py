"""Settings weights validation."""

from __future__ import annotations

import pytest
from pydantic import ValidationError

from grand_lyon_mcp.settings import JourneyScoringWeights, Settings


def test_weights_bad_sum() -> None:
    with pytest.raises(ValidationError):
        JourneyScoringWeights(duration=1.0, reliability=1.0)


def test_settings_defaults() -> None:
    s = Settings(offline=True)
    assert s.offline is True
    assert s.resolved_db_path().name.endswith(".db")
