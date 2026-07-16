#!/usr/bin/env bash
# FINAL-2.0 readiness aggregator — honest blockers + engineering_complete.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export PATH="/opt/homebrew/bin:$HOME/.cargo/bin:$HOME/.local/bin:$PATH"
export PROTOC="${PROTOC:-/opt/homebrew/bin/protoc}"
ART="$ROOT/artifacts/readiness"
mkdir -p "$ART"
TMP="$(mktemp)"
trap 'rm -f "$TMP"' EXIT

echo "=== Lumen verify-readiness (FINAL-2.0) ==="

record() {
  local id="$1" result="$2" detail="${3:-}"
  echo "$id|$result|$detail" >>"$TMP"
  case "$result" in
    PASS) echo "  PASS $id${detail:+ — $detail}" ;;
    SKIP) echo "  SKIP $id — $detail" ;;
    *) echo "  FAIL $id — $detail" ;;
  esac
}

run_script() {
  local id="$1" script="$2"
  set +e
  out=$("$script" 2>&1)
  ec=$?
  set -e
  if [[ $ec -eq 0 ]]; then
    record "$id" PASS
  elif [[ $ec -eq 2 ]]; then
    record "$id" SKIP "exit 2"
  else
    record "$id" FAIL "exit $ec"
  fi
}

run_script defaults "$ROOT/scripts/assert-defaults.sh"
run_script security "$ROOT/scripts/smoke-security.sh"
run_script m2 "$ROOT/scripts/smoke-m2.sh"
run_script parity "$ROOT/scripts/parity-run.sh"
run_script eval_harness "$ROOT/scripts/eval-coding.sh"
run_script verify_cli "$ROOT/scripts/smoke-verify.sh"
run_script verticals "$ROOT/scripts/doctor-verticals.sh"

if [[ -n "${DEEPSEEK_API_KEY:-}" ]]; then
  run_script L0_connect "$ROOT/scripts/smoke-deepseek.sh"
  run_script L1_tool_calls "$ROOT/scripts/smoke-deepseek-agent.sh"
  run_script L2_min_e2e "$ROOT/scripts/smoke-deepseek-l2.sh"
  run_script L3_multi_tool "$ROOT/scripts/smoke-deepseek-l3.sh"
  run_script L4_fault_cancel "$ROOT/scripts/smoke-deepseek-l4.sh"
  run_script L5_long_session "$ROOT/scripts/smoke-deepseek-l5.sh"
else
  record L0_connect SKIP "no DEEPSEEK_API_KEY"
  record L1_tool_calls SKIP "no DEEPSEEK_API_KEY"
  record L2_min_e2e SKIP "no DEEPSEEK_API_KEY"
  record L3_multi_tool SKIP "no DEEPSEEK_API_KEY"
  record L4_fault_cancel SKIP "no DEEPSEEK_API_KEY"
  record L5_long_session SKIP "no DEEPSEEK_API_KEY"
fi

run_script R0_min "$ROOT/scripts/smoke-r0-min.sh"

if [[ -f "$ROOT/SOURCE_LOCK.json" ]]; then
  record source_lock PASS
else
  record source_lock FAIL "run scripts/source-lock.sh"
fi

# Human productivity gate (must fail until ≥15 real journals)
set +e
pg_out=$("$ROOT/scripts/productivity-gate.sh" 2>&1)
pg_ec=$?
set -e
echo "$pg_out" | sed 's/^/  | /'
if [[ $pg_ec -eq 0 ]]; then
  record M6_15_day_self_use PASS
else
  record M6_15_day_self_use FAIL "human_gate count_lt_15"
fi

python3 - "$TMP" "$ART/status.json" "$ART/engineering_complete.json" <<'PY2'
import json, sys
from datetime import datetime, timezone
from pathlib import Path

rows = Path(sys.argv[1]).read_text().strip().splitlines()
checks, blockers = [], []
for line in rows:
    parts = line.split("|", 2)
    if len(parts) < 2:
        continue
    cid, result = parts[0], parts[1]
    detail = parts[2] if len(parts) > 2 else ""
    ok = result == "PASS"
    checks.append({"id": cid, "pass": ok, "result": result, "detail": detail})
    if result != "PASS":
        blockers.append(f"{cid}:{detail or result}")

can_tool = any(c["id"] == "L1_tool_calls" and c["pass"] for c in checks)
l0 = any(c["id"] == "L0_connect" and c["pass"] for c in checks)

# Engineering complete = all automated gates pass; only human M6 may remain
auto_ids = {
    "defaults", "security", "m2", "parity", "eval_harness", "verify_cli",
    "verticals", "L0_connect", "L1_tool_calls", "L2_min_e2e", "L3_multi_tool",
    "L4_fault_cancel", "L5_long_session", "R0_min", "source_lock",
}
auto_blockers = [b for b in blockers if not b.startswith("M6_15_day_self_use")]
eng_ok = len(auto_blockers) == 0 and all(
    any(c["id"] == i and c["pass"] for c in checks) for i in auto_ids
)

ready = len(blockers) == 0  # publish READY requires productivity gate too
status = {
    "schema_version": 1,
    "generated_at": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
    "ready": ready,
    "state": "READY" if ready else "BLOCKED",
    "can_tool_call": can_tool,
    "l0_pass": l0,
    "engineering_complete": eng_ok,
    "blockers": blockers,
    "checks": checks,
    "note": "ready=true only when ALL gates pass including 15 productivity days. engineering_complete=true when only M6 human gate remains.",
}
Path(sys.argv[2]).write_text(json.dumps(status, indent=2) + "\n")

eng = {
    "schema_version": 1,
    "check_id": "engineering_complete",
    "pass": eng_ok,
    "meaning": "All automatable FINAL-2.0 gates pass; publish ready still requires M6_15_day_self_use",
    "auto_blockers": auto_blockers,
    "can_tool_call": can_tool,
    "generated_at": status["generated_at"],
}
Path(sys.argv[3]).write_text(json.dumps(eng, indent=2) + "\n")

print()
print(f"state={status['state']} ready={status['ready']} can_tool_call={status['can_tool_call']} engineering_complete={eng_ok}")
print("blockers:", blockers if blockers else "[]")
print("wrote", sys.argv[2])
print("wrote", sys.argv[3], "pass=", eng_ok)
PY2
