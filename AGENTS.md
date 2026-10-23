# Grand Lyon MCP

## Scope

This Go module exposes read-only Lyon mobility tools through MCP and a native CLI.
Keep MCP integration in `internal/adapter/`.
Keep provider access separate from models and services.
Embedded fixtures reside in `internal/assets/fixtures/`.
Offline process tests use temporary SQLite databases and copied fixtures.

## Commands

```bash
go mod download
make lint
make test
make vulnerability
```

`make quality` combines lint, race-enabled tests, and the pinned vulnerability check.
Use temporary configuration, data, and database paths for CLI checks.
Use embedded fixtures for offline validation.

## Testing policy

This policy applies to every existing and future project, including projects outside this directory.
Keep these requirements when creating, importing, or revising project instructions.
Use local instructions for commands, fixtures, retained failure modes, and artifact paths.

- Never write unit tests after you write code.
- Highly prefer E2E tests as the sole testing mechanism.
- Use E2E tests to verify complex features through observable results.
- At the end of each E2E run, produce a verifiable and repeatable artifact.
- Record the command, source revision, environment, fixtures, and results with the artifact.
- Include the working diff identity when the source has uncommitted changes.
- Before isolated testing, first write down all identified ways the system could fail.
- Then write the necessary tests before the implementation.
- Keep an isolated test only for a concrete failure that the E2E suite does not detect.
- Examine actual assertions, fixtures, mocks, and skips before removing a test.
- Do not treat disabled E2E tests or simulated boundaries as proof of equivalent coverage.
- Do not add tests for coverage percentages, type contracts, dependency behavior, or mocked call sequences alone.

Browser, CLI, API, and protocol E2E tests can verify observable behavior.
A report, trace, exported file, or captured protocol result can provide an artifact.
The artifact must include results and enough information to repeat the check.
Passing isolated tests does not establish E2E success.

Keep reports and repeatable evidence in local storage.
Never upload or archive reports on GitHub.
This rule includes Actions report artifacts, SARIF uploads, and Docker build records.
Keep release deliverables separate from reports.

### Project checks

`make test` runs all Go tests with the race detector.
It gives the E2E runner a new external evidence directory.
Set `GLMCP_ARTIFACT_DIR` to select a new external base path.
It instruments the application binary and enforces the 85% coverage gate.
`GLMCP_COVERAGE_DIR` selects the external raw counter directory.
The gate keeps the legacy-equivalent adapter, CLI, and live-provider exclusions.
The report also shows unfiltered internal-package coverage.
`tests/e2e/` builds the binary once and starts CLI and MCP processes outside the checkout.
The suite verifies all ten tools, exact fixture results, startup failures, GTFS rollback, and EOF shutdown.
Set `GLMCP_BINARY` to test an absolute release binary without a source build.
The E2E report retains source identity, fixture hashes, commands, results, and protocol transcripts.
CI keeps reports in runner temporary storage and does not upload them.
Keep isolated tests only for failures that process results cannot show.
These failures include parser edges, transaction state, resource closure, retention, concurrency, and bounded transfers.
