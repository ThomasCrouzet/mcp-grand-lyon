# grand-lyon-mcp

[![CI](https://github.com/ThomasCrouzet/mcp-grand-lyon/actions/workflows/ci.yml/badge.svg)](https://github.com/ThomasCrouzet/mcp-grand-lyon/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Python](https://img.shields.io/badge/python-3.12%20%7C%203.13-blue.svg)](pyproject.toml)

**FR** | [English below](#english)

Serveur **MCP** local qui agrège les services ouverts de la **Métropole de Lyon** (TCL, Vélo’v, parkings, trafic, équipements, déchets, briefings) derrière **10 outils métier** en lecture seule. Se branche sur n’importe quel client MCP en stdio (Claude Desktop, Cursor, agents maison…).

> Les identifiants DataGrandLyon ne sont **jamais** versionnés, loggés ou renvoyés par les outils.

## Fonctionnalités

- Prochains passages TCL (temps réel + repli GTFS théorique)
- Alertes / mobilité / accessibilité
- Vélo’v + historique local
- Parkings & P+R
- Trafic & événements routiers
- Équipements (toilettes, fontaines, parcs…)
- Environnement (indicateurs optionnels)
- Classification déchets + déchèteries
- Comparaison d’options de trajet
- Briefings personnels configurables

## Architecture

```text
MCP adapter → application services → domain protocols → providers → HTTP / SQLite
```

Le code métier n’importe pas le SDK MCP (`adapters/mcp/` uniquement).

## Prérequis

- Python ≥ 3.12
- [uv](https://docs.astral.sh/uv/)
- Compte [DataGrandLyon](https://data.grandlyon.com/) (optionnel : sans compte → mode offline/fixtures)

## Installation

### Assistant recommandé

```bash
git clone git@github.com:ThomasCrouzet/mcp-grand-lyon.git
cd mcp-grand-lyon
make setup              # TUI interactive (gum si dispo, sinon prompts)
# ou sans interaction :
make setup-offline      # fixtures, aucun compte requis
```

L’assistant écrit les secrets dans `secrets.env` (chmod 600), copie les YAML de config, installe les deps, migre SQLite et lance `doctor`.

> **Chemin des secrets selon l’OS** (résolu par [platformdirs](https://pypi.org/project/platformdirs/)) :
> - Linux : `~/.config/grand-lyon-mcp/secrets.env`
> - macOS : `~/Library/Application Support/grand-lyon-mcp/secrets.env`
> - Windows : `%APPDATA%\grand-lyon-mcp\secrets.env`
>
> `grand-lyon-mcp env` affiche le chemin réel résolu sur votre machine. Vous pouvez aussi forcer un répertoire via `GRAND_LYON_MCP_CONFIG_DIR`.

### Manuel

```bash
uv sync --all-extras --dev
cp .env.example "$(uv run grand-lyon-mcp env --config-dir)/secrets.env"
# éditer : DATAGRANDLYON_USERNAME / PASSWORD, puis chmod 600
make migrate
```

Variables : voir `.env.example`. Le Makefile charge automatiquement `secrets.env` et un `.env` local gitignored.

```bash
make help               # toutes les cibles
make env                # état config (secrets masqués)
make serve OFFLINE=true LOG_LEVEL=DEBUG
make client-config      # bloc mcpServers JSON (chemins absolus)
```

## Configuration

Les templates de configuration (`settings`, `sources`, `profiles`, `waste-taxonomy`) sont **livrés avec le paquet** (`grand_lyon_mcp/_data/config/`). L'assistant les copie dans votre répertoire de config :

```bash
grand-lyon-mcp setup            # copie les YAML + migre (ou : make setup)
```

Pour les inspecter ou repartir des templates empaquetés manuellement :

```bash
python -c "from grand_lyon_mcp.resources import config_dir; print(config_dir())"
```

## Migrations & données

```bash
uv run grand-lyon-mcp db migrate
uv run grand-lyon-mcp db info
uv run grand-lyon-mcp doctor
uv run grand-lyon-mcp catalog scan
uv run grand-lyon-mcp catalog validate
uv run grand-lyon-mcp sync gtfs
# offline GTFS :
uv run grand-lyon-mcp sync gtfs --from-file tests/fixtures/gtfs/mini_gtfs.zip
uv run grand-lyon-mcp snapshot velov
```

## Démarrage MCP (stdio)

```bash
uv run grand-lyon-mcp serve --transport stdio
# ou
./scripts/run_mcp.sh
```

`stdout` est réservé au protocole MCP ; les logs applicatifs vont sur **stderr**.

## Brancher un client MCP

Le serveur parle le protocole MCP en **stdio** : il fonctionne avec tout client compatible. Récupérez un bloc prêt à coller (chemins absolus) avec :

```bash
make client-config      # ou : uv run grand-lyon-mcp client-config
```

Exemple de configuration **Claude Desktop** (`claude_desktop_config.json`) :

```json
{
  "mcpServers": {
    "grand-lyon": {
      "command": "/chemin/absolu/mcp-grand-lyon/scripts/run_mcp.sh",
      "args": ["serve", "--transport", "stdio"]
    }
  }
}
```

Le wrapper `scripts/run_mcp.sh` charge `secrets.env` hors dépôt et bascule en offline si aucun identifiant n’est présent. Le même bloc convient à Cursor et aux autres clients MCP stdio. Détails et alternatives (env hérité, secrets) : [`docs/mcp-clients.md`](docs/mcp-clients.md).

## Outils MCP (10)

| Outil | Rôle |
|-------|------|
| `lyon_resolve_place` | Résolution de lieux (adresse, arrêt, station, lieu personnel) |
| `lyon_next_departures` | Prochains passages TCL |
| `lyon_mobility_status` | Alertes & trafic |
| `lyon_trip_options` | Comparaison de modes |
| `lyon_parking_options` | Parkings / P+R |
| `lyon_accessibility_check` | Accessibilité |
| `lyon_nearby_facilities` | Équipements |
| `lyon_environment_brief` | Environnement |
| `lyon_waste_dropoff` | Déchets |
| `lyon_personal_briefing` | Briefing profil |

Aucun outil admin / requête brute DataGrandLyon n’est exposé. Arguments détaillés et exemples d’entrée/sortie : [`docs/tools.md`](docs/tools.md).

Exemple; `lyon_next_departures` :

```jsonc
// entrée
{ "stop": { "query": "Bellecour" }, "line": "A", "limit": 3 }
// sortie (extrait de l'enveloppe)
{
  "status": "ok",
  "data": { "departures": [
    { "line_name": "A", "destination": "Vaulx-en-Velin La Soie",
      "expected_at": "2026-07-20T08:05:00+02:00", "realtime": true }
  ] },
  "sources": [ { "provider": "DataGrandLyon", "attribution": "SYTRAL Mobilités", "realtime": true } ]
}
```

## Tests

```bash
make quality
# ou
uv run ruff format --check .
uv run ruff check .
uv run mypy src
uv run pytest -m "not live" --cov
```

Tests live : `RUN_LIVE_TESTS=1` + credentials. La suite par défaut tourne **sans réseau ni identifiants**.

## Documentation

| Doc | Contenu |
|-----|---------|
| [`docs/architecture.md`](docs/architecture.md) | Architecture |
| [`docs/tools.md`](docs/tools.md) | Les 10 outils MCP (arguments, exemples) |
| [`docs/mcp-clients.md`](docs/mcp-clients.md) | Configuration des clients MCP |
| [`docs/data-sources.md`](docs/data-sources.md) | Sources & licences |
| [`ATTRIBUTIONS.md`](ATTRIBUTIONS.md) | Attributions open data & dépendances |
| [`docs/operations.md`](docs/operations.md) | Exploitation |
| [`docs/troubleshooting.md`](docs/troubleshooting.md) | Dépannage |
| [`docs/adr/`](docs/adr/) | Décisions d’architecture |

## Limites (v0.1)

- Transitous optionnel (feature flag) ; sans routeur, `lyon_trip_options` reste partiel pour TCL.
- Indicateurs environnementaux activés seulement si une source est résolue.
- Transport HTTP MCP non inclus (stdio uniquement).

## Vie privée / RGPD

- **100 % local, aucune télémétrie.** Le serveur tourne sur votre machine ; les profils (domicile/travail), briefings et l’historique Vélo’v restent dans une base SQLite locale.
- **Ce qui quitte la machine** (mode live uniquement) : les requêtes et coordonnées nécessaires au géocodage et au calcul d’itinéraire sont envoyées aux services concernés (DataGrandLyon, instance Photon de la Métropole, Transitous si activé), soumis à leurs propres politiques.
- Les profils d’exemple (`profiles.example.yaml`) n’utilisent que des lieux génériques (Bellecour, Lyon 3ᵉ): aucune donnée personnelle réelle n’est versionnée.

## Licence & attribution

Code sous licence **MIT** (voir [`LICENSE`](LICENSE)). Les données proviennent de sources tierces avec leurs propres licences et obligations d’attribution, voir [`ATTRIBUTIONS.md`](ATTRIBUTIONS.md) et [`docs/data-sources.md`](docs/data-sources.md).

> Projet **indépendant**, non affilié à la Métropole de Lyon, SYTRAL Mobilités, Keolis-TCL ni JCDecaux. « TCL », « Vélo’v » et les autres noms cités sont des marques de leurs titulaires respectifs, employées ici de façon purement descriptive.

---

<a id="english"></a>

## English

Local **MCP server** aggregating **Métropole de Lyon** open data behind **10 read-only business tools**. Works with any stdio MCP client (Claude Desktop, Cursor, custom agents).

### Install

```bash
uv sync --all-extras --dev
uv run grand-lyon-mcp db migrate
uv run grand-lyon-mcp doctor
uv run grand-lyon-mcp serve --transport stdio
```

Credentials via env only (`DATAGRANDLYON_USERNAME` / `DATAGRANDLYON_PASSWORD`), never committed, logged, or returned by tools. Application logs go to **stderr**; **stdout** is MCP protocol only. Run without credentials → offline mode (fixtures).

### Connect a client

Get a ready-to-paste `mcpServers` block with `make client-config`. See [`docs/mcp-clients.md`](docs/mcp-clients.md).

### Quality gate

```bash
uv run ruff format --check . && uv run ruff check . && uv run mypy src
uv run pytest -m "not live" --cov
```

### Docs & licensing

Architecture, tools, data sources/licenses, operations and troubleshooting under `docs/`. Attributions in [`ATTRIBUTIONS.md`](ATTRIBUTIONS.md). Code is MIT; data belongs to its respective providers. Independent project, not affiliated with Métropole de Lyon, SYTRAL, Keolis-TCL or JCDecaux.
