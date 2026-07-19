"""Geographic helpers (pure)."""

from __future__ import annotations

import math
from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class Point:
    latitude: float
    longitude: float


def haversine_m(a: Point, b: Point) -> float:
    """Great-circle distance in metres."""
    r = 6_371_000.0
    lat1, lat2 = math.radians(a.latitude), math.radians(b.latitude)
    dlat = lat2 - lat1
    dlon = math.radians(b.longitude - a.longitude)
    h = math.sin(dlat / 2) ** 2 + math.cos(lat1) * math.cos(lat2) * math.sin(dlon / 2) ** 2
    return 2 * r * math.asin(min(1.0, math.sqrt(h)))


def bounding_box(center: Point, radius_m: float) -> tuple[float, float, float, float]:
    """Return (min_lat, min_lon, max_lat, max_lon) approx for radius search."""
    # ~111_320 m per degree latitude
    dlat = radius_m / 111_320.0
    cos_lat = max(0.01, abs(math.cos(math.radians(center.latitude))))
    dlon = radius_m / (111_320.0 * cos_lat)
    return (
        center.latitude - dlat,
        center.longitude - dlon,
        center.latitude + dlat,
        center.longitude + dlon,
    )


def normalize_name(value: str) -> str:
    """Lowercase, strip accents-ish via NFKD, remove hyphens/spaces noise."""
    import unicodedata

    nfkd = unicodedata.normalize("NFKD", value)
    ascii_ish = "".join(c for c in nfkd if not unicodedata.combining(c))
    cleaned = ascii_ish.lower().replace("-", " ").replace("'", " ")
    return " ".join(cleaned.split())
