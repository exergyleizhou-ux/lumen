#!/usr/bin/env bash
# Ultimate science verification — all plan gates in one command.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SCRATCH="${SCRATCH:-$ROOT/.science-verify-scratch}"
mkdir -p "$SCRATCH"
cd "$ROOT"
export GOTOOLCHAIN=local

echo "▸ full-verify scratch=$SCRATCH"

START=$(date +%s)
bash scripts/test-science-quick.sh > "$SCRATCH/science-test-quick.log" 2>&1
QEND=$(date +%s)
echo "quick:0 elapsed:$((QEND-START))s" > "$SCRATCH/verify-exit-codes.txt"

bash scripts/test-science-all.sh > "$SCRATCH/science-test-all.log" 2>&1
echo "all:$?" >> "$SCRATCH/verify-exit-codes.txt"

grep -r '^func Test' internal/science --include='*_test.go' | wc -l | tr -d ' ' > "$SCRATCH/test-count.txt"

if command -v gitleaks >/dev/null 2>&1; then
  gitleaks detect --source internal/science --config .gitleaks.toml --redact --no-git > "$SCRATCH/gitleaks.log" 2>&1
  echo "gitleaks:$?" >> "$SCRATCH/verify-exit-codes.txt"
fi

bash scripts/science/rm-preflight.sh > "$SCRATCH/rm-preflight.log" 2>&1
echo "rm:$?" >> "$SCRATCH/verify-exit-codes.txt"

bash scripts/science/rm-offline-auto.sh > "$SCRATCH/rm-offline-auto.log" 2>&1
echo "rm-auto:$?" >> "$SCRATCH/verify-exit-codes.txt"

if [[ "$(uname -s)" == "Darwin" ]]; then
  SCRATCH="$SCRATCH" bash scripts/science/verify-desktop-health.sh > "$SCRATCH/desktop-health-run.log" 2>&1
  echo "desktop:$?" >> "$SCRATCH/verify-exit-codes.txt"
fi

bash scripts/goal-native-workbench-verify.sh > "$SCRATCH/native-verify.log" 2>&1
echo "native:$?" >> "$SCRATCH/verify-exit-codes.txt"

{ git tag -l '*science*' 2>/dev/null || true; echo '---'; gh release view --json tagName,url,assets 2>/dev/null || true; } > "$SCRATCH/release-info.txt" || true

echo "✓ full-verify PASS — artifacts in $SCRATCH"
cat "$SCRATCH/verify-exit-codes.txt"
echo "tests: $(cat "$SCRATCH/test-count.txt")"