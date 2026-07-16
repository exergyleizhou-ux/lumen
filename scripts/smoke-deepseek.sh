#!/usr/bin/env bash
# DeepSeek live smoke for Lumen release binary.
#
# Proves REAL DeepSeek routing (not user xAI session / grok-4.5):
#   1. Isolated GROK_HOME (no ~/.grok auth.json / sessions)
#   2. Embedded BYOK defaults (base_url + DEEPSEEK_API_KEY) + written config
#   3. Logs must show model deepseek-chat + api.deepseek.com
#   4. Must NOT resolve to grok-4.5 or require grok.com login
#
# Exit 0 when:
#   - no key (structural SKIP)
#   - live reply OK
#   - DeepSeek routing proven even if API key returns 401
# Exit 1 on login/routing failures.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export PATH="/opt/homebrew/bin:$HOME/.cargo/bin:$PATH"
export PROTOC="${PROTOC:-/opt/homebrew/bin/protoc}"

"$ROOT/scripts/assert-defaults.sh"

BIN="$ROOT/agent/target/release/lumen"
if [[ ! -x "$BIN" ]]; then
  echo "Building release lumen (this may take several minutes)..."
  (cd "$ROOT/agent" && CARGO_BUILD_JOBS="${CARGO_BUILD_JOBS:-2}" cargo build -p xai-grok-pager-bin --release)
fi
test -x "$BIN" || { echo "FAIL: $BIN not executable" >&2; exit 1; }

KEY="${DEEPSEEK_API_KEY:-}"
if [[ -z "$KEY" ]]; then
  echo "SKIP: no DEEPSEEK_API_KEY — structural defaults OK; live DeepSeek smoke skipped"
  exit 0
fi

SMOKE_HOME="$(mktemp -d "${TMPDIR:-/tmp}/lumen-smoke-XXXXXX")"
PROOF_DIR="$ROOT/journal/artifacts"
mkdir -p "$PROOF_DIR"
PROOF="$PROOF_DIR/smoke-deepseek-$(date +%Y%m%d-%H%M%S).txt"
DEBUG_LOG="$SMOKE_HOME/smoke-debug.log"
OUT_LOG="$SMOKE_HOME/smoke-out.txt"
EC=1

cleanup() {
  {
    echo "timestamp=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    echo "GROK_HOME_was_isolated=true"
    echo "exit=${EC:-?}"
    echo "--- response (truncated) ---"
    head -c 4000 "$OUT_LOG" 2>/dev/null || true
    echo
    echo "--- debug routing lines ---"
    if [[ -f "$DEBUG_LOG" ]]; then
      grep -E 'model_id=|currentModelId|resolved credentials|deepseek|base_url|chat/completions' "$DEBUG_LOG" 2>/dev/null | head -60 || true
    fi
  } >"$PROOF" 2>/dev/null || true
  echo "Proof saved: $PROOF"
  rm -rf "$SMOKE_HOME"
}
trap cleanup EXIT

export GROK_HOME="$SMOKE_HOME"
export DEEPSEEK_API_KEY="$KEY"
unset XAI_API_KEY GROK_CODE_XAI_API_KEY OPENAI_API_KEY OPENAI_BASE_URL || true

cat >"$SMOKE_HOME/config.toml" <<'EOF'
[models]
default = "deepseek-chat"

[model.deepseek-chat]
model = "deepseek-chat"
name = "DeepSeek Chat"
base_url = "https://api.deepseek.com/v1"
api_backend = "chat_completions"
env_key = "DEEPSEEK_API_KEY"

[cli]
auto_update = false
EOF

echo "GROK_HOME=$GROK_HOME (isolated)"
echo "Running: lumen -m deepseek-chat --single …"

set +e
"$BIN" \
  -m deepseek-chat \
  --single "Reply with exactly the word pong and nothing else." \
  --output-format plain \
  --always-approve \
  --max-turns 2 \
  --debug-file "$DEBUG_LOG" \
  >"$OUT_LOG" 2>&1
EC=$?
set -e

echo "----- stdout/stderr -----"
cat "$OUT_LOG"
echo "----- end -----"

# Files to scan (debug may be missing if early crash)
LOGS=("$OUT_LOG")
[[ -f "$DEBUG_LOG" ]] && LOGS+=("$DEBUG_LOG")

has() {
  # grep files; never fail the script on no-match
  grep -E "$1" "${LOGS[@]}" >/dev/null 2>&1
}

# Hard fail: still on xAI login path
if has 'Not signed in'; then
  echo "FAIL: still hitting xAI login gate — BYOK not effective" >&2
  exit 1
fi

# Hard fail: grok-4.x selected
if has 'model_id=grok-4|currentModelId.:.grok-4|Model:[[:space:]]+grok-4'; then
  echo "FAIL: session resolved to grok-4.x — not isolated DeepSeek" >&2
  exit 1
fi

# Must prove deepseek-chat model
if ! has 'model_id=deepseek-chat|currentModelId.:.deepseek-chat|Model:[[:space:]]+deepseek-chat|model=deepseek-chat'; then
  echo "FAIL: no proof of deepseek-chat model selection" >&2
  exit 1
fi

# Must prove api.deepseek.com host
if ! has 'api\.deepseek\.com'; then
  echo "FAIL: no api.deepseek.com evidence" >&2
  exit 1
fi

if [[ $EC -eq 0 ]]; then
  if ! grep -Eiq '\bpong\b' "$OUT_LOG"; then
    echo "WARN: exit 0 but response did not contain 'pong'"
  fi
  echo "OK: live DeepSeek smoke — model=deepseek-chat @ api.deepseek.com, isolated GROK_HOME"
  exit 0
fi

# Routing-proven auth failure (invalid key) — M0 wiring gate passes
if has 'Unauthorized \(401\) from https://api\.deepseek\.com|api\.deepseek\.com.*authentication_error|Authentication Fails'; then
  echo "OK: DeepSeek ROUTING proven (model=deepseek-chat @ api.deepseek.com, Auth=ApiKey)"
  echo "NOTE: API key rejected by DeepSeek (401). Refresh DEEPSEEK_API_KEY for full reply smoke."
  exit 0
fi

echo "FAIL: live smoke exit $EC without DeepSeek routing success pattern" >&2
exit "$EC"
