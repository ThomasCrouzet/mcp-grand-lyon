# Repeatable verification

Run the offline quality checks from `AGENTS.md`. Keep the 85% coverage gate.
Coverage shows executed code. It does not prove correct service results.

For a quality report with source identity, fixture hashes, JUnit, and coverage:

```bash
uv run --no-sync python scripts/verify_quality.py --artifact-dir "$QUALITY_ARTIFACT_DIR"
```

The directory must not exist. Both runners execute checks sequentially. On a
shared machine, set `UV_CONCURRENT_BUILDS=1`, `UV_CONCURRENT_DOWNLOADS=2`, and
`UV_CONCURRENT_INSTALLS=2` to limit wheel installation concurrency.

## Installed-wheel protocol checks

Run this command with a new external artifact directory:

```bash
uv run --no-sync python scripts/verify_wheel.py --artifact-dir "$ARTIFACT_DIR"
```

The runner builds and installs a wheel with locked runtime dependencies. It
starts CLI and MCP processes outside the checkout. It checks initialization,
the ten public tools, nonempty place results, exact departures, parking filters,
startup failure, and EOF shutdown. All provider data is local fixture data.
This check does not establish live upstream availability.

The report records the command, revision, working diff identity, environment,
wheel and fixture hashes, results, and protocol transcript. Failed runs also
write a report. CI uploads the evidence even when an assertion fails.

## Retained failure cases

- Startup: invalid configuration, failed migrations, cancellation, and failure
  during resource close must release acquired resources.
- Transit: reject wrong lines and directions. Apply service calendars and date
  exceptions. Include previous-day times above 24 hours and next-day service.
  Use the GTFS service-day origin (local noon minus twelve elapsed hours) at
  both DST changes. Apply result limits after time and line filters.
- Parking: keep zero separate from missing occupancy. Capacity is not available
  space. Enforce radius and type filters. Report mixed availability as partial.
- Storage: remove data after its maximum stale age. Bound both entry count and
  UTF-8 payload bytes. Keep an explicit warning for permitted stale reads.
  Retain source health by the latest event and by entry count.
- Live providers: refresh in-memory parking and Velo'v data after their TTL.

Isolated checks use real SQLite connections and HTTP fixtures where process
tests cannot inspect resource state or exercise live provider mapping. These
checks supplement the protocol evidence. They do not replace it.

For a retained pytest result, add `--junitxml="$ARTIFACT_DIR/results.xml"`.
