"""Infrastructure geo helpers (re-export pure domain + CRS)."""

from __future__ import annotations

from grand_lyon_mcp.domain.geo import Point, bounding_box, haversine_m, normalize_name

__all__ = ["Point", "bounding_box", "haversine_m", "normalize_name", "to_wgs84"]


def to_wgs84(x: float, y: float, source_crs: str) -> tuple[float, float]:
    """Convert coordinates to WGS84 (lon, lat). Identity if already EPSG:4326."""
    if source_crs in ("EPSG:4326", "CRS84", "WGS84", "ogc:1.3:CRS84"):
        # Assume input is lon, lat for CRS84 / lat,lon varies — callers pass lon,lat
        return x, y
    from pyproj import Transformer

    transformer = Transformer.from_crs(source_crs, "EPSG:4326", always_xy=True)
    lon, lat = transformer.transform(x, y)
    return float(lon), float(lat)
