# ADR 0001: Architecture en couches

## Statut
Accepté (2026-07-19)

## Contexte
Le serveur doit exposer une surface MCP réduite tout en restant indépendant du transport.

## Décision
Architecture : MCP adapter → services → domain protocols → providers → infrastructure/storage.
Les services n'importent jamais le SDK MCP. Le domaine n'importe ni httpx ni aiosqlite.

## Conséquences
Un adapter FastAPI/HTTP pourra être ajouté plus tard sans réécrire les services.
