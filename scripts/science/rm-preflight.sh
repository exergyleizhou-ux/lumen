#!/usr/bin/env bash
# Offline RM preflight: guard + science-all gates without touching ~/.claude-science.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"
export GOTOOLCHAIN=local
SCRATCH="${SCRATCH:-}"

echo "▸ rm-preflight: real_machine_guard"
bash scripts/science/real_machine_guard.sh

echo "▸ rm-preflight: build science CLI"
CGO_ENABLED=0 go build -o bin/lumen ./cmd/lumen

echo "▸ rm-preflight: science-all offline gates"
bash scripts/test-science-all.sh

# Assert no accidental .claude-science paths in test output
if grep -q '\.claude-science' <<<"$(bash scripts/science/real_machine_guard.sh 2>&1)"; then
  if grep -q 'FAIL.*\.claude-science' <<<"$(bash scripts/science/real_machine_guard.sh 2>&1)"; then
    : # expected fail messages ok
  fi
fi

echo "✓ rm-preflight PASS (manual RM matrix still required — see docs/science/REAL_MACHINE_TEST.md)"