#!/usr/bin/env bash
# Native Science workbench verify: 5-ship MCP fleet + brief + GUI API.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
SCRATCH="${SCRATCH:-}"

export LUMEN_MCP_PUBMED="${LUMEN_MCP_PUBMED:-$ROOT/bin/lumen-mcp-pubmed}"
export LUMEN_MCP_OASIS="${LUMEN_MCP_OASIS:-$ROOT/bin/lumen-mcp-oasis}"
export LUMEN_MCP_CHEMBL="${LUMEN_MCP_CHEMBL:-$ROOT/bin/lumen-mcp-chembl}"
export LUMEN_MCP_C2D="${LUMEN_MCP_C2D:-$ROOT/bin/lumen-mcp-c2d}"
export LUMEN_MCP_GEO="${LUMEN_MCP_GEO:-$ROOT/bin/lumen-mcp-geo}"
MCPS=(lumen-mcp-pubmed lumen-mcp-oasis lumen-mcp-chembl lumen-mcp-c2d lumen-mcp-geo)

echo "▸ build MCP binaries (5-ship fleet)"
for mcp in "${MCPS[@]}"; do
  CGO_ENABLED=0 go build -o "bin/$mcp" "./cmd/$mcp"
done
CGO_ENABLED=0 go build -o bin/lumen ./cmd/lumen

echo "▸ unit tests (plan step 1 — full mcp + native + gui)"
GOTOOLCHAIN=local go test -count=1 -timeout 300s \
  ./internal/science/mcp/... \
  ./internal/science/native/... \
  ./internal/science/gui/... 2>&1

echo "▸ native live verify (expect 5 shipped members)"
FLEET_JSON=$(./bin/lumen science native list)
echo "$FLEET_JSON" | grep -q '"status": "shipped"' || { echo "fleet missing shipped members"; exit 1; }
./bin/lumen science native verify --live

echo "▸ research brief (full output below)"
OUT=$(./bin/lumen science brief "aspirin mechanism" 2>&1)
if [[ -n "$SCRATCH" ]]; then
  mkdir -p "$SCRATCH"
  printf '%s\n' "$OUT" > "$SCRATCH/brief-sample.txt"
fi
printf '%s\n' "$OUT"
echo "$OUT" | grep -q "Research Brief" || { echo "brief missing header"; exit 1; }
echo "$OUT" | grep -q "溯源" || { echo "brief missing provenance"; exit 1; }
echo "$OUT" | grep -qE "PubMed|文献证据" || { echo "brief missing pubmed section"; exit 1; }
echo "$OUT" | grep -qE "ChEMBL|化合物" || { echo "brief missing chembl section"; exit 1; }
echo "$OUT" | grep -qE "GEO|表达数据" || { echo "brief missing geo section"; exit 1; }
echo "$OUT" | grep -qE "绿洲|已验证数据集" || { echo "brief missing oasis section"; exit 1; }

echo "✓ native workbench verify PASS"