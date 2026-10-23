# Architecture

```text
MCP adapter -> services -> provider contracts -> providers -> HTTP or SQLite
```

## Boundaries

| Package | Responsibility |
| --- | --- |
| `cmd/grand-lyon-mcp/` | Native executable entry point and signal handling |
| `internal/adapter/` | MCP tool registration and stdio transport |
| `internal/cli/` | Setup, maintenance, diagnostics, and server commands |
| `internal/model/` | Domain models, provider contracts, and shared rules |
| `internal/service/` | Tool behavior, validation, scoring, and result envelopes |
| `internal/providers/` | DataGrandLyon, Photon, SIRI, Transitous, and fixture access |
| `internal/httpclient/` | Bounded HTTPS, redirects, retries, timeouts, and concurrency |
| `internal/storage/` | SQLite repositories, GTFS import, and retention |
| `internal/config/` | Platform paths, YAML, secrets, environment precedence, and validation |
| `internal/assets/` | Embedded configuration, fixtures, schemas, and migrations |

Keep MCP SDK use in `internal/adapter/`.
Services depend on provider contracts from `internal/model/`.
Providers map external data into domain models.
No package performs network access during initialization.

The public surface has exactly ten read-only tools.
Administrative operations stay in the CLI.
Tool arguments do not accept arbitrary URLs, SQL, CQL, or tables.

## Binary and assets

Go embeds configuration templates, tool schemas, fixtures, and SQLite migrations in the executable.
An installed release does not need repository files.
Offline mode reads embedded fixtures unless `GRAND_LYON_MCP_FIXTURES_DIR` selects an override.

The release build sets `CGO_ENABLED=0`.
The pure-Go SQLite driver keeps Linux, macOS, and Windows archives self-contained.

## Lifecycle

The CLI loads and validates configuration before it creates the application.
The application opens SQLite, applies embedded migrations, and cleans legacy retention tables.
It then selects embedded fixtures or live providers.

Startup closes SQLite and HTTP resources after any initialization failure.
Normal shutdown closes both resources after the MCP stdio loop ends.
User `SIGINT` and `SIGTERM` signals request a clean exit.
Each migration and its version record share one transaction.
GTFS replacement and stop-entity indexing use one transaction.

## Storage

SQLite keeps the server local and avoids a separate database service.
Embedded SQL migrations preserve the existing schema and migration identifiers.
The Go release opens databases created by the earlier Python release.

Storage supports FTS5 search and RTree spatial indexes.
The live providers do not use a persistent HTTP cache or persistent source-health records.
Legacy cache and source-health tables remain for database compatibility.
Startup prunes these legacy tables before serving requests.

## Time and data quality

The business timezone is `Europe/Paris`.
Stored instants include a UTC offset.
GTFS applies service calendars, date exceptions, and times above 24 hours.
UTC comparisons preserve order during repeated DST hours.
The fallback searches the next 24 elapsed hours and marks results theoretical.

Services retain warnings when data is incomplete.
Missing parking occupancy differs from total capacity.
Unknown accessibility stays unknown.

## Dependencies and evidence

`go.mod` defines the Go version and module dependencies.
`go.sum` records dependency checksums.
The MCP adapter uses the official Go SDK.

[Testing](testing.md) describes process-level evidence and retained isolated checks.
[Bounded HTTP transfers](adr/0005-bounded-http-transfers.md) records the network transfer decision.
