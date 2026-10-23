# MCP clients

The server uses MCP over stdio.
The client starts the binary as a child process.
JSON-RPC messages use stdin and stdout.
Errors and diagnostics use stderr.

## Generate configuration

Run setup once:

```bash
grand-lyon-mcp setup --yes --offline
```

Print a configuration block with absolute paths:

```bash
grand-lyon-mcp client-config
```

`--bin` and `--wrapper` are compatibility options.
Both options select the current Go executable.

Example:

```json
{
  "mcpServers": {
    "grand-lyon": {
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

Setup writes the same block to `mcp-client.snippet.json`.
The file is in the resolved configuration directory.
Add the block to your MCP client.

## Live credentials

Keep credentials out of MCP configuration files.
Set the credentials in the process environment before live setup:

POSIX shell:

```bash
export DATAGRANDLYON_USERNAME='account-name'
export DATAGRANDLYON_PASSWORD='account-password'
grand-lyon-mcp setup --yes --live
```

PowerShell:

```powershell
$env:DATAGRANDLYON_USERNAME = 'account-name'
$env:DATAGRANDLYON_PASSWORD = 'account-password'
.\grand-lyon-mcp.exe setup --yes --live
```

Setup stores supported values in the user `secrets.env` file.
On Unix systems, setup writes that file with mode `0600`.
The CLI parses the file without shell evaluation.
Nonempty process environment values take precedence.

The generated MCP block contains only paths and the offline setting.
It never contains DataGrandLyon credentials.

## Verify the connection

1. Reload the MCP client configuration.
2. List the available tools.
3. Confirm that the client shows exactly ten tools.
4. Call `lyon_resolve_place` with `{"query":"Part-Dieu"}`.
5. Inspect stderr after a startup or initialization failure.

Running `serve` in a terminal does not show an interactive prompt.
Use `grand-lyon-mcp smoke` for a local diagnostic.
Use the [process checks](testing.md) for strict result assertions.
