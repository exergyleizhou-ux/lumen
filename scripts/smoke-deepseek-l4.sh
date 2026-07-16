#!/usr/bin/env bash
# L4-min: fault recovery + cancel/teardown (FINAL-2.0 automatable slice).
# 1) Tool error then recover (false → echo L4_RECOVER_OK)
# 2) Real process-group kill_all suite (xai-tty-utils) — cancel/teardown contract
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export PATH="/opt/homebrew/bin:$HOME/.cargo/bin:$HOME/.local/bin:$PATH"
export PROTOC="${PROTOC:-/opt/homebrew/bin/protoc}"
PROOF="$ROOT/artifacts/readiness"
mkdir -p "$PROOF"

KEY="${DEEPSEEK_API_KEY:-}"
if [[ -z "$KEY" ]]; then
  echo "SKIP: DEEPSEEK_API_KEY required for L4 agent recovery leg"
  exit 2
fi

BIN="$ROOT/agent/target/release/lumen"
[[ -x "$BIN" ]] || (cd "$ROOT/agent" && CARGO_BUILD_JOBS=2 cargo build -p xai-grok-pager-bin --release)
test -x "$BIN"

echo "=== L4-min A: process kill_all (real shipped tty-utils) ==="
(
  cd "$ROOT/agent"
  cargo test -p xai-tty-utils --lib kill_all -- --nocapture
)

HOME_ISO="$(mktemp -d "${TMPDIR:-/tmp}/lumen-l4-home-XXXXXX")"
DEBUG="$HOME_ISO/debug.log"
OUT="$HOME_ISO/out.txt"
export GROK_HOME="$HOME_ISO"
export DEEPSEEK_API_KEY="$KEY"
unset XAI_API_KEY GROK_CODE_XAI_API_KEY 2>/dev/null || true

cat >"$HOME_ISO/config.toml" <<'CFG'
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

PROMPT='You MUST use terminal tools in this order (two separate tool calls):
1) run exactly: false
   (this will fail — that is intended)
2) after the failure, run exactly: echo L4_RECOVER_OK
Then reply with only: L4_RECOVER_OK
Do not skip the failing command.'

echo "=== L4-min B: tool error then recover ==="
set +e
"$BIN" -m deepseek-chat --single "$PROMPT" --output-format plain --always-approve --max-turns 12 \
  --debug-file "$DEBUG" >"$OUT" 2>&1
EC=$?
set -e
echo "----- out -----"
cat "$OUT"

python3 -c "
import json, re
from datetime import datetime, timezone
from pathlib import Path
out = Path(r'''$OUT''').read_text(errors='replace')
dbg = Path(r'''$DEBUG''').read_text(errors='replace') if Path(r'''$DEBUG''').exists() else ''
red = lambda s: re.sub(r'sk-[a-zA-Z0-9]+', 'sk-REDACTED', s)
tools = re.findall(r\"Model requesting tool: name='([^']+)'\", dbg)
# look for failed bash + success
fail_seen = bool(re.search(r'exit_code=Some\\(1\\)|exit_code=Some\\([^0]\\)|Command failed|exit code 1', dbg, re.I))
recover = 'L4_RECOVER_OK' in out
ok = ($EC == 0) and len(tools) >= 2 and recover
art = {
  'schema_version': 1,
  'check_id': 'L4_min',
  'pass': ok,
  'exit_code': $EC,
  'tool_count': len(tools),
  'tools': tools[:20],
  'recover_marker': recover,
  'failure_evidence_in_debug': fail_seen,
  'process_kill_suite': 'xai-tty-utils kill_all (ran)',
  'scope': 'minimal — not full 429/partition chaos matrix',
  'generated_at': datetime.now(timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ'),
  'output_snippet': red(out)[:500],
}
Path(r'''$PROOF/L4-fault-cancel.json''').write_text(json.dumps(art, indent=2)+'\n')
print(json.dumps({'pass': ok, 'tools': tools, 'recover': recover, 'fail_seen': fail_seen}, indent=2))
raise SystemExit(0 if ok else 1)
"
PEC=$?
rm -rf "$HOME_ISO"
[[ $PEC -eq 0 ]] || { echo "FAIL: L4-min" >&2; exit 1; }
echo "OK: L4-min signed (error recovery + process kill suite)"
exit 0
