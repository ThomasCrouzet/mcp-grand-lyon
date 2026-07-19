# Changelog

Format inspiré de [Keep a Changelog](https://keepachangelog.com/fr/1.1.0/) ;
versionnage [SemVer](https://semver.org/lang/fr/).

## [0.1.0] — 2026-07-19

Première version publique open source.

### Ajouté
- Serveur MCP stdio exposant **10 outils** métier `lyon_*` en lecture seule (transit TCL, Vélo’v, parkings/P+R, trafic, accessibilité, équipements, environnement, déchets, itinéraires, briefings).
- CLI `grand-lyon-mcp` : `serve`, `doctor`, `catalog scan|validate`, `sync gtfs|static`, `snapshot velov`, `db migrate|info`, `setup`, `env`, `client-config`, `smoke`, `version`.
- Mode **offline/fixtures** (sans compte ni réseau) et mode **live** DataGrandLyon.
- Providers : DataPusher, OGC API Features, GTFS, SIRI Lite, Photon, Transitous (optionnel).
- Stockage SQLite (FTS5 + RTree), cache HTTP (ETag/Last-Modified, stale-on-error, single-flight), redaction systématique des secrets, allowlist réseau anti-SSRF.
- Paquet **installable** (`pip`/`uvx`) : templates de config et fixtures de démo empaquetés (`grand_lyon_mcp/_data`), résolus via `importlib.resources`.
- Documentation FR (README, `docs/tools.md`, `docs/mcp-clients.md`, `docs/architecture.md`, `docs/data-sources.md`, ADR) ; `ATTRIBUTIONS.md`, `SECURITY.md`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`.
- CI GitHub Actions : lint/format (ruff), types (mypy strict), tests offline (pytest, matrice 3.12/3.13), build + smoke du wheel, scan de secrets (gitleaks), CodeQL, publication PyPI (Trusted Publishing).

[0.1.0]: https://github.com/ThomasCrouzet/mcp-grand-lyon/releases/tag/v0.1.0
