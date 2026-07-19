# Architecture / Architecture

## FR

Couches strictes :

1. **adapters/mcp** — SDK MCP, enregistrement des 10 outils, sérialisation d’enveloppes
2. **services** — logique métier, injection des providers
3. **domain** — modèles Pydantic, protocoles, pure geo/errors
4. **providers** — DataGrandLyon, Photon, GTFS, SIRI, Transitous (optionnel)
5. **storage / infrastructure** — SQLite, cache HTTP, logs stderr, redaction, retry

Règles :

- pas d’I/O réseau à l’import de module
- outils MCP en lecture seule
- arguments MCP ne définissent jamais table/SQL/URL arbitraire
- fuseau métier `Europe/Paris` ; instants stockés avec timezone

## EN

Strict layering: MCP adapter → services → domain protocols → providers → HTTP/SQLite.
Domain/services must not import MCP SDK types. Network I/O never runs at import time.
Public surface is exactly ten `lyon_*` tools with a shared response envelope.
