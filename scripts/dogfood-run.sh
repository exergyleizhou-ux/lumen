#!/usr/bin/env bash
# dogfood-run.sh — run the dogfood regression set and record what actually
# happened, not how it felt.
#
# Why this exists: on 2026-07-26 six manual dogfood runs found two real
# defects (multi-language verify gated out, and the tool registry never
# receiving a workspace root) that 13,732 unit tests could not see, because
# no unit test runs the product. Manual dogfood is not repeatable, so it
# cannot be a gate. This makes it one.
#
# What it measures per task, beyond pass/fail:
#   - turns              : how many model turns it took
#   - wall_seconds       : end-to-end latency
#   - auto_verify_fired  : did edit->verify actually reach the model
#   - discipline_fired   : storm-breaker / repeat-success / delivery reminders
# A task that "passes" while auto-verify never fired is a silent regression
# of the core promise, which is exactly the class of bug that motivated this.
#
# Usage:
#   scripts/dogfood-run.sh                 # all tasks, writes artifacts/dogfood/latest.json
#   scripts/dogfood-run.sh d01-py-boundary # one task
#   DOGFOOD_BASELINE=1 scripts/dogfood-run.sh   # also write baseline.json
#
# Requires DEEPSEEK_API_KEY (or set LUMEN_DOGFOOD_MODEL to a local model).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export PATH="$HOME/.local/bin:$HOME/sdk/node/bin:$HOME/go/bin:$HOME/.cargo/bin:/opt/homebrew/bin:$PATH"

TASKS_DIR="$ROOT/evals/dogfood"
OUT_DIR="$ROOT/artifacts/dogfood"
LUMEN_BIN="${LUMEN_BIN:-$ROOT/agent/target/release/lumen}"
MODEL="${LUMEN_DOGFOOD_MODEL:-deepseek-v4-pro}"
MAX_TURNS="${LUMEN_DOGFOOD_MAX_TURNS:-15}"
TASK_TIMEOUT="${LUMEN_DOGFOOD_TIMEOUT:-300}"

[[ -x "$LUMEN_BIN" ]] || { echo "FAIL: no lumen binary at $LUMEN_BIN (build with scripts/install-local.sh)" >&2; exit 1; }
[[ -n "${DEEPSEEK_API_KEY:-}" ]] || { echo "FAIL: DEEPSEEK_API_KEY required" >&2; exit 1; }
mkdir -p "$OUT_DIR"

# Portable timeout: macOS has no coreutils timeout by default.
run_with_timeout() {
  local secs="$1"; shift
  "$@" &
  local pid=$!
  local waited=0
  while kill -0 "$pid" 2>/dev/null; do
    if [[ $waited -ge $secs ]]; then
      kill -9 "$pid" 2>/dev/null || true
      wait "$pid" 2>/dev/null || true
      return 124
    fi
    sleep 1
    waited=$((waited + 1))
  done
  wait "$pid"
}

selected=("$@")
if [[ ${#selected[@]} -eq 0 ]]; then
  while IFS= read -r d; do selected+=("$(basename "$d")"); done < <(find "$TASKS_DIR" -mindepth 1 -maxdepth 1 -type d | sort)
fi

# Probe: make a deliberately broken edit IN THE TASK'S OWN LANGUAGE and have
# the model echo the tool result verbatim — the only honest way to observe
# model-visible feedback (it never reaches stdout/stderr). A single .py probe
# would only ever exercise the Python path, reporting false "no verification"
# for Go/Rust/JS projects.
probe_prompt_for_lang() {
  case "$1" in
    python) echo '在工作区新建 _probe_verify.py，内容正好一行：return 1 （故意的语法错误，用于探测工具链）。然后逐字复述你收到的写入工具的完整返回内容。不要运行任何命令，不要修正这个错误。' ;;
    go)     echo '把工作区里已有的某个 .go 文件中任意一个函数体改成 return 1 +++ 2 （故意的语法错误，用于探测工具链）。然后逐字复述你收到的写入工具的完整返回内容。不要运行任何命令，不要修正这个错误。' ;;
    typescript) echo '把工作区里 index.js 的第一行改成 export const _probe = ((( （故意的语法错误，用于探测工具链）。然后逐字复述你收到的写入工具的完整返回内容。不要运行任何命令，不要修正这个错误。' ;;
    *)      echo '' ;;
  esac
}

