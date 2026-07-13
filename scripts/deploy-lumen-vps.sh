#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
compose="$root/deploy/docker-compose.prod.yml"
env_file="${LUMEN_ENV_FILE:-$root/deploy/.env.production}"
action="${1:-check}"

if [[ ! -f "$env_file" ]]; then
  echo "missing environment file: $env_file" >&2
  exit 2
fi

dc=(docker compose --env-file "$env_file" -f "$compose")

case "$action" in
  check)
    "${dc[@]}" config --quiet
    ;;
  migrate)
    "${dc[@]}" pull migrate
    "${dc[@]}" run --rm migrate
    ;;
  deploy)
    "$0" check
    "$0" migrate
    "${dc[@]}" pull code lab caddy
    "${dc[@]}" up -d --no-deps code lab
    "${dc[@]}" up -d --no-deps caddy
    "${dc[@]}" ps
    ;;
  smoke)
    set -a; source "$env_file"; set +a
    curl --fail --silent --show-error -H "Host: ${LUMEN_CODE_HOST}" "http://127.0.0.1:${LUMEN_PROXY_PORT:-8088}/healthz" >/dev/null
    curl --fail --silent --show-error -H "Host: ${LUMEN_CODE_HOST}" "http://127.0.0.1:${LUMEN_PROXY_PORT:-8088}/readyz" >/dev/null
    curl --fail --silent --show-error -H "Host: ${LUMEN_LAB_HOST}" "http://127.0.0.1:${LUMEN_PROXY_PORT:-8088}/api/lab/readyz" >/dev/null
    ;;
  rollback)
    set -a; source "$env_file"; set +a
    : "${LUMEN_PREVIOUS_IMAGE:?set LUMEN_PREVIOUS_IMAGE to an immutable digest}"
    LUMEN_IMAGE="$LUMEN_PREVIOUS_IMAGE" "${dc[@]}" up -d --no-deps code lab
    ;;
  down)
    "${dc[@]}" down
    ;;
  *)
    echo "usage: $0 {check|migrate|deploy|smoke|rollback|down}" >&2
    exit 2
    ;;
esac
