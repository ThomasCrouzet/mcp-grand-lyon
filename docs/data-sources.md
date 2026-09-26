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
| `parking_realtime` | OGC Features, then DataPusher | Live occupancy when a valid field is available; default memory TTL 60 seconds |
| Parking capacity | DataPusher `pvoparking` | Static total capacity; separate from available spaces |
| `park_and_ride` | DataPusher `tclparcrelaistr` | Availability and real coordinates, when available |
| `traffic` | Not resolved | No live traffic-condition feed configured |
| `road_events` | DataPusher | Road events and works |
| Facilities and waste | DataPusher | Supported toilets, fountains, pumps, and collection facilities |
| Environmental indicators | Not configured | Explicit unsupported-indicator warnings in live mode |
| Photon | Métropole geocoding API | Address and place lookup |
| Transitous | HTTP journey API | Optional; disabled by default |

This table describes implementation support. It does not certify upstream uptime.
Fixtures under `src/grand_lyon_mcp/_data/fixtures/` are packaged demonstrations.
They are not current live measurements.

## Discovery

The source registry uses exact table hints or a unique sufficiently strong
candidate. Ambiguous candidates stay `UNRESOLVED`. `doctor` reports each source's
status. This preserves the decision to avoid guessing evolving table names.

The package includes `grand_lyon_mcp/_data/config/sources.known.yaml`.
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
- Parking availability candidate: `parkings-de-la-metropole-de-lyon-disponibilites-temps-reel-v2`
- Photon: `https://download.data.grandlyon.com/geocoding/photon-bal/api`
- GTFS: `https://download.data.grandlyon.com/files/rdata/tcl_sytral.tcltheorique/GTFS_TCL.ZIP`

## Freshness and missing data

The registry stores `ttl_seconds` and `maximum_stale_seconds` per source. Cache
storage preserves these limits per entry. Live parking and Vélo'v memory caches
use their registry TTL. Other providers do not all use persistent HTTP caching.
See [Operations](operations.md#retention) for retention and stale-read rules.

- GTFS departures always have `realtime=false` and a fallback warning.
- Missing parking occupancy stays `null`, even when total capacity is known.
- Park-and-ride records without real coordinates are excluded.
- Missing accessibility data does not establish an accessible route.
- Without a route planner, TCL duration can be `null`.

Dataset licenses and attribution requirements vary. Read the portal metadata for
each dataset. [Attributions](../ATTRIBUTIONS.md) lists producers and software credits.
