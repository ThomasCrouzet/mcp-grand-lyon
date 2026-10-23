# MCP clients

The server uses MCP over **stdio**. The client starts the server as a child process
and exchanges JSON-RPC messages on stdin and stdout. Logs go to stderr.

## Source checkout

Generate a configuration block with absolute paths:

```bash
uv run grand-lyon-mcp client-config
```

Example for clients with an `mcpServers` configuration, such as Claude Desktop or
Cursor:

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

The wrapper uses the checkout's `.venv/bin/grand-lyon-mcp`. Run
`uv sync --locked --all-extras --dev` before you use it.

## Installed wheel

Install the released package in an isolated environment:

```bash
uv tool install grand-lyon-mcp
grand-lyon-mcp setup --yes --offline
grand-lyon-mcp client-config
```

The final command prints a configuration with the absolute installed executable
path. Add that configuration to the client.

```json
{
  "mcpServers": {
    "grand-lyon": {
      "command": "/absolute/path/venv/bin/grand-lyon-mcp",
      "args": ["serve", "--transport", "stdio"],
      "env": {"GRAND_LYON_MCP_OFFLINE": "true"}
    }
  }
}
```

Configuration templates, SQL migrations, and demonstration fixtures are included
in the wheel. Setup migrates the database. The application also runs migrations
at startup. A source checkout is not required at runtime.

## Credentials

Keep credentials out of client configuration files. For live use, run
`grand-lyon-mcp setup` and select live mode. The CLI loads `secrets.env` from the
user configuration directory. Nonempty process environment values take precedence.

The source-checkout wrapper checks the Linux configuration path, then the macOS
path. Set `GRAND_LYON_MCP_SECRETS_FILE` to select a different wrapper credential file.

The wrapper then loads the checkout's `.env`, if present. Without credentials,
it enables fixture mode unless offline mode was explicitly set. Use
`GRAND_LYON_MCP_OFFLINE=true` to block network access during demonstration use.

## Verify the connection

1. Reload the client's MCP configuration.
2. List tools. The server must expose exactly the ten tools in the
   [tool reference](tools.md).
3. Call `lyon_resolve_place` with `{"query": "Part-Dieu"}`.
4. Inspect stderr if startup or initialization fails.

Running `serve` in a terminal produces no interactive prompt. The `smoke` command
can diagnose handler results without an MCP client. The
[installed-wheel checks](testing.md) verify the real protocol and save a transcript.
