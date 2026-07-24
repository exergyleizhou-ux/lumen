#!/usr/bin/env bash
# Grok 4.5 cache evidence probe with an explicit billable proof mode.
#
# It deliberately prints only provider usage counters and sanitized durable
# request evidence summaries. It never prints prompts, provider bodies,
# headers, request IDs, credentials, or the isolated session directory.
set -euo pipefail

MODE="${1:---probe}"
case "$MODE" in
  --probe)
    echo "PROBE: no provider request made. Use --proof with LUMEN_ALLOW_BILLABLE_GROK_CACHE_PROOF=1 for a strict live proof."
    exit 0
    ;;
  --proof) ;;
  *)
    echo "usage: $0 [--probe|--proof]" >&2
    exit 64
    ;;
esac

if [[ "${LUMEN_ALLOW_BILLABLE_GROK_CACHE_PROOF:-0}" != "1" ]]; then
  echo "BLOCKED: --proof is billable; set LUMEN_ALLOW_BILLABLE_GROK_CACHE_PROOF=1 to authorize provider requests" >&2
  exit 2
fi

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export PATH="/opt/homebrew/bin:$HOME/.cargo/bin:$PATH"
export PROTOC="${PROTOC:-/opt/homebrew/bin/protoc}"

ACCOUNT_GROK_HOME="${GROK_HOME:-$HOME/.grok}"
ACCOUNT_AUTH_FILE="$ACCOUNT_GROK_HOME/auth.json"
if [[ ! -f "$ACCOUNT_AUTH_FILE" ]]; then
  echo "BLOCKED: xAI account auth is absent; no provider request was made" >&2
  exit 2
fi
if ! python3 - "$ACCOUNT_AUTH_FILE" <<'PY'
import json
import base64
import pathlib
import sys

store = json.loads(pathlib.Path(sys.argv[1]).read_text())
issuer = "https://auth.x.ai"
client_id = "b1a00492-073a-47ea-816f-4c329264a828"
entry = store.get(f"{issuer}::{client_id}") if isinstance(store, dict) else None
if not (
    isinstance(entry, dict)
    and entry.get("auth_mode") == "oidc"
    and entry.get("oidc_issuer") == issuer
    and entry.get("oidc_client_id") == client_id
):
    raise SystemExit(1)
token = entry.get("key")
parts = token.split(".") if isinstance(token, str) else []
if len(parts) != 3:
    raise SystemExit(1)
try:
    payload = parts[1] + "=" * ((4 - len(parts[1]) % 4) % 4)
    claims = json.loads(base64.urlsafe_b64decode(payload))
except Exception:
    raise SystemExit(1)
scope = claims.get("scope", claims.get("scp", []))
if isinstance(scope, str):
    scope = set(scope.split())
elif isinstance(scope, list):
    scope = set(scope)
else:
    scope = set()
required = {
    "openid",
    "profile",
    "email",
    "offline_access",
    "grok-cli:access",
    "api:access",
    "conversations:read",
    "conversations:write",
    "workspaces:read",
    "workspaces:write",
}
audience = claims.get("aud")
if isinstance(audience, str):
    audiences = {audience}
elif isinstance(audience, list):
    audiences = set(audience)
else:
    audiences = set()
if not required <= scope or client_id not in audiences:
    raise SystemExit(1)
PY
then
  echo "BLOCKED: xAI account OAuth credential does not satisfy the current CLI scope contract; no provider request was made" >&2
  exit 2
fi

(cd "$ROOT/agent" && CARGO_BUILD_JOBS="${CARGO_BUILD_JOBS:-2}" \
  cargo build --locked -p xai-grok-pager-bin)
BIN="$ROOT/agent/target/debug/lumen"
test -x "$BIN"

PROOF_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/lumen-grok45-cache-proof-XXXXXX")"
cleanup() {
  if [[ "${LUMEN_GROK_CACHE_PROOF_KEEP_ARTIFACTS:-0}" == "1" ]]; then
    return
  fi
  rm -rf "$PROOF_ROOT"
}
trap cleanup EXIT
export LUMEN_HOME="$PROOF_ROOT/lumen-home"
export GROK_HOME="$ACCOUNT_GROK_HOME"
export GROK_DISABLE_API_KEY_AUTH=1
mkdir -p "$LUMEN_HOME"
unset XAI_API_KEY DEEPSEEK_API_KEY KIMI_CODE_API_KEY OPENAI_API_KEY \
  OPENAI_BASE_URL ANTHROPIC_API_KEY ANTHROPIC_AUTH_TOKEN GROK_API_KEY \
  GROK_CODE_XAI_API_KEY GROK_OIDC_ISSUER GROK_OIDC_CLIENT_ID \
  GROK_OIDC_SCOPES GROK_OIDC_AUDIENCE GROK_OAUTH2_ISSUER \
  GROK_OAUTH2_CLIENT_ID GROK_OAUTH2_SCOPES GROK_OAUTH2_PRINCIPAL_TYPE \
  GROK_OAUTH2_PRINCIPAL_ID GROK_AUTH_PROVIDER_COMMAND GROK_LOCAL_AUTH \
  2>/dev/null || true
