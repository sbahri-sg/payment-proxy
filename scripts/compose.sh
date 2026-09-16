#!/bin/sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
LOCAL_ENV_FILE="$ROOT_DIR/.env"
STATE_DIR=${PAYMENT_PROXY_DEPLOY_STATE_DIR:-"$ROOT_DIR/.deploy"}
PRODUCTION_ENV_FILE="$STATE_DIR/production.env"

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

usage() {
  echo "Usage: sh scripts/compose.sh <compose-command> [arguments]"
  echo "Examples: up -d --build --wait | build | ps -a | logs --tail=100 | down"
  echo "APP_ENV in .env (or the shell) selects development or production."
  echo "First production up/build/config/create prepares persistent secrets and TLS."
}

# Read only the requested metadata, never execute .env or import its secrets.
dotenv_value() {
  [ -f "$LOCAL_ENV_FILE" ] || return 0
  awk -v name="$1" '
    {
      line=$0
      sub(/\r$/, "", line)
      sub(/^[[:space:]]*export[[:space:]]+/, "", line)
      equals=index(line, "=")
      if (!equals) next
      key=substr(line, 1, equals-1)
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", key)
      if (key != name) next
      value=substr(line, equals+1)
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", value)
      quote=substr(value, 1, 1)
      if (quote == "\"" || quote == sprintf("%c", 39)) {
        closing=index(substr(value, 2), quote)
        if (!closing || substr(value, closing+2) !~ /^[[:space:]]*(#.*)?$/) {
          print "ERROR: Invalid quoted value for " name " in .env" > "/dev/stderr"
          invalid=1
          exit 1
        }
        value=substr(value, 2, closing-1)
      } else {
        sub(/[[:space:]]+#.*$/, "", value)
        sub(/[[:space:]]+$/, "", value)
      }
      result=value
    }
    END { if (!invalid) print result }
  ' "$LOCAL_ENV_FILE"
}

setting() {
  if setting_result=$(printenv "$1" 2>/dev/null); then
    printf '%s\n' "$setting_result"
  else
    dotenv_value "$1"
  fi
}

case "${1:-help}" in
  help|-h|--help) usage; exit 0 ;;
  -*) fail "Pass a Compose command first; topology and env-file are selected by APP_ENV" ;;
esac

mode=$(setting APP_ENV)
mode=${mode:-development}
case "$mode" in
  development|production) ;;
  *) fail "APP_ENV must be development or production" ;;
esac
command -v docker >/dev/null 2>&1 || fail "docker is required"
export APP_ENV="$mode"

if [ "$mode" = "development" ]; then
  [ -f "$LOCAL_ENV_FILE" ] || fail "Create .env from .env.example first"
  echo "Docker mode: development (docker-compose.yml)"
  exec docker compose --env-file "$LOCAL_ENV_FILE" -f "$ROOT_DIR/docker-compose.yml" "$@"
fi

if [ ! -f "$PRODUCTION_ENV_FILE" ]; then
  case "$1" in
    up|build|config|create) ;;
    *) fail "Production is not initialized; run sh scripts/compose.sh up -d --build --wait first" ;;
  esac
  domain=$(setting PAYMENT_PROXY_DOMAIN)
  if [ -z "$domain" ]; then
    public_url=$(setting PAYMENT_PROXY_PUBLIC_BASE_URL)
    case "$public_url" in
      https://*) domain=${public_url#https://}; domain=${domain%/} ;;
      *) fail "Set PAYMENT_PROXY_DOMAIN to your hostname (or a HTTPS PAYMENT_PROXY_PUBLIC_BASE_URL) in .env" ;;
    esac
  fi
  callback=$(setting PAYMENT_PROXY_PRODUCTION_WEBHOOK_URL)
  echo "Preparing first production install; development secrets and callback are not imported."
  sh "$ROOT_DIR/scripts/deploy-production.sh" init "$domain" "$callback"
fi

echo "Docker mode: production (docker-compose.production.yml)"
exec docker compose --env-file "$PRODUCTION_ENV_FILE" -f "$ROOT_DIR/docker-compose.production.yml" "$@"
