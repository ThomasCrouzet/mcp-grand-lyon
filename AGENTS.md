# Grand Lyon MCP

## Scope

This Python package exposes read-only Lyon mobility tools through MCP and a Typer CLI.
Keep MCP integration in `adapters/mcp/`. Keep provider access separate from domain models and services.
Packaged fixtures reside in `src/grand_lyon_mcp/_data/fixtures/`.
Offline integration tests use temporary SQLite databases and copied configuration fixtures.

## Commands

```bash
uv sync --locked --all-extras --dev
uv run ruff check .
uv run ruff format --check .
uv run mypy src
GRAND_LYON_MCP_OFFLINE=true uv run --offline --no-sync pytest -m "not live" --cov
```

`make quality` combines lint, types, and offline tests. Keep the existing coverage gate.
Use temporary configuration, data, and database paths for CLI checks.
The Makefile runtime targets can load local credentials. Use the fixture tests for offline validation.

## Testing policy

- Never write unit tests after you write code.
- Highly prefer E2E tests as the sole testing mechanism.
- Use E2E tests to verify complex features through observable results.
- At the end of each E2E run, produce a verifiable and repeatable artifact.
- Record the command, source revision, environment, fixtures, and results with the artifact.
- Include the working diff identity when the source has uncommitted changes.
- If isolation is necessary, first document all identified failure modes. Then write the tests and implementation.
- Keep an isolated test only for a concrete failure that E2E tests cannot detect.
- Review assertions, fixtures, mocks, and skips before removing a test.
- Do not treat disabled E2E tests or simulated boundaries as equivalent coverage.
- Do not add tests for coverage percentages, type contracts, dependency behavior, or mocked call sequences alone.

`uv run pytest tests/contract tests/integration -m "not live"` exercises dispatch, FastMCP registration, CLI callbacks, storage, and HTTP fixtures.
These tests run in process. They do not test stdio framing or process startup.
The CI wheel smoke starts CLI processes, but it accepts missing place results. It does not test MCP transport.
Keep isolated checks for source parsing, strict transit filters, fallback accuracy, missing availability, accessibility, redaction, and bounded transfers.
Also keep storage, retry, concurrency, taxonomy, scoring, setup, and custom validation checks absent from the permissive dispatch assertions.
Add `--junitxml="$ARTIFACT_DIR/results.xml"` to retain pytest results in a dedicated external directory.
CI saves `coverage.xml`; that file measures execution, not complete user behavior.
No process-level MCP E2E suite or repeatable protocol transcript currently exists.
`scripts/live_smoke.py` writes `$LIVE_OUT/live_report.json`, with `/tmp/grand-lyon-live-tests` as its default directory.
That live diagnostic accepts all tool status values, including errors. It does not prove successful service results.
