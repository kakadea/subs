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

if ! "${COMPOSE[@]}" ps --status running --services | grep -qx 'app'; then
  echo "O serviço app não está em execução. Suba-o antes de trocar a senha." >&2
  exit 1
fi

if [[ $# -gt 1 ]]; then
  echo "Uso: $0 [ADMIN_EMAIL]" >&2
  exit 1
fi

email="${1:-}"
if [[ -z "$email" ]]; then
  read -r -p "E-mail do administrador: " email
fi
if [[ -z "$email" ]]; then
  echo "E-mail do administrador não pode ficar vazio." >&2
  exit 1
fi

read -r -s -p "Nova senha (mínimo 12 caracteres): " password
printf '\n'
read -r -s -p "Repita a nova senha: " confirmation
printf '\n'

if [[ "$password" != "$confirmation" ]]; then
  echo "As senhas não conferem." >&2
  exit 1
fi
if [[ "${#password}" -lt 12 ]]; then
  echo "A senha precisa ter pelo menos 12 caracteres." >&2
  exit 1
fi

# A senha segue somente por stdin. Ela não aparece na lista de processos,
# no histórico do shell, no .env ou nos logs da aplicação.
printf '%s\n' "$password" | "${COMPOSE[@]}" exec -T app /app/subs admin-password "$email"
unset password confirmation

echo "Troca concluída. Todas as sessões administrativas anteriores foram invalidadas; faça login novamente."
