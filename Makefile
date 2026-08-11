# grand-lyon-mcp: commandes de développement et d'installation
#
#   make help          liste les cibles
#   make setup         assistant TUI (recommandé pour démarrer)
#   make setup-offline setup non interactif sans credentials

SHELL := /bin/bash
.DEFAULT_GOAL := help

ROOT        := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))
UV          := uv
RUN         := $(UV) run
CLI         := $(RUN) grand-lyon-mcp
WRAPPER     := $(ROOT)/scripts/run_mcp.sh
# platformdirs : Linux ~/.config/…: macOS ~/Library/Application Support/…
SECRETS_FILE ?= $(HOME)/.config/grand-lyon-mcp/secrets.env
SECRETS_FILE_MACOS ?= $(HOME)/Library/Application Support/grand-lyon-mcp/secrets.env

# Charge secrets + .env local dans un sous-shell (pas d'include Make : évite fuites / parsing)
define WITH_ENV
	set -euo pipefail; \
	if [ -r "$(SECRETS_FILE)" ]; then set -a; source "$(SECRETS_FILE)"; set +a; \
	elif [ -r "$(SECRETS_FILE_MACOS)" ]; then set -a; source "$(SECRETS_FILE_MACOS)"; set +a; fi; \
	if [ -r "$(ROOT)/.env" ]; then set -a; source "$(ROOT)/.env"; set +a; fi; \
	$(if $(OFFLINE),export GRAND_LYON_MCP_OFFLINE="$(OFFLINE)";) \
	$(if $(LOG_LEVEL),export GRAND_LYON_MCP_LOG_LEVEL="$(LOG_LEVEL)";) \
	$(if $(CONFIG_DIR),export GRAND_LYON_MCP_CONFIG_DIR="$(CONFIG_DIR)";) \
	$(if $(DATA_DIR),export GRAND_LYON_MCP_DATA_DIR="$(DATA_DIR)";) \
	$(if $(DB_PATH),export GRAND_LYON_MCP_DB_PATH="$(DB_PATH)";)
endef

.PHONY: help setup setup-offline setup-live install env env-edit client-config \
	migrate db-info doctor serve serve-wrapper smoke version \
	catalog-scan catalog-validate sync-gtfs sync-gtfs-fixture snapshot-velov \
	format lint typecheck test test-live cov quality clean print-env

##@ Démarrage

help: ## Affiche cette aide
	@awk 'BEGIN {FS = ":.*##"; printf "\n\033[1mgrand-lyon-mcp\033[0m, cibles Make\n\n"} \
		/^##@/ {printf "\n\033[1m%s\033[0m\n", substr($$0, 5)} \
		/^[a-zA-Z0-9_-]+:.*?##/ {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)
	@echo ""
	@echo "Variables utiles (make serve OFFLINE=true LOG_LEVEL=DEBUG) :"
	@echo "  SECRETS_FILE  chemin secrets (défaut: $(SECRETS_FILE))"
	@echo "  OFFLINE       true|false → GRAND_LYON_MCP_OFFLINE"
	@echo "  LOG_LEVEL     INFO|DEBUG|…"
	@echo "  CONFIG_DIR DATA_DIR DB_PATH"
	@echo ""

setup: install ## Assistant TUI : demande identifiant/mdp DataGrandLyon
	$(CLI) setup

setup-offline: install ## Setup sans compte (fixtures, non interactif)
	$(CLI) setup --yes --offline

setup-live: install ## Setup live non interactif (DATAGRANDLYON_* déjà exportés)
	@if [ -z "$${DATAGRANDLYON_USERNAME:-}" ] || [ -z "$${DATAGRANDLYON_PASSWORD:-}" ]; then \
		echo "Exportez DATAGRANDLYON_USERNAME et DATAGRANDLYON_PASSWORD, ou utilisez: make setup"; \
		exit 2; \
	fi
	$(CLI) setup --yes --live

install: ## Installe le package + deps dev (uv)
	$(UV) sync --all-extras --dev
	@chmod +x $(ROOT)/scripts/run_mcp.sh $(ROOT)/scripts/record_fixture.py 2>/dev/null || true

##@ Configuration

env: ## Affiche l'état de la config (secrets masqués)
	@$(WITH_ENV) $(CLI) env

env-edit: ## Ouvre secrets.env dans $$EDITOR
	$(CLI) env --edit

client-config: ## Affiche le bloc mcpServers JSON (Claude Desktop, Cursor…)
	$(CLI) client-config