cat >"$LUMEN_HOME/config.toml" <<'CFG'
[models]
default = "grok-4.5"
[cli]
auto_update = false
[auth]
preferred_method = "oidc"
CFG

classify_first_failure() {
  local failure_class="provider_or_transport"
  local refresh_class="no_failure_marker"
  local files=("$PROOF_ROOT/first.out" "$PROOF_ROOT/first.debug")
  local http_status="unavailable"
  local durable_request_evidence="false"
  if grep -Eiq '(^|[^0-9])402([^0-9]|$)|payment required|free.?usage.?exhausted|buy.?credits|upgrade.*plan' "${files[@]}" 2>/dev/null; then
    failure_class="billing_or_plan_entitlement"
  elif grep -Eiq '(^|[^0-9])401([^0-9]|$)|unauthorized|credentials|auth context|authentication|bearer token' "${files[@]}" 2>/dev/null; then
    failure_class="authentication"
  elif grep -Eiq '(^|[^0-9])403([^0-9]|$)|forbidden|entitlement|permission denied' "${files[@]}" 2>/dev/null; then
    failure_class="entitlement_or_policy"
  elif grep -Eiq '(^|[^0-9])429([^0-9]|$)|rate.?limit|quota|usage.?exhausted' "${files[@]}" 2>/dev/null; then
    failure_class="rate_limit_or_quota"
  elif grep -Eiq '(^|[^0-9])404([^0-9]|$)|model[^[:alnum:]]+(not found|unavailable|unsupported)' "${files[@]}" 2>/dev/null; then
    failure_class="model_unavailable"
  elif grep -Eiq 'timeout|timed out|connection|connect error|dns|tls|error sending request|transport' "${files[@]}" 2>/dev/null; then
    failure_class="transport"
  fi
  for status in 400 401 402 403 404 408 409 422 429 500 502 503 504; do
    if grep -Eiq "(^|[^0-9])${status}([^0-9]|$)" "${files[@]}" 2>/dev/null; then
      http_status="$status"
      break
    fi
  done
  if find "$PROOF_ROOT" -type f -name cache_request_evidence.jsonl -size +0c -print -quit |
    grep -q .
  then
    durable_request_evidence="true"
  fi
  if grep -Eiq 'auth\\.refresh\\.success|auth recovery:.*recovered' "${files[@]}" 2>/dev/null; then
    refresh_class="succeeded"
  elif grep -Eiq 'auth\\.refresh\\.(permanent_failure|transient_failure)|auth recovery:.*(failed|giving up)' "${files[@]}" 2>/dev/null; then
    refresh_class="failed"
  elif grep -Eiq 'auth_retry_backoff|attempting refresh|unauthorized_recovery' "${files[@]}" 2>/dev/null; then
    refresh_class="attempted"
  fi
  printf 'endpoint_class=cli_chat_proxy failure_class=%s http_status=%s refresh_class=%s durable_request_evidence=%s\n' \
    "$failure_class" "$http_status" "$refresh_class" "$durable_request_evidence" >&2
  python3 - "$ACCOUNT_AUTH_FILE" <<'PY' >&2
import json
import pathlib
import sys

try:
    store = json.loads(pathlib.Path(sys.argv[1]).read_text())
    entry = store.get(
        "https://auth.x.ai::b1a00492-073a-47ea-816f-4c329264a828", {}
    )
    blocked = bool(entry.get("user_blocked_reason")) or bool(
        entry.get("team_blocked_reasons")
    )
    print(f"auth_metadata_blocked={str(blocked).lower()}")
except Exception:
    print("auth_metadata_blocked=unavailable")
PY
}

COMMON=(-m grok-4.5 --reasoning-effort low --output-format plain --always-approve --max-turns 2)
if ! "$BIN" "${COMMON[@]}" --single "Reply with exactly: cache-proof-one." \
  --debug-file "$PROOF_ROOT/first.debug" >"$PROOF_ROOT/first.out" 2>&1; then
  classify_first_failure
  echo "FAIL: first Grok 4.5 provider request failed" >&2
  exit 1
fi

