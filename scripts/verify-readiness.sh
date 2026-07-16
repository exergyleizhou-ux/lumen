#!/usr/bin/env bash
# FINAL-2.0 readiness aggregator — honest blockers.
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
else
  record L0_connect SKIP "no DEEPSEEK_API_KEY"
  record L1_tool_calls SKIP "no DEEPSEEK_API_KEY"
  record L2_min_e2e SKIP "no DEEPSEEK_API_KEY"
  record L3_multi_tool SKIP "no DEEPSEEK_API_KEY"
fi

run_script R0_min "$ROOT/scripts/smoke-r0-min.sh"

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

can_tool = any(c["id"] == "L1_tool_calls" and c["pass"] for c in checks)
l0 = any(c["id"] == "L0_connect" and c["pass"] for c in checks)

# Residual FINAL-2.0 levels not fully automated as L4/L5 chaos/long-session
for missing in ("L4_fault_cancel", "L5_long_session"):
    if not any(c["id"] == missing and c["pass"] for c in checks):
        blockers.append(f"{missing}:not_signed")

# R0_full_suite superseded by R0_min when R0_min passes
if any(c["id"] == "R0_min" and c["pass"] for c in checks):
    blockers = [b for b in blockers if not b.startswith("R0_full_suite")]
else:
    if not any(b.startswith("R0") for b in blockers):
        blockers.append("R0_min:not_signed")

seen = set()
blockers = [b for b in blockers if not (b in seen or seen.add(b))]
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
    "note": "ready=true only if no blockers. L4/L5 remain explicit residual unless signed. R0_min is process kill/idempotency via xai-tty-utils, not full R0 matrix.",
}
Path(sys.argv[2]).write_text(json.dumps(status, indent=2) + "\n")
print()
print(f"state={status['state']} ready={status['ready']} can_tool_call={status['can_tool_call']}")
print("blockers:", blockers if blockers else "[]")
print("wrote", sys.argv[2])
PY2
