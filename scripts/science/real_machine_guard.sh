#!/usr/bin/env bash
# Real-machine guard: preflight env for RM tests without touching ~/.claude-science.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
GUARD_HOME="${GUARD_HOME:-$(mktemp -d /tmp/lumen-rm-guard-XXXXXX)}"
SCIENCE_REAL_HOME="${SCIENCE_REAL_HOME:-$(eval echo ~${SUDO_USER:-$USER})}"
export SCIENCE_REAL_HOME
export HOME="$GUARD_HOME"
export LUMEN_SCIENCE_DIR="$GUARD_HOME/.lumen/science"
export SCIENCE_BIN="${SCIENCE_BIN:-/Applications/Claude Science.app/Contents/Resources/bin/claude-science}"

mkdir -p "$LUMEN_SCIENCE_DIR" "$LUMEN_SCIENCE_DIR/sandbox/home"

echo "▸ guard HOME=$HOME"
echo "▸ guard LUMEN_SCIENCE_DIR=$LUMEN_SCIENCE_DIR"

# Iron law: never write into real Science tree
REAL_SCIENCE="$HOME/.claude-science"
if [[ -e "$REAL_SCIENCE" ]]; then
  echo "FAIL: guard HOME must not contain .claude-science symlink" >&2
  exit 1
fi

# 8765 baseline PID invariant (read-only probe)
BASELINE_8765=""
if command -v lsof >/dev/null 2>&1; then
  BASELINE_8765="$(lsof -nP -iTCP:8765 -sTCP:LISTEN 2>/dev/null | awk 'NR==2{print $2}' || true)"
fi
export RM_BASELINE_8765_PID="${BASELINE_8765:-none}"
echo "▸ baseline 8765 PID: ${RM_BASELINE_8765_PID}"

assert_8765_unchanged() {
  local now=""
  if command -v lsof >/dev/null 2>&1; then
    now="$(lsof -nP -iTCP:8765 -sTCP:LISTEN 2>/dev/null | awk 'NR==2{print $2}' || true)"
  fi
  if [[ "${RM_BASELINE_8765_PID}" != "${now:-none}" ]]; then
    echo "FAIL: port 8765 listener changed (${RM_BASELINE_8765_PID} -> ${now:-none})" >&2
    exit 1
  fi
}

# Build isolated binaries
cd "$ROOT"
CGO_ENABLED=0 go build -o "$GUARD_HOME/lumen" ./cmd/lumen

# Smoke: config dir isolation
"$GUARD_HOME/lumen" science doctor >/dev/null

assert_8765_unchanged

# Refuse writes under real home even if misconfigured
if grep -rq '\.claude-science' "$LUMEN_SCIENCE_DIR" 2>/dev/null; then
  echo "FAIL: science dir references .claude-science" >&2
  exit 1
fi

echo "✓ real_machine_guard preflight PASS"
echo "  GUARD_HOME=$GUARD_HOME"
echo "  RM_BASELINE_8765_PID=$RM_BASELINE_8765_PID"