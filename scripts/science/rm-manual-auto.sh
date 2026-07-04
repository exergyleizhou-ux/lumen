#!/usr/bin/env bash
# Automated real-machine RM (RM-04..14) in isolated guard HOME.
# Reads real Science assets via SCIENCE_REAL_HOME (read-only); never writes ~/.claude-science.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SCRATCH="${SCRATCH:-$ROOT/.science-rm-scratch}"
mkdir -p "$SCRATCH"

# Load repo .env for API keys (never logged).
if [[ -f "$ROOT/.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "$ROOT/.env"
  set +a
fi

# Real user home for asset clone (read-only)
SCIENCE_REAL_HOME="${SCIENCE_REAL_HOME:-$(eval echo ~${SUDO_USER:-$USER})}"
GUARD_HOME="${GUARD_HOME:-$(mktemp -d /tmp/lumen-rm-manual-XXXXXX)}"
export SCIENCE_REAL_HOME
export HOME="$GUARD_HOME"
export LUMEN_SCIENCE_DIR="$GUARD_HOME/.lumen/science"
export LUMEN_BIN="$ROOT/bin/lumen"
mkdir -p "$LUMEN_SCIENCE_DIR"

echo "▸ rm-manual-auto SCIENCE_REAL_HOME=$SCIENCE_REAL_HOME"
echo "▸ rm-manual-auto GUARD_HOME=$GUARD_HOME"

# Prove we never write real cred dir
REAL_SCIENCE="$SCIENCE_REAL_HOME/.claude-science"
if [[ -L "$REAL_SCIENCE" ]]; then
  echo "FAIL: real .claude-science is symlink" >&2
  exit 1
fi

cd "$ROOT"
CGO_ENABLED=0 go build -o "$LUMEN_BIN" ./cmd/lumen
CGO_ENABLED=0 go build -o "$ROOT/bin/lumen-science-rm" ./cmd/lumen-science-rm
for mcp in lumen-mcp-pubmed lumen-mcp-oasis lumen-mcp-chembl lumen-mcp-c2d lumen-mcp-geo; do
  CGO_ENABLED=0 go build -o "$ROOT/bin/$mcp" "./cmd/$mcp"
done

# RM-09 DSML rewrite e2e
echo "▸ RM-09 DSML shim rewrite"
go test ./internal/science/proxy/... -count=1 -short -timeout 60s -run 'DSMLE2ERewrite' >/dev/null

# RM-04..14 orchestrator (core path first — avoids desktop GUI stealing focus/time)
export SCIENCE_RM_SKIP_OPEN=1
export LUMEN_BIN="$ROOT/bin/lumen"
"$ROOT/bin/lumen-science-rm" 2>&1 | tee "$SCRATCH/rm-manual-core.log"

# RM-15/16 via native (live network)
echo "▸ RM-15 native fleet live"
LUMEN_MCP_PUBMED="$ROOT/bin/lumen-mcp-pubmed" \
LUMEN_MCP_OASIS="$ROOT/bin/lumen-mcp-oasis" \
LUMEN_MCP_CHEMBL="$ROOT/bin/lumen-mcp-chembl" \
LUMEN_MCP_C2D="$ROOT/bin/lumen-mcp-c2d" \
LUMEN_MCP_GEO="$ROOT/bin/lumen-mcp-geo" \
  "$LUMEN_BIN" science native verify --live 2>&1 | tee "$SCRATCH/rm-15-native.log" | tail -3

echo "▸ RM-16 research brief"
"$LUMEN_BIN" science brief "aspirin mechanism" 2>&1 | tee "$SCRATCH/rm-16-brief.log" | grep -q "Research Brief"

# RM-17 desktop health (macOS)
if [[ "$(uname -s)" == "Darwin" ]]; then
  SCRATCH="$SCRATCH" bash scripts/science/verify-desktop-health.sh > "$SCRATCH/rm-17-desktop.log" 2>&1
fi

echo "✓ rm-manual-auto PASS — logs in $SCRATCH"