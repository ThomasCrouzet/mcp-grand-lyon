# Attributions

`grand-lyon-mcp` agrège des données ouvertes de tiers et s’appuie sur des logiciels libres. Le **code** de ce dépôt est sous licence [MIT](LICENSE). Les **données** restituées à l’exécution appartiennent à leurs producteurs et sont soumises à leurs propres licences et obligations d’attribution, à respecter par tout déployeur qui rediffuse ces données.

> Projet **indépendant**, non affilié à la Métropole de Lyon, SYTRAL Mobilités, Keolis-TCL ni JCDecaux. Les marques citées appartiennent à leurs titulaires et sont employées de façon descriptive.

## Sources de données

| Source | Producteur / responsable | Portail | Licence (à vérifier par jeu) |
|--------|--------------------------|---------|------------------------------|
| Portail open data (parkings, trafic, équipements, déchets, environnement…) | **Métropole de Lyon** | [data.grandlyon.com](https://data.grandlyon.com/) | Majoritairement Licence Ouverte / Open Licence (Etalab): attribution requise |
| GTFS théorique, prochains passages, SIRI temps réel, accessibilité TCL | **SYTRAL Mobilités** (réseau TCL) | data.grandlyon.com | Selon le jeu ; certains flux temps réel peuvent avoir des conditions propres |
| Disponibilités Vélo’v | **JCDecaux** (exploitant), via la Métropole de Lyon | data.grandlyon.com | Selon le jeu |
| Géocodage (adresses / lieux) | Instance **Photon** de la Métropole de Lyon (moteur [Photon](https://github.com/komoot/photon), Apache-2.0) sur données [OpenStreetMap](https://www.openstreetmap.org/copyright) | download.data.grandlyon.com | Données OSM sous **ODbL**: attribution « © les contributeurs OpenStreetMap » |
| Calcul d’itinéraire (optionnel, désactivé par défaut) | [Transitous](https://transitous.org/): service communautaire bénévole (moteur MOTIS) + feeds GTFS des réseaux | api.transitous.org | Service gratuit à usage équitable ; feeds sous licences propres |

**Conformité de réutilisation.** La licence exacte de chaque jeu doit être lue dans ses métadonnées sur le portail (cf. [`docs/data-sources.md`](docs/data-sources.md)). Ce dépôt **ne redistribue aucun jeu de données réel** : seules des fixtures synthétiques/anonymisées de démonstration sont incluses (`grand_lyon_mcp/_data/fixtures/`). L’attribution de chaque source est par ailleurs transportée au runtime dans l’enveloppe MCP (champ `sources[].attribution`).

**Usage équitable des services tiers gratuits.** L’instance Photon de la Métropole et l’API Transitous sont des services partagés. Un déploiement à fort volume devrait envisager d’auto-héberger (Photon, MOTIS) plutôt que de solliciter ces instances publiques. L’URL Transitous est configurable (`TRANSITOUS_BASE_URL`) et la fonctionnalité est désactivée par défaut.

## Dépendances logicielles

Toutes les dépendances runtime sont sous licences permissives (MIT / BSD / Apache-2.0) : `mcp`, `httpx`, `pydantic`, `pydantic-settings`, `aiosqlite`, `pyyaml`, `typer`, `tenacity`, `platformdirs`, `rapidfuzz`, `shapely`, `pyproj`, `python-dateutil`. Aucune dépendance copyleft au niveau du paquet.

> Note : les wheels de `shapely` et `pyproj` embarquent respectivement **GEOS** (LGPL-2.1) et **PROJ** ; ces binaires sont résolus à l’installation par leurs paquets amont et ne sont pas redistribués par ce dépôt.
