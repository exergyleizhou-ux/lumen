#!/usr/bin/env bash
# DeepSeek V4 cache evidence probe with an explicit, billable proof mode.
#
# It deliberately prints only provider usage counters and sanitized durable
# request evidence summaries.  It never prints prompts, provider bodies,
# headers, request IDs, credentials, or the isolated session directory.
set -euo pipefail

MODE="${1:---probe}"
case "$MODE" in
  --probe)
    # The default is intentionally offline: this tells an operator exactly
    # which command would make provider calls without building or sending one.
    echo "PROBE: no provider request made. Use --proof with LUMEN_ALLOW_BILLABLE_CACHE_PROOF=1 for a strict live proof."
    exit 0
    ;;
  --proof) ;;
  *)
    echo "usage: $0 [--probe|--proof]" >&2
    exit 64
    ;;
esac

if [[ "${LUMEN_ALLOW_BILLABLE_CACHE_PROOF:-0}" != "1" ]]; then
  echo "BLOCKED: --proof is billable; set LUMEN_ALLOW_BILLABLE_CACHE_PROOF=1 to authorize provider requests" >&2
  exit 2
fi

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export PATH="/opt/homebrew/bin:$HOME/.cargo/bin:$PATH"
export PROTOC="${PROTOC:-/opt/homebrew/bin/protoc}"

if [[ -z "${DEEPSEEK_API_KEY:-}" ]]; then
  echo "BLOCKED: DEEPSEEK_API_KEY is absent; no provider request was made" >&2
  exit 2
fi

(cd "$ROOT/agent" && CARGO_BUILD_JOBS="${CARGO_BUILD_JOBS:-2}" \
  cargo build --locked -p xai-grok-pager-bin)
BIN="$ROOT/agent/target/debug/lumen"
test -x "$BIN"

PROOF_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/lumen-deepseek-cache-proof-XXXXXX")"
cleanup() {
  if [[ "${LUMEN_CACHE_PROOF_KEEP_ARTIFACTS:-0}" == "1" ]]; then
    # Deliberately do not print the path: it can contain local debug output.
    return
  fi
  rm -rf "$PROOF_ROOT"
}
trap cleanup EXIT
export LUMEN_HOME="$PROOF_ROOT/lumen-home"
export GROK_HOME="$PROOF_ROOT/grok-home"
mkdir -p "$LUMEN_HOME" "$GROK_HOME"
unset XAI_API_KEY GROK_CODE_XAI_API_KEY GROK_API_KEY OPENAI_API_KEY OPENAI_BASE_URL 2>/dev/null || true
cat >"$LUMEN_HOME/config.toml" <<'CFG'
[models]
default = "deepseek-v4-pro"
[cli]
auto_update = false
CFG

COMMON=(-m deepseek-v4-pro --output-format plain --always-approve --max-turns 2)
# The second real request resumes the exact same durable session.  Its stable
# prefix includes the first request/response; only provider usage can prove a
# cache read, never body similarity or local hashes.
"$BIN" "${COMMON[@]}" --single "Reply with exactly: cache-proof-one." \
  --debug-file "$PROOF_ROOT/first.debug" >"$PROOF_ROOT/first.out" 2>&1

SESSION_DIR="$(find "$PROOF_ROOT" -type f -name chat_history.jsonl -print -quit | xargs -n1 dirname)"
if [[ -z "$SESSION_DIR" ]] || [[ ! -f "$SESSION_DIR/cache_epoch.json" ]]; then
  echo "FAIL: product request did not persist a session cache epoch" >&2
  exit 1
fi
SESSION_ID="$(basename "$SESSION_DIR")"
EPOCH_BEFORE="$PROOF_ROOT/epoch-before.json"
cp "$SESSION_DIR/cache_epoch.json" "$EPOCH_BEFORE"

"$BIN" "${COMMON[@]}" --resume "$SESSION_ID" --single "Reply with exactly: cache-proof-two." \
  --debug-file "$PROOF_ROOT/second.debug" >"$PROOF_ROOT/second.out" 2>&1

