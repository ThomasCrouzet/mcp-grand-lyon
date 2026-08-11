"""Live tests, require RUN_LIVE_TESTS=1 and credentials."""

from __future__ import annotations

import os

import pytest

pytestmark = pytest.mark.live


def test_live_credentials_present() -> None:
    if os.environ.get("RUN_LIVE_TESTS") != "1":
        pytest.skip("RUN_LIVE_TESTS not set")
    if not os.environ.get("DATAGRANDLYON_USERNAME"):
        pytest.skip("no credentials")
    assert os.environ.get("DATAGRANDLYON_PASSWORD")
