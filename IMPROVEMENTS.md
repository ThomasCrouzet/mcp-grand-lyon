# Improvement priorities

Keep the ten read-only tools and the layers in [Architecture](docs/architecture.md).
Use [Operations](docs/operations.md) for deployment and data maintenance.

## P1: Verify startup failure cleanup

- Component: `bootstrap.py`, `storage/database.py`.
- Benefit: make failed configuration and migration cleanup observable.
- Completion: invalid configuration and migration fixtures terminate promptly
  and release HTTP clients, SQLite connections, and background threads.

## P1: Retain executable mobility evidence

- Component: `adapters/mcp`, CLI, packaged fixtures.
- Benefit: prove process startup and useful results, not only handler dispatch.
- Completion: an installed wheel serves nonempty place, departure, and parking
  results through stdio and saves a protocol transcript with source identity.

## P2: Exercise combined transit fallback cases

- Component: `services/transit_service.py`, GTFS and SIRI providers.
- Benefit: preserve strict line filtering and honest theoretical provenance.
- Completion: dated fixtures cover wrong-line realtime data, missing realtime,
  midnight service days, DST, and unavailable static data with exact departures.

## P2: Bound persistent cache growth

- Component: `storage/cache.py`, source-health storage.
- Benefit: control disk usage during long-running provider use.
- Completion: retained fixtures demonstrate expiry and capacity cleanup while
  keeping explicit stale-data warnings and source-specific freshness limits.

## P2: Verify parking radius and mixed availability

- Component: `services/parking_service.py`, live and fixture parking providers.
- Benefit: keep distance filtering separate from occupancy and capacity claims.
- Completion: a protocol fixture returns only in-radius options and never
  converts total capacity into available spaces when live data is missing.
