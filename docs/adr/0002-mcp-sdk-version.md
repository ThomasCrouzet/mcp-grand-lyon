# ADR 0002 — Version du SDK MCP Python

## Statut
Accepté (2026-07-19)

## Contexte
Le SDK MCP Python v2 est encore en préversion (alpha/beta). Nous ciblons une branche stable, compatible avec les clients MCP courants (Claude Desktop, Cursor…).

## Décision
Utiliser `mcp>=1.28.1,<2` (ligne stable v1.x). Isoler les imports dans `src/grand_lyon_mcp/adapters/mcp/`.

## Conséquences
Migration v2 planifiée après stabilisation officielle ; seuls les adapters MCP seront impactés.
