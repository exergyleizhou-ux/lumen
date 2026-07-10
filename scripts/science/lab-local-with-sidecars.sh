#!/usr/bin/env bash
# Start local Lab with optional sidecars (LangGraph + OnlyOffice if ready).
# Usage:
#   ./scripts/science/lab-local-with-sidecars.sh
#   ADDR=0.0.0.0:18992 ./scripts/science/lab-local-with-sidecars.sh
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
ADDR="${ADDR:-0.0.0.0:18992}"
LUMEN_HOME="${LUMEN_HOME:-$HOME/.lumen}"
VENV="${LUMEN_LANGGRAPH_VENV:-$LUMEN_HOME/langgraph-venv}"
SCRIPT="${LUMEN_LANGGRAPH_SCRIPT:-$LUMEN_HOME/langgraph_runner.py}"
OO_PORT="${ONLYOFFICE_PORT:-8088}"

export PATH="${HOME}/.local/bin:${PATH}"

# LangGraph: enable only if venv can import langgraph
if [[ -x "$VENV/bin/python3" ]] && "$VENV/bin/python3" -c "import langgraph" 2>/dev/null; then
  export LUMEN_LANGGRAPH=1
  export LUMEN_LANGGRAPH_VENV="$VENV"
  export LUMEN_LANGGRAPH_SCRIPT="$SCRIPT"
  echo "[lab-local] LangGraph enabled ($VENV)"
else
  unset LUMEN_LANGGRAPH || true
  echo "[lab-local] LangGraph not ready (run scripts/science/setup-langgraph.sh)"
fi

# OnlyOffice: enable if something answers on local port
if curl -sS -o /dev/null -w "%{http_code}" "http://127.0.0.1:${OO_PORT}/" 2>/dev/null | grep -Eq '200|301|302'; then
  export LUMEN_ONLYOFFICE_URL="http://127.0.0.1:${OO_PORT}"
  echo "[lab-local] OnlyOffice enabled ($LUMEN_ONLYOFFICE_URL)"
else
  unset LUMEN_ONLYOFFICE_URL || true
  echo "[lab-local] OnlyOffice not listening on :${OO_PORT} (run scripts/science/setup-onlyoffice.sh)"
fi

cd "$REPO_ROOT"
if command -v lumen >/dev/null 2>&1; then
  exec lumen science lab --addr "$ADDR" --no-browser
fi
# Fallback: go run from module
exec go run ./cmd/lumen science lab --addr "$ADDR" --no-browser
