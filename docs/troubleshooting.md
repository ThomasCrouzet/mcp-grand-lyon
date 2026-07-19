# Dépannage / Troubleshooting

| Symptôme | Cause probable | Action |
|----------|----------------|--------|
| 401 DataGrandLyon | Mauvais credentials / compte | Vérifier env, `doctor` (`auth: ERROR`) |
| `catalog_list: FORBIDDEN` (403) | Compte sans droit de lister `*/all.json` | **Ce n’est pas un échec d’auth.** `doctor` affiche `auth: OK` + `catalog_list: FORBIDDEN`. Utiliser `catalog scan --mode auto\|known\|ogc` + `grand_lyon_mcp/_data/config/sources.known.yaml` (livrée avec le paquet) |
| SOURCE UNRESOLVED | Table non accessible / non listée | Probe borné, ajouter entrée YAML known, rescan |
| RTree / FTS5 ERROR | SQLite sans extensions | Rebuild Python/SQLite avec FTS5+RTREE |
| Le client MCP ne voit pas les outils | Mauvais `command` / venv | Chemin absolu, `list tools` |
| stdout corrompu en stdio | Logs sur stdout | Vérifier que logging → stderr |
| timeout | Réseau / maxfeatures | Augmenter timeouts settings, bornes requêtes |
| données stale | TTL dépassé, stale-on-error | Warning `STALE_DATA`, status `partial` |
| GTFS invalide | ZIP incomplet | Re-télécharger, valider tables minimales |
| SIRI modifié | Schéma évolutif | Parsers `extra=allow`, fixtures à jour |
| départs mauvaise ligne | Filtre LineRef naïf (ancien) | Matching strict tokens (`domain/transit_line.py`) ; sinon GTFS + `PARTIAL_RESULT` |
| parking sans places libres | Source dispo non résolue | `partial` + warning ; capacité seule, jamais inventée |
| heat/pollen unsupported | Pas de source résolue | Warning `UNSUPPORTED_INDICATOR` (comportement correct) |
| accessibilité toujours unknown | Pas d’incident + pas de GTFS wheelchair | Honnête par défaut ; `evidence[]` + notes |
| durée TCL absente | Transitous off / routeur KO | `estimated_duration_seconds=null` + `ROUTING_UNAVAILABLE` |

## Doctor — lecture des lignes

```
OK           auth — authenticated (403 is not auth failure)
FORBIDDEN    catalog_list — 403 — pas la permission (limite métier, use known+ogc)
OK           source:velov_realtime
UNRESOLVED   source:traffic
```

Ne jamais confondre `auth: OK` + `catalog_list: FORBIDDEN` avec un problème de mot de passe.

## EN

Most operational issues show up in `grand-lyon-mcp doctor` as `OK` / `WARNING` / `ERROR` / `DISABLED` / `UNRESOLVED` / `FORBIDDEN`. HTTP 403 on catalog list is a métier permission limit, not authentication failure. Never expect secrets in doctor output. If the MCP client cannot list tools, fix the absolute path to the CLI and ensure stdio transport.
