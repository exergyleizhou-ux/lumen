#!/usr/bin/env bash
# DeepSeek smoke for Lumen release binary.
# With DEEPSEEK_API_KEY (or OPENAI_API_KEY): attempt a non-interactive prompt.
# Without a key: exit 0 after one explicit SKIP line; defaults still proven by assert-defaults.sh.
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

KEY="${DEEPSEEK_API_KEY:-${OPENAI_API_KEY:-}}"
if [[ -z "$KEY" ]]; then
  echo "SKIP: no API key (set DEEPSEEK_API_KEY or OPENAI_API_KEY for live DeepSeek smoke)"
  exit 0
fi

# Headless single-turn: -p / --single <PROMPT> (see lumen --help)
export OPENAI_API_KEY="$KEY"
export DEEPSEEK_API_KEY="$KEY"
export OPENAI_BASE_URL="${OPENAI_BASE_URL:-https://api.deepseek.com/v1}"
set +e
OUT="$("$BIN" --single "Reply with exactly the word pong and nothing else." \
  --output-format plain \
  --always-approve \
  2>&1)"
EC=$?
set -e
echo "$OUT"
if [[ $EC -ne 0 ]]; then
  set +e
  OUT="$("$BIN" -p "Reply with exactly the word pong and nothing else." 2>&1)"
  EC=$?
  set -e
  echo "$OUT"
fi
[[ $EC -eq 0 ]] || { echo "FAIL: live smoke exit $EC" >&2; exit $EC; }
echo "OK: live DeepSeek/headless smoke finished exit=0"
exit 0
