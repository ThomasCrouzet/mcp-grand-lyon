# Data sources

Providers map logical source IDs to catalogue entries or versioned known tables.
MCP tools do not accept raw table names, provider filters, or arbitrary URLs.

## Provider coverage

| Source | Interface | Current behavior |
| --- | --- | --- |
| `velov_realtime` | DataPusher | Live station availability; default memory TTL 45 seconds |
| `tcl_departures` | SIRI and DataPusher | Strict stop, line, and direction filters |
| `gtfs_tcl` | Local imported GTFS ZIP | Theoretical fallback with service calendars and date exceptions |
| `tcl_alerts` | SIRI situation exchange and DataPusher | Transit alerts |
| `tcl_accessibility_alerts` | DataPusher | Accessibility incidents |
| `parking_realtime` | OGC API Features, WFS, then DataPusher | Live positions and occupancy; default memory TTL 60 seconds |
| Parking capacity | DataPusher `pvoparking` | Static total capacity; separate from available spaces |
| `park_and_ride` | DataPusher `tclparcrelaistr` | Availability and real coordinates, when available |
| `traffic` | Not resolved | No live traffic-condition feed configured |
| `road_events` | DataPusher | Road events and works |
| Facilities and waste | DataPusher | Supported toilets, fountains, pumps, and collection facilities |
| Environmental indicators | Not configured | Explicit unsupported-indicator warnings in live mode |
| Photon | Métropole geocoding API | Address and place lookup |
| Transitous | HTTP journey API | Optional; disabled by default |

This table describes implementation support. It does not certify upstream uptime.
Fixtures under `internal/assets/fixtures/` are embedded demonstrations.
They are not current live measurements.
Offline envelopes include `OFFLINE_FIXTURE`, `degraded=true`, and `Fixtures` provenance.
Their provenance uses the `offline_demo` dataset identifier.
Simulated records never claim realtime status.

## Discovery

The source registry uses exact table hints or a unique sufficiently strong candidate.
Ambiguous candidates stay `UNRESOLVED`.
Catalogue commands report discovery results.
Resolved service and table names persist while the source configuration remains unchanged.
Live providers use these resolved values for requests.
A source with `enabled: false` is a runtime circuit breaker.
The application does not query a disabled source.
`doctor` reports `source_id`, `enabled`, `status`, `service`, and `table_name`.
It does not report persistent source-health history.
This behavior avoids guesses about evolving table names.

The binary embeds `internal/assets/config/sources.known.yaml`.
Catalogue HTTP 403 is a permission limit. In `auto` mode, discovery falls back
to known tables and public OGC candidates.

```bash
grand-lyon-mcp catalog scan --mode auto
grand-lyon-mcp catalog scan --mode known
grand-lyon-mcp catalog scan --mode ogc
grand-lyon-mcp catalog validate
```

## Endpoints

- DataPusher: `https://data.grandlyon.com/fr/datapusher/ws/`
- OGC Features: `https://data.grandlyon.com/geoserver/ogc/features/v1/`
- WFS: `https://data.grandlyon.com/geoserver/ows`
- Parking collection: `metropole-de-lyon:parkings-de-la-metropole-de-lyon-disponibilites-temps-reel-v2`
- Photon: `https://download.data.grandlyon.com/geocoding/photon-bal/api`
- GTFS: `https://download.data.grandlyon.com/files/rdata/tcl_sytral.tcltheorique/GTFS_TCL.ZIP`

Parking availability first uses OGC API Features for the configured collection.
It then uses bounded WFS 2.0.0 `GetFeature` with the same collection and GeoJSON output.
DataPusher is the final fallback.
Live rows provide actual coordinates and occupancy when the provider supplies valid values.
The separate capacity feed does not establish current occupancy.

## Freshness and missing data

Live parking and Vélo'v caches use memory only.
They use each source's registry `ttl_seconds` value.
No live provider uses the legacy persistent HTTP cache.
The application does not guarantee stale recovery after a provider error.

- GTFS departures always have `realtime=false` and a fallback warning.
- Missing parking occupancy stays `null`, even when total capacity is known.
- Park-and-ride records without real coordinates are excluded.
- Missing accessibility data does not establish an accessible route.
- Without a route planner, TCL duration can be `null`.

Dataset licenses and attribution requirements vary. Read the portal metadata for
each dataset. [Attributions](../ATTRIBUTIONS.md) lists producers and software credits.