results_file="$(mktemp)"
trap 'rm -f "$results_file"' EXIT

echo "=== dogfood: ${#selected[@]} task(s), model=$MODEL, binary=$(basename "$LUMEN_BIN") ==="

for task in "${selected[@]}"; do
  task_dir="$TASKS_DIR/$task"
  [[ -d "$task_dir/workspace" ]] || { echo "SKIP $task (no workspace)"; continue; }

  prompt="$(cat "$task_dir/prompt.txt")"
  test_cmd="$(python3 -c "import json,sys;print(json.load(open('$task_dir/meta.json'))['test_cmd'])")"
  expect_verify="$(python3 -c "import json;print(json.load(open('$task_dir/meta.json')).get('expect_auto_verify',False))")"

  scratch="$(mktemp -d "${TMPDIR:-/tmp}/dogfood-$task.XXXXXX")"
  cp -R "$task_dir/workspace" "$scratch/ws"
  home="$scratch/home"; mkdir -p "$home"
  log="$scratch/agent.log"

  started=$(date +%s)
  set +e
  HOME="$home" GROK_HOME="$home/grok" LUMEN_HOME="$home/lumen" \
  run_with_timeout "$TASK_TIMEOUT" "$LUMEN_BIN" \
      --cwd "$scratch/ws" \
      -m "$MODEL" \
      --always-approve \
      --permission-mode bypassPermissions \
      --max-turns "$MAX_TURNS" \
      --output-format plain \
      -p "$prompt" >"$log" 2>&1
  agent_ec=$?
  set -e
  elapsed=$(( $(date +%s) - started ))

  # Did the task actually get fixed?
  set +e
  (cd "$scratch/ws" && eval "$test_cmd") >"$scratch/test.log" 2>&1
  test_ec=$?
  set -e

  # Behavioural signals.
  #
  # CRITICAL: verifier feedback and discipline reminders ride the TOOL RESULT
  # into the model's context. They are NOT printed to stdout/stderr, so
  # grepping the agent log finds nothing even when both are working — I made
  # exactly that mistake twice today, once concluding a healthy feature was
  # dead. The honest probe is to ask the model to echo what it received, so
  # each task run appends a verification-echo turn and we inspect ITS output.
  verify_fired=0
  discipline_fired=0
  lang="$(python3 -c "import json;print(json.load(open('$task_dir/meta.json'))['lang'])")"
  probe_prompt="$(probe_prompt_for_lang "$lang")"
  if [[ "$expect_verify" == "True" && -n "$probe_prompt" ]]; then
    echo_log="$scratch/echo.log"
    set +e
    HOME="$home" GROK_HOME="$home/grok" LUMEN_HOME="$home/lumen" \
    run_with_timeout 120 "$LUMEN_BIN" \
        --cwd "$scratch/ws" \
        -m "$MODEL" \
        --always-approve \
        --permission-mode bypassPermissions \
        --max-turns 4 \
        --output-format plain \
        -p "$probe_prompt" >"$echo_log" 2>&1
    set -e
    verify_fired=$(grep -cE "verify-after-edit|invalid-syntax|ruff|pytest|go build|go vet|tsc" "$echo_log" 2>/dev/null || true)
    discipline_fired=$(grep -cE "storm-breaker|repeat-success|delivery-reminder" "$echo_log" 2>/dev/null || true)
  fi

  status="FAIL"
  [[ $test_ec -eq 0 ]] && status="PASS"
  [[ $agent_ec -eq 124 ]] && status="TIMEOUT"

  printf '%s|%s|%s|%s|%s|%s|%s\n' \
    "$task" "$status" "$elapsed" "$verify_fired" "$discipline_fired" "$expect_verify" "$agent_ec" \
    >>"$results_file"

  printf '  %-22s %-7s %3ds  verify=%-3s discipline=%-3s\n' \
    "$task" "$status" "$elapsed" "$verify_fired" "$discipline_fired"

  # Keep failures for inspection; drop the rest so /tmp does not fill up.
  if [[ "$status" == "PASS" ]]; then rm -rf "$scratch"; else echo "    kept: $scratch"; fi
