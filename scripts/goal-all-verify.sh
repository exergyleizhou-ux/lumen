#!/usr/bin/env bash
# Master /goal verification — coding agent evidence + science full-verify.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
export GOTOOLCHAIN=local
export LUMEN_REPO_ROOT="$ROOT"

SCRATCH="${SCRATCH:-$ROOT/.goal-verify-scratch}"
mkdir -p "$SCRATCH"
FAIL=0

echo "▸ goal-all-verify scratch=$SCRATCH"

echo "▸ build bin/lumen"
CGO_ENABLED=0 go build -o "$SCRATCH/lumen" ./cmd/lumen

echo "▸ goal evidence (AC1 dogfood + AC3 eval baseline6)"
export LUMEN_GOAL_SCRATCH="$SCRATCH"
if ! CGO_ENABLED=0 go test -count=1 -timeout 120s -run '^TestGoalEvidence$' ./cmd/lumen -v; then
  echo "✗ TestGoalEvidence failed" >&2
  FAIL=1
fi

echo "▸ science full-verify"
if ! SCRATCH="$SCRATCH/science" bash scripts/science/full-verify.sh; then
  echo "✗ science full-verify failed" >&2
  FAIL=1
fi

echo "▸ make check (build+vet+test)"
if ! make check; then
  echo "✗ make check failed" >&2
  FAIL=1
fi

if [[ "$FAIL" -ne 0 ]]; then
  echo "✗ goal-all-verify FAIL" >&2
  exit 1
fi
echo "✓ goal-all-verify PASS"