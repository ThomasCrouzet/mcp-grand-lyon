# Security policy

## Report a vulnerability

Use [GitHub private vulnerability reporting](https://github.com/ThomasCrouzet/mcp-grand-lyon/security/advisories/new).
Do not disclose an unpatched vulnerability in a public issue.
Include reproduction steps, affected versions, and expected impact.
The target response time is seven days.

Only the latest release receives security fixes.

## Boundaries

- MCP tools are read-only.
- Tool arguments do not accept arbitrary URLs, SQL, CQL, or table names.
- The CLI reads credentials from process variables or the user `secrets.env` file.
- The CLI does not evaluate `secrets.env` through a shell.
- Nonempty process values override `secrets.env` values.
- Setup rejects symbolic links and nonregular files at managed destinations.
- On Unix, `secrets.env` cannot grant group or other permissions.
- Windows protects `secrets.env` through filesystem ACLs, not POSIX mode bits.
- Generated MCP configuration never contains credentials.
- Basic authentication uses HTTPS headers, not URL fields.
- MCP protocol output uses stdout.
- Errors and diagnostic output use stderr.
- Outbound HTTP permits approved HTTPS hosts on port 443 only.
- Redirects use the same origin policy.
- The client removes authorization before any cross-origin redirect.
- Normal JSON responses have an 8 MiB limit.
- GTFS archives have a 300 MiB compressed-file limit.
- The importer limits each expanded GTFS member to 1 GiB.
- The importer limits total selected GTFS data to 2 GiB.
- HTTP requests use bounded concurrency, timeouts, and retries.

The default HTTP hosts are `data.grandlyon.com` and `download.data.grandlyon.com`.
Enabling Transitous also permits `api.transitous.org`.
A custom Transitous URL cannot add a host to the allowlist.

Disabling TLS verification permits interception and is outside the supported security configuration.
Local GTFS archives and configuration files must come from a trusted source.
Review fixture captures because URLs and bodies can contain sensitive data.

See [the bounded-transfer decision](docs/adr/0005-bounded-http-transfers.md).