python3 - "$EPOCH_BEFORE" "$SESSION_DIR/cache_epoch.json" \
  "$SESSION_DIR/cache_request_evidence.jsonl" "$GROK_HOME/logs/unified.jsonl" <<'PY'
import json, pathlib, sys

before_path, after_path, ledger_path, unified_path = map(pathlib.Path, sys.argv[1:])
before = json.loads(before_path.read_text())
after = json.loads(after_path.read_text())
if before["domain_fingerprint"] != after["domain_fingerprint"]:
    raise SystemExit("FAIL: restart changed cache domain fingerprint")
if after["generation"] < before["generation"]:
    raise SystemExit("FAIL: cache epoch generation moved backwards")

rows = [json.loads(line) for line in ledger_path.read_text().splitlines() if line.strip()]
if len(rows) < 2:
    raise SystemExit("FAIL: fewer than two durable request evidence records")
required = {"schema_version", "cache_domain_hash", "cache_epoch_id", "transport_hash",
            "provider_cache_material_hash", "body_bytes", "serialization_kind",
            "mutation_reasons", "attempt_index"}
for row in rows:
    if not required <= set(row):
        raise SystemExit("FAIL: durable request evidence schema is incomplete")
    forbidden = {"prompt", "authorization", "api_key", "headers", "request_id"} & set(row)
    if forbidden:
        raise SystemExit("FAIL: durable request evidence contains forbidden fields")
    if row["cache_epoch_id"] not in {before["epoch_id"], after["epoch_id"]}:
        raise SystemExit("FAIL: request evidence has an unknown cache epoch")

# Read only Lumen's structured, sanitized telemetry context; never inspect a
# debug stream or a provider response body. Strict proof accepts only a
# validated provider report from the second call, never request success,
# transport hashes, or local prefix similarity.
usage = []
if unified_path.exists():
    for line in unified_path.read_text(errors="replace").splitlines():
        entry = json.loads(line)
        if entry.get("msg") != "shell.turn.inference_done":
            continue
        ctx = entry.get("ctx") or {}
        usage.append((ctx.get("prompt_tokens"),
                      ctx.get("provider_cache_accounting"),
                      ctx.get("provider_cache_hit_tokens"),
                      ctx.get("provider_cache_miss_tokens")))
if len(usage) < 2:
    raise SystemExit("FAIL: two sanitized provider usage telemetry records were not persisted")
prompt, accounting, hit, miss = usage[-1]
if accounting not in {"reported", "unavailable", "contradictory"}:
    raise SystemExit("FAIL: provider cache accounting telemetry is invalid")
if accounting != "reported":
    raise SystemExit(f"FAIL: strict proof requires internally consistent provider accounting, got {accounting}")
if not isinstance(prompt, int) or prompt <= 0:
    raise SystemExit("FAIL: strict proof requires provider-reported positive prompt tokens on the second request")
if not isinstance(hit, int) or hit <= 0 or hit > prompt:
    raise SystemExit("FAIL: strict proof requires a nonzero provider-reported cache hit on the second request")
if miss is not None and (not isinstance(miss, int) or miss < 0 or hit + miss != prompt):
    raise SystemExit("FAIL: strict proof rejected inconsistent provider cache hit/miss accounting")
attempts = sorted({row["attempt_index"] for row in rows})
backends = sorted({row["serialization_kind"] for row in rows})
print("PASS: DeepSeek V4 strict cache live proof "
      f"provider_cache_accounting={accounting} provider_cache_hit=true "
      f"provider_cache_hit_tokens={hit if hit is not None else 'unavailable'} "
      f"provider_cache_miss_tokens={miss if miss is not None else 'unavailable'} "
      f"durable_epoch_generation_before={before['generation']} after={after['generation']} evidence_records={len(rows)} "
      f"attempt_indices={','.join(map(str, attempts))} serialization={','.join(backends)}")
PY
