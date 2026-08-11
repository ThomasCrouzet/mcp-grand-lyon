"""Photon GeoJSON → PlaceCandidate."""

from __future__ import annotations

from typing import Any

from grand_lyon_mcp.domain.places import EntityType, PlaceCandidate


def parse_photon_response(data: Any, *, limit: int = 5) -> list[PlaceCandidate]:
    features: list[dict[str, Any]] = []
    if isinstance(data, dict) and isinstance(data.get("features"), list):
        features = data["features"]
    elif isinstance(data, list):
        features = [f for f in data if isinstance(f, dict)]

    results: list[PlaceCandidate] = []
    for feat in features[:limit]:
        props = feat.get("properties") or {}
        geom = feat.get("geometry") or {}
        coords = geom.get("coordinates") or [None, None]
        lon, lat = coords[0], coords[1]
        if lat is None or lon is None:
            continue
        name = str(props.get("name") or props.get("street") or props.get("city") or "unknown")
        city = props.get("city") or props.get("locality") or ""
        postcode = props.get("postcode") or ""
        label_parts = [name]
        if postcode or city:
            label_parts.append(f"{postcode} {city}".strip())
        label = ", ".join(label_parts)
        osm_id = props.get("osm_id") or props.get("osm_value") or name
        conf_raw = props.get("confidence")
        # Photon often exposes "extent" as a bbox list: never cast that to float.
        if isinstance(conf_raw, (int, float, str)):
            try:
                conf = float(conf_raw)
            except (TypeError, ValueError):
                conf = 0.7
        else:
            conf = 0.7
        if conf > 1:
            conf = min(1.0, conf / 100.0)
        try:
            lat_f = float(lat)
            lon_f = float(lon)
        except (TypeError, ValueError):
            continue
        results.append(
            PlaceCandidate(
                id=f"photon:{osm_id}",
                name=name,
                label=label,
                type=EntityType.POINT_OF_INTEREST,
                latitude=lat_f,
                longitude=lon_f,
                confidence=conf,
            )
        )
    return results
