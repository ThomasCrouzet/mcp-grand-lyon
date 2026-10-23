# Contributing

## Development setup

Install Go 1.26 or later.
Download the locked module dependencies:

```bash
go mod download
```

Tests use temporary SQLite databases and embedded fixtures.
A live account and user setup are not necessary.
Keep verification paths separate from personal configuration and data.

## Quality gate

```bash
make quality
```

The target checks formatting, module state, Go diagnostics, race-enabled tests, and known vulnerabilities.
The vulnerability check uses `govulncheck` version `v1.9.0`.
The test target enforces at least 85% application coverage.
The gate keeps documented legacy-equivalent boundary exclusions.
The report also shows unfiltered internal-package coverage.

Use these targets for focused checks:

```bash
make format-check
make vet
make test
make vulnerability
```

Keep generated reports in local storage.
Do not upload reports to GitHub.

## Change rules

- Follow the testing policy in `AGENTS.md`.
- Write necessary isolated tests before implementation.
- Record failure modes before isolated testing.
- Prefer observable E2E results.
- Keep the ten read-only tools and the documented layer boundaries.
- Mark theoretical or incomplete data accurately.
- Do not invent missing values.
- Keep credentials and personal data out of commits, logs, and fixtures.
- Update maintained procedures when behavior changes.
- Add a design decision when the rationale needs a durable record.
- Use English documentation and short, direct instructions.

## Repository layout

| Path | Purpose |
| --- | --- |
| `cmd/grand-lyon-mcp/` | Native command entry point |
| `internal/adapter/` | MCP registration and stdio transport |
| `internal/cli/` | Administrative commands and setup |
| `internal/model/` | Domain models and provider contracts |
| `internal/service/` | Tool behavior and result envelopes |
| `internal/providers/` | Fixture and live provider implementations |
| `internal/httpclient/` | Bounded outbound HTTP |
| `internal/storage/` | SQLite storage and GTFS import |
| `internal/config/` | Configuration loading and validation |
| `internal/assets/` | Embedded fixtures, templates, schemas, and migrations |
| `tests/e2e/` | Installed-binary CLI and MCP process checks |

## Fixtures

Canonical fixtures live in `internal/assets/fixtures/`.
Use dated inputs and exact expected results for time-sensitive behavior.
Review each capture before adding it to embedded assets.
Remove credentials, personal data, and unstable fields.

## Release checks

Create release files in a new external directory:

```bash
make release RELEASE_DIR=/new/external/release-directory
```

The release script builds Linux, macOS, and Windows archives.
It also writes `SHA256SUMS`.
See [Testing](docs/testing.md) for installed-binary verification.

## Pull requests

Explain the problem, behavior change, and validation evidence.
Keep changes focused and use a descriptive commit message.
Contributions use the [MIT license](LICENSE) and follow the [Code of Conduct](CODE_OF_CONDUCT.md).
