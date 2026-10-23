# Migration from the Python release

Version 0.2.0 replaces the Python runtime with one native Go executable.
The Go release does not require Python, `uv`, or a virtual environment.

The Go executable keeps these public contracts:

- The executable name remains `grand-lyon-mcp`.
- CLI command names remain unchanged.
- The ten MCP tool names remain unchanged.
- Configuration environment variables remain unchanged.
- User YAML file names and active fields remain unchanged.
- The SQLite schema and migration identifiers remain unchanged.
- MCP clients continue to use stdio.

## Replace the executable

1. Stop MCP clients that use the old process.
2. Back up the existing SQLite database.
3. Download the 0.2.0 archive for your platform.
4. Verify the archive with `SHA256SUMS`.
5. Extract the native executable.
6. Move the executable to a directory in `PATH`.
7. Run `grand-lyon-mcp doctor`.
8. Run `grand-lyon-mcp client-config`.
9. Replace the MCP client block with the generated block.
10. Restart the MCP client.

Keep the existing configuration directory, data directory, and database.
The Go process reads them without conversion.
It applies any pending embedded SQLite migrations at startup.

The generated client configuration points directly to the Go executable.
It does not use the former source wrapper or virtual environment.

## Remove the old Python installation

Remove the previous Python tool only after the Go client connection succeeds.
For a prior `uv tool` installation, use the corresponding `uv tool uninstall` command.
Do not remove the user configuration directory or SQLite database.

The compatibility flags `setup --skip-install` and `client-config --wrapper` remain accepted.
They do not invoke Python, `uv`, or a wrapper script.
