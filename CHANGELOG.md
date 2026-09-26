# Changelog

This project follows [Semantic Versioning](https://semver.org/).

## Unreleased

### Fixed

- Close HTTP and SQLite resources after startup errors, migration errors, and cancellation.
- Close SQLite when HTTP shutdown fails. Apply each migration in a transaction.
- Apply GTFS calendars and date exceptions. Handle previous and next service days,
  both DST changes, and departure limits after filtering.
- Keep realtime line and direction filters strict before theoretical fallback.
- Preserve zero parking availability. Keep missing occupancy separate from capacity
  and enforce radius filtering at the service boundary.
- Refresh live parking and Vélo'v memory caches after their source TTL.

### Added

- Installed-wheel CLI and MCP stdio verification with dated mobility fixtures,
  startup-failure scenarios, exact results, and retained protocol evidence.
- Automatic count and byte limits for HTTP cache storage, source-health retention,
  and explicit stale-read warnings.
- Sequential quality verification with source identity, fixture hashes, JUnit,
  coverage, and logs. CI uploads quality and protocol evidence on failure too.
- `GRAND_LYON_MCP_FIXTURES_DIR` for alternate local fixture sets.

### Documentation

- Consolidate setup and runtime procedures in maintained English guides.
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
