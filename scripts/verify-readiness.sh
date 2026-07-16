#!/usr/bin/env bash
# FINAL-2.0 readiness aggregator — honest blockers.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export PATH="/opt/homebrew/bin:$HOME/.cargo/bin:$HOME/.local/bin:$PATH"
export PROTOC="${PROTOC:-/opt/homebrew/bin/protoc}"
ART="$ROOT/artifacts/readiness"
mkdir -p "$ART"
TMP="$(mktemp)"
trap 'rm -f "$TMP" "$TMP.checks"' EXIT

echo "=== Lumen verify-readiness (FINAL-2.0) ==="

record() {
  local id="$1" result="$2" detail="${3:-}"
  echo "$id|$result|$detail" >>"$TMP"
  if [[ "$result" == "PASS" ]]; then
    echo "  PASS $id"
  elif [[ "$result" == "SKIP" ]]; then
    echo "  SKIP $id — $detail"
  else
    echo "  FAIL $id — $detail"
  fi
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
else
  record L0_connect SKIP "no DEEPSEEK_API_KEY"
  record L1_tool_calls SKIP "no DEEPSEEK_API_KEY"
fi

if grep -Rql --include='*.rs' 'tool_calls' "$ROOT/agent/crates/codegen/xai-grok-shell/src/session" \
  && grep -Rql --include='*.rs' 'persistence' "$ROOT/agent/crates/codegen/xai-grok-shell/src/session"; then
  record R_struct_session PASS "structural only; full R0 pending S2"
else
  record R_struct_session FAIL "session markers missing"
fi

if [[ -f "$ROOT/SOURCE_LOCK.json" ]]; then
  record source_lock PASS
else
  record source_lock FAIL "run scripts/source-lock.sh"
fi

python3 - "$TMP" "$ART/status.json" <<'PY2'
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
# FINAL-2.0: ready=true only when L0-L5 all pass. Current script only auto-runs L0/L1.
can_tool = any(c["id"] == "L1_tool_calls" and c["pass"] for c in checks)
l0 = any(c["id"] == "L0_connect" and c["pass"] for c in checks)
for missing in ("L2_min_e2e", "L3_multi_tool", "L4_fault_cancel", "L5_long_session", "R0_full_suite"):
    if not any(c["id"] == missing and c["pass"] for c in checks):
        blockers.append(f"{missing}:not_signed")
# de-dupe
seen=set(); blockers=[b for b in blockers if not (b in seen or seen.add(b))]
ready = len(blockers) == 0
status = {
    "schema_version": 1,
    "generated_at": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
    "ready": ready,
    "state": "READY" if ready else "BLOCKED",
    "can_tool_call": can_tool,
    "l0_pass": l0,
    "blockers": blockers,
    "checks": checks,
    "note": "can_tool_call=L1 only. ready=true requires L0-L5 + R0 (FINAL-2.0).",
}
Path(sys.argv[2]).write_text(json.dumps(status, indent=2) + "\n")
print()
print(f"state={status['state']} ready={status['ready']} can_tool_call={status['can_tool_call']}")
print("blockers:", blockers if blockers else "[]")
print("wrote", sys.argv[2])
# exit 0 always after writing status — ready flag is the truth
PY2
