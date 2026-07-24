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

TEST_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/lumen-grok45-proof-admission-XXXXXX")"
cleanup() {
  rm -rf "$TEST_ROOT"
}
trap cleanup EXIT
python3 - "$TEST_ROOT/auth.json" <<'PY'
import base64
import json
import pathlib
import sys

issuer = "https://auth.x.ai"
client_id = "b1a00492-073a-47ea-816f-4c329264a828"
old_scopes = [
    "openid",
    "profile",
    "email",
    "offline_access",
    "grok-cli:access",
    "api:access",
    "conversations:read",
    "conversations:write",
]
payload = base64.urlsafe_b64encode(
    json.dumps({"aud": client_id, "scope": " ".join(old_scopes)}).encode()
).decode().rstrip("=")
token = f"eyJhbGciOiJub25lIn0.{payload}."
store = {
    f"{issuer}::{client_id}": {
        "auth_mode": "oidc",
        "oidc_issuer": issuer,
        "oidc_client_id": client_id,
        "key": token,
    }
}
pathlib.Path(sys.argv[1]).write_text(json.dumps(store))
PY

set +e
old_scope_output="$(
  GROK_HOME="$TEST_ROOT" \
    LUMEN_ALLOW_BILLABLE_GROK_CACHE_PROOF=1 \
    "$SCRIPT" --proof 2>&1
)"
old_scope_status=$?
set -e
[[ $old_scope_status -eq 2 ]]
[[ "$old_scope_output" == *"does not satisfy the current CLI scope contract"* ]]

echo "PASS: Grok 4.5 cache proof defaults offline and blocks unapproved or stale-scope proof"
