# MCP tool reference

The server exposes exactly ten read-only tools.
Embedded JSON Schemas reject unknown fields and invalid values.
Go input decoding also rejects unknown fields.
All results use the same envelope.
Tool descriptions are in French.
Envelope summaries use concise status text.

## Result envelope

```json
{
  "schema_version": "1.0",
  "request_id": "request-uuid",
  "status": "partial",
  "generated_at": "2026-07-20T08:00:00+02:00",
  "summary": "Theoretical GTFS departures.",
  "data": {"departures": []},
  "sources": [],
  "warnings": [],
  "degraded": true
}
```

`status` is one of `ok`, `partial`, `not_found`, `ambiguous`, `unavailable`, or
`invalid_request`. Read `warnings` and source provenance before using incomplete
results. A successful protocol response can contain an unavailable domain result.

Offline fixture results include an `OFFLINE_FIXTURE` warning and `degraded=true`.
Their source provider is `Fixtures`, with dataset `offline_demo`.
Nested simulated records use `realtime=false`.

Source records can include `provider`, `source_id`, `dataset`, `attribution`,
`license`, `observed_at`, `retrieved_at`, `age_seconds`, `realtime`, and `stale`.
Not every provider currently supplies source records.

## Place references

A `PlaceRef` accepts exactly one of these forms:

```jsonl
{"query": "Part-Dieu"}
{"place_id": "gtfs:stop:BEL1"}
{"latitude": 45.76, "longitude": 4.86}
{"profile_place": "home"}
```

Queries and IDs have a 200-character limit. Profile-place names have a 64-character
limit. Coordinates must be valid latitude and longitude values.
Each reference must use exactly one form.

## `lyon_resolve_place`

Resolve an address, stop, station, or personal place. The result contains `candidates`.

| Argument | Type | Default or limit |
| --- | --- | --- |
| `query` | string | Required; up to 200 characters |
| `near` | PlaceRef or null | Optional location hint |
| `types` | string list or null | Up to 20 items |
| `limit` | integer | 5; range 1–20 |

Example: `{"query": "Part-Dieu", "limit": 3}`.

## `lyon_next_departures`

Get departures at one stop. Results contain `stop` and `departures`.
Coordinates and personal places select the nearest imported stop within 500 metres.
Import GTFS data before using this proximity lookup.
Parent stations include their platforms. A platform reference keeps its own departures.

| Argument | Type | Default or limit |
| --- | --- | --- |
| `stop` | PlaceRef | Required |
| `line` | string or null | Up to 32 characters |
| `direction` | string or null | Destination text; up to 100 characters |
| `at` | datetime or null | Current time |
| `limit` | integer | 6; range 1–20 |

Example: `{"stop": {"query": "Bellecour"}, "line": "A", "limit": 3}`.

Line matching is strict. A failed realtime filter can trigger GTFS fallback.
GTFS uses service calendars and date exceptions within the next 24 elapsed hours.
It handles midnight service days and both DST changes. GTFS departures always
have `realtime=false`, with `STALE_DATA` or `PARTIAL_RESULT` warnings.
`at` controls the theoretical query; the realtime feed does not provide a historical archive.

## `lyon_mobility_status`

Get transit alerts, accessibility incidents, traffic information, and road works.

| Argument | Type | Limit |
| --- | --- | --- |
| `lines` | string list or null | 20 items |
| `areas` | PlaceRef list or null | 10 items |
| `include` | string list or null | 10 items: `transit`, `accessibility`, `traffic`, `roadworks` |

## `lyon_trip_options`

Compare travel modes between two places. The server computes deterministic scores.

| Argument | Type | Default or limit |
| --- | --- | --- |
| `origin`, `destination` | PlaceRef | Required |
| `departure_at`, `arrival_before` | datetime or null | RFC 3339 with a UTC offset |
| `modes` | string list or null | `tcl`, `velov`, `park_and_ride`, `car`, `walk` |
| `preferences` | object or null | Fields below |

Preferences: `max_walking_m` (default 800, range 0–5000), `minimum_velov_bikes`
and `minimum_velov_docks` (default 3, range 0–50), `avoid_disruptions` (default
`true`), and `wheelchair` (default `false`). TCL duration can be unavailable when
Transitous is disabled or fails.

Omitting `preferences` or using `{}` selects every field default.
Explicit zero and false values remain unchanged.

