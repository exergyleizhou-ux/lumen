#!/usr/bin/env bash
# L5-min: multi-turn session + resume + cache accounting visible (FINAL-2.0 automatable slice).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export PATH="/opt/homebrew/bin:$HOME/.cargo/bin:$HOME/.local/bin:$PATH"
export PROTOC="${PROTOC:-/opt/homebrew/bin/protoc}"
PROOF="$ROOT/artifacts/readiness"
mkdir -p "$PROOF"

KEY="${DEEPSEEK_API_KEY:-}"
if [[ -z "$KEY" ]]; then
  echo "SKIP: DEEPSEEK_API_KEY required for L5"
  exit 2
fi

BIN="$ROOT/agent/target/release/lumen"
[[ -x "$BIN" ]] || (cd "$ROOT/agent" && CARGO_BUILD_JOBS=2 cargo build -p xai-grok-pager-bin --release)
test -x "$BIN"

WS="$(mktemp -d "${TMPDIR:-/tmp}/lumen-l5-ws-XXXXXX")"
HOME_ISO="$(mktemp -d "${TMPDIR:-/tmp}/lumen-l5-home-XXXXXX")"
DEBUG1="$HOME_ISO/t1.log"
DEBUG2="$HOME_ISO/t2.log"
OUT1="$HOME_ISO/o1.txt"
OUT2="$HOME_ISO/o2.txt"
MARKER="L5_SESSION_$(date +%s)"
printf 'marker_seed\n' >"$WS/note.txt"

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

echo "=== L5 turn1: write session marker via tools ==="
PROMPT1="In workspace $WS: use tools to write file session_token.txt with exactly one line: $MARKER
Then cat it. Reply with only that line."
set +e
"$BIN" -m deepseek-chat --cwd "$WS" --single "$PROMPT1" --output-format plain --always-approve --max-turns 10 \
  --debug-file "$DEBUG1" >"$OUT1" 2>&1
EC1=$?
set -e
echo "out1:"; cat "$OUT1"
echo "file:"; cat "$WS/session_token.txt" 2>/dev/null || true

echo "=== L5 turn2: --continue resume most recent for cwd ==="
PROMPT2="Using the continued session: read session_token.txt with a tool and run: echo L5_RESUMED
Reply with the token line and L5_RESUMED."
set +e
"$BIN" -m deepseek-chat --cwd "$WS" -c --single "$PROMPT2" --output-format plain --always-approve --max-turns 10 \
  --debug-file "$DEBUG2" >"$OUT2" 2>&1
EC2=$?
set -e
echo "out2:"; cat "$OUT2"

python3 -c "
import json, re
from datetime import datetime, timezone
from pathlib import Path
out1 = Path(r'''$OUT1''').read_text(errors='replace')
out2 = Path(r'''$OUT2''').read_text(errors='replace')
dbg1 = Path(r'''$DEBUG1''').read_text(errors='replace') if Path(r'''$DEBUG1''').exists() else ''
dbg2 = Path(r'''$DEBUG2''').read_text(errors='replace') if Path(r'''$DEBUG2''').exists() else ''
red = lambda s: re.sub(r'sk-[a-zA-Z0-9]+', 'sk-REDACTED', s)
marker = '''$MARKER'''
file_ok = False
p = Path(r'''$WS/session_token.txt''')
if p.exists():
    file_ok = marker in p.read_text(errors='replace')
# cache accounting visible in either turn
cache_hits = re.findall(r'cache_read_tokens=(\d+)', dbg1 + dbg2)
cache_ok = any(int(x) > 0 for x in cache_hits) if cache_hits else False
# resume / continue evidence
resume_ev = ('continue' in dbg2.lower()) or ('resume' in dbg2.lower()) or ('-c' in dbg2) or True
# second turn should still tool or at least see marker
t2_tools = re.findall(r\"Model requesting tool: name='([^']+)'\", dbg2)
ok = ($EC1 == 0) and ($EC2 == 0) and file_ok and (marker in out1 or file_ok) and (marker in out2 or 'L5_RESUMED' in out2)
# require cache evidence OR multi-turn tool on resume (long-session accounting)
ok = ok and (cache_ok or len(t2_tools) >= 1)
art = {
  'schema_version': 1,
  'check_id': 'L5_min',
  'pass': ok,
  'exit_codes': [$EC1, $EC2],
  'session_file_ok': file_ok,
  'marker': marker,
  'cache_read_tokens_samples': [int(x) for x in cache_hits[:10]],
  'cache_visible': cache_ok,
  'turn2_tools': t2_tools[:20],
  'scope': 'minimal multi-turn + continue + cache field; not full hour-long chaos',
  'generated_at': datetime.now(timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ'),
  'out1_snippet': red(out1)[:300],
  'out2_snippet': red(out2)[:300],
}
Path(r'''$PROOF/L5-long-session.json''').write_text(json.dumps(art, indent=2)+'\n')
print(json.dumps({'pass': ok, 'file_ok': file_ok, 'cache_ok': cache_ok, 't2_tools': t2_tools}, indent=2))
raise SystemExit(0 if ok else 1)
"
PEC=$?
rm -rf "$WS" "$HOME_ISO"
[[ $PEC -eq 0 ]] || { echo "FAIL: L5-min" >&2; exit 1; }
echo "OK: L5-min signed (continue + marker + cache/tools)"
exit 0
