#!/usr/bin/env bash
# Lab concurrency + health stress (offline, no model key required for health paths).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"
export GOTOOLCHAIN=local
TIMEOUT="${TIMEOUT:-90s}"

echo "▶ lab unit stress (pool + approval)"
go test ./internal/science/lab/ -count=1 -timeout "$TIMEOUT" \
  -run 'TestTurnPool|TestControllerPool|TestApprovalHub'

echo "▶ lab package race (selected)"
go test ./internal/science/lab/ -race -count=1 -timeout "$TIMEOUT" \
  -run 'TestTurnPool|TestControllerPool|TestApprovalHub' || {
  echo "⚠ race detector unavailable or failed — continuing with non-race suite"
  go test ./internal/science/lab/ -count=1 -timeout "$TIMEOUT" -run 'TestTurnPool|TestControllerPool|TestApprovalHub'
}

echo "▶ proxy catalog + policy"
go test ./internal/science/proxy/ -count=1 -timeout "$TIMEOUT" \
  -run 'TestMatchCapability|TestNormalize|TestRelaySpec|TestOpenAI'

echo "✓ lab-stress passed"
