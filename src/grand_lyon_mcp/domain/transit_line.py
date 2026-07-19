"""Transit line code normalization and strict matching.

Never use naive substring matching on SIRI LineRef values
(e.g. ``"a" in "ActIV:Line::C12:SYTRAL"`` is a false positive).
"""

from __future__ import annotations

import re
from collections.abc import Sequence
from typing import TypeVar

T = TypeVar("T")

# Tokens that appear in SIRI / DataGrandLyon line refs but are not line codes.
_NOISE_TOKENS = frozenset(
    {
        "sytral",
        "tcl",
        "line",
        "activ",
        "mdl",
        "metro",
        "métro",
        "tram",
        "bus",
        "funiculaire",
        "funi",
        "ligne",
        "lineid",
        "route",
    }
)

# User-facing aliases → short code
_USER_ALIASES: dict[str, str] = {
    "metro a": "A",
    "métro a": "A",
    "ligne a": "A",
    "line a": "A",
    "metro b": "B",
    "métro b": "B",
    "ligne b": "B",
    "line b": "B",
    "metro c": "C",
    "métro c": "C",
    "ligne c": "C",
    "line c": "C",
    "metro d": "D",
    "métro d": "D",
    "ligne d": "D",
    "line d": "D",
    "tram t1": "T1",
    "tramway t1": "T1",
    "ligne t1": "T1",
    "tram t2": "T2",
    "tramway t2": "T2",
    "ligne t2": "T2",
    "tram t3": "T3",
    "tramway t3": "T3",
    "ligne t3": "T3",
    "tram t4": "T4",
    "tramway t4": "T4",
    "ligne t4": "T4",
    "tram t5": "T5",
    "tramway t5": "T5",
    "ligne t5": "T5",
    "tram t6": "T6",
    "tramway t6": "T6",
    "ligne t6": "T6",
    "tram t7": "T7",
    "tramway t7": "T7",
    "ligne t7": "T7",
}


def _alnum_tokens(value: str) -> list[str]:
    """Split on non-alphanumeric boundaries; keep alnum tokens only."""
    return [t for t in re.split(r"[^a-zA-Z0-9]+", value) if t]


def normalize_line_code(raw: str | None) -> str:
    """Normalize a SIRI LineRef or free-text line to a short code (A, C12, T1).

    Examples:
        ``ActIV:Line::C12:SYTRAL`` → ``C12``
        ``métro a`` → ``A``
        ``A`` → ``A``
    """
    if raw is None:
        return ""
    text = str(raw).strip()
    if not text:
        return ""

    lowered = text.lower().strip()
    # collapse whitespace
    lowered = re.sub(r"\s+", " ", lowered)
    if lowered in _USER_ALIASES:
        return _USER_ALIASES[lowered]

    # Strip common prefixes like "ligne ", "metro "
    for prefix in ("ligne ", "line ", "métro ", "metro ", "tram ", "tramway ", "bus "):
        if lowered.startswith(prefix):
            rest = text[len(prefix) :].strip()
            return normalize_line_code(rest)

    # Already a short code?
    if re.fullmatch(r"[A-Za-z]\d{0,3}", text) or re.fullmatch(r"[TtCc]\d{1,2}", text):
        return text.upper()

    # SIRI-style colon-separated ref
    parts = [p for p in text.replace("::", ":").split(":") if p]
    for part in reversed(parts):
        token = part.strip()
        if not token:
            continue
        if token.lower() in _NOISE_TOKENS:
            continue
        # Prefer short alnum codes (≤6 chars)
        if re.fullmatch(r"[A-Za-z0-9]{1,6}", token):
            # Normalize single-letter / T1 style to upper
            if re.fullmatch(r"[A-Za-z]\d{0,3}", token) or re.fullmatch(r"[TtCc]\d{1,2}", token):
                return token.upper()
            return token.upper() if len(token) <= 3 else token
    # Fallback: last non-noise token
    for part in reversed(parts):
        if part.lower() not in _NOISE_TOKENS:
            return part
    return text


def line_tokens(value: str | None) -> set[str]:
    """Alphanumeric tokens from a line name/id, lowercased, noise removed."""
    if not value:
        return set()
    tokens = {t.lower() for t in _alnum_tokens(value)}
    return {t for t in tokens if t not in _NOISE_TOKENS and t}


