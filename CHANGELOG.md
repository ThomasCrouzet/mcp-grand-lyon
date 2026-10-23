# Changelog

This project follows [Semantic Versioning](https://semver.org/).

## 0.2.0: 2026-10-23

### Changed

- Replace the Python runtime with a native Go executable and the official MCP Go SDK.
- Keep the ten read-only tools, CLI commands, configuration paths, and SQLite migration history.
- Embed configuration examples, demonstration fixtures, and migrations in the executable.
- Publish Linux and macOS binaries for amd64 and arm64, plus Windows amd64 binaries.
- Distribute release archives and SHA-256 checksums through GitHub Releases.
- Remove the Python package and PyPI publication workflow.

### Fixed

- Close HTTP and SQLite resources after startup errors, migration errors, cancellation, and process signals.
- Apply migrations and GTFS imports in transactions. Update the stop index in the same import transaction.
- Apply GTFS calendars and date exceptions. Handle previous and next service days,
  both DST changes, and departure limits after filtering.
- Keep realtime line and direction filters strict before theoretical fallback.
- Preserve zero parking availability. Keep missing occupancy separate from capacity
  and enforce radius filtering at the service boundary.
- Fall back to the official WFS parking feed when the OGC API Features endpoint fails.
- Refresh live parking and Vélo'v memory caches after their source TTL.
- Preserve catalogue resolutions across restarts and honor disabled sources in live tools.
- Read current Vélo'v timestamps and expose their age without inventing observation times.
- Include platform departures when a request targets a parent GTFS station.
- Resolve coordinate and personal-place departure requests through the nearest imported stop within 500 metres.
- Check total walking distance for Vélo'v trips and avoid recommending full park-and-ride facilities.
- Preserve accessibility evidence, numeric environmental indicators, waste acceptance limits, and dated opening hours.
- Reject unknown configuration keys, nonfinite settings, unsafe setup destinations, and unsafe secrets files.
- Preserve available briefing facts when local profiles, snapshots, or history writes fail.

### Added

- Process tests for the installed binary, all ten MCP tools, startup failures, signals, input limits, and GTFS rollback.
- Explicit demonstration warnings and provenance for offline results. Simulated records no longer claim realtime status.
- Repeatable verification with source identity, fixture hashes, coverage, and protocol transcripts stored outside the repository.
- Retention limits for legacy cache and source-health tables. These tables do not provide an active persistent provider cache.
- `GRAND_LYON_MCP_FIXTURES_DIR` for alternate local fixture sets.

### CI maintenance

- Validate Go 1.26 and 1.27 with formatting checks, vet, race detection, and the existing 85% coverage gate.
- Check dependency vulnerabilities and secrets. Analyze Go with CodeQL on Ubuntu 24.04.
- Keep verification reports in local or runner temporary storage. Disable CodeQL
  uploads while retaining analysis.

### Documentation

- Consolidate setup and runtime procedures in maintained English guides.
- Document migration from Python, native client configuration, source limitations, and binary verification.
- Merge troubleshooting into Operations and short design notes into reference pages.
- Retire the completed improvement list and correct outdated cache and SDK claims.

## 0.1.0: 2026-07-19

- Initial read-only MCP server with ten `lyon_*` tools and a Typer CLI.
- DataPusher, OGC Features, GTFS, SIRI, Photon, and optional Transitous providers.
- Offline fixture providers, packaged configuration, SQLite FTS5/RTree, and local history.
- HTTP cache storage and concurrency helpers. These components do not imply that
  all live providers use persistent response caching or stale-on-error.
- GitHub Actions for quality checks, wheel builds, secret scanning, and CodeQL.
  Tag-triggered release workflow for GitHub Releases and PyPI trusted publishing.
