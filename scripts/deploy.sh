#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

ENV_FILE="${ENV_FILE:-$ROOT_DIR/.env}"
if [[ ! -f "$ENV_FILE" ]]; then
  echo "Arquivo de ambiente não encontrado: $ENV_FILE" >&2
  echo "Execute: ./scripts/bootstrap-env.sh" >&2
  exit 1
fi

if docker info >/dev/null 2>&1; then
  DOCKER=(docker)
else
  DOCKER=(sudo docker)
fi

COMPOSE=("${DOCKER[@]}" compose --env-file "$ENV_FILE")
"${COMPOSE[@]}" config --quiet
"${COMPOSE[@]}" pull
"${COMPOSE[@]}" build --pull app
"${COMPOSE[@]}" up -d --remove-orphans
"${COMPOSE[@]}" ps

echo
echo "Deploy concluído. Verifique: ${COMPOSE[*]} logs -f app"
