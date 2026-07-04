#!/usr/bin/env bash
# Lab smoke: health + project create + skills list. Exit 2 if research pack missing.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SCRATCH="${SCRATCH:-}"
cd "$ROOT"
export GOTOOLCHAIN=local

go build -o bin/lumen ./cmd/lumen

if ! bin/lumen science research verify >/dev/null 2>&1; then
  echo "⊘ lab-smoke SKIP — research pack not healthy (run: lumen science start)" >&2
  exit 2
fi

PORT="${LAB_PORT:-18992}"
bin/lumen science lab --port "$PORT" --no-browser &
PID=$!
trap 'kill $PID 2>/dev/null || true' EXIT
sleep 3

curl -sf "http://127.0.0.1:${PORT}/api/lab/health" | grep -q '"status":"ok"'
PROJ=$(curl -sf -X POST "http://127.0.0.1:${PORT}/api/lab/projects" \
  -H 'Content-Type: application/json' -d '{"title":"smoke-lab"}')
echo "$PROJ" | grep -q '"slug"'
curl -sf "http://127.0.0.1:${PORT}/api/lab/skills" | grep -q '"skills"'
curl -sf "http://127.0.0.1:${PORT}/api/lab/artifacts?project_id=$(echo "$PROJ" | python3 -c "import sys,json; print(json.load(sys.stdin)['slug'])")" | grep -q '"artifacts"'

if [[ -n "$SCRATCH" ]]; then
  mkdir -p "$SCRATCH"
  curl -sf "http://127.0.0.1:${PORT}/api/lab/health" > "$SCRATCH/lab-health.json"
  echo "$PROJ" > "$SCRATCH/lab-project.json"
  curl -sf "http://127.0.0.1:${PORT}/api/lab/skills" > "$SCRATCH/lab-skills.json"
fi

echo "✓ lab-smoke PASS"