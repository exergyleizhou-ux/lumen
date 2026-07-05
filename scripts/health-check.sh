#!/bin/bash
# Lumen 每日健康巡检 — 可配合 launchd/systemd 定时运行
# 检查所有服务状态，写报告到 findings/daily/
set -euo pipefail

LUMEN_BIN="${LUMEN_BIN:-/usr/local/bin/lumen}"
SCI_DIR="${HOME}/.lumen/science"
OUT_DIR="${HOME}/.lumen/findings/daily"
mkdir -p "$OUT_DIR"

STAMP=$(date +%Y%m%d-%H%M%S)
REPORT="$OUT_DIR/report-$STAMP.md"
LOCK="$OUT_DIR/.run.lock"

if ! mkdir "$LOCK" 2>/dev/null; then
  echo "[$STAMP] 已有巡检在跑，跳过" >> "$OUT_DIR/log"
  exit 0
fi
trap 'rmdir "$LOCK" 2>/dev/null || true' EXIT

{
  echo "# Lumen 健康巡检 · $STAMP"
  echo ""
  echo "## 服务状态"
  
  # 1. Bridge GUI
  if curl -sf --max-time 3 http://127.0.0.1:18990/api/health > /dev/null 2>&1; then
    echo "- ✅ Bridge GUI (:18990) — 在线"
  else
    echo "- ❌ Bridge GUI — 离线"
  fi

  # 2. Lab
  if curl -sf --max-time 3 http://127.0.0.1:18992/api/lab/health > /dev/null 2>&1; then
    FLEET=$(curl -sf --max-time 3 http://127.0.0.1:18992/api/lab/health | python3 -c "import sys,json;d=json.load(sys.stdin);print(d['fleet']['connected_total'])" 2>/dev/null || echo "?")
    echo "- ✅ Lab (:18992) — fleet $FLEET/28"
  else
    echo "- ❌ Lab — 离线"
  fi

  # 3. Proxy
  if curl -sf --max-time 3 http://127.0.0.1:18991/health > /dev/null 2>&1; then
    CACHE=$(curl -sf --max-time 3 http://127.0.0.1:18991/health | python3 -c "import sys,json;d=json.load(sys.stdin);print(d.get('cache_session_hit_pct',0))" 2>/dev/null || echo "?")
    echo "- ✅ Proxy (:18991) — cache $CACHE%"
  else
    echo "- ⚠️ Proxy — 离线（可能未启动 Bridge）"
  fi

  # 4. Sandbox
  if curl -sf --max-time 3 http://127.0.0.1:8990/health > /dev/null 2>&1; then
    echo "- ✅ Sandbox (:8990) — 在线"
  else
    echo "- ⚠️ Sandbox — 离线（可能未启动 Bridge）"
  fi

  # 5. Coding UI
  if curl -sf --max-time 3 http://127.0.0.1:8787/ > /dev/null 2>&1; then
    echo "- ✅ 编程端 (:8787) — 在线"
  else
    echo "- ⚠️ 编程端 — 离线"
  fi

  echo ""
  echo "## 建议"
  if [ "$FLEET" -lt 20 ] 2>/dev/null; then
    echo "- ⚠️ Fleet 连接数偏低 ($FLEET)，建议检查 research pack"
  fi
  
  echo ""
  echo "---"
  echo "自动生成于 $(date)"
} > "$REPORT"

echo "[$STAMP] 巡检完成 → $REPORT"
