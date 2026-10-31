# Grand Lyon MCP

## What it is

Local read-only MCP server for Métropole de Lyon open data.
Ten tools expose TCL departures, Vélo'v, parking, accessibility, facilities, waste guidance, travel options, and personal briefings.
The Go binary speaks MCP over stdio and also carries an administrative CLI; no server process to host.
Released as 0.2.0 after the Python-to-Go migration; offline mode runs on embedded fixtures and live mode needs a DataGrandLyon account.

## Stack

- Go 1.26.0 (`go.mod`); CI tests 1.26.x and 1.27.x.
- Go modules plus a Makefile. No Python at runtime: root `.venv/`, `.pytest_cache/`, `.mypy_cache/`, `.ruff_cache/` are Python leftovers.
- Dependencies: `modelcontextprotocol/go-sdk` v1.8.0, `google/jsonschema-go` v0.4.3, `modernc.org/sqlite` v1.61.0 (pure Go, `CGO_ENABLED=0`), `go.yaml.in/yaml/v3` v3.0.5.
- Storage: local SQLite file, six migrations in `internal/assets/migrations/`.
- External services: data.grandlyon.com DataPusher, OGC/WFS, Photon geocoding, the TCL GTFS ZIP, Transitous (optional, off by default).

## Commands

Run from the repository root. `make help` lists every target.

| Command | Purpose | Constraint |
| --- | --- | --- |
| `go mod download` | Fetch locked modules. | network |
| `make lint` | gofmt check, `go mod verify`, `go mod tidy -diff`, `go vet`. | Go 1.26+ |
| `make test` | Race tests, process-level E2E, 85% coverage gate. | fresh `GLMCP_ARTIFACT_DIR` |
| `make vulnerability` | govulncheck v1.9.0. | downloads the tool |
| `make quality` | lint, test, vulnerability. | as above |
| `make build` | Writes `bin/grand-lyon-mcp`. | Go 1.26+ |
| `make run` | `go run ./cmd/grand-lyon-mcp serve`; reads JSON-RPC on stdin and prints no prompt. | config |
| `make release RELEASE_DIR=/new/path` | Five archives plus `SHA256SUMS`. | path must not exist |

`make format`, `make vet`, `make format-check`, and `make clean` are the smaller steps behind `make lint`.
Go 1.27.3 is on `PATH` since 2026-10-30; `go build ./...` and `go vet ./...` pass from the root.

## Layout

| Path | Role |
| --- | --- |
| `cmd/grand-lyon-mcp/` | Binary entry point. |
| `internal/adapter/` | MCP tool registration and stdio transport. |
| `internal/cli/` | Setup, `doctor`, `client-config`, `catalog`, `smoke`. |
| `internal/service/` | Tool behavior, filters, and result envelopes. |
| `internal/providers/` | Fixture and live providers, source catalogue, SIRI, Photon, Transitous. |
| `internal/storage/` | SQLite store, source registry, GTFS import, retention. |
| `internal/assets/` | Embedded fixtures, `config/sources.known.yaml`, `schemas.json`, migrations. |
| `tests/e2e/` | Installed-binary CLI and MCP process checks. |
| `scripts/` | `build-release.sh`, `live-smoke`, `notices`. |

Generated, never edited by hand: `bin/`, coverage and evidence directories, the local `*.db` file, plus `secrets.env` and `mcp-client.snippet.json` from `setup`.
`internal/assets/` is embedded with `go:embed`; changes need a rebuild.

## Invariants

- Keep MCP integration in `internal/adapter/`; tool transport stays separate from tool logic.
- Keep provider access out of models and services; fixtures and live sources stay interchangeable.
- Keep the ten tools read-only; the server never writes upstream.
- Never invent a value: parking occupancy stays `null`, accessibility stays `unknown`, GTFS departures stay `realtime=false`.
- Do not accept raw table names, provider filters, or arbitrary URLs from tool input; resolution belongs to the source registry.
- Keep canonical fixtures in `internal/assets/fixtures/`; offline answers stay labelled `OFFLINE_FIXTURE`, `degraded=true`, `offline_demo`.
- Add a source as a `sources.known.yaml` entry plus `catalog validate`; no business code change.
- Keep the 85% coverage gate and its exclusions (`adapter/mcp.go`, `cli/`, `providers/live.go`, `providers/transitous.go`).
- Keep credentials in `secrets.env` (mode 0600) or the process environment; generated MCP blocks carry paths only.
- Keep `internal/version/version.go` and `server.json` on the same version.

