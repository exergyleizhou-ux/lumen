#!/usr/bin/env bash
# eval-coding-live.sh — Live coding eval: for each of 20 tasks, copy workspace, run lumen headless to fix,
# then re-run deterministic tests.
#
# Outputs:
#   evidence/eval/eval-run-<run_id>.json   EvalRun schema v2 (run_id/profile/tasks[]/aggregate) — DEBT-033 A1
#   artifacts/readiness/eval-run-latest.json  same content, for readiness visibility
#   artifacts/readiness/eval-live.json     gate file schema v1 (readiness) — unless EVAL_RUN_ONLY=1
#
# Gate: ≥18/20 PASS for publish readiness. EVAL_RUN_ONLY=1 skips the gate file (baseline/对照 runs).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export PATH="/opt/homebrew/bin:$HOME/.cargo/bin:$HOME/.local/bin:$PATH"
TASKS="$ROOT/evals/tasks"
ART="$ROOT/artifacts/readiness"
EVAL_DIR="$ROOT/evidence/eval"
LIVE_DIR="${EVAL_LIVE_DIR:-$ART/eval-live}"
mkdir -p "$LIVE_DIR" "$ART" "$EVAL_DIR"
# lumen rejects relative --cwd (os error 2); normalize to absolute (2026-08-05 finding)
LIVE_DIR="$(cd "$LIVE_DIR" && pwd)"

RUN_ID="${EVAL_RUN_ID:-baseline-$(date +%Y%m%d-%H%M%S)}"
PROFILE="${LUMEN_EVAL_MODEL:-deepseek-v4-pro}"
RUN_ONLY="${EVAL_RUN_ONLY:-0}"

LUMEN_BIN="${LUMEN_BIN:-$HOME/.local/bin/lumen}"
if [[ ! -x "$LUMEN_BIN" ]]; then
  LUMEN_BIN="$ROOT/agent/target/release/lumen"
fi
[[ -x "$LUMEN_BIN" ]] || { echo "FAIL: no lumen binary"; exit 1; }
[[ -n "${DEEPSEEK_API_KEY:-}" ]] || { echo "FAIL: DEEPSEEK_API_KEY required for live eval"; exit 1; }

MAX_TURNS="${EVAL_MAX_TURNS:-12}"
MIN_PASS="${EVAL_MIN_PASS:-18}"
LIMIT="${EVAL_LIMIT:-0}"  # 0 = all

echo "=== eval-coding-live ==="
echo "lumen=$LUMEN_BIN max_turns=$MAX_TURNS min_pass=$MIN_PASS run_id=$RUN_ID profile=$PROFILE"

ms_now() { python3 -c 'import time; print(int(time.time()*1000))'; }

run_tests() {
  local ws="$1"
  if [[ -f "$ws/go.mod" ]]; then
    (cd "$ws" && go test ./... -count=1 -timeout 30s) >/dev/null 2>&1
  elif find "$ws" -name 'test_*.py' -o -name '*_test.py' 2>/dev/null | grep -q .; then
    (cd "$ws" && python3 -m pytest -q) >/dev/null 2>&1
  elif [[ -f "$ws/package.json" ]]; then
    (cd "$ws" && npx --yes vitest run) >/dev/null 2>&1
  else
    return 2
  fi
}

# Collect session metrics for one task work dir:
# "tool_calls|input_bytes|epochs|hit_tokens|miss_tokens|output_tokens"
collect_metrics() {
  python3 - "$1" <<'PY'
import json, os, sys, glob
work = sys.argv[1]
tool_calls = 0
input_bytes = 0
epochs = 0
hit_tokens = 0
miss_tokens = 0
output_tokens = 0
for sr in glob.glob(os.path.join(work, 'grok-home', 'grok-home', 'sessions', '*', '*')):
    ev = os.path.join(sr, 'events.jsonl')
    if os.path.exists(ev):
        try:
            for line in open(ev):
                e = json.loads(line)
                if e.get('type') == 'tool_completed':
                    tool_calls += 1
        except Exception:
            pass
    ce = os.path.join(sr, 'cache_request_evidence.jsonl')
    if os.path.exists(ce):
        try:
            for line in open(ce):
                e = json.loads(line)
                input_bytes += int(e.get('body_bytes') or 0)
        except Exception:
            pass
    ch = os.path.join(sr, 'cache_health.jsonl')  # DEBT-033 A2-a
    if os.path.exists(ch):
        try:
            for line in open(ch):
                e = json.loads(line)
                hit_tokens += int(e.get('hit_tokens') or 0)
                miss_tokens += int(e.get('miss_tokens') or 0)
                output_tokens += int(e.get('output_tokens') or 0)
        except Exception:
            pass
    if os.path.exists(os.path.join(sr, 'cache_epoch.json')):
        epochs += 1
print(f"{tool_calls}|{input_bytes}|{epochs}|{hit_tokens}|{miss_tokens}|{output_tokens}")
PY
}

