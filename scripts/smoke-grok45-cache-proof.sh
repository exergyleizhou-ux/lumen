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

if [[ -z "${XAI_API_KEY:-}" ]]; then
  echo "BLOCKED: XAI_API_KEY is absent; no provider request was made" >&2
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
export GROK_HOME="$PROOF_ROOT/grok-home"
mkdir -p "$LUMEN_HOME" "$GROK_HOME"
unset DEEPSEEK_API_KEY KIMI_CODE_API_KEY OPENAI_API_KEY OPENAI_BASE_URL \
  ANTHROPIC_API_KEY ANTHROPIC_AUTH_TOKEN GROK_API_KEY GROK_CODE_XAI_API_KEY \
  2>/dev/null || true
cat >"$LUMEN_HOME/config.toml" <<'CFG'
[models]
default = "grok-4.5"
[cli]
auto_update = false
CFG

COMMON=(-m grok-4.5 --reasoning-effort low --output-format plain --always-approve --max-turns 2)
if ! "$BIN" "${COMMON[@]}" --single "Reply with exactly: cache-proof-one." \
  --debug-file "$PROOF_ROOT/first.debug" >"$PROOF_ROOT/first.out" 2>&1; then
  echo "FAIL: first Grok 4.5 provider request failed" >&2
  exit 1
fi

SESSION_DIR="$(find "$PROOF_ROOT" -type f -name chat_history.jsonl -print -quit | xargs -n1 dirname)"
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
  "$SESSION_DIR/cache_request_evidence.jsonl" "$GROK_HOME/logs/unified.jsonl" <<'PY'
import json
import pathlib
import sys

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
        if entry.get("msg") != "shell.turn.inference_done":
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
