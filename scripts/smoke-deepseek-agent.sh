#!/usr/bin/env bash
# L1 Agent readiness: real tool_calls via DeepSeek (FINAL-2.0).
# Exit 0 = tool evidence + marker; 2 = no key; 1 = fail.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export PATH="/opt/homebrew/bin:$HOME/.cargo/bin:$HOME/.local/bin:$PATH"
export PROTOC="${PROTOC:-/opt/homebrew/bin/protoc}"

KEY="${DEEPSEEK_API_KEY:-}"
if [[ -z "$KEY" ]]; then
  echo "SKIP: set DEEPSEEK_API_KEY for L1 agent smoke"
  exit 2
fi

BIN="$ROOT/agent/target/release/lumen"
if [[ ! -x "$BIN" ]]; then
  echo "Building release lumen..."
  (cd "$ROOT/agent" && CARGO_BUILD_JOBS="${CARGO_BUILD_JOBS:-2}" cargo build -p xai-grok-pager-bin --release)
fi
test -x "$BIN"

SMOKE_HOME="$(mktemp -d "${TMPDIR:-/tmp}/lumen-agent-smoke-XXXXXX")"
PROOF_DIR="$ROOT/artifacts/readiness"
mkdir -p "$PROOF_DIR"
DEBUG_LOG="$SMOKE_HOME/debug.log"
OUT_LOG="$SMOKE_HOME/out.txt"
export GROK_HOME="$SMOKE_HOME"
export DEEPSEEK_API_KEY="$KEY"
unset XAI_API_KEY GROK_CODE_XAI_API_KEY OPENAI_API_KEY OPENAI_BASE_URL 2>/dev/null || true

cat >"$SMOKE_HOME/config.toml" <<'CFG'
[models]
default = "deepseek-chat"
[model.deepseek-chat]
model = "deepseek-chat"
base_url = "https://api.deepseek.com/v1"
api_backend = "chat_completions"
env_key = "DEEPSEEK_API_KEY"
[cli]
auto_update = false
CFG

MARKER="LUMEN_TOOL_OK"
PROMPT="You MUST use a bash/shell/terminal tool (run_terminal_command) to run exactly:
echo ${MARKER}
Do not invent the output. After the tool returns, reply with only that stdout line."

echo "Running L1 agent tool smoke (isolated GROK_HOME)..."
set +e
"$BIN" -m deepseek-chat --single "$PROMPT" --output-format plain --always-approve --max-turns 8 \
  --debug-file "$DEBUG_LOG" >"$OUT_LOG" 2>&1
EC=$?
set -e

echo "----- output -----"
cat "$OUT_LOG"
echo "----- end -----"

AUTH_FAIL=0
grep -Eq 'Unauthorized \(401\)|Authentication Fails|Not signed in' "$OUT_LOG" && AUTH_FAIL=1 || true
TOOL=0
grep -Eq 'stop_reason="tool_calls"|response.has_tool_call=true|Model requesting tool:' "$DEBUG_LOG" && TOOL=1 || true
MARKER_OK=0
grep -q "$MARKER" "$OUT_LOG" && MARKER_OK=1 || true
[[ $MARKER_OK -eq 1 ]] && TOOL=1 || true

python3 -c "
import json, re
from datetime import datetime, timezone
from pathlib import Path
out = Path(r'''$OUT_LOG''').read_text(errors='replace')
dbg = Path(r'''$DEBUG_LOG''').read_text(errors='replace') if Path(r'''$DEBUG_LOG''').exists() else ''
red = lambda s: re.sub(r'sk-[a-zA-Z0-9]+', 'sk-REDACTED', s)
ok = ($EC == 0) and ($TOOL == 1) and ($MARKER_OK == 1) and ($AUTH_FAIL == 0)
art = {
  'schema_version': 1,
  'check_id': 'L1',
  'pass': ok,
  'exit_code': $EC,
  'marker_seen': bool($MARKER_OK),
  'tool_evidence': bool($TOOL),
  'auth_fail': bool($AUTH_FAIL),
  'generated_at': datetime.now(timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ'),
  'output_snippet': red(out)[:400],
}
Path(r'''$PROOF_DIR/L1-tool-calls.json''').write_text(json.dumps(art, indent=2) + '\n')
print('wrote L1-tool-calls.json pass=', ok)
raise SystemExit(0 if ok else 1)
"
PASS_EC=$?
rm -rf "$SMOKE_HOME"

if [[ $PASS_EC -ne 0 ]]; then
  echo "FAIL: L1 agent smoke (need tool_call + $MARKER, no 401)" >&2
  exit 1
fi
echo "OK: L1 Agent smoke — CanToolCall evidence OK"
exit 0
