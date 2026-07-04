#!/usr/bin/env bash
# RM-HUMAN: Real Claude subscription OAuth — starts sandbox, opens browser, you click login.
# Iron law: writes only under ~/.lumen/science/sandbox (never ~/.claude-science).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SCRATCH="${SCRATCH:-$ROOT/.science-rm-scratch}"
mkdir -p "$SCRATCH"
LOG="$SCRATCH/human-oauth-start.log"

# Default: your real HOME so login persists. Override with GUARD_HOME for isolated test.
if [[ -n "${GUARD_HOME:-}" ]]; then
  export HOME="$GUARD_HOME"
  export LUMEN_SCIENCE_DIR="${LUMEN_SCIENCE_DIR:-$HOME/.lumen/science}"
  mkdir -p "$LUMEN_SCIENCE_DIR"
  echo "▸ isolated GUARD_HOME=$HOME"
else
  echo "▸ using real HOME=$HOME (sandbox only; ~/.claude-science untouched)"
fi

if [[ -f "$ROOT/.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "$ROOT/.env"
  set +a
fi

export LUMEN_BIN="${LUMEN_BIN:-$ROOT/bin/lumen}"
cd "$ROOT"
CGO_ENABLED=0 go build -o "$LUMEN_BIN" ./cmd/lumen

# Prove we never symlink real cred dir
REAL_SCIENCE="${SCIENCE_REAL_HOME:-$HOME}/.claude-science"
if [[ -L "$REAL_SCIENCE" ]]; then
  echo "FAIL: real .claude-science is a symlink" >&2
  exit 1
fi

echo "▸ starting proxy + sandbox (background)…"
: >"$LOG"
"$LUMEN_BIN" science start --no-browser >>"$LOG" 2>&1 &
SPID=$!
cleanup() {
  if kill -0 "$SPID" 2>/dev/null; then
    kill -INT "$SPID" 2>/dev/null || true
    wait "$SPID" 2>/dev/null || true
  fi
}
trap cleanup EXIT

URL=""
for _ in $(seq 1 90); do
  URL="$(grep -Eo 'http://localhost:[0-9]+/[^[:space:]]+' "$LOG" 2>/dev/null | head -1 || true)"
  if [[ -n "$URL" ]]; then
    break
  fi
  if ! kill -0 "$SPID" 2>/dev/null; then
    echo "FAIL: science start exited early" >&2
    tail -30 "$LOG" >&2
    exit 1
  fi
  sleep 1
done

if [[ -z "$URL" ]]; then
  echo "FAIL: sandbox URL not found in log" >&2
  tail -30 "$LOG" >&2
  exit 1
fi

echo ""
echo "════════════════════════════════════════════════════════════"
echo "  请在浏览器完成 Claude Science 登录（真实订阅 OAuth）"
echo "════════════════════════════════════════════════════════════"
echo ""
echo "  沙箱地址: $URL"
echo ""
echo "  1. 浏览器将自动打开上述地址"
echo "  2. 在页面里用 Anthropic / Claude 账号登录"
echo "  3. 登录成功后回到此终端按 Enter"
echo ""
echo "  安全: 仅写入 ~/.lumen/science/sandbox，不会修改 ~/.claude-science"
echo "════════════════════════════════════════════════════════════"
echo ""

if [[ "$(uname -s)" == "Darwin" ]]; then
  open "$URL" || true
else
  xdg-open "$URL" 2>/dev/null || true
fi

read -r -p "完成登录后按 Enter 继续验证… " _

DATA_DIR="$HOME/.lumen/science/sandbox/home/.claude-science"
BIN="${SCIENCE_BIN:-/Applications/Claude Science.app/Contents/Resources/bin/claude-science}"
if [[ -x "$BIN" ]] && [[ -d "$DATA_DIR" ]]; then
  echo "▸ sandbox status:"
  HOME="$HOME/.lumen/science/sandbox/home" "$BIN" status --data-dir "$DATA_DIR" 2>/dev/null | head -8 || true
fi

# Best-effort: detect non-virtual email in oauth blob (real login vs virtual bootstrap)
if command -v python3 >/dev/null 2>&1 && [[ -d "$DATA_DIR/.oauth-tokens" ]]; then
  python3 - <<'PY' "$DATA_DIR" 2>/dev/null || true
import json, os, sys, glob
data = sys.argv[1]
active = os.path.join(data, "active-org.json")
if os.path.isfile(active):
    with open(active) as f:
        print("  active-org:", json.load(f).get("org_uuid", "?"))
encs = glob.glob(os.path.join(data, ".oauth-tokens", "*.enc"))
print(f"  oauth token files: {len(encs)}")
PY
fi

echo ""
echo "✓ 人工 OAuth 步骤结束 — 沙箱保持运行，代理已按 quit 语义停止"
echo "  再次启动: lumen science start"
echo "  完全停止: lumen science stop --all"
echo "  日志: $LOG"