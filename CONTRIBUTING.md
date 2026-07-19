# Contribuer

Merci de votre intérêt ! Ce projet vise un serveur MCP local, honnête et testable
pour l’open data de la Métropole de Lyon.

## Mise en place

```bash
uv sync --all-extras --dev
make setup-offline        # config + fixtures, aucun compte requis
```

## Boucle de développement

```bash
uv run ruff format .          # format
uv run ruff check .           # lint
uv run mypy src               # types (strict)
uv run pytest -m "not live"   # tests offline (sans réseau ni credentials)
# ou tout d'un coup :
make quality
```

Lancer **un seul test** :

```bash
uv run pytest tests/unit/test_waste.py::test_classify_battery -q
```

## Règles

1. **Tests offline par défaut** — un `pytest` nu doit passer sans réseau ni identifiants.
2. Le **domaine n’importe jamais** le SDK MCP, `httpx` ni `aiosqlite` (cf. `docs/architecture.md`).
3. `ruff` + `mypy --strict` doivent rester verts. Pas de `type: ignore` ni de `except Exception` silencieux non justifiés.
4. **Jamais de secret** dans un commit, une fixture, un log ou une sortie MCP.
5. Documenter les décisions non triviales dans un ADR (`docs/adr/`).
6. Rester fidèle au principe d’**honnêteté** : ne jamais présenter une donnée théorique (GTFS) comme temps réel, ni inventer une valeur absente.

## Enregistrer une fixture

Les tests utilisent des fixtures locales (`grand_lyon_mcp/_data/fixtures/`). Pour en
capturer une nouvelle depuis une vraie source :

```bash
python scripts/record_fixture.py --url <URL_ALLOWLISTÉE> --out <chemin>.json --confirm
```

Le script vérifie l’allowlist de host, supprime les en-têtes et les identifiants, et
écrit dans `tests/fixtures/recorded/` (scratch local, gitignored). **Inspectez la
redaction** avant de promouvoir une fixture dans le paquet.

## Flux de contribution

1. Forkez et créez une branche (`feat/…`, `fix/…`).
2. Implémentez + tests offline, gardez `make quality` vert.
3. Ouvrez une Pull Request en décrivant le _pourquoi_. La CI (`ruff`/`mypy`/`pytest`/`build`) doit passer.
4. Les commits peuvent être en français ou en anglais ; soyez descriptif.

Toute contribution est soumise au [Code de conduite](CODE_OF_CONDUCT.md) et publiée sous
licence [MIT](LICENSE).
