from grand_lyon_mcp.version import __version__


def test_version_nonempty() -> None:
    assert __version__
    assert "." in __version__
