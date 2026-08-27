#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

if [[ -f .env ]]; then
  echo ".env já existe em $ROOT_DIR; nada foi sobrescrito."
  exit 0
fi

command -v openssl >/dev/null 2>&1 || { echo "openssl é obrigatório." >&2; exit 1; }

read -r -p "URL pública [https://subtitle.oldagesubs.com.br]: " BASE_URL
BASE_URL="${BASE_URL:-https://subtitle.oldagesubs.com.br}"
read -r -p "E-mail do administrador: " ADMIN_EMAIL
if [[ -z "$ADMIN_EMAIL" || "$ADMIN_EMAIL" != *@*.* ]]; then
  echo "Informe um e-mail válido." >&2
  exit 1
fi

while true; do
  read -r -s -p "Senha do administrador (mínimo 12 caracteres, sem espaços, #, = ou $): " ADMIN_PASSWORD
  echo
  if [[ ${#ADMIN_PASSWORD} -lt 12 ]]; then
    echo "A senha precisa ter pelo menos 12 caracteres." >&2
    continue
  fi
  if [[ "$ADMIN_PASSWORD" =~ [[:space:]#=\$] ]]; then
    echo "Use uma senha sem espaços, #, = ou $." >&2
    continue
  fi
  break
done

SESSION_SECRET="$(openssl rand -hex 32)"
DB_PASSWORD="$(openssl rand -hex 24)"
DB_ROOT_PASSWORD="$(openssl rand -hex 24)"

umask 077
cat > .env <<EOF
BASE_URL=$BASE_URL
COOKIE_SECURE=true
LOCAL_PROXY_PORT=18180
MAX_UPLOAD_MB=25
SESSION_TTL_HOURS=168
DOWNLOAD_LINK_TTL_HOURS=24
SESSION_SECRET=$SESSION_SECRET
DB_PASSWORD=$DB_PASSWORD
DB_ROOT_PASSWORD=$DB_ROOT_PASSWORD
ADMIN_EMAIL=$ADMIN_EMAIL
ADMIN_PASSWORD=$ADMIN_PASSWORD
EOF

chmod 600 .env
echo ".env criado com permissão 600. Não o versionar nem enviar para o GitHub."
