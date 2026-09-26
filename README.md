# grand-lyon-mcp

[![CI](https://github.com/ThomasCrouzet/mcp-grand-lyon/actions/workflows/ci.yml/badge.svg)](https://github.com/ThomasCrouzet/mcp-grand-lyon/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Python](https://img.shields.io/badge/python-3.12%20%7C%203.13-blue.svg)](pyproject.toml)

A local MCP server for Métropole de Lyon open data. It provides **ten read-only
tools** for TCL departures, parking, accessibility, facilities, waste, travel
options, and personal briefings. It uses Vélo'v data in travel options and briefings.

The server uses **stdio**. Application logs go to stderr. Tool summaries are in
French. It runs with local fixtures when offline mode is enabled or DataGrandLyon
credentials are absent.

## Install from source

Requirements: Python 3.12 or later and [uv](https://docs.astral.sh/uv/).
CI checks Python 3.12 and 3.13. A DataGrandLyon account is optional.

```bash
git clone git@github.com:ThomasCrouzet/mcp-grand-lyon.git
cd mcp-grand-lyon
uv sync --locked --all-extras --dev
uv run grand-lyon-mcp setup --yes --offline --skip-install
```

The setup command copies packaged configuration templates and migrates the local
SQLite database. For interactive live setup, run `uv run grand-lyon-mcp setup`.
It stores credentials in a user configuration file with mode `600`.

Import the packaged demonstration stops and timetable:

```bash
uv run grand-lyon-mcp sync gtfs --from-file src/grand_lyon_mcp/_data/fixtures/gtfs/mini_gtfs.zip
uv run grand-lyon-mcp doctor
uv run grand-lyon-mcp smoke
```

The fixture dates are fixed. They are demonstration data, not current departures.
The `smoke` command is a diagnostic; it can report unavailable tool results.
For strict success checks, use the [verification procedure](docs/testing.md).

## Connect an MCP client

For a source checkout, generate a configuration block with absolute paths:

```bash
uv run grand-lyon-mcp client-config
```

Example:

```json
{
  "mcpServers": {
    "grand-lyon": {
      "command": "/absolute/path/mcp-grand-lyon/scripts/run_mcp.sh",
      "args": ["serve", "--transport", "stdio"]
    }
  }
}
```

The wrapper loads local credentials and starts the server. A stdio server waits
for a client on stdin; it does not show an interactive prompt. See
[MCP clients](docs/mcp-clients.md) for wheel installations and credential paths.

## Tools

| Tool | Purpose |
| --- | --- |
| `lyon_resolve_place` | Resolve an address, stop, or personal place |
| `lyon_next_departures` | TCL realtime departures with theoretical GTFS fallback |
| `lyon_mobility_status` | Transit alerts, accessibility incidents, and road events |
| `lyon_trip_options` | Compare travel modes |
| `lyon_parking_options` | Public parking and park-and-ride options |
| `lyon_accessibility_check` | Available accessibility evidence |
| `lyon_nearby_facilities` | Nearby facilities |
| `lyon_environment_brief` | Environmental indicators, where supported |
| `lyon_waste_dropoff` | Classify waste and find collection facilities |
| `lyon_personal_briefing` | Build a briefing from a local profile |

See [the tool reference](docs/tools.md) for arguments, limits, and result status.
GTFS results are always theoretical. Parking capacity never becomes an invented
available-space count. Missing accessibility data means `unknown`.

## Configuration and data

Configuration templates and fixtures are installed with the package under
`grand_lyon_mcp/_data/`. User files use the paths from `platformdirs`.

- Linux configuration: `~/.config/grand-lyon-mcp/`
- macOS configuration: `~/Library/Application Support/grand-lyon-mcp/`
- Find the current path: `grand-lyon-mcp env --config-dir`
- Override paths with `GRAND_LYON_MCP_CONFIG_DIR`, `GRAND_LYON_MCP_DATA_DIR`, and
  `GRAND_LYON_MCP_DB_PATH`.
- Enable offline mode with `GRAND_LYON_MCP_OFFLINE=true`.

The CLI does not load the checkout's `.env` automatically. The wrapper and
Makefile runtime targets do. See [Operations](docs/operations.md) for GTFS sync,
SQLite maintenance, source discovery, cache limits, and troubleshooting.

## Development and verification

```bash
uv sync --locked --all-extras --dev
make quality
```

The quality gate checks formatting, lint, strict types, and offline tests with an
85% coverage minimum. The installed-wheel protocol checks start real CLI and MCP
processes and retain exact results. CI uploads both quality and protocol evidence.
See [Testing](docs/testing.md) and [Contributing](CONTRIBUTING.md).

## Limits and privacy

- MCP transport is stdio only.
- Transitous routing is optional and disabled by default. Without a router,
  TCL journey duration can be unavailable.
- Live environmental indicators are not configured. They return an explicit
  unsupported-indicator warning.
- Fixtures and HTTP simulations verify local behavior, not upstream availability.
- Profiles, briefings, and Vélo'v history stay in local SQLite storage. There is
  no telemetry. Live geocoding and routing send the required places or coordinates
  to their providers.

## Documentation

- [Architecture](docs/architecture.md)
- [MCP clients](docs/mcp-clients.md)
- [Tool reference](docs/tools.md)
- [Data sources](docs/data-sources.md)
- [Operations](docs/operations.md)
- [Testing](docs/testing.md)
- [Changelog](CHANGELOG.md), [Security](SECURITY.md), [Attributions](ATTRIBUTIONS.md)

## License

Code: [MIT](LICENSE). Data has source-specific licenses; see
[Attributions](ATTRIBUTIONS.md). This project is independent of Métropole de Lyon,
SYTRAL Mobilités, Keolis-TCL, and JCDecaux. Their names and marks identify services.
