# Sources de données / Data sources

Les noms techniques DataGrandLyon sont résolus via catalogue (`catalog scan`) et stockés localement — **jamais** exposés comme paramètres MCP.

| source_id (logique) | Producteur | Interface | Auth | Required | Status | Attribution (indicatif) | Licence |
|---------------------|------------|-----------|------|----------|--------|-------------------------|---------|
| velov_realtime | Métropole / JCDecaux | DataPusher | Basic | oui | **enabled** | Métropole de Lyon / JCDecaux | voir métadonnées portail |
| tcl_departures | SYTRAL | DataPusher / SIRI | Basic | oui | **enabled** | SYTRAL Mobilités | voir portail |
| tcl_alerts | SYTRAL | DataPusher / SIRI SX | Basic | non | **enabled** | SYTRAL Mobilités | voir portail |
| tcl_accessibility_alerts | SYTRAL | DataPusher | Basic | non | **enabled** | SYTRAL Mobilités | voir portail |
| gtfs_tcl | SYTRAL | ZIP GTFS | public download | oui (statique) | **enabled** | SYTRAL Mobilités | voir portail |
| parking_capacity | Métropole | DataPusher `pvoparking` | Basic | non | **enabled** (capacité) | Métropole de Lyon | voir portail |
| parking_realtime | Métropole | OGC Features dispo v2 (+ DataPusher si résolu) | Basic / public OGC | non | **partial** — live si OGC résolu, sinon capacité seule | Métropole de Lyon | voir portail |
| park_and_ride | SYTRAL | DataPusher `tclparcrelaistr` | Basic | non | **enabled** (dispo P+R ; coords si présentes / cache entities) | SYTRAL Mobilités | voir portail |
| traffic | Métropole | DataPusher | Basic | non | **unresolved** | Métropole de Lyon | voir portail |
| road_events | Métropole | DataPusher | Basic | non | **enabled** | Métropole de Lyon | voir portail |
| toilets / facilities | Métropole | DataPusher / OGC | Basic | non | **enabled** | Métropole de Lyon | voir portail |
| waste_facilities | Métropole | DataPusher | Basic | non | **enabled** | Métropole de Lyon | voir portail |
| env_pollen | — | — | — | non | **disabled / unsupported** | — | — |
| env_air_quality | — | — | — | non | **disabled / unsupported** | — | — |
| env_heat | — | — | — | non | **disabled / unsupported** | — | — |
| photon | Métropole | Photon API | public | non | **enabled** | Métropole de Lyon | voir portail |
| transitous | Communauté | HTTP API | none | non (flag) | **disabled by default** | Transitous | open community |

### Tables connues (403-safe)

Liste versionnée : `grand_lyon_mcp/_data/config/sources.known.yaml` (livrée avec le paquet) (pas de secrets).  
Quand `*/all.json` renvoie **403** (limite métier, pas un échec d’auth), `catalog scan --mode auto` bascule sur known + candidats OGC.

```bash
grand-lyon-mcp catalog scan --mode auto   # full → known+ogc si 403
grand-lyon-mcp catalog scan --mode known  # YAML only + probe
grand-lyon-mcp catalog scan --mode ogc    # index OGC public
grand-lyon-mcp catalog validate
```

### Entrées techniques

- Catalogues : `https://data.grandlyon.com/fr/datapusher/ws/rdata/all.json`, `.../grandlyon/all.json`
- OGC Features : `https://data.grandlyon.com/geoserver/ogc/features/v1/`
- OGC parking dispo (candidat) : `parkings-de-la-metropole-de-lyon-disponibilites-temps-reel-v2`
- Photon : `https://download.data.grandlyon.com/geocoding/photon-bal/api`
- GTFS candidat : `https://download.data.grandlyon.com/files/rdata/tcl_sytral.tcltheorique/GTFS_TCL.ZIP`
- SIRI Lite : `.../siri-lite/2.0/{estimated-timetables,situation-exchange,vehicle-monitoring}.json`

**TTL / stale** : configurables dans le registre (`ttl_seconds`, `maximum_stale_seconds`).

### Règles d’honnêteté

| Domaine | Règle |
|---------|--------|
| Départs | Matching ligne **strict** (tokens alphanumériques) — jamais substring naïve sur `LineRef` SIRI (`ActIV:…`). GTFS théorique **jamais** `realtime=true`. |
| Parkings | Capacité seule **n’invente pas** `available_spaces`. P+R sans coords réelles exclus (pas de distance nulle artificielle). |
| Environnement | Indicateur live **uniquement** si table/collection validée + champs + licence. Sinon `UNSUPPORTED_INDICATOR`. Pas d’indice composite maison. |
| Accessibilité | Absence de donnée ≠ `accessible`. Preuves : incidents live, GTFS `wheelchair_boarding` déclaratif, `evidence[]`. |
| Trajets TCL | Sans routeur : `estimated_duration_seconds=null`, summary « durée non disponible ». Scoring pénalise l’absence de durée face à Vélo’v live. |

**Repli GTFS** : `realtime=false` toujours + warning `STALE_DATA` / `PARTIAL_RESULT`.

### EN

Logical source IDs are mapped internally after catalog discovery. MCP tools never accept raw table names, CQL/SQL, or arbitrary URLs. Licenses must be read from official metadata per dataset — do not assume a single licence for all Métropole feeds. Environment indicators stay unsupported until a validated live source is wired. Catalog HTTP 403 is a permission limit, not an authentication failure.
