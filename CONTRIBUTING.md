# Contributing

## Development setup

```bash
uv sync --locked --all-extras --dev
```

Tests use temporary SQLite databases and packaged fixtures. A live account and
user-level setup are not required. Keep verification paths separate from personal
configuration and data.

## Quality gate

```bash
uv run ruff format --check .
uv run ruff check .
uv run mypy src
GRAND_LYON_MCP_OFFLINE=true uv run --offline --no-sync pytest -m "not live" --cov
```

`make quality` combines these checks. [Testing](docs/testing.md) gives commands
that retain quality reports and installed-wheel MCP transcripts. CI must pass
for Python 3.12 and 3.13. Keep the 85% coverage minimum.

## Change rules

- Follow the testing policy in `AGENTS.md`. Write necessary isolated tests before
  implementation, after recording the failure modes. Prefer observable E2E results.
- Keep the ten read-only tools and [layer boundaries](docs/architecture.md).
- Mark theoretical or incomplete data accurately. Do not invent missing values.
- Keep credentials and personal data out of commits, logs, and fixtures.
- Update maintained procedures when behavior changes. Record a design decision
  in `docs/adr/` when the rationale needs a separate durable record.
- Use English documentation and short, direct instructions.

## Fixtures

Packaged fixtures live in `src/grand_lyon_mcp/_data/fixtures/`. Use dated inputs
and exact expected results for time-sensitive behavior.

`scripts/record_fixture.py` can capture an explicitly selected provider response:

```bash
uv run python scripts/record_fixture.py --url "https://data.grandlyon.com/SELECTED_PATH" --out sample.json --confirm
```

The capture goes to the ignored `tests/fixtures/recorded/` directory. The script
redacts selected response headers; it does not guarantee removal of secrets from
the URL or body. Inspect and sanitize the complete capture before you add it to
the packaged fixtures.

## Pull requests

Explain the problem, behavior change, and validation evidence. Keep changes
focused and use a descriptive commit message. Contributions use the
[MIT license](LICENSE) and follow the [Code of Conduct](CODE_OF_CONDUCT.md).
