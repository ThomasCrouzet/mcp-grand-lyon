# Exploitation / Operations

## FR

### SQLite

- Chemin par défaut : `platformdirs` data dir / `grand_lyon_mcp.db`
- `grand-lyon-mcp db migrate` puis `db info` (FTS5 / RTree)
- WAL activé ; backup = copie du fichier à froid ou `sqlite3 .backup`

### Sources

- `catalog scan --mode auto|full|known|ogc` : découverte hybride
  - `auto` : listing complet, bascule **known + OGC** si 403
  - `known` : tables versionnées dans `grand_lyon_mcp/_data/config/sources.known.yaml` (livrée avec le paquet) + probe `maxfeatures=1`
  - `ogc` : index OGC public → candidats
- `catalog validate` : requête bornée par source OK
- Source optionnelle en panne → résultat `partial` + warning, pas d’échec global
- **403 catalogue ≠ 401 auth** : `doctor` sépare `auth` / `catalog_list` / `source:*`

### GTFS

- `sync gtfs` (réseau) ou `--from-file` offline
- Réindexe les arrêts dans `entities` + FTS/RTree

### Vélo’v

- `snapshot velov` pour l’historique de fiabilité
- Échantillon insuffisant → confiance `low`, pas de score « faux fiable »

### Offline

`GRAND_LYON_MCP_OFFLINE=true` : fixtures, HTTP bloqué.

### Logs

JSON sur **stderr**. Redaction des secrets. Pas de body fournisseur complet en prod.

## EN

Migrate DB, scan/validate sources, sync GTFS, snapshot Vélo’v. Optional sources degrade to partial. Offline mode uses fixtures only. Logs on stderr with secret redaction.
