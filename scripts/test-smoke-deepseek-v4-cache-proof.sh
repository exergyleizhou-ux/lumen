#!/usr/bin/env bash
# Offline contract test for the DeepSeek cache-proof entrypoint.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT/scripts/smoke-deepseek-v4-cache-proof.sh"

probe_output="$($SCRIPT)"
[[ "$probe_output" == *"PROBE: no provider request made"* ]]

set +e
proof_output="$(LUMEN_ALLOW_BILLABLE_CACHE_PROOF=0 "$SCRIPT" --proof 2>&1)"
proof_status=$?
set -e
[[ $proof_status -eq 2 ]]
[[ "$proof_output" == *"BLOCKED: --proof is billable"* ]]

echo "PASS: DeepSeek cache proof defaults offline and blocks unapproved proof"
