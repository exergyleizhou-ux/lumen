#!/usr/bin/env bash
# Ultimate science verification — all plan gates in one command.
# Exits non-zero if any gate fails (no false PASS).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SCRATCH="${SCRATCH:-$ROOT/.science-verify-scratch}"
mkdir -p "$SCRATCH"
cd "$ROOT"
export GOTOOLCHAIN=local

echo "▸ full-verify scratch=$SCRATCH"

FAIL=0
: > "$SCRATCH/verify-exit-codes.txt"

run_gate() {
  local name="$1"
  shift
  local ec=0
  "$@" > "$SCRATCH/${name}.log" 2>&1 || ec=$?
  echo "${name}:${ec}" >> "$SCRATCH/verify-exit-codes.txt"
  if [[ "$ec" -ne 0 ]]; then
    echo "✗ ${name} failed (exit ${ec}) — see $SCRATCH/${name}.log" >&2
    FAIL=1
  else
    echo "✓ ${name}"
  fi
  return 0
}

START=$(date +%s)
run_gate science-test-quick bash scripts/test-science-quick.sh
QEND=$(date +%s)
echo "quick-elapsed:$((QEND-START))s" >> "$SCRATCH/verify-exit-codes.txt"

run_gate science-test-all bash scripts/test-science-all.sh

grep -r '^func Test' internal/science --include='*_test.go' | wc -l | tr -d ' ' > "$SCRATCH/test-count.txt"

if command -v gitleaks >/dev/null 2>&1; then
  run_gate gitleaks gitleaks detect --source internal/science --config .gitleaks.toml --redact --no-git
else
  echo "gitleaks:skipped" >> "$SCRATCH/verify-exit-codes.txt"
  echo "⊘ gitleaks not installed — skipped"
fi

run_gate rm-preflight bash scripts/science/rm-preflight.sh
run_gate rm-offline-auto bash scripts/science/rm-offline-auto.sh

if [[ "$(uname -s)" == "Darwin" ]]; then
  run_gate desktop-health env SCRATCH="$SCRATCH" bash scripts/science/verify-desktop-health.sh
else
  echo "desktop:skipped" >> "$SCRATCH/verify-exit-codes.txt"
  echo "⊘ desktop-health skipped (not macOS)"
fi

run_gate native-verify bash scripts/goal-native-workbench-verify.sh

{ git tag -l '*science*' 2>/dev/null || true; echo '---'; gh release view --json tagName,url,assets 2>/dev/null || true; } > "$SCRATCH/release-info.txt" || true

echo "---"
cat "$SCRATCH/verify-exit-codes.txt"
echo "tests: $(cat "$SCRATCH/test-count.txt")"

if [[ "$FAIL" -ne 0 ]]; then
  echo "✗ full-verify FAIL — artifacts in $SCRATCH" >&2
  exit 1
fi
echo "✓ full-verify PASS — artifacts in $SCRATCH"