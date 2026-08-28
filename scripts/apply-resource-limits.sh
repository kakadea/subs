#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

ENV_FILE="${ENV_FILE:-$ROOT_DIR/.env}"
if [[ ! -f "$ENV_FILE" ]]; then
  echo "Arquivo de ambiente não encontrado: $ENV_FILE" >&2
  exit 1
fi

if docker info >/dev/null 2>&1; then
  DOCKER=(docker)
else
  DOCKER=(sudo docker)
fi

COMPOSE=("${DOCKER[@]}" compose --env-file "$ENV_FILE")
"${COMPOSE[@]}" config --quiet

echo "Aplicando limites somente nos containers do subs..."
"${COMPOSE[@]}" up -d --no-deps --force-recreate mariadb nginx-files app
"${COMPOSE[@]}" ps

echo
echo "Limites aplicados. Nenhum volume foi removido e nenhum outro projeto Docker foi alterado."
