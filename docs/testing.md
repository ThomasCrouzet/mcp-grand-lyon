# Repeatable verification

## Quality gate

Run the complete local gate:

```bash
make quality
```

The gate checks formatting, module consistency, Go diagnostics, race-enabled tests, and known vulnerabilities.
It runs `govulncheck` version `v1.9.0`.

Run focused checks when necessary:

```bash
make format-check
make vet
make test
make vulnerability
```

`make test` creates a new external evidence directory by default.
Set `GLMCP_ARTIFACT_DIR` to select a new external base path.
The selected path must not exist when `make test` starts.
The target instruments the application binary and enforces 85% application coverage.
The gate excludes the MCP adapter, CLI, and live-provider boundaries.
These exclusions match the earlier coverage scope.
The report also shows unfiltered internal-package coverage.

Keep reports in local storage.
Do not upload test reports or protocol transcripts to GitHub.

## Live diagnostic

Run the live diagnostic from the repository worktree:

```bash
go run ./scripts/live-smoke \
  --binary /absolute/path/grand-lyon-mcp \
  --artifact-dir /new/external/live-evidence \
  --timeout 2m \
  --sync-gtfs
```

`--binary` selects the executable under test.
`--artifact-dir` must select a new path outside the repository.
`--timeout` accepts values from 10 seconds through 10 minutes.
The default timeout is two minutes.

`--sync-gtfs` downloads and imports GTFS into the isolated test database.
The diagnostic records the public command output and continues MCP checks after a synchronization failure.

Use an existing live archive instead of downloading one:

```bash
go run ./scripts/live-smoke \
  --binary /absolute/path/grand-lyon-mcp \
  --artifact-dir /new/external/live-evidence \
  --gtfs-from-file /absolute/path/GTFS_TCL.ZIP
```

`--gtfs-from-file` and `--sync-gtfs` are mutually exclusive.
Each run imports into a new isolated database.
The report records the archive path, size, and SHA-256 digest under `gtfs_archive`.

The diagnostic reads credentials from the user configuration.
It does not record credentials or the child environment.
It writes `live-report.json` with mode `0600`.

The parking freshness check is separate from static parking rows.
Verification requires numeric realtime availability observed within five minutes.
A `not_found` result becomes `not_verified`, which makes the diagnostic partial.

Exit code `0` means every live check is verified.
Exit code `2` means the result is partial or a source is unavailable.
Exit code `2` also reports a command usage error.
Exit code `1` means the diagnostic failed.
A partial result is not complete live verification.

## Process-level acceptance checks

Run the E2E package directly:

```bash
GLMCP_ARTIFACT_DIR=/new/external/base go test -count=1 -v ./tests/e2e
```

Without `GLMCP_BINARY`, the harness builds the executable once in its evidence directory.
It starts every CLI and MCP process outside the checkout.

Test a previously built release binary:

```bash
GLMCP_BINARY=/absolute/path/grand-lyon-mcp \
GLMCP_ARTIFACT_DIR=/new/external/base \
go test -count=1 -v ./tests/e2e
```

The installed-binary suite verifies these behaviors:

- Setup does not write into the installation directory.
- `secrets.env` uses mode `0600` on Unix systems.
- Process environment values override stored values.
- Generated MCP configuration contains no credentials.
- MCP exposes exactly ten tools.
- Every tool returns asserted fixture data.
- Offline results carry warning, provenance, degradation, and simulated realtime markers.
- Dated GTFS fallback handles calendars, midnight, and DST folds.
- Strict stop and line filters reject unrelated realtime records.
- Parking keeps zero availability separate from missing availability.
- Accessibility preserves incident and GTFS evidence.
- Waste checks preserve unknown acceptance and dated opening hours.
- Failed GTFS import preserves the prior timetable.
- Invalid YAML and invalid SQLite data stop startup.
- Setup rejects unsafe destination files.
- Configuration rejects nonfinite values.
- MCP rejects invalid references, control payloads, and oversized frames.
- Signals stop the server cleanly.
- MCP exits successfully after stdin reaches EOF.

Each run writes `report.json`, `working.patch`, command records, and protocol transcripts.
The report includes the source revision, working-tree identity, binary hash, fixture hashes, environment, and results.
Failed runs also retain evidence.

Set `GLMCP_COVERAGE_DIR` to an external path for raw Go coverage counters.
The default harness build instruments all packages in this module.
Each child process receives that directory through `GOCOVERDIR`.
An external `GLMCP_BINARY` must already contain coverage instrumentation.

## Isolated checks

Keep isolated tests only for failures that process results cannot show.
Current exceptions cover parsers, resource closure, storage state, retention, concurrency, and bounded HTTP transfers.
These tests use real SQLite connections and local HTTP servers.
They do not replace the process-level suite.

## Release verification

Build release archives in a new external directory:

```bash
make release RELEASE_DIR=/new/external/release-directory
```

Extract a release archive.
Run the E2E package with `GLMCP_BINARY` set to the extracted executable.
This check verifies the archive's actual binary, not a new source build.
