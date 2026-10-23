# Operations

## Setup and paths

Create an offline configuration without prompts:

```bash
grand-lyon-mcp setup --yes --offline
```

Use `--live` for live providers.
Live setup requires both DataGrandLyon credential variables.
Use `--force-config` to replace existing YAML files.
Use `--skip-migrate` to omit the setup migration step.

`--skip-install` remains a compatibility option.
It does not run an installer.

Print the resolved configuration directory:

```bash
grand-lyon-mcp env --config-dir
```

Print effective nonsecret settings:

```bash
grand-lyon-mcp env
```

Open `secrets.env` with the configured editor:

```bash
grand-lyon-mcp env --edit
```

## Configuration

Each command reads `settings.yaml` and `secrets.env` from the configuration directory.
The secrets parser does not evaluate shell syntax.
Nonempty process environment values override file values.
Setup rejects symbolic links and nonregular files at managed destinations.
On Unix, setup writes `secrets.env` with mode `0600`.
The loader rejects any group or other permission bits.
Windows uses filesystem ACLs instead of POSIX mode bits.

| Variable | Purpose |
| --- | --- |
| `GRAND_LYON_MCP_CONFIG_DIR` | YAML files and `secrets.env` |
| `GRAND_LYON_MCP_DATA_DIR` | Downloaded data and default database directory |
| `GRAND_LYON_MCP_DB_PATH` | Explicit SQLite path |
| `GRAND_LYON_MCP_FIXTURES_DIR` | Alternate fixture tree for offline mode |
| `GRAND_LYON_MCP_OFFLINE` | Enable embedded fixture providers and block HTTP |
| `GRAND_LYON_MCP_LOG_LEVEL` | Diagnostic log level |
| `GRAND_LYON_MCP_VERIFY_TLS` | Enable TLS certificate verification |
| `GRAND_LYON_MCP_HTTP_CONNECT_TIMEOUT_SECONDS` | HTTP connection timeout |
| `GRAND_LYON_MCP_HTTP_READ_TIMEOUT_SECONDS` | HTTP response timeout |
| `GRAND_LYON_MCP_MAX_PARALLEL_REQUESTS` | Maximum concurrent outbound requests |
| `GRAND_LYON_MCP_FUZZY_SCORE_THRESHOLD` | Validated compatibility value from 0 through 100 |
| `DATAGRANDLYON_USERNAME` | DataGrandLyon account name |
| `DATAGRANDLYON_PASSWORD` | DataGrandLyon password |
| `TRANSITOUS_ENABLED` | Enable optional journey planning |
| `TRANSITOUS_BASE_URL` | Transitous API base URL |

`settings.yaml` supports runtime, place matching, network, and journey scoring controls.
Journey scoring weights must each use values from zero through one.
Their sum must equal one.
Numeric settings reject `NaN` and infinite values.
Unknown YAML settings cause configuration loading to fail.

These nine legacy settings remain accepted but do not change Go runtime behavior:

- `runtime.timezone` and `runtime.locale`
- `results.maximum_items_per_tool` and `results.maximum_warning_count`
- `places.default_radius_m` and `places.maximum_radius_m`
- `cache.allow_stale_on_error` and `cache.negative_ttl_seconds`
- `network.maximum_attempts`

The cache compatibility settings do not guarantee stale recovery.
`places.fuzzy_score_threshold` and its environment equivalent are validated and stored.
The current ranking logic does not apply this compatibility value.

## SQLite

```bash
grand-lyon-mcp db migrate
grand-lyon-mcp db info
```

Migrations also run at application startup.
The Go release uses the existing SQLite schema and migration identifiers.
It can continue with a database from the Python release.

Use SQLite's backup API or `sqlite3 .backup` for an active database.
Stop the server before making a file copy.
Copy the database only after all connections close.

### Legacy retention tables

The database keeps `http_cache` and `source_health` tables for schema compatibility.
The Go runtime does not populate or read these tables during provider requests.
Startup can remove expired or excess legacy rows.
`doctor` does not report persistent source-health history.

SQLite reuses free pages after retention deletes rows.
It does not reduce the file size after every deletion.
Back up the database before manual compaction.

## Source discovery

```bash
grand-lyon-mcp catalog scan --mode auto
grand-lyon-mcp catalog scan --mode known
grand-lyon-mcp catalog scan --mode ogc
grand-lyon-mcp catalog validate
grand-lyon-mcp doctor
```

Catalogue commands are disabled in offline mode.
`auto` can use known source definitions after catalogue restrictions.
Successful catalogue resolutions persist when the source configuration stays unchanged.
Live providers use resolved service and table names from the registry.
A source with `enabled: false` is not queried at runtime.
See [Data sources](data-sources.md).

## GTFS and Vélo'v

Download and import the current GTFS archive in live mode:

```bash
grand-lyon-mcp sync gtfs
```

Import a trusted local archive in any mode:

```bash
grand-lyon-mcp sync gtfs --from-file /absolute/path/to/GTFS_TCL.ZIP
```

The import replaces timetable tables and indexes GTFS stops in one transaction.
Its result includes an `entities_indexed` count.
A failed import keeps the prior timetable and entity index.
GTFS results always use `realtime=false`.

Capture one Vélo'v reliability snapshot:

```bash
grand-lyon-mcp snapshot velov
```

The application does not install a scheduler.
`grand-lyon-mcp sync static` currently reports `UNSUPPORTED`.

## Diagnostics

```bash
grand-lyon-mcp version
grand-lyon-mcp doctor
grand-lyon-mcp smoke
grand-lyon-mcp smoke --tool lyon_resolve_place
```

`smoke` accepts several domain statuses.
It does not prove live upstream availability.
`doctor` and `smoke` report the effective application mode after provider selection.
`doctor` also reports safe source registry fields.
These fields are `source_id`, `enabled`, `status`, `service`, and `table_name`.
They do not contain persistent source-health history.

| Symptom | Check and action |
| --- | --- |
| HTTP 401 | Check credential variables and run `doctor`. |
| Catalogue HTTP 403 | Use `catalog scan --mode auto` or `--mode known`. |
| `SOURCE_UNRESOLVED` | Inspect `sources.yaml`, then run catalogue validation. |
| No tools in the client | Check the absolute binary path and stdio arguments. |
| Protocol JSON errors | Keep stdout reserved for MCP. |
| Startup failure | Correct the YAML or database error shown on stderr. |
| No theoretical departures | Import GTFS and check service dates. |
| Parking capacity without spaces | Treat `null` as unknown availability. |
| Environmental indicator unsupported | No live source is configured for that indicator. |
| Accessibility `unknown` | Available evidence does not establish accessibility. |
| TCL duration unavailable | Enable Transitous or accept an unknown duration. |
