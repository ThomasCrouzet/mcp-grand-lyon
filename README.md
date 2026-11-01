<!-- README standard: filmcraft-inspired/v4; profile: backend -->

<h1 align="center">Grand Lyon MCP</h1>
<p align="center"><b>Lyon mobility and local services through ten read-only MCP tools.</b></p>

Grand Lyon MCP connects an MCP client to Métropole de Lyon open data through a local native binary.
Look up departures, compare travel options, find parking and facilities, or build a personal briefing.
The executable embeds configuration templates, schemas, SQLite migrations, and offline demonstration fixtures.

<p align="center">
  <a href="#get-started"><b>Get started</b></a> ·
  <a href="#tools">Tools</a> ·
  <a href="#connect-an-mcp-client">MCP setup</a> ·
  <a href="#documentation">Documentation</a>
</p>

<!-- readme:section positioning -->
## Why Grand Lyon MCP?

Use it when an assistant needs structured local mobility information with explicit warnings about incomplete data.
The same tools work with live providers or bundled fixtures, and distinguish theoretical GTFS departures, unknown parking occupancy, and missing accessibility evidence.

The server runs locally over stdio and uses SQLite without a separate database service.
Live results depend on upstream sources; offline fixtures demonstrate behavior rather than current conditions.

<a id="limits-and-privacy"></a>
<!-- readme:section privacy -->
## Privacy and data flows

**Accounts:** No project account or credentials are needed for offline use. Live mode requires a DataGrandLyon username and password. Transitous routing is optional, disabled by default, and uses no additional credential in this implementation. Your MCP client may have its own account requirements.

**Telemetry:** The server implements no telemetry or crash reporting. Errors and diagnostics go to stderr; MCP messages use stdout. Your MCP client's data handling is separate.

**Data:** Configuration and credentials live in user files; SQLite stores imported timetables, indexed places, profiles, briefing snapshots, and Vélo'v history. Offline mode blocks application HTTP requests. Live requests go to DataGrandLyon services; geocoding sends queries and optional location hints to Métropole's Photon instance. Enabled Transitous routing sends origin and destination coordinates and departure time to Transitous. Tool results are returned to your MCP client.

<!-- readme:section capabilities -->
## Tools

| Tool | Purpose |
| --- | --- |
| `lyon_resolve_place` | Resolve an address, stop, or personal place |
| `lyon_next_departures` | Get TCL departures with theoretical GTFS fallback |
| `lyon_mobility_status` | Get transit alerts, accessibility incidents, traffic, and road events where supported |
| `lyon_trip_options` | Compare travel modes, including TCL, Vélo'v, car, walking, and park-and-ride |
| `lyon_parking_options` | Find public parking and park-and-ride options |
| `lyon_accessibility_check` | Check available accessibility evidence |
| `lyon_nearby_facilities` | Find facilities near a place |
| `lyon_environment_brief` | Get supported environmental indicators; live sources are not configured |
| `lyon_waste_dropoff` | Classify waste and find collection facilities |
| `lyon_personal_briefing` | Build a briefing from a local profile |

![Ten MCP tools use application services, local SQLite data, and embedded or live providers. GTFS fallback stays theoretical.](docs/diagrams/data-flow.svg)

See the [tool reference](docs/tools.md) for arguments, defaults, filters, and limitations, and [data sources](docs/data-sources.md) for live provider coverage.

<!-- readme:section interface -->
## Reading results

Every tool returns an envelope with `status`, `summary`, `data`, `sources`, `warnings`, and `degraded`, plus a schema version, request ID, and generation time.
Statuses are `ok`, `partial`, `not_found`, `ambiguous`, `unavailable`, or `invalid_request`.
A successful MCP exchange can contain an unavailable domain result: inspect warnings, source dates, and `realtime` flags before relying on it.

- **Input validation.** Schemas reject unknown fields and invalid values. A place reference uses exactly one of a query, place ID, coordinate pair, or profile-place name.
- **Data quality.** GTFS results are always theoretical. Missing parking occupancy stays `null`; zero means full. Missing accessibility evidence stays `unknown`.
- **Local writes.** Tools do not change upstream data. Some queries save local briefing snapshots or Vélo'v observations, and startup applies pending SQLite migrations.