## Pitfalls

- `catalog scan` returns 403 and `catalog_list: FORBIDDEN`: standard accounts lack the métier catalogue list. Use `--mode auto` (falls back to known tables and public OGC candidates) or `--mode known`.
- A source stays `UNRESOLVED` and tools answer `SOURCE_UNAVAILABLE`: discovery found no unique strong candidate. Set an exact table hint or add a `sources.known.yaml` entry, then run `catalog validate`.
- Departures come back empty while the table returns rows: the stop, line, and direction filters drop non-matching rows, and a renamed upstream field reads as `""`. Check the key aliases in `internal/providers/live.go` (`text(row, "stop_id", "code_arret", ...)`) and add the new name.
- Parking shows a capacity but a `null` occupancy: `pvoparking` is the static capacity feed, and free spaces come only from the OGC or WFS collection. Use `catalog scan --mode ogc` to check.
- After an upstream type change, fields become `null` instead of raising an error: `number`, `integer`, and `parseTime` return zero values on a mismatch. Compare raw rows before trusting an empty result.
- `make test` stops on `test ! -e`: the evidence path must not exist yet. Give each run a new `GLMCP_ARTIFACT_DIR`.
- `make release` exits 2: `RELEASE_DIR` is unset or already exists. Pass a new external path.
- `make run` looks hung: the server waits for JSON-RPC on stdin. Use `grand-lyon-mcp smoke`, `client-config`, or `tests/e2e` instead of a terminal.
- Embedded fixture results carry fixed dates: nothing there reflects current conditions, and `OFFLINE_FIXTURE` plus `degraded=true` mark them.

## Conventions

- Branches: `main` (CI also watches `master`). Work on a topic branch and open a pull request.
- Commits and pull requests: state the problem, the change, and the validation evidence; keep them focused.
- CI: `ci.yml` runs the quality matrix on 1.26.x and 1.27.x, builds the binary, verifies it through MCP stdio, and runs gitleaks; `codeql.yml`, `dependabot-auto-merge.yml`, and `release.yml` complete the set.
- Release: push a `v*` tag or dispatch `release.yml` with an existing tag. Bump `internal/version/version.go` and `server.json` first.
- Docs and comments stay in English, short and direct.

### Testing policy

This policy applies to every project, including projects outside this directory.

- Write the test before the code. Never write unit tests after the implementation.
- Prefer E2E tests as the sole mechanism; verify complex features through observable results.
- List every way the system could fail before writing any isolated test.
- Keep an isolated test only for a concrete failure that the E2E suite cannot show, such as parser edges, transaction state, resource closure, retention, concurrency, or bounded transfers.
- End each E2E run with a repeatable artifact: the command, source revision, environment, fixtures, and results. Include the working diff identity when the tree is dirty.
- A report, trace, exported file, or protocol transcript counts as an artifact when it carries the results and enough detail to repeat the check.
- Do not treat disabled E2E tests or simulated boundaries as proof of coverage, and do not read passing isolated tests as E2E success.
- Skip tests that only chase coverage percentages, type contracts, dependency behavior, or mocked call sequences.
- Inspect assertions, fixtures, mocks, and skips before removing any test.

The suite covers ten tools with exact fixture results, startup failures, GTFS rollback, and EOF shutdown.
`make test` builds the E2E runner a new external evidence directory, instruments the binary, and enforces the gate; `GLMCP_BINARY` points the suite at an absolute release binary.
The report also prints unfiltered internal-package coverage.
Reports stay in local storage or runner temporary storage. Never upload them to GitHub, including Actions artifacts, SARIF uploads, and Docker build records.
