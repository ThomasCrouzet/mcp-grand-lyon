# Security policy

## Report a vulnerability

Use [GitHub private vulnerability reporting](https://github.com/ThomasCrouzet/mcp-grand-lyon/security/advisories/new).
Do not disclose an unpatched vulnerability in a public issue. Include reproduction
steps, affected versions, and expected impact. The target response time is seven days.

The maintained version series is `0.1.x`.

## Boundaries

- MCP tools are read-only. They do not accept arbitrary URLs, SQL, CQL, or tables.
- Credentials enter through environment variables. The local wrapper can load a
  private credential file. Basic authentication uses HTTPS headers, not URL fields.
- Logs use secret redaction and go to stderr. Fixture captures still require review.
- Outgoing HTTP requires an allowed HTTPS host on port 443. The default hosts are
  `data.grandlyon.com` and `download.data.grandlyon.com`. Enabling Transitous also
  permits `api.transitous.org`.
- Redirects use the same origin policy. Removed credentials are not reapplied.
- Normal HTTP responses have an 8 MiB body limit and a 60-second total deadline.
  Compressed HTTP bodies are rejected before decompression.
- GTFS downloads have a 300 MiB archive limit and use atomic file replacement.
  The importer checks archive-member sizes, but still loads CSV records in memory.

See [the bounded-transfer decision](docs/adr/0005-bounded-http-transfers.md).
Disabling TLS verification permits interception and is outside the supported
security configuration. A custom Transitous URL remains subject to the HTTP
allowlist. Local GTFS files and configuration files must come from a trusted source.
