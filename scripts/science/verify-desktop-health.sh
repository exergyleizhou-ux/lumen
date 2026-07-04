#!/usr/bin/env bash
# Fresh GUI health check under isolated HOME (verification plan step 5).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SCRATCH="${SCRATCH:-}"
OUT="${1:-}"
if [[ -z "$OUT" && -n "$SCRATCH" ]]; then
  OUT="$SCRATCH/desktop-health.json"
fi
if [[ -z "$OUT" ]]; then
  echo "usage: SCRATCH=... $0 [output.json]" >&2
  exit 1
fi

GUARD_HOME="${GUARD_HOME:-$(mktemp -d /tmp/lumen-desktop-health-XXXXXX)}"
export HOME="$GUARD_HOME"
export LUMEN_SCIENCE_DIR="$GUARD_HOME/.lumen/science"
export LUMEN_BIN="${LUMEN_BIN:-$ROOT/bin/lumen}"
mkdir -p "$LUMEN_SCIENCE_DIR"

GUI_PORT="${GUI_PORT:-18990}"
APP_PATH="${APP_PATH:-$ROOT/desktop/lumen-science/src-tauri/target/release/bundle/macos/Lumen Science.app}"

stop_gui_port() {
  if command -v lsof >/dev/null 2>&1; then
    local pids
    pids="$(lsof -tiTCP:"$GUI_PORT" -sTCP:LISTEN 2>/dev/null || true)"
    if [[ -n "$pids" ]]; then
      echo "▸ stopping stale GUI listeners: $pids"
      kill $pids 2>/dev/null || true
      sleep 1
    fi
  fi
}

wait_fresh_health() {
  local out="$1"
  for _ in $(seq 1 40); do
    if curl -sf "http://127.0.0.1:${GUI_PORT}/api/health" -o "$out" 2>/dev/null; then
      local uptime
      uptime="$(python3 -c "import json; print(json.load(open('$out')).get('uptime_ms',0))" 2>/dev/null || echo 0)"
      if [[ "$uptime" -lt 30000 ]]; then
        echo "▸ fresh health uptime_ms=$uptime"
        cat "$out"
        return 0
      fi
      echo "  waiting for fresh instance (uptime_ms=$uptime)..."
    fi
    sleep 0.25
  done
  return 1
}

echo "▸ verify-desktop-health HOME=$HOME port=$GUI_PORT"
stop_gui_port
CGO_ENABLED=0 go build -o "$LUMEN_BIN" "$ROOT/cmd/lumen"

echo "▸ phase 1: CLI gui launch"
"$LUMEN_BIN" science gui --no-browser --port "$GUI_PORT" &
GPID=$!
if ! wait_fresh_health "$OUT"; then
  kill "$GPID" 2>/dev/null || true
  echo "FAIL: CLI gui health check" >&2
  exit 1
fi
kill "$GPID" 2>/dev/null || true
wait "$GPID" 2>/dev/null || true
echo "✓ desktop-health OK (CLI)"

if [[ -d "$APP_PATH" ]]; then
  echo "▸ phase 2: Acceptance .app launch ($APP_PATH)"
  stop_gui_port
  env HOME="$HOME" LUMEN_BIN="$LUMEN_BIN" open -n "$APP_PATH"
  APP_OUT="${OUT%.json}-app.json"
  if wait_fresh_health "$APP_OUT"; then
    echo "✓ desktop-health OK (.app)"
    cp "$APP_OUT" "$OUT"
  else
    echo "WARN: .app health not fresh; CLI result kept in $OUT" >&2
  fi
fi