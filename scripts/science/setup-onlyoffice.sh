#!/usr/bin/env bash
# Local OnlyOffice Document Server for Lumen Lab (macOS Docker Desktop).
# Does NOT install on small VPS (~3.4GiB). Safe to re-run.
set -euo pipefail

NAME="${ONLYOFFICE_CONTAINER:-onlyoffice}"
PORT="${ONLYOFFICE_PORT:-8088}"
# Official image first; optional mirrors (large layers often fail mid-transfer).
IMAGE="${ONLYOFFICE_IMAGE:-onlyoffice/documentserver:latest}"
MIRRORS=(
  "$IMAGE"
  "docker.1ms.run/onlyoffice/documentserver:latest"
  "dockerproxy.net/onlyoffice/documentserver:latest"
)

if ! command -v docker >/dev/null 2>&1; then
  echo "[setup-onlyoffice] docker not found" >&2
  exit 1
fi

if ! docker info >/dev/null 2>&1; then
  echo "[setup-onlyoffice] docker daemon not running (start Docker Desktop)" >&2
  exit 1
fi

pulled=""
for cand in "${MIRRORS[@]}"; do
  echo "[setup-onlyoffice] pulling $cand (large ~3GB; may fail on bad network)…"
  if docker pull "$cand"; then
    pulled="$cand"
    break
  fi
  echo "[setup-onlyoffice] pull failed for $cand, trying next…"
done
if [[ -z "$pulled" ]]; then
  echo "[setup-onlyoffice] all pulls failed. Retry on a better network or set ONLYOFFICE_IMAGE to a reachable mirror." >&2
  exit 2
fi
IMAGE="$pulled"
# Normalize tag for local use if pulled from mirror path
if [[ "$IMAGE" != onlyoffice/documentserver* ]]; then
  docker tag "$IMAGE" onlyoffice/documentserver:latest || true
  IMAGE=onlyoffice/documentserver:latest
fi

if docker ps -a --format '{{.Names}}' | grep -qx "$NAME"; then
  echo "[setup-onlyoffice] removing existing container $NAME"
  docker rm -f "$NAME" >/dev/null
fi

echo "[setup-onlyoffice] starting $NAME on 127.0.0.1:$PORT"
docker run -d \
  --name "$NAME" \
  -p "${PORT}:80" \
  -e JWT_ENABLED=false \
  --restart unless-stopped \
  "$IMAGE" >/dev/null

echo "[setup-onlyoffice] waiting for HTTP ready (up to ~5 min first boot)…"
ok=0
for i in $(seq 1 60); do
  code=$(curl -sS -o /dev/null -w "%{http_code}" "http://127.0.0.1:${PORT}/" 2>/dev/null || echo 000)
  echo "  try $i http=$code"
  if [[ "$code" == "200" || "$code" == "302" || "$code" == "301" ]]; then
    ok=1
    break
  fi
  sleep 5
done

if [[ "$ok" != "1" ]]; then
  echo "[setup-onlyoffice] container did not become ready; check: docker logs $NAME" >&2
  exit 3
fi

cat <<EOF

[setup-onlyoffice] ready at http://127.0.0.1:${PORT}

Start Lab with:
  export LUMEN_ONLYOFFICE_URL=http://127.0.0.1:${PORT}
  lumen science lab --addr 0.0.0.0:18992 --no-browser

Health should show onlyoffice.configured=true.
EOF
