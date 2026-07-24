#!/usr/bin/env bash
# Offline contract test for the Grok 4.5 cache proof entrypoint.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT/scripts/smoke-grok45-cache-proof.sh"

probe_output="$($SCRIPT)"
[[ "$probe_output" == *"PROBE: no provider request made"* ]]

set +e
proof_output="$(LUMEN_ALLOW_BILLABLE_GROK_CACHE_PROOF=0 "$SCRIPT" --proof 2>&1)"
proof_status=$?
set -e
[[ $proof_status -eq 2 ]]
[[ "$proof_output" == *"BLOCKED: --proof is billable"* ]]

echo "PASS: Grok 4.5 cache proof defaults offline and blocks unapproved proof"
