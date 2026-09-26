# Architecture

```text
MCP adapter → application services → domain protocols → providers → HTTP / SQLite
```

## Boundaries

| Layer | Responsibility |
| --- | --- |
| `adapters/mcp/` | FastMCP registration, input validation, serialization, stdio |
| `services/` | Application behavior and response envelopes |
| `domain/` | Models, provider protocols, matching rules, and domain errors |
| `providers/` | DataGrandLyon, Photon, SIRI, GTFS, Transitous, and fixture access |
| `storage/` | SQLite repositories and explicit SQL migrations |
| `infrastructure/` | Bounded HTTP, retry, concurrency, redaction, time, and geometry |

Keep MCP SDK imports in the adapter. Domain code does not import `httpx` or
`aiosqlite`. Services use provider protocols so transport changes do not require
business-logic changes. Modules do not perform network I/O at import time.

The public surface has exactly ten read-only tools. Administrative commands stay
in the CLI. Tool arguments do not accept arbitrary SQL, CQL, tables, or URLs.

## Lifecycle

`build_app()` acquires SQLite and HTTP resources through an async exit stack.
It transfers ownership to the application container only after all configuration
and services are ready. Errors and cancellation close acquired resources.
The container closes SQLite even when HTTP shutdown raises an error.

Connection initialization and migrations close the connection on failure. Each
migration and its version record share a transaction. The MCP entry point closes
the container after server registration failure, transport failure, or EOF.

## Storage decisions

SQLite keeps the server local and avoids a separate database service.
`aiosqlite` supplies async access. Explicit SQL migrations preserve direct control
of FTS5 text search and RTree spatial indexes. `schema_migrations` records versions.

HTTP cache storage has count and payload-byte limits. Source-health storage has
count and age limits. Startup and store writes apply retention. See
[Operations](operations.md) for exact limits and disk maintenance.

## Time and data quality

The business timezone is `Europe/Paris`. Stored instants include a timezone.
GTFS uses service calendars, date exceptions, and elapsed time from local noon
minus twelve hours. UTC comparisons preserve order during repeated DST hours.
The fallback searches the next 24 elapsed hours and marks all results theoretical.

Services retain explicit warnings when data is incomplete. A missing parking
occupancy value is separate from total capacity. Unknown accessibility stays unknown.

## Dependencies and evidence

`pyproject.toml` defines the supported MCP SDK range. `uv.lock` selects the tested
version. Use locked installs; do not copy a version range from an old design note.

[Testing](testing.md) describes installed-wheel protocol evidence and the retained
isolated checks. [Bounded HTTP transfers](adr/0005-bounded-http-transfers.md)
records the network transfer decision and its limits.
