# ADR 0003: Stockage SQLite + migrations SQL

## Statut
Accepté (2026-07-19)

## Contexte
Besoin de FTS5, RTree, cache HTTP local, historique Vélo'v, sans base distante.

## Décision
`aiosqlite` + migrations SQL explicites (pas SQLAlchemy). FTS5 pour la recherche textuelle, RTree pour le spatial.

## Conséquences
Contrôle fin des index virtuels ; schéma versionné via `schema_migrations`.
