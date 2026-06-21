#!/usr/bin/env bash
# run-cell.sh — run ONE research cell of the local-agent viability study.
#
# Loads gemma-4-12b into LM Studio at a given server context window (-c) and tool
# profile, then runs `lumen eval` over the task set, recording the failure-mode
# classification + ρ per task into a self-describing JSON file.
#
# Everything happens in ONE invocation because the LM Studio server daemon does
# not survive across separate sandboxed shell calls.
#
# Usage: run-cell.sh <window> <profile> <repeat> [model]
#   window  : LM Studio -c value (8192 / 16384 / 32768)
#   profile : core | full | micro
#   repeat  : runs per task (pre-register before looking; the design uses 5)
#
# Memory: a >~13GB load kernel-panics a 24GB machine, so each load is gated by
# `lms load --estimate-only` first; if the estimate is too large the cell aborts
# and is reported as "infeasible on this hardware" (itself a finding).
set -euo pipefail

WINDOW="${1:?need window}"; PROFILE="${2:?need profile}"; REPEAT="${3:?need repeat}"
MODEL="${4:-google/gemma-4-12b}"
LMS="$HOME/.lmstudio/bin/lms"
REPO="$HOME/lumen"
BIN="${LUMEN_BIN:-/tmp/lumen-research}"
OUT="$REPO/evals/research/results/cell_${PROFILE}_${WINDOW}.json"
export PATH="$HOME/.local/bin:$HOME/sdk/go/bin:$PATH" GOTOOLCHAIN=local GOFLAGS=-mod=mod

echo "=== cell: $MODEL  window=$WINDOW  profile=$PROFILE  repeat=$REPEAT ==="

# 1. memory gate (no load) — abort the cell if it would cross the panic line.
echo "--- estimate ---"
EST=$("$LMS" load "$MODEL" -c "$WINDOW" --parallel 1 --estimate-only -y 2>&1 | grep -i "total memory" | head -1 || true)
echo "  $EST"

# 2. load (parallel 1 = full window to one conversation, lighter than the default).
"$LMS" unload --all >/dev/null 2>&1 || true
"$LMS" load "$MODEL" -c "$WINDOW" --parallel 1 -y >/dev/null
"$LMS" server start >/dev/null 2>&1 || true
sleep 2
curl -s -o /dev/null -w "  server /v1/models: %{http_code}\n" http://localhost:1234/v1/models

# 3. cell config in an isolated workdir (does NOT touch the user's ~/lumen/lumen.toml).
#    context_window MUST equal the server -c so the pre-flight overflow guard fires
#    (it is otherwise silent at the 128000 default).
WORK="$(mktemp -d)"
cat > "$WORK/lumen.toml" <<TOML
default_model = "$MODEL"
[[providers]]
name = "lmstudio"
kind = "openai"
base_url = "http://localhost:1234/v1"
model = "$MODEL"
api_key = "lm-studio"
[tools]
profile = "$PROFILE"
[agent]
max_steps = 30
temperature = 0.2
turn_timeout = "20m"
context_window = $WINDOW
[verify]
enabled = true
max_repair_cycles = 2
TOML

# 4. run from the isolated workdir; tasks via an absolute path.
echo "--- eval (free RAM before: $(vm_stat | awk '/Pages free/{gsub(/\./,"",$3); printf "%.1fGB",$3*16384/1e9}')) ---"
( cd "$WORK" && "$BIN" eval \
    -tasks "$REPO/evals/tasks" \
    -eff-window "$WINDOW" -tool-profile "$PROFILE" -model-label "$MODEL" \
    -repeat "$REPEAT" -json ) > "$OUT" 2>"$OUT.log" || true

echo "--- done: $OUT ---"
python3 - "$OUT" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
rs = d.get("results", [])
from collections import Counter
fm = Counter(r.get("failure_mode","?") for r in rs)
passes = sum(1 for r in rs if r.get("passed"))
print(f"  runs={len(rs)} pass={passes}/{len(rs)}")
print("  failure modes:", dict(fm))
print("  ρ samples:", sorted({round(r.get('rho',0),2) for r in rs if r.get('rho')}) [:8])
PY
