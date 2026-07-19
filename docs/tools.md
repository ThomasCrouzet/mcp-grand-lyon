# Outils MCP

Le serveur expose exactement **10 outils** en lecture seule. Toutes les entrées sont validées par des modèles Pydantic stricts (`extra="forbid"` : un argument inconnu est rejeté). Toutes les sorties partagent une **enveloppe commune**.

## Enveloppe de sortie

```jsonc
{
  "schema_version": "1.0",
  "request_id": "uuid",
  "status": "ok",              // ok | partial | not_found | ambiguous | unavailable | invalid_request
  "generated_at": "2026-07-20T08:00:00+02:00",
  "summary": "Résumé factuel court en français.",
  "data": { /* charge utile spécifique à l'outil */ },
  "sources": [
    { "provider": "DataGrandLyon", "source_id": "…", "attribution": "…",
      "observed_at": "…", "retrieved_at": "…", "age_seconds": 12,
      "realtime": true, "stale": false }
  ],
  "warnings": [],
  "degraded": false
}
```

Un lieu (`PlaceRef`) accepte **exactement un** mode : `{"query": "Part-Dieu"}`, `{"place_id": "lyon:…"}`, `{"latitude": 45.76, "longitude": 4.86}` ou `{"profile_place": "home"}`.

---

## `lyon_resolve_place`

Résout une adresse, un arrêt TCL, une station Vélo’v, un quartier ou un lieu personnel.

| Argument | Type | Défaut | Bornes |
|----------|------|--------|--------|
| `query` | string \| null | – | ≤ 200 car. |
| `near` | PlaceRef \| null | – | |
| `types` | list[str] \| null | – | ≤ 20 |
| `limit` | int | 5 | 1–20 |

```jsonc
// entrée
{ "query": "Part-Dieu", "near": { "latitude": 45.764, "longitude": 4.835 }, "limit": 5 }
// data
{ "candidates": [
  { "id": "lyon:transport_station:…", "name": "Gare Part-Dieu Villette",
    "type": "transport_station", "latitude": 45.7606, "longitude": 4.8618,
    "distance_m": 120, "confidence": 0.94 }
] }
```

## `lyon_next_departures`

Prochains passages TCL à un arrêt/station.

| Argument | Type | Défaut | Bornes |
|----------|------|--------|--------|
| `stop` | PlaceRef | requis | |
| `line` | string \| null | – | ≤ 32 |
| `direction` | string \| null | – | ≤ 100 |
| `at` | datetime \| null | maintenant | |
| `limit` | int | 6 | 1–20 |

```jsonc
{ "stop": { "query": "Bellecour" }, "line": "A", "limit": 3 }
```

## `lyon_mobility_status`

Alertes TCL, accessibilité, trafic et chantiers pour des lignes/zones.

| Argument | Type | Bornes |
|----------|------|--------|
| `lines` | list[str] \| null | ≤ 20 |
| `areas` | list[PlaceRef] \| null | ≤ 10 |
| `include` | list[str] \| null | ≤ 10 (`transit`, `accessibility`, `traffic`, `roadworks`) |

## `lyon_trip_options`

Compare des modes de déplacement (TCL, Vélo’v, P+R, voiture, marche) entre deux lieux. Le score est calculé côté serveur, déterministe.

| Argument | Type | Défaut | Bornes |
|----------|------|--------|--------|
| `origin` / `destination` | PlaceRef | requis | |
| `departure_at` / `arrival_before` | datetime \| null | – | |
| `modes` | list[str] \| null | – | ≤ 10 |
| `preferences` | objet \| null | – | `max_walking_m`, `minimum_velov_bikes/docks`, `avoid_disruptions`, `wheelchair` |

## `lyon_parking_options`

Parkings publics et P+R près d’une destination.

| Argument | Type | Défaut | Bornes |
|----------|------|--------|--------|
| `destination` | PlaceRef | requis | |
| `types` | list[str] \| null | – | ≤ 5 (`public_parking`, `park_and_ride`) |
| `radius_m` | int | 1500 | 50–5000 |
| `minimum_spaces` | int | 0 | 0–500 |
| `limit` | int | 10 | 1–20 |

## `lyon_accessibility_check`

Vérifie l’accessibilité d’arrêts ou d’un trajet. **L’absence de donnée n’est jamais assimilée à « accessible »** (`unknown`).

| Argument | Type | Bornes |
|----------|------|--------|
| `origin` / `destination` | PlaceRef \| null | |
| `stops` | list[PlaceRef] \| null | ≤ 20 |
| `needs` | list[str] \| null | ≤ 10 (`wheelchair`, `step_free`) |

## `lyon_nearby_facilities`

Équipements urbains à proximité.

| Argument | Type | Défaut | Bornes |
|----------|------|--------|--------|
| `location` | PlaceRef | requis | |
| `categories` | list[str] | requis | ≤ 20 (`toilet`, `drinking_water`, `park`, `bike_pump`, `velov_station`…) |
| `radius_m` | int | 1000 | 50–5000 |
| `open_at` | datetime \| null | – | |
| `limit_per_category` | int | 5 | 1–20 |

## `lyon_environment_brief`

Indicateurs environnementaux disponibles (pollen, qualité de l’air, chaleur). Les indicateurs sans source résolue renvoient `UNSUPPORTED_INDICATOR` sans bloquer les autres.

| Argument | Type | Bornes |
|----------|------|--------|
| `location` | PlaceRef | requis |
| `at` | datetime \| null | |
| `indicators` | list[str] \| null | ≤ 10 (`pollen`, `air_quality`, `heat`) |

## `lyon_waste_dropoff`

Classe un objet (taxonomie déterministe, sans LLM) et propose des déchèteries / points de collecte.

| Argument | Type | Défaut | Bornes |
|----------|------|--------|--------|
| `item` | string | requis | 1–200 car. |
| `location` | PlaceRef \| null | – | |
| `transport` | string | `car` | ≤ 20 |
| `open_at` | datetime \| null | – | |
| `radius_m` | int | 15000 | 100–50000 |
| `limit` | int | 10 | 1–20 |

## `lyon_personal_briefing`

Briefing selon un profil local (trajet habituel). N’envoie aucune notification : le déclenchement et la livraison sont à la charge du client MCP appelant.

| Argument | Type | Défaut | Bornes |
|----------|------|--------|--------|
| `profile` | string | requis | 1–64 car. |
| `at` | datetime \| null | – | |
| `compare_with_previous` | bool | `true` | |

---

## Outils volontairement absents

Aucun outil « admin » ni requête brute n’est exposé au MCP : pas de `datagrandlyon_query(table, filters, url)`, pas d’URL/SQL/CQL arbitraire. Ces opérations restent internes/CLI. C’est une garantie de sécurité de la surface publique.
