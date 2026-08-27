#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

log() { printf '\n[subs] %s\n' "$*"; }
die() { printf '\n[subs] ERRO: %s\n' "$*" >&2; exit 1; }

[[ "${EUID}" -eq 0 ]] || die "execute este instalador como root: sudo ./scripts/install-hestia.sh ..."
command -v docker >/dev/null 2>&1 || die "Docker não encontrado. Instale Docker antes de continuar."
docker compose version >/dev/null 2>&1 || die "Docker Compose não encontrado. Instale o plugin docker-compose-plugin antes de continuar."
command -v curl >/dev/null 2>&1 || die "curl não encontrado. Instale curl antes de continuar."
HESTIA_ROOT="/usr/local/hestia"
[[ -x "$HESTIA_ROOT/bin/v-restart-proxy" ]] || die "HestiaCP não encontrado (v-restart-proxy ausente)."
[[ -x "$HESTIA_ROOT/bin/v-list-web-domain" ]] || die "CLI do HestiaCP não encontrada."
[[ -d "$HESTIA_ROOT/data/templates/web/nginx" ]] || die "diretório de templates Nginx do Hestia não encontrado."

DOMAIN="${1:-}"
HESTIA_USER="${2:-}"
ENV_FILE="${ENV_FILE:-$ROOT_DIR/.env}"

[[ -n "$DOMAIN" ]] || die "uso: sudo ./scripts/install-hestia.sh legenda.seudominio.com usuario_hestia"
[[ -n "$HESTIA_USER" ]] || die "informe o usuário Hestia dono do domínio"
[[ "$DOMAIN" != */* && "$DOMAIN" != *:* ]] || die "domínio inválido"
[[ -f "$ENV_FILE" ]] || die "arquivo de ambiente não encontrado: $ENV_FILE"

if ! id "$HESTIA_USER" >/dev/null 2>&1; then
  die "usuário Hestia não encontrado: $HESTIA_USER"
fi

if ! "$HESTIA_ROOT/bin/v-list-web-domain" "$HESTIA_USER" "$DOMAIN" json >/dev/null 2>&1; then
  die "domínio não encontrado no Hestia: $DOMAIN (usuário: $HESTIA_USER)"
fi

LOCAL_PROXY_PORT="$(awk -F= '$1 == "LOCAL_PROXY_PORT" {print $2}' "$ENV_FILE" | tail -n 1)"
LOCAL_PROXY_PORT="${LOCAL_PROXY_PORT:-18180}"
if [[ "$LOCAL_PROXY_PORT" == "8081" ]]; then
  sed -i 's/^LOCAL_PROXY_PORT=.*/LOCAL_PROXY_PORT=18180/' "$ENV_FILE"
  LOCAL_PROXY_PORT=18180
  log "porta antiga 8081 detectada; migrada automaticamente para 18180"
fi
[[ "$LOCAL_PROXY_PORT" == "18180" ]] || die "LOCAL_PROXY_PORT precisa ser 18180 para coincidir com o template Hestia subs"

if [[ -f "$HESTIA_ROOT/conf/hestia.conf" ]]; then
  # shellcheck disable=SC1091
  source "$HESTIA_ROOT/conf/hestia.conf"
fi
HESTIA_BIN="${HESTIA:-$HESTIA_ROOT}/bin"

if docker info >/dev/null 2>&1; then
  DOCKER=(docker)
else
  DOCKER=(sudo docker)
fi
COMPOSE=("${DOCKER[@]}" compose --env-file "$ENV_FILE")

log "validando configuração do Compose"
"${COMPOSE[@]}" config --quiet

log "instalando templates subs no HestiaCP"
install -o root -g root -m 0644 deploy/hestia/subs.tpl "$HESTIA_ROOT/data/templates/web/nginx/subs.tpl"
install -o root -g root -m 0644 deploy/hestia/subs.stpl "$HESTIA_ROOT/data/templates/web/nginx/subs.stpl"

log "subindo o MariaDB isolado"
"${COMPOSE[@]}" up -d mariadb

log "construindo somente o app"
"${COMPOSE[@]}" build app
"${COMPOSE[@]}" up -d --no-deps app

log "subindo o Nginx interno"
"${COMPOSE[@]}" up -d nginx-files

log "validando serviço local em 127.0.0.1:18180"
for attempt in $(seq 1 30); do
  if curl -fsS --max-time 2 http://127.0.0.1:18180/healthz >/dev/null 2>&1; then
    break
  fi
  [[ "$attempt" -eq 30 ]] && die "o Nginx interno não respondeu em 127.0.0.1:18180"
  sleep 2
done

log "configurando proxy do domínio no HestiaCP"
PROXY_EXT="jpg,jpeg,gif,png,webp,ico,svg,css,js,zip,tgz,gz,rar,bz2,doc,xls,exe,pdf,ppt,txt,odt,ods,odp,odf,tar,wav,bmp,rtf,mp3,avi,mpeg,flv,html,htm,srt,ass,ssa,vtt,sub"
DOMAIN_INFO="$("$HESTIA_BIN/v-list-web-domain" "$HESTIA_USER" "$DOMAIN" json 2>/dev/null || true)"
if grep -Eq '"PROXY"[[:space:]]*:[[:space:]]*"[^"]+"' <<<"$DOMAIN_INFO"; then
  "$HESTIA_BIN/v-change-web-domain-proxy-tpl" "$HESTIA_USER" "$DOMAIN" subs "$PROXY_EXT" yes
else
  "$HESTIA_BIN/v-add-web-domain-proxy" "$HESTIA_USER" "$DOMAIN" subs "$PROXY_EXT" yes
fi

log "testando a configuração do proxy do Hestia"
"$HESTIA_BIN/v-restart-proxy" yes
if command -v nginx >/dev/null 2>&1; then
  nginx -t
fi

cat <<EOF

[subs] INSTALAÇÃO CONCLUÍDA

Domínio:    https://$DOMAIN
Upstream:   http://127.0.0.1:18180
Template:   subs

O HestiaCP continua na frente terminando TLS e encaminhando o domínio para o Nginx interno Docker.
A aplicação e o MariaDB não foram publicados diretamente na Internet.

Comandos úteis:
  ${COMPOSE[*]} ps
  ${COMPOSE[*]} logs -f app
  ${COMPOSE[*]} logs -f nginx-files
EOF
