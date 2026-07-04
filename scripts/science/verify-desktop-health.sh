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
echo "▸ verify-desktop-health HOME=$HOME port=$GUI_PORT"

# Stop stale listeners on GUI port
if command -v lsof >/dev/null 2>&1; then
  PIDS="$(lsof -tiTCP:"$GUI_PORT" -sTCP:LISTEN 2>/dev/null || true)"
  if [[ -n "$PIDS" ]]; then
    echo "▸ stopping stale GUI listeners: $PIDS"
    kill $PIDS 2>/dev/null || true
    sleep 1
  fi
fi

CGO_ENABLED=0 go build -o "$LUMEN_BIN" "$ROOT/cmd/lumen"

"$LUMEN_BIN" science gui --no-browser --port "$GUI_PORT" &
GPID=$!
cleanup() { kill "$GPID" 2>/dev/null || true; wait "$GPID" 2>/dev/null || true; }
trap cleanup EXIT

for i in $(seq 1 40); do
  if curl -sf "http://127.0.0.1:${GUI_PORT}/api/health" -o "$OUT" 2>/dev/null; then
    UPTIME="$(python3 -c "import json; print(json.load(open('$OUT')).get('uptime_ms',0))" 2>/dev/null || echo 0)"
    if [[ "$UPTIME" -lt 30000 ]]; then
      echo "▸ fresh health uptime_ms=$UPTIME"
      cat "$OUT"
      echo "✓ desktop-health OK"
      exit 0
    fi
    echo "  waiting for fresh instance (uptime_ms=$UPTIME)..."
  fi
  sleep 0.25
done

echo "FAIL: could not obtain fresh /api/health on :$GUI_PORT" >&2
cat "$OUT" 2>/dev/null || true
exit 1

# Optional: Acceptance .app launch (when bundle exists)
APP_PATH="${APP_PATH:-$ROOT/desktop/lumen-science/src-tauri/target/release/bundle/macos/Lumen Science.app}"
if [[ -d "$APP_PATH" && "${VERIFY_DESKTOP_APP:-0}" == "1" ]]; then
  echo "▸ launching Acceptance .app: $APP_PATH"
  env HOME="$HOME" LUMEN_BIN="$LUMEN_BIN" open -n "$APP_PATH"
  sleep 3
  curl -sf "http://127.0.0.1:${GUI_PORT}/api/health" -o "$OUT"
  cat "$OUT"
fi