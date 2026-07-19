#!/usr/bin/env bash
# Wrapper client-MCP : charge les secrets hors dépôt puis lance le serveur stdio.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SECRETS_FILE="${GRAND_LYON_MCP_SECRETS_FILE:-${HOME}/.config/grand-lyon-mcp/secrets.env}"
SECRETS_FILE_MACOS="${HOME}/Library/Application Support/grand-lyon-mcp/secrets.env"
LOCAL_ENV="${ROOT}/.env"
BIN="${ROOT}/.venv/bin/grand-lyon-mcp"

load_env_file() {
  local file="$1"
  if [[ -r "${file}" ]]; then
    set -a
    # shellcheck disable=SC1090
    source "${file}"
    set +a
    return 0
  fi
  return 1
}

# Ordre : secrets utilisateur (Linux ou macOS platformdirs), puis .env local
if ! load_env_file "${SECRETS_FILE}"; then
  load_env_file "${SECRETS_FILE_MACOS}" || true
fi
load_env_file "${LOCAL_ENV}" || true

if [[ ! -x "${BIN}" ]]; then
  echo "Binaire introuvable: ${BIN}" >&2
  echo "Lancez: make install   (ou uv sync --all-extras --dev)" >&2
  exit 1
fi

# Sans credentials et sans offline explicite → offline pour rester utilisable
if [[ -z "${DATAGRANDLYON_USERNAME:-}" || -z "${DATAGRANDLYON_PASSWORD:-}" ]]; then
  if [[ -z "${GRAND_LYON_MCP_OFFLINE:-}" ]]; then
    export GRAND_LYON_MCP_OFFLINE=true
    echo "grand-lyon-mcp: pas d'identifiants DataGrandLyon → OFFLINE=true (fixtures)" >&2
  fi
fi

exec "${BIN}" serve --transport stdio
