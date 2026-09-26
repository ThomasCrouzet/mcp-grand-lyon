# MCP tool reference

The server exposes exactly ten read-only tools. Pydantic input models reject
unknown fields. All results use the same envelope. Summaries and user-facing
messages are in French.

## Result envelope

```json
{
  "schema_version": "1.0",
  "request_id": "request-uuid",
  "status": "partial",
  "generated_at": "2026-07-20T08:00:00+02:00",
  "summary": "Horaires théoriques GTFS.",
  "data": {"departures": []},
  "sources": [],
  "warnings": [],
  "degraded": true
}
```

`status` is one of `ok`, `partial`, `not_found`, `ambiguous`, `unavailable`, or
`invalid_request`. Read `warnings` and source provenance before using incomplete
results. A successful protocol response can contain an unavailable domain result.

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

## `lyon_resolve_place`

Resolve an address, stop, station, or personal place. The result contains `candidates`.

| Argument | Type | Default or limit |
| --- | --- | --- |
| `query` | string or null | Up to 200 characters |
| `near` | PlaceRef or null | Optional location hint |
| `types` | string list or null | Up to 20 items |
| `limit` | integer | 5; range 1–20 |

Example: `{"query": "Part-Dieu", "limit": 3}`.

## `lyon_next_departures`

Get departures at one stop. Results contain `stop` and `departures`.

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
| `departure_at`, `arrival_before` | datetime or null | Optional |
| `modes` | string list or null | Up to 10 items |
| `preferences` | object or null | Fields below |

Preferences: `max_walking_m` (default 800, range 0–5000), `minimum_velov_bikes`
and `minimum_velov_docks` (default 3, range 0–50), `avoid_disruptions` (default
`true`), and `wheelchair` (default `false`). TCL duration can be unavailable when
Transitous is disabled or fails.

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
| `needs` | string list or null | 10 items, including `wheelchair` and `step_free` |

## `lyon_nearby_facilities`

Find supported nearby facilities.

| Argument | Type | Default or limit |
| --- | --- | --- |
| `location` | PlaceRef | Required |
| `categories` | string list | Required; up to 20 items |
| `radius_m` | integer | 1000; range 50–5000 |
| `open_at` | datetime or null | Optional |
| `limit_per_category` | integer | 5; range 1–20 |

Categories include `toilet`, `drinking_water`, `park`, `bike_pump`, and
`velov_station`. Actual provider support varies. Missing opening-hours data does
not establish that a facility is open.

## `lyon_environment_brief`

Get supported environmental indicators. Live indicators without a configured
source return `UNSUPPORTED_INDICATOR`.

| Argument | Type | Limit |
| --- | --- | --- |
| `location` | PlaceRef | Required |
| `at` | datetime or null | Optional |
| `indicators` | string list or null | 10 items: `pollen`, `air_quality`, `heat` |

## `lyon_waste_dropoff`

Classify an item with a deterministic taxonomy and find collection facilities.

| Argument | Type | Default or limit |
| --- | --- | --- |
| `item` | string | Required; 1–200 characters |
| `location` | PlaceRef or null | Optional |
| `transport` | string | `car`; up to 20 characters |
| `open_at` | datetime or null | Optional |
| `radius_m` | integer | 15000; range 100–50000 |
| `limit` | integer | 10; range 1–20 |

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