An explicit `modes` list cannot be empty.
`arrival_before` must occur after `departure_at`.
The deadline removes late or untimed options.
`avoid_disruptions` removes options with known alerts.
`wheelchair` keeps only options with verified accessible status.
Without a deadline, an unplanned TCL option can remain with `ROUTING_UNAVAILABLE`.

## `lyon_parking_options`

Find public parking and park-and-ride facilities. Results contain `options`.

| Argument | Type | Default or limit |
| --- | --- | --- |
| `destination` | PlaceRef | Required |
| `types` | string list or null | Up to 5 items: `public_parking`, `park_and_ride` |
| `radius_m` | integer | 1500; range 50–5000 |
| `minimum_spaces` | integer | 0; range 0–500 |
| `limit` | integer | 10; range 1–20 |

All options are within the requested radius. `capacity` is total capacity;
`available_spaces` is a separate live value. Zero means full. `null` means unknown.
Unknown occupancy has `realtime=false` and does not imply an open or full car park.
Mixed availability produces a partial-data warning.

`minimum_spaces` removes known counts below the threshold. It retains unknown
counts with a warning; it does not guarantee that those options meet the threshold.

## `lyon_accessibility_check`

Check available accessibility evidence. Missing evidence means `unknown`, not accessible.

| Argument | Type | Limit |
| --- | --- | --- |
| `origin`, `destination` | PlaceRef or null | Optional |
| `stops` | PlaceRef list or null | 20 items |
| `needs` | string list or null | `wheelchair`, `step_free` |

Provide an origin, a destination, or at least one stop.
Omitting `needs` selects `wheelchair`.
An explicit empty list is invalid.
`step_free` always returns partial evidence because sources cannot verify a complete path.
GTFS wheelchair boarding evidence can support one segment, but it does not verify a complete path.
Each segment includes evidence objects with source, fact, confidence, detail, and status.
Incident evidence stays stronger than declarative GTFS accessibility.

## `lyon_nearby_facilities`

Find supported nearby facilities.

| Argument | Type | Default or limit |
| --- | --- | --- |
| `location` | PlaceRef | Required |
| `categories` | string list | Required; up to 20 items |
| `radius_m` | integer | 1000; range 50–5000 |
| `open_at` | datetime or null | RFC 3339 with a UTC offset |
| `limit_per_category` | integer | 5; range 1–20 |

Categories are `toilet`, `drinking_water`, `park`, `garden`, `bike_pump`, `velov_station`, and `waste_facility`.
The category list cannot be empty.
Provider support varies.
With `open_at`, results omit closed facilities and facilities with unverified hours.
Omitted unverified hours produce a `PARTIAL_RESULT` warning.
Opening-hour checks support simple `HH:MM-HH:MM` values, including overnight ranges.

## `lyon_environment_brief`

Get supported environmental indicators. Live indicators without a configured
source return `UNSUPPORTED_INDICATOR`.

| Argument | Type | Limit |
| --- | --- | --- |
| `location` | PlaceRef | Required |
| `at` | datetime or null | RFC 3339 with a UTC offset |
| `indicators` | string list or null | 10 items: `pollen`, `air_quality`, `heat` |

A missing list selects all three indicators.
An explicit empty list is invalid.
Results omit unsupported or missing indicators and include warnings.

## `lyon_waste_dropoff`

Classify an item with a deterministic taxonomy and find collection facilities.

| Argument | Type | Default or limit |
| --- | --- | --- |
| `item` | string | Required; 1–200 characters |
| `location` | PlaceRef or null | Optional |
| `transport` | string | `car` only |
| `open_at` | datetime or null | RFC 3339 with a UTC offset |
| `radius_m` | integer | 15000; range 100–50000 |
| `limit` | integer | 10; range 1–20 |

An unresolved location returns `not_found`.
It does not use a central Lyon fallback.
With `open_at`, results omit facilities with unknown or closed status.
Omitted unverified hours produce a `PARTIAL_RESULT` warning.
An empty accepted-category list means unknown acceptance and produces a warning.
A hazardous classification does not prove that a suggested facility accepts the item.

## `lyon_personal_briefing`

Build a briefing from a local profile. The caller controls scheduling and delivery;
the tool sends no notifications.

| Argument | Type | Default or limit |
| --- | --- | --- |
| `profile` | string | Required; 1–64 characters |
| `at` | datetime or null | Optional |
| `compare_with_previous` | boolean | `true` |

Administrative and raw-provider operations are not MCP tools. Use the CLI for
source discovery, data import, and database maintenance.