Administrative tasks such as setup, GTFS imports, and catalogue discovery are CLI commands.
Run `grand-lyon-mcp --help` for command names and see [Operations](docs/operations.md) for procedures.

<!-- readme:section get-started -->
## Get started

Choose a release binary or build from source. Then run the offline example below; it needs no credentials or external data.

### Install release 0.2.0

Download your archive and `SHA256SUMS` from [release v0.2.0](https://github.com/ThomasCrouzet/mcp-grand-lyon/releases/tag/v0.2.0).
Verify the archive against its checksum before extraction, then put `grand-lyon-mcp` or `grand-lyon-mcp.exe` in `PATH`.
The installed binary needs neither Go nor Python nor a source checkout.

| Operating system | Architectures | Archive suffix |
| --- | --- | --- |
| Linux | `amd64`, `arm64` | `linux_<architecture>.tar.gz` |
| macOS | `amd64`, `arm64` | `darwin_<architecture>.tar.gz` |
| Windows | `amd64` | `windows_amd64.zip` |

Archive names begin with `grand-lyon-mcp_0.2.0_`.
For example, from the download directory on Linux amd64:

```sh
sha256sum --ignore-missing --check SHA256SUMS &&
  tar -xzf grand-lyon-mcp_0.2.0_linux_amd64.tar.gz
```

Confirm that the selected archive is reported `OK`. On macOS, use `shasum -a 256` to calculate its hash; on Windows, use PowerShell `Get-FileHash -Algorithm SHA256`, and compare with the matching entry in `SHA256SUMS`.

### Build from source

**Requirements:** Go 1.26 or later, Git, and Make for the commands below.

```sh
git clone https://github.com/ThomasCrouzet/mcp-grand-lyon.git
cd mcp-grand-lyon
go mod download
make build
export PATH="$PWD/bin:$PATH"
```

`make build` sets `CGO_ENABLED=0` and writes `bin/grand-lyon-mcp`.
The shell examples use a POSIX shell; on Windows, invoke the extracted `.exe` from PowerShell.

### Run your first operation

For a disposable demo, run these commands in the same POSIX shell with the binary in `PATH`.
The exported paths keep the example separate from your normal configuration and database:

```sh
demo_dir="$(mktemp -d)"
export GRAND_LYON_MCP_CONFIG_DIR="$demo_dir/config"
export GRAND_LYON_MCP_DATA_DIR="$demo_dir/data"
export GRAND_LYON_MCP_DB_PATH="$demo_dir/data/grand_lyon_mcp.db"
export GRAND_LYON_MCP_OFFLINE=true
grand-lyon-mcp setup --yes --offline
grand-lyon-mcp doctor
grand-lyon-mcp smoke --tool lyon_resolve_place
```

Setup writes YAML templates, `secrets.env`, and `mcp-client.snippet.json` in the configuration directory and migrates SQLite.
`doctor` should report `status: "OK"` and `offline: true`.
The smoke command resolves `Part-Dieu` with a limit of three; with bundled fixtures and a fresh database, its last two lines are:

```text
lyon_resolve_place	partial	2 place candidate(s).
1/1 tools returned an accepted envelope.
```

This result was verified against the embedded fixtures. Its `partial` status reflects demonstration data, not current conditions.
Continue to [MCP setup](#connect-an-mcp-client) to use the same operation from a client.
For a regular installation, open a fresh shell without these demo overrides and run `grand-lyon-mcp setup --yes --offline` again.

<!-- readme:section configuration -->
## Configuration and data

The binary uses native user directories by default:

| Platform | Configuration | Data |
| --- | --- | --- |
| Linux | `$XDG_CONFIG_HOME/grand-lyon-mcp` or `~/.config/grand-lyon-mcp` | `$XDG_DATA_HOME/grand-lyon-mcp` or `~/.local/share/grand-lyon-mcp` |
| macOS | `~/Library/Application Support/grand-lyon-mcp` | Same as configuration |
| Windows | `%APPDATA%\grand-lyon-mcp` | `%LOCALAPPDATA%\grand-lyon-mcp` |

Run `grand-lyon-mcp env --config-dir` to print the resolved configuration directory.
Configuration precedence, from highest to lowest, is nonempty process environment, nonempty `secrets.env` values, active `settings.yaml` fields, then built-in defaults.
The initial configuration directory selects which `secrets.env` is read. The parser never evaluates shell syntax, and the binary does not load the repository's `.env` file.

| Setting | Required | Built-in default | Purpose |
| --- | --- | --- | --- |
| `GRAND_LYON_MCP_CONFIG_DIR` | No | Platform configuration directory | YAML and `secrets.env` location |
| `GRAND_LYON_MCP_DATA_DIR` | No | Platform data directory | Downloads and default database directory |
| `GRAND_LYON_MCP_DB_PATH` | No | `grand_lyon_mcp.db` in the data directory | SQLite path |
| `GRAND_LYON_MCP_OFFLINE` | No | `false`; noninteractive setup selects offline | Use fixtures and block HTTP |
| `DATAGRANDLYON_USERNAME`, `DATAGRANDLYON_PASSWORD` | Live mode only | None | DataGrandLyon credentials |
| `GRAND_LYON_MCP_FIXTURES_DIR` | No | None; fixtures are embedded | Alternate offline fixture tree |
| `GRAND_LYON_MCP_VERIFY_TLS` | No | `true` | Verify upstream TLS certificates |
| `TRANSITOUS_ENABLED` | No | `false` | Enable optional journey planning |
| `TRANSITOUS_BASE_URL` | No | `https://api.transitous.org/api/` | Planner base URL; does not expand the HTTP host allowlist |

**Effective mode:** If either DataGrandLyon credential is missing, application startup selects offline providers even when the offline setting is `false`. Check `doctor` for the effective mode.
For live use, set both credentials in your environment, run `grand-lyon-mcp setup --yes --live`, and regenerate the MCP configuration.
Setup stores credentials in user `secrets.env`; on Unix it uses mode `0600`. Keep credentials out of client JSON and commits.

Edit the generated `profiles.yaml` to replace demonstration home/work places and the `weekday_morning` briefing.
See [Operations](docs/operations.md#configuration) for timeouts, concurrency, journey weights, and accepted legacy settings that do not affect runtime behavior.
Read [Migration from Python](docs/migration-from-python.md) before replacing a prior installation or reusing its database.

<!-- readme:section mcp -->
## Connect an MCP client

The only transport is **stdio**. The client launches `grand-lyon-mcp serve --transport stdio` as a child process.
No HTTP endpoint or separately running server is needed. Running `serve` in a terminal waits for protocol messages; it has no interactive prompt.

After setup, print absolute paths and the launch configuration:

```sh
grand-lyon-mcp client-config
```

The generated document uses a top-level `mcpServers` object and contains paths and the offline flag, without credentials.
Adapt its structure to your client's documented format.

### VS Code

With AI features enabled, open a local workspace and create or edit `.vscode/mcp.json`.
Use the `command` and `env` values from `client-config` in this [VS Code configuration format](https://code.visualstudio.com/docs/agents/reference/mcp-configuration):

```json
{
  "servers": {
    "grand-lyon": {
      "type": "stdio",
      "command": "/absolute/path/grand-lyon-mcp",
      "args": ["serve", "--transport", "stdio"],
      "env": {
        "GRAND_LYON_MCP_CONFIG_DIR": "/absolute/config/path",
        "GRAND_LYON_MCP_DATA_DIR": "/absolute/data/path",
        "GRAND_LYON_MCP_DB_PATH": "/absolute/data/path/grand_lyon_mcp.db",
        "GRAND_LYON_MCP_OFFLINE": "true"
      }
    }
  }
}
```

Replace every example path with its generated value; on Windows, JSON paths need escaped backslashes or forward slashes.
Save, run **MCP: List Servers**, select `grand-lyon`, and start or restart it. Review the client trust prompt.
Enable the server's tools in the chat tool picker and confirm that discovery lists ten `lyon_*` tools.
Ask the agent to call `lyon_resolve_place` on `grand-lyon` with these arguments:

```json
{"query": "Part-Dieu", "limit": 3}
```

With the fresh offline setup above, inspect the returned envelope for `status: "partial"`, `summary: "2 place candidate(s)."`, candidates named `Part-Dieu` and `Gare Part-Dieu`, and the `OFFLINE_FIXTURE` warning.
`degraded` is `true`, provenance identifies `Fixtures` / `offline_demo`, and simulated records use `realtime=false`.
Request IDs and generation/retrieval times vary. This example result was verified through an initialized stdio session using bundled fixtures.

For startup failures, inspect the client's server output for stderr diagnostics.
See [MCP clients](docs/mcp-clients.md) for generated snippets and live credential setup, and [VS Code's setup guide](https://code.visualstudio.com/docs/agent-customization/mcp-servers) for client controls.

<!-- readme:section documentation -->
## Documentation

| Guide | Contents |
| --- | --- |
| [Tool reference](docs/tools.md) | Arguments, result envelopes, filters, and limits |
| [MCP clients](docs/mcp-clients.md) | Generated configuration and live credentials |
| [Operations](docs/operations.md) | Configuration, backup, source discovery, GTFS imports, snapshots, and troubleshooting |
| [Data sources](docs/data-sources.md) | Provider coverage, endpoints, and freshness |
| [Architecture](docs/architecture.md) | Package boundaries, lifecycle, storage, and time handling |
| [Testing](docs/testing.md) · [Contributing](CONTRIBUTING.md) | Local evidence, quality checks, and contribution rules |
| [Migration from Python](docs/migration-from-python.md) | Upgrade while keeping configuration and SQLite data |
| [Changelog](CHANGELOG.md) · [Releases](https://github.com/ThomasCrouzet/mcp-grand-lyon/releases) | Version history and release archives |
| [Security policy](SECURITY.md) · [Attributions](ATTRIBUTIONS.md) | Vulnerability reporting and source-specific licensing |

### Development and verification

From the source checkout:

```sh
make quality
```

The gate checks formatting, module state, Go diagnostics, race-enabled tests, and known vulnerabilities.
Process tests launch CLI and MCP children outside the checkout and retain repeatable local evidence and protocol transcripts.
`make test` enforces 85% coverage with documented boundary exclusions; see [Testing](docs/testing.md) for the scope and external artifact paths.
Keep reports in local storage; do not upload them to GitHub.

To build the five release archives and `SHA256SUMS`, choose an output path that does not exist:

```sh
make release RELEASE_DIR=/new/external/release-directory
```

<!-- readme:section status -->
## Status

Version 0.2.0 replaces the Python runtime with a native Go executable while retaining the ten tool names, configuration variables, and SQLite migration history.

Current limitations:

- **Provider coverage.** Live traffic conditions and environmental indicators have no configured feed. Other live sources can be incomplete or unavailable.
- **Routing.** Transitous is optional and disabled by default. TCL duration can be unavailable without a successful planner result.
- **Offline data.** Fixtures have fixed dates and always carry demonstration warnings. They do not describe current availability or conditions.
- **Local imports.** Theoretical fallback needs an imported GTFS timetable. Departure lookup near coordinates or personal places requires an imported stop within 500 metres.
- **Maintenance.** `sync static` reports `UNSUPPORTED`; the application installs no scheduler for Vélo'v snapshots.

Report reproducible bugs through [GitHub Issues](https://github.com/ThomasCrouzet/mcp-grand-lyon/issues), including the binary version, effective mode, command or tool arguments, and redacted diagnostics.
Use the [security policy](SECURITY.md) for vulnerabilities.

<a id="license"></a>
<!-- readme:section license -->
## License and credits

Code uses the [MIT license](LICENSE). Copyright (c) 2026 Thomas Crouzet.
Data has source-specific licenses; see [Attributions](ATTRIBUTIONS.md), including the requirement to credit OpenStreetMap-derived data to “© OpenStreetMap contributors”.
Release archives include `THIRD_PARTY_NOTICES.txt` for bundled software.

This project is independent of Métropole de Lyon, SYTRAL Mobilités, Keolis-TCL, and JCDecaux.
