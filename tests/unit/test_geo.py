"""Geo pure functions."""

from __future__ import annotations

from grand_lyon_mcp.domain.geo import Point, bounding_box, haversine_m, normalize_name


def test_haversine_same_point() -> None:
    p = Point(45.75, 4.85)
    assert haversine_m(p, p) < 1e-6


def test_haversine_part_dieu_bellecour() -> None:
    a = Point(45.7606, 4.8618)
    b = Point(45.7578, 4.8320)
    d = haversine_m(a, b)
    assert 2000 < d < 3500


def test_normalize_part_dieu() -> None:
    assert "part dieu" in normalize_name("Part-Dieu")
    assert normalize_name("Part Dieu") == normalize_name("Part-Dieu")


def test_bounding_box() -> None:
    p = Point(45.75, 4.85)
    min_lat, min_lon, max_lat, max_lon = bounding_box(p, 1000)
    assert min_lat < p.latitude < max_lat
    assert min_lon < p.longitude < max_lon
