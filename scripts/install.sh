#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

DOMAIN="${1:-}"
HESTIA_USER="${2:-}"

if [[ -z "$DOMAIN" || -z "$HESTIA_USER" ]]; then
  echo "Uso: sudo ./scripts/install-hestia.sh legenda.seudominio.com usuario_hestia" >&2
  exit 1
fi

if [[ ! -f "$ROOT_DIR/.env" ]]; then
  echo "Primeira execução: criando .env seguro..."
  ./scripts/bootstrap-env.sh
fi

if [[ "${EUID}" -ne 0 ]]; then
  exec sudo --preserve-env=ENV_FILE "$ROOT_DIR/scripts/install-hestia.sh" "$DOMAIN" "$HESTIA_USER"
fi

exec "$ROOT_DIR/scripts/install-hestia.sh" "$DOMAIN" "$HESTIA_USER"
