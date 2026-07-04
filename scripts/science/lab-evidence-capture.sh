#!/usr/bin/env bash
# Capture lab verification evidence into SCRATCH (provenance.jsonl, chat SSE, health).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SCRATCH="${SCRATCH:?SCRATCH required}"
PORT="${LAB_PORT:-18992}"
cd "$ROOT"
export GOTOOLCHAIN=local

go build -o bin/lumen ./cmd/lumen
mkdir -p "$SCRATCH"

git diff --name-only HEAD~2..HEAD > "$SCRATCH/CHANGED_FILES.txt" 2>/dev/null || git diff --name-only > "$SCRATCH/CHANGED_FILES.txt"
git log -2 --oneline > "$SCRATCH/GIT_COMMITS.txt"

bin/lumen science lab --port "$PORT" --no-browser &
PID=$!
trap 'kill $PID 2>/dev/null || true' EXIT
sleep 5

curl -sf "http://127.0.0.1:${PORT}/api/lab/health" > "$SCRATCH/lab-health.json"
PROJ=$(curl -sf -X POST "http://127.0.0.1:${PORT}/api/lab/projects" \
  -H 'Content-Type: application/json' \
  -d '{"title":"provenance-evidence","template":"example_crispr_screen"}')
echo "$PROJ" > "$SCRATCH/lab-project.json"
SLUG=$(echo "$PROJ" | python3 -c "import sys,json; print(json.load(sys.stdin)['slug'])")

curl -sf "http://127.0.0.1:${PORT}/api/lab/artifacts?project_id=$SLUG" > "$SCRATCH/lab-artifacts.json"

curl -sf -X POST "http://127.0.0.1:${PORT}/api/lab/brief" \
  -H 'Content-Type: application/json' \
  -d "{\"project_id\":\"$SLUG\",\"topic\":\"aspirin evidence\"}" > "$SCRATCH/lab-brief.json"

curl -s -N -X POST "http://127.0.0.1:${PORT}/api/lab/chat" \
  -H 'Content-Type: application/json' \
  -d "{\"project_id\":\"$SLUG\",\"prompt\":\"调用 science_domain_call domain=pubmed tool=search_articles arguments query aspirin max_results 1\",\"mode\":\"plan\"}" \
  --max-time 180 > "$SCRATCH/lab-chat-provenance.sse" 2>&1 || true

PROJ_DIR="$HOME/.lumen/science/lab/projects/$SLUG"
cp "$PROJ_DIR/provenance.jsonl" "$SCRATCH/provenance.jsonl" 2>/dev/null || echo "no provenance yet" > "$SCRATCH/provenance.jsonl"
wc -l "$SCRATCH/provenance.jsonl" | awk '{print "provenance_lines:"$1}' >> "$SCRATCH/evidence-summary.txt"
grep -c mcp_call "$SCRATCH/provenance.jsonl" 2>/dev/null | awk '{print "mcp_call_records:"$1}' >> "$SCRATCH/evidence-summary.txt" || true

echo "✓ lab-evidence-capture done → $SCRATCH"