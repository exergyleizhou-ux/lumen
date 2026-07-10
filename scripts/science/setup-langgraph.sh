#!/usr/bin/env bash
# One-shot local LangGraph sidecar setup for Lumen Lab.
# Does NOT touch VPS. Safe to re-run.
set -euo pipefail

LUMEN_HOME="${LUMEN_HOME:-$HOME/.lumen}"
VENV="${LUMEN_LANGGRAPH_VENV:-$LUMEN_HOME/langgraph-venv}"
SCRIPT="${LUMEN_LANGGRAPH_SCRIPT:-$LUMEN_HOME/langgraph_runner.py}"
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
REPO_SCRIPT="$REPO_ROOT/scripts/science/langgraph_runner.py"

mkdir -p "$LUMEN_HOME"

if [[ ! -x "$VENV/bin/python3" ]]; then
  echo "[setup-langgraph] creating venv at $VENV"
  python3 -m venv "$VENV"
fi

echo "[setup-langgraph] installing langgraph + langchain-core"
"$VENV/bin/python3" -m pip install -U pip setuptools wheel >/dev/null
"$VENV/bin/python3" -m pip install 'langgraph' 'langchain-core'

if [[ -f "$REPO_SCRIPT" ]]; then
  cp "$REPO_SCRIPT" "$SCRIPT"
  chmod +x "$SCRIPT"
  echo "[setup-langgraph] installed runner → $SCRIPT"
elif [[ ! -f "$SCRIPT" ]]; then
  echo "[setup-langgraph] ERROR: missing $REPO_SCRIPT and no existing $SCRIPT" >&2
  exit 1
fi

"$VENV/bin/python3" -c "import langgraph, langchain_core; print('[setup-langgraph] import OK')"
echo "[setup-langgraph] smoke runner…"
OUT="$("$VENV/bin/python3" "$SCRIPT" --project-id demo --prompt 'hello from setup')"
echo "$OUT" | python3 -m json.tool
echo "$OUT" | python3 -c "import sys,json; d=json.load(sys.stdin); assert d.get('ok') is True, d; assert 'result' in d"

cat <<EOF

[setup-langgraph] done.

Start Lab with:
  export LUMEN_LANGGRAPH=1
  export LUMEN_LANGGRAPH_VENV=$VENV
  export LUMEN_LANGGRAPH_SCRIPT=$SCRIPT
  lumen science lab --addr 127.0.0.1:18992 --no-browser

Or:
  $REPO_ROOT/scripts/science/lab-local-with-sidecars.sh
EOF
