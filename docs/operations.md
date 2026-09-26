# Operations

Run these commands in the configured environment. In a source checkout, prefix
them with `uv run`. Makefile runtime targets and `scripts/run_mcp.sh` load local
credential files; the CLI itself reads environment variables and YAML settings.

## Configuration

`grand-lyon-mcp setup` copies packaged templates to the user configuration
directory. `grand-lyon-mcp env --config-dir` prints that directory.

| Variable | Purpose |
| --- | --- |
| `GRAND_LYON_MCP_CONFIG_DIR` | User YAML files |
| `GRAND_LYON_MCP_DATA_DIR` | Downloaded data and default database directory |
| `GRAND_LYON_MCP_DB_PATH` | Explicit SQLite path |
| `GRAND_LYON_MCP_OFFLINE=true` | Fixture providers and blocked HTTP access |
| `GRAND_LYON_MCP_FIXTURES_DIR` | Alternate fixture directory for fixture mode |
| `DATAGRANDLYON_USERNAME`, `DATAGRANDLYON_PASSWORD` | Live DataGrandLyon credentials |
| `TRANSITOUS_ENABLED=true` | Enable the optional journey planner |

An alternate fixture directory does not enable live providers. Use a separate
configuration, data directory, and database for verification.

## SQLite

```bash
grand-lyon-mcp db migrate
grand-lyon-mcp db info
```

SQLite requires FTS5 and RTree. WAL mode is enabled. Migrations also run at
application startup. Invalid configuration or migration failure closes resources
before the process exits.

Use SQLite's backup API or `sqlite3 .backup` for a running database. For a file
copy, stop the server and close all connections first. Do not copy only the main
file while active WAL writes can exist.

### Retention

| Store | Default bound | Cleanup |
| --- | --- | --- |
| HTTP response cache | 1,024 entries and 32 MiB of UTF-8 payloads | Startup and every write |
| Source health | 256 sources and 30 days since the latest success or failure | Startup and every event |

Cache cleanup first removes entries at or beyond `maximum_stale_at`. It then
evicts the oldest entries until both capacity limits hold. An oversized refresh
removes the old value for that key. Each entry keeps its source-specific TTL and
maximum stale age. `get_usable()` returns a `STALE_DATA` warning for an explicitly
permitted stale read; it never returns an entry past its maximum age.

`HttpCache` is a storage component. The shared live HTTP client does not enable
persistent response caching for every provider. Do not assume all tool calls use
stale-on-error. Parking and Vélo'v use short in-memory caches. Their registry TTLs
default to 60 and 45 seconds. Expired data is refreshed, not relabeled as new data.

These bounds apply to cache payloads and health rows, not the entire database.
GTFS, entities, profiles, and history use separate tables. SQLite reuses freed
pages. It does not reduce the database file size after each deletion. To reclaim
an old high-water allocation, stop the server, back up the database, then run
`PRAGMA wal_checkpoint(TRUNCATE); VACUUM;` with SQLite.

## Source discovery

```bash
grand-lyon-mcp catalog scan --mode auto
grand-lyon-mcp catalog validate
grand-lyon-mcp doctor
```

`auto` first tries catalogue listings. On HTTP 403, it uses packaged known tables
and public OGC candidates. `known` and `ogc` select those methods directly.
Catalogue permission failure is separate from HTTP 401 authentication failure.
See [Data sources](data-sources.md).

## GTFS and Vélo'v

```bash
grand-lyon-mcp sync gtfs
grand-lyon-mcp sync gtfs --from-file /absolute/path/to/GTFS_TCL.ZIP
grand-lyon-mcp snapshot velov
```

GTFS import updates static timetable tables and indexes stops for place search.
Keep the feed current. Fallback respects weekdays, calendar date limits, and
added or removed service dates. It includes previous-service-day times above
24:00:00. The query horizon is 24 elapsed hours. Results always have `realtime=false`.

The Vélo'v snapshot command writes local reliability history. A small sample has
low confidence. The application does not install a scheduler.

## Troubleshooting

| Symptom | Check and action |
| --- | --- |
| HTTP 401 | Check credential environment variables and the account with `doctor`. |
| Catalogue HTTP 403 | Use `catalog scan --mode auto` or `--mode known`; do not assume a password error. |
| `SOURCE_UNRESOLVED` | Inspect source configuration and run bounded catalogue validation. |
| FTS5 or RTree failure | Use a Python/SQLite build with both extensions. |
| No tools in the MCP client | Check the absolute executable path and stdio configuration. |
| Protocol JSON errors | Keep stdout reserved for MCP. Application logs belong on stderr. |
| Startup failure | Check the YAML file and migration error on stderr. Correct the input, then restart. |
| No theoretical departures | Import a valid feed and check its service dates and line filter. |
| `STALE_DATA` or `PARTIAL_RESULT` on departures | Read provenance: GTFS fallback is theoretical. |
| Parking capacity without available spaces | The live value is missing. `null` does not mean zero or total capacity. |
| Environmental indicator unsupported | No live source is configured for that indicator. |
| Accessibility `unknown` | Incident data and GTFS evidence do not establish accessibility. |
| TCL duration unavailable | Transitous is disabled or unavailable. |

The `smoke` and live diagnostic commands report statuses, including errors. Use
[the installed-wheel verification](testing.md) for strict local result assertions.