def line_matches(
    requested: str | None,
    line_name: str = "",
    line_id: str = "",
) -> bool:
    """Strict line match using normalized codes and alphanumeric tokens.

    Never does naive substring matching on the full SIRI LineRef string.
    Empty / None requested matches everything.
    """
    if requested is None or not str(requested).strip():
        return True
    want = normalize_line_code(requested)
    if not want:
        return True
    want_l = want.lower()

    name_code = normalize_line_code(line_name) if line_name else ""
    if name_code and name_code.lower() == want_l:
        return True

    id_code = normalize_line_code(line_id) if line_id else ""
    if id_code and id_code.lower() == want_l:
        return True

    # Token equality (not substring): "a" must be a full token, not part of "activ"
    name_tokens = line_tokens(line_name)
    id_tokens = line_tokens(line_id)
    if want_l in name_tokens or want_l in id_tokens:
        return True

    # Also accept if the pretty short name equals want after stripping prefixes
    for candidate in (line_name, line_id):
        if not candidate:
            continue
        pretty = normalize_line_code(candidate)
        if pretty.lower() == want_l:
            return True

    return False


def filter_departures_by_line(departures: Sequence[T], line: str | None) -> list[T]:
    """Filter departure-like objects that have line_name / line_id attributes."""
    if not line:
        return list(departures)
    return [
        d
        for d in departures
        if line_matches(line, getattr(d, "line_name", "") or "", getattr(d, "line_id", "") or "")
    ]


def stop_id_tokens(stop_id: str | None) -> set[str]:
    """Tokens usable to match a resolved stop id / label against SIRI StopPointRef."""
    if not stop_id:
        return set()
    raw = str(stop_id).strip()
    # strip common prefixes
    for prefix in ("gtfs:stop:", "stop:", "tcl:", "siri:"):
        if raw.lower().startswith(prefix):
            raw = raw[len(prefix) :]
    from grand_lyon_mcp.domain.geo import normalize_name

    parts = set()
    n = normalize_name(raw)
    if n:
        parts.add(n)
        parts.update(t for t in n.split() if len(t) >= 2)
    # also keep raw alnum chunks (codes like BEL1)
    for t in re.split(r"[^a-zA-Z0-9]+", raw):
        if t and len(t) >= 2:
            parts.add(t.lower())
    return parts


def stop_matches(
    requested_stop_id: str | None,
    *,
    stop_ref: str | None = None,
    stop_name: str | None = None,
    stop_lat: float | None = None,
    stop_lon: float | None = None,
    ref_lat: float | None = None,
    ref_lon: float | None = None,
    radius_m: float = 100.0,
) -> bool:
    """Match a departure call to the requested stop (id/name tokens or ~100 m spatial).

    If neither identity nor coordinates can be checked, returns False (honest empty → GTFS).
    """
    if not requested_stop_id and ref_lat is None:
        return True  # no stop context — do not invent filter

    want = stop_id_tokens(requested_stop_id)
    # Identity: StopPointRef / stop name
    candidates = []
    if stop_ref:
        candidates.append(stop_ref)
    if stop_name:
        candidates.append(stop_name)
    for cand in candidates:
        cand_tokens = stop_id_tokens(cand)
        if want and cand_tokens and (want & cand_tokens):
            return True
        # substring on normalized full names for multi-word stops
        from grand_lyon_mcp.domain.geo import normalize_name

        if requested_stop_id and cand:
            n_req = normalize_name(requested_stop_id.removeprefix("gtfs:stop:"))
            n_cand = normalize_name(cand)
            if (
                n_req
                and n_cand
                and (n_req in n_cand or n_cand in n_req)
                and (len(n_req) >= 4 or len(n_cand) >= 4)
            ):
                return True

    # Spatial fallback (~100 m)
    if (
        ref_lat is not None
        and ref_lon is not None
        and stop_lat is not None
        and stop_lon is not None
    ):
        from grand_lyon_mcp.domain.geo import Point, haversine_m

        d = haversine_m(Point(ref_lat, ref_lon), Point(stop_lat, stop_lon))
        return d <= radius_m

    return False


def filter_departures_by_stop(
    departures: Sequence[T],
    stop_id: str | None,
    *,
    stop_lat: float | None = None,
    stop_lon: float | None = None,
    radius_m: float = 100.0,
) -> list[T]:
    """Keep only departures that match the stop; drop network-wide unknowns."""
    if not stop_id and stop_lat is None:
        return list(departures)
    out: list[T] = []
    for d in departures:
        ref = getattr(d, "stop_ref", None)
        # optional lat/lon on departure objects
        d_lat = getattr(d, "stop_lat", None)
        d_lon = getattr(d, "stop_lon", None)
        if stop_matches(
            stop_id,
            stop_ref=str(ref) if ref else None,
            stop_lat=float(d_lat) if d_lat is not None else None,
            stop_lon=float(d_lon) if d_lon is not None else None,
            ref_lat=stop_lat,
            ref_lon=stop_lon,
            radius_m=radius_m,
        ):
            out.append(d)
    return out