SESSION_DIR="$(find "$PROOF_ROOT" -type f -name chat_history.jsonl -exec dirname {} \; -quit)"
if [[ -z "$SESSION_DIR" ]] || [[ ! -f "$SESSION_DIR/cache_epoch.json" ]]; then
  echo "FAIL: product request did not persist a session cache epoch" >&2
  exit 1
fi
SESSION_ID="$(basename "$SESSION_DIR")"
EPOCH_BEFORE="$PROOF_ROOT/epoch-before.json"
cp "$SESSION_DIR/cache_epoch.json" "$EPOCH_BEFORE"

if ! "$BIN" "${COMMON[@]}" --resume "$SESSION_ID" --single "Reply with exactly: cache-proof-two." \
  --debug-file "$PROOF_ROOT/second.debug" >"$PROOF_ROOT/second.out" 2>&1; then
  echo "FAIL: second Grok 4.5 provider request failed" >&2
  exit 1
fi

python3 - "$EPOCH_BEFORE" "$SESSION_DIR/cache_epoch.json" \
  "$SESSION_DIR/cache_request_evidence.jsonl" "$GROK_HOME/logs/unified.jsonl" \
  "$SESSION_ID" <<'PY'
import json
import pathlib
import sys

before_path, after_path, ledger_path, unified_path = map(pathlib.Path, sys.argv[1:5])
session_id = sys.argv[5]
before = json.loads(before_path.read_text())
after = json.loads(after_path.read_text())
if before["domain_fingerprint"] != after["domain_fingerprint"]:
    raise SystemExit("FAIL: restart changed cache domain fingerprint")
if after["generation"] < before["generation"]:
    raise SystemExit("FAIL: cache epoch generation moved backwards")

rows = [json.loads(line) for line in ledger_path.read_text().splitlines() if line.strip()]
if len(rows) < 2:
    raise SystemExit("FAIL: fewer than two durable request evidence records")
required = {
    "schema_version",
    "cache_domain_hash",
    "cache_epoch_id",
    "transport_hash",
    "provider_cache_material_hash",
    "body_bytes",
    "serialization_kind",
    "mutation_reasons",
    "attempt_index",
}
for row in rows:
    if not required <= set(row):
        raise SystemExit("FAIL: durable request evidence schema is incomplete")
    forbidden = {"prompt", "authorization", "api_key", "headers", "request_id"} & set(row)
    if forbidden:
        raise SystemExit("FAIL: durable request evidence contains forbidden fields")
    if row["cache_epoch_id"] not in {before["epoch_id"], after["epoch_id"]}:
        raise SystemExit("FAIL: request evidence has an unknown cache epoch")

usage = []
if unified_path.exists():
    for line in unified_path.read_text(errors="replace").splitlines():
        entry = json.loads(line)
        if (
            entry.get("sid") != session_id
            or entry.get("msg") != "shell.turn.inference_done"
        ):
            continue
        ctx = entry.get("ctx") or {}
        usage.append(
            (
                ctx.get("prompt_tokens"),
                ctx.get("provider_cache_accounting"),
                ctx.get("provider_cache_hit_tokens"),
                ctx.get("provider_cache_miss_tokens"),
            )
        )
if len(usage) < 2:
    raise SystemExit("FAIL: two sanitized provider usage telemetry records were not persisted")
prompt, accounting, hit, miss = usage[-1]
if accounting not in {"reported", "unavailable", "contradictory"}:
    raise SystemExit("FAIL: provider cache accounting telemetry is invalid")
if accounting != "reported":
    raise SystemExit(
        f"FAIL: strict proof requires internally consistent provider accounting, got {accounting}"
    )
if not isinstance(prompt, int) or prompt <= 0:
    raise SystemExit(
        "FAIL: strict proof requires provider-reported positive prompt tokens on the second request"
    )
if not isinstance(hit, int) or hit <= 0 or hit > prompt:
    raise SystemExit(
        "FAIL: strict proof requires a nonzero provider-reported cache hit on the second request"
    )
if miss is not None and (
    not isinstance(miss, int) or miss < 0 or hit + miss != prompt
):
    raise SystemExit(
        "FAIL: strict proof rejected inconsistent provider cache hit/miss accounting"
    )
attempts = sorted({row["attempt_index"] for row in rows})
backends = sorted({row["serialization_kind"] for row in rows})
print(
    "PASS: Grok 4.5 strict cache live proof "
    f"provider_cache_accounting={accounting} provider_cache_hit=true "
    f"provider_cache_hit_tokens={hit} "
    f"provider_cache_miss_tokens={miss if miss is not None else 'unavailable'} "
    f"durable_epoch_generation_before={before['generation']} after={after['generation']} "
    f"evidence_records={len(rows)} "
    f"attempt_indices={','.join(map(str, attempts))} "
    f"serialization={','.join(backends)}"
)
PY