PASS=0
FAIL=0
TOTAL=0
results_tmp="$(mktemp)"
metrics_tmp="$(mktemp)"
trap 'rm -f "$results_tmp" "$metrics_tmp"' EXIT

for task_dir in "$TASKS"/*/; do
  name=$(basename "$task_dir")
  TOTAL=$((TOTAL + 1))
  if [[ "$LIMIT" -gt 0 && "$TOTAL" -gt "$LIMIT" ]]; then
    TOTAL=$((TOTAL - 1))
    break
  fi

  prompt_file="$task_dir/prompt.txt"
  src_ws="$task_dir/workspace"
  [[ -f "$prompt_file" && -d "$src_ws" ]] || { echo "  SKIP $name missing"; continue; }

  work="$LIVE_DIR/$name"
  rm -rf "$work"
  mkdir -p "$work"
  # copy workspace content (exclude caches)
  rsync -a --exclude node_modules --exclude .pytest_cache --exclude target "$src_ws/" "$work/ws/"
  prompt=$(cat "$prompt_file")
  log="$work/agent.log"
  home="$work/grok-home"
  mkdir -p "$home"

  echo "  RUN $name …"
  start_ms=$(ms_now)
  set +e
  # isolated home + always approve tools; headless single prompt with multi-turn agent loop
  HOME="$home" \
  GROK_HOME="$home/grok-home" \
  LUMEN_HOME="$home/lumen-home" \
  DEEPSEEK_API_KEY="$DEEPSEEK_API_KEY" \
  "$LUMEN_BIN" \
    --cwd "$work/ws" \
    -m "$PROFILE" \
    --always-approve \
    --permission-mode bypassPermissions \
    --max-turns "$MAX_TURNS" \
    --output-format plain \
    -p "$prompt" \
    >"$log" 2>&1
  agent_ec=$?
  set -e
  end_ms=$(ms_now)
  latency_ms=$((end_ms - start_ms))

  set +e
  run_tests "$work/ws"
  test_ec=$?
  set -e

  if [[ $test_ec -eq 0 ]]; then
    echo "  PASS $name (agent_ec=$agent_ec, ${latency_ms}ms)"
    echo "$name|PASS|$agent_ec" >>"$results_tmp"
    PASS=$((PASS + 1))
  else
    echo "  FAIL $name (agent_ec=$agent_ec test_ec=$test_ec, ${latency_ms}ms)"
    echo "$name|FAIL|$agent_ec" >>"$results_tmp"
    FAIL=$((FAIL + 1))
    # tail for diagnosis
    tail -5 "$log" | sed 's/^/    | /' || true
  fi
  metrics=$(collect_metrics "$work")
  echo "$name|$latency_ms|$metrics" >>"$metrics_tmp"
done

# ---- EvalRun JSON (schema v2, DEBT-033 A1) ----
python3 - "$RUN_ID" "$PROFILE" "$LUMEN_BIN" "$results_tmp" "$metrics_tmp" "$PASS" "$FAIL" "$TOTAL" "$EVAL_DIR" "$ART" <<'PY'
import json, sys, os
from datetime import datetime, timezone
run_id, profile, binary, rows_path, metrics_path = sys.argv[1:6]
pass_n, fail_n, total = map(int, sys.argv[6:9])
eval_dir, art_dir = sys.argv[9], sys.argv[10]

rows = []
for line in open(rows_path).read().splitlines():
    parts = line.split("|")
    if len(parts) >= 2:
        rows.append({"task": parts[0], "result": parts[1],
                     "agent_ec": int(parts[2]) if len(parts) > 2 and parts[2].isdigit() else parts[2]})

metrics_by_task = {}
for line in open(metrics_path).read().splitlines():
    p = line.split("|")
    if len(p) >= 7:
        metrics_by_task[p[0]] = {
            "latency_ms": int(p[1]),
            "tool_calls": int(p[2]),
            "input_bytes": int(p[3]),
            "cache_epochs": int(p[4]),
            "hit_tokens": int(p[5]),
            "miss_tokens": int(p[6]),
            "output_tokens": int(p[7]),
        }
for r in rows:
    r.update(metrics_by_task.get(r["task"], {"latency_ms": None, "tool_calls": None,
                                              "input_bytes": None, "cache_epochs": None,
                                              "hit_tokens": None, "miss_tokens": None,
                                              "output_tokens": None}))

lat = [r["latency_ms"] for r in rows if r.get("latency_ms") is not None]
tc = [r["tool_calls"] for r in rows if r.get("tool_calls") is not None]
ib = [r["input_bytes"] for r in rows if r.get("input_bytes") is not None]
ht = [r["hit_tokens"] for r in rows if r.get("hit_tokens") is not None]
mt = [r["miss_tokens"] for r in rows if r.get("miss_tokens") is not None]
ot = [r["output_tokens"] for r in rows if r.get("output_tokens") is not None]
# cache_health.jsonl is written by the runtime (DEBT-033 A2-a); present only
# when the binary under eval includes that wiring.
hit_sum, miss_sum, out_sum = sum(ht), sum(mt), sum(ot)
has_health = bool(ht)

aggregate = {
    "pass_rate": round(pass_n / total, 4) if total else None,
    "pass_count": pass_n,
    "fail_count": fail_n,
    "total": total,
    "total_input_bytes": sum(ib) if ib else None,
    "total_input_tokens": (hit_sum + miss_sum) if has_health else None,
    "total_output_tokens": out_sum if has_health else None,
    "avg_cache_hit_ratio": round(hit_sum / (hit_sum + miss_sum), 4)
        if has_health and (hit_sum + miss_sum) > 0 else None,
    "avg_verify_count": None,     # verify counting lands with Cycle B (④)
    "avg_tool_calls": round(sum(tc) / len(tc), 2) if tc else None,
    "avg_latency_ms": round(sum(lat) / len(lat), 1) if lat else None,
}

art = {
    "schema_version": 2,
    "check_id": "eval_run",
    "run_id": run_id,
    "profile": profile,
    "binary": binary,
    "generated_at": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
    "aggregate": aggregate,
    "tasks": rows,
    "notes": [
        "total_input_tokens/total_output_tokens/avg_cache_hit_ratio real only when runtime writes cache_health.jsonl (DEBT-033 A2-a wiring present in the evaluated binary)",
        "avg_verify_count null until Cycle B (④ verification-obligation loop)",
        "total_input_bytes = sum of cache_request_evidence body_bytes (input proxy)",
    ],
}
out1 = os.path.join(eval_dir, f"eval-run-{run_id}.json")
out2 = os.path.join(art_dir, "eval-run-latest.json")
for out in (out1, out2):
    os.makedirs(os.path.dirname(out), exist_ok=True)
    with open(out, "w") as f:
        f.write(json.dumps(art, indent=2) + "\n")
print(f"wrote {out1}")
print(f"wrote {out2} aggregate={json.dumps(aggregate)}")
PY

# ---- Gate file (schema v1) unless EVAL_RUN_ONLY ----
if [[ "$RUN_ONLY" == "1" ]]; then
  echo "EVAL_RUN_ONLY=1: gate file skipped (run_id=$RUN_ID)"
  exit 0
fi

python3 - "$ART/eval-live.json" "$results_tmp" "$PASS" "$FAIL" "$TOTAL" "$MIN_PASS" "$LUMEN_BIN" <<'PY'
import json, sys
from datetime import datetime, timezone
from pathlib import Path
out, rows_path, pass_n, fail_n, total, mn, binary = sys.argv[1:8]
pass_n, fail_n, total, mn = map(int, (pass_n, fail_n, total, mn))
rows = []
for line in Path(rows_path).read_text().splitlines():
    parts = line.split("|")
    if len(parts) >= 2:
        rows.append({"task": parts[0], "result": parts[1], "agent_ec": int(parts[2]) if len(parts)>2 and parts[2].isdigit() else parts[2]})
ok = pass_n >= mn and fail_n == (total - pass_n)  # no silent pass inflation
art = {
  "schema_version": 1,
  "check_id": "eval_live",
  "pass": pass_n >= mn,
  "pass_count": pass_n,
  "fail_count": fail_n,
  "total": total,
  "min_required": mn,
  "silent_corruption": 0,
  "tasks": rows,
  "binary": binary,
  "generated_at": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
  "note": "Live agent solves; PASS only when deterministic workspace tests pass after agent edit.",
}
Path(out).write_text(json.dumps(art, indent=2) + "\n")
print(f"wrote {out} pass_count={pass_n}/{total} gate_pass={art['pass']}")
sys.exit(0 if art["pass"] else 1)
PY