print-env: ## Affiche les variables d'environnement actives (masquées)
	@$(WITH_ENV) \
	echo "GRAND_LYON_MCP_OFFLINE=$${GRAND_LYON_MCP_OFFLINE:-<unset>}"; \
	echo "GRAND_LYON_MCP_LOG_LEVEL=$${GRAND_LYON_MCP_LOG_LEVEL:-<unset>}"; \
	echo "GRAND_LYON_MCP_CONFIG_DIR=$${GRAND_LYON_MCP_CONFIG_DIR:-<unset>}"; \
	echo "GRAND_LYON_MCP_DATA_DIR=$${GRAND_LYON_MCP_DATA_DIR:-<unset>}"; \
	echo "GRAND_LYON_MCP_DB_PATH=$${GRAND_LYON_MCP_DB_PATH:-<unset>}"; \
	echo "TRANSITOUS_ENABLED=$${TRANSITOUS_ENABLED:-<unset>}"; \
	if [ -n "$${DATAGRANDLYON_USERNAME:-}" ]; then echo "DATAGRANDLYON_USERNAME=[set]"; else echo "DATAGRANDLYON_USERNAME=<unset>"; fi; \
	if [ -n "$${DATAGRANDLYON_PASSWORD:-}" ]; then echo "DATAGRANDLYON_PASSWORD=[set]"; else echo "DATAGRANDLYON_PASSWORD=<unset>"; fi; \
	if [ -r "$(SECRETS_FILE)" ]; then echo "SECRETS_FILE=$(SECRETS_FILE) (yes)"; \
	elif [ -r "$(SECRETS_FILE_MACOS)" ]; then echo "SECRETS_FILE=$(SECRETS_FILE_MACOS) (yes)"; \
	else echo "SECRETS_FILE=missing (tried Linux + macOS paths)"; fi

##@ Runtime

migrate: ## Applique les migrations SQLite
	@$(WITH_ENV) $(CLI) db migrate

db-info: ## Infos base (FTS5, RTree, migrations)
	@$(WITH_ENV) $(CLI) db info

doctor: ## Diagnostic installation (sans fuite de secrets)
	@$(WITH_ENV) $(CLI) doctor

serve: ## Serveur MCP stdio (silencieux = normal ; infos sur stderr)
	@echo "→ Démarrage MCP stdio (bandeau sur stderr)."
	@echo "  stdout = protocole JSON pour le client MCP, pas de prompt interactif."
	@echo "  Test local des outils : make smoke"
	@echo "  Config client MCP     : make client-config"
	@echo "  Arrêt                 : Ctrl+C"
	@echo ""
	@$(WITH_ENV) $(CLI) serve --transport stdio

serve-wrapper: ## Serveur via scripts/run_mcp.sh (même logique côté client MCP)
	@test -x $(WRAPPER) || chmod +x $(WRAPPER)
	@$(WRAPPER)

smoke: ## Teste les 10 outils en local (sans client MCP)
	@$(WITH_ENV) $(CLI) smoke

version: ## Version du package
	$(CLI) version

##@ Données (live)

catalog-scan: ## Scan des catalogues DataGrandLyon
	@$(WITH_ENV) $(CLI) catalog scan

catalog-validate: ## Validation bornée des sources résolues
	@$(WITH_ENV) $(CLI) catalog validate

sync-gtfs: ## Télécharge et importe le GTFS TCL
	@$(WITH_ENV) $(CLI) sync gtfs

sync-gtfs-fixture: ## Importe le mini GTFS de test (offline)
	@$(WITH_ENV) $(CLI) sync gtfs --from-file $(ROOT)/tests/fixtures/gtfs/mini_gtfs.zip

snapshot-velov: ## Snapshot disponibilités Vélo'v
	@$(WITH_ENV) $(CLI) snapshot velov

##@ Qualité

format: ## Formate le code (ruff)
	$(RUN) ruff format .
	$(RUN) ruff check --fix .

lint: ## Vérifie format + lint
	$(RUN) ruff format --check .
	$(RUN) ruff check .

typecheck: ## mypy strict sur src/
	$(RUN) mypy src

test: ## Tests offline + couverture
	$(RUN) pytest -m "not live" --cov --cov-report=term-missing

test-live: ## Tests live (RUN_LIVE_TESTS=1 + credentials)
	@$(WITH_ENV) RUN_LIVE_TESTS=1 $(RUN) pytest -m live -v

cov: test ## Alias de test

quality: lint typecheck test ## Gate qualité complète

clean: ## Nettoie caches Python / couverture
	rm -rf .pytest_cache .mypy_cache .ruff_cache .coverage htmlcov
	find $(ROOT) -type d -name __pycache__ -prune -exec rm -rf {} + 2>/dev/null || true