done

python3 - "$results_file" "$OUT_DIR" "${DOGFOOD_BASELINE:-0}" "$MODEL" <<'PY'
import json, sys
from datetime import datetime, timezone
from pathlib import Path

rows = [l.split("|") for l in Path(sys.argv[1]).read_text().strip().splitlines() if l.strip()]
out_dir, want_baseline, model = Path(sys.argv[2]), sys.argv[3] == "1", sys.argv[4]

tasks = []
for task, status, secs, verify, discipline, expect_verify, agent_ec in rows:
    tasks.append({
        "id": task,
        "status": status,
        "wall_seconds": int(secs),
        "auto_verify_fired": int(verify) > 0,
        "discipline_fired": int(discipline) > 0,
        "expected_auto_verify": expect_verify == "True",
        "agent_exit": int(agent_ec),
    })

passed = sum(1 for t in tasks if t["status"] == "PASS")
# A task that passes without the verifier ever reaching the model is a silent
# regression of a core promise, even though its tests are green.
silent = [t["id"] for t in tasks
          if t["expected_auto_verify"] and t["status"] == "PASS" and not t["auto_verify_fired"]]

report = {
    "schema_version": 1,
    "generated_at": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
    "model": model,
    "total": len(tasks),
    "passed": passed,
    "pass_rate": round(passed / len(tasks), 3) if tasks else 0.0,
    "median_wall_seconds": sorted(t["wall_seconds"] for t in tasks)[len(tasks)//2] if tasks else 0,
    "auto_verify_coverage": round(
        sum(1 for t in tasks if t["expected_auto_verify"] and t["auto_verify_fired"])
        / max(1, sum(1 for t in tasks if t["expected_auto_verify"])), 3),
    "silent_verify_regressions": silent,
    "tasks": tasks,
}

(out_dir / "latest.json").write_text(json.dumps(report, indent=2) + "\n")
if want_baseline:
    (out_dir / "baseline.json").write_text(json.dumps(report, indent=2) + "\n")

print()
print(f"pass {passed}/{len(tasks)}  rate={report['pass_rate']}  "
      f"median={report['median_wall_seconds']}s  "
      f"auto_verify_coverage={report['auto_verify_coverage']}")
if silent:
    print(f"SILENT VERIFY REGRESSION in: {', '.join(silent)}")
print(f"wrote {out_dir / 'latest.json'}" + ("  (+ baseline.json)" if want_baseline else ""))

# Compare against the baseline when one exists: >20% degradation is a failure.
base_path = out_dir / "baseline.json"
if base_path.is_file() and not want_baseline:
    base = json.loads(base_path.read_text())
    drop = base.get("pass_rate", 0) - report["pass_rate"]
    if drop > 0.2:
        print(f"FAIL: pass rate dropped {drop:.1%} vs baseline")
        raise SystemExit(1)
    cov_drop = base.get("auto_verify_coverage", 0) - report["auto_verify_coverage"]
    if cov_drop > 0.2:
        print(f"FAIL: auto-verify coverage dropped {cov_drop:.1%} vs baseline")
        raise SystemExit(1)
raise SystemExit(0 if not silent else 1)
PY
