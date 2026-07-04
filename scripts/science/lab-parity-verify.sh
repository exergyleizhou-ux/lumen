#!/usr/bin/env bash
# Lab parity: research pack + fleet + brief path. Requires healthy research pack.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SCRATCH="${SCRATCH:-}"
cd "$ROOT"

bin/lumen science research verify
PORT="${LAB_PORT:-18992}"
bin/lumen science lab --port "$PORT" --no-browser &
PID=$!
trap 'kill $PID 2>/dev/null || true' EXIT
sleep 4

HEALTH=$(curl -sf "http://127.0.0.1:${PORT}/api/lab/health")
echo "$HEALTH" | grep -q '"healthy":true'
echo "$HEALTH" | grep -qE '"cs_connected":[1-9]'
echo "$HEALTH" | grep -qE '"domain_tools":[1-9][0-9]+'

PROJ=$(curl -sf -X POST "http://127.0.0.1:${PORT}/api/lab/projects" \
  -H 'Content-Type: application/json' -d '{"title":"parity-test"}')
SLUG=$(echo "$PROJ" | python3 -c "import sys,json; print(json.load(sys.stdin)['slug'])")

curl -sf -X POST "http://127.0.0.1:${PORT}/api/lab/brief" \
  -H 'Content-Type: application/json' \
  -d "{\"project_id\":\"$SLUG\",\"topic\":\"aspirin\"}" | grep -q 'Research Brief'

if [[ -n "$SCRATCH" ]]; then
  mkdir -p "$SCRATCH"
  echo "$HEALTH" > "$SCRATCH/lab-parity-health.json"
fi

echo "✓ lab-parity-verify PASS"