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
    # keep last line of detail short
    detail=$(echo "$out" | tail -3 | tr '\n' ' ' | head -c 200)
    record "$id" FAIL "exit $ec ${detail}"
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

# R0: full contract smoke (writes R0-full.json + updates R0-min.json)
if [[ -x "$ROOT/scripts/smoke-r0.sh" ]]; then
  set +e
  r0_out=$("$ROOT/scripts/smoke-r0.sh" 2>&1)
  r0_ec=$?
  set -e
  if [[ $r0_ec -eq 0 ]]; then
    record R0_full PASS
    record R0_min PASS "via R0_full"
  else
    echo "$r0_out" | tail -20 | sed 's/^/  | /'
    record R0_full FAIL "exit $r0_ec"
    run_script R0_min "$ROOT/scripts/smoke-r0-min.sh"
  fi
else
  run_script R0_min "$ROOT/scripts/smoke-r0-min.sh"
fi

# SBOM + LEGAL package
if [[ -x "$ROOT/scripts/generate-sbom.sh" ]]; then
  run_script sbom "$ROOT/scripts/generate-sbom.sh"
else
  if [[ -f "$ROOT/SBOM.spdx.json" ]]; then
    record sbom PASS "present"
  else
    record sbom FAIL "missing SBOM.spdx.json"
  fi
fi
if [[ -f "$ROOT/LEGAL.md" ]] && [[ $(wc -c <"$ROOT/LEGAL.md") -gt 200 ]]; then
  record legal PASS
else
  record legal FAIL "LEGAL.md missing or too short"
fi

# Live eval (optional unless EVAL_LIVE=1 or prior artifact pass)
if [[ "${EVAL_LIVE:-0}" == "1" && -n "${DEEPSEEK_API_KEY:-}" ]]; then
  run_script eval_live "$ROOT/scripts/eval-coding-live.sh"
elif [[ -f "$ART/eval-live.json" ]]; then
  # honor prior signed live run
  if python3 -c 'import json,sys; d=json.load(open(sys.argv[1])); sys.exit(0 if d.get("pass") else 1)' "$ART/eval-live.json"; then
    record eval_live PASS "prior artifact"
  else
    record eval_live FAIL "prior artifact pass=false"
  fi
else
  record eval_live SKIP "set EVAL_LIVE=1 to run live coding eval (≥18/20)"
fi

# Reconcile evidence (source lock + binary sha + R7)
if [[ -x "$ROOT/scripts/reconcile-evidence.sh" ]]; then
  run_script reconcile "$ROOT/scripts/reconcile-evidence.sh"
else
  record reconcile FAIL "missing reconcile-evidence.sh"
fi

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
import json, sys, hashlib
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
    if result == "FAIL":
        blockers.append(f"{cid}:{detail or result}")
    # SKIP is not a hard blocker for engineering_complete unless required

can_tool = any(c["id"] == "L1_tool_calls" and c["pass"] for c in checks)
l0 = any(c["id"] == "L0_connect" and c["pass"] for c in checks)

# Engineering complete = all automated required gates pass; only human M6 may remain.
# eval_live SKIP is allowed for eng_complete if not run; FAIL blocks.
auto_required = {
    "defaults", "security", "m2", "parity", "eval_harness", "verify_cli",
    "verticals", "L0_connect", "L1_tool_calls", "L2_min_e2e", "L3_multi_tool",
    "L4_fault_cancel", "L5_long_session", "R0_min", "source_lock",
    "sbom", "legal", "reconcile",
}
# R0_full preferred; if present must pass
if any(c["id"] == "R0_full" for c in checks):
    auto_required.add("R0_full")

def check_ok(i):
    for c in checks:
        if c["id"] == i:
            return c["result"] == "PASS"
    return False

auto_blockers = [b for b in blockers if not b.startswith("M6_15_day_self_use")]
# also block eng if required missing
for i in auto_required:
    if not check_ok(i):
        # find if SKIP
        st = next((c["result"] for c in checks if c["id"] == i), "MISSING")
        if st != "PASS":
            tag = f"{i}:{st}"
            if tag not in auto_blockers and not any(b.startswith(i + ":") for b in auto_blockers):
                auto_blockers.append(tag)

eng_ok = len(auto_blockers) == 0

# Publish readiness is stricter than engineering_complete: every automated
# requirement, the live eval, and the human M6 gate must be PASS in this
# aggregate. A SKIP may explain an unconfigured developer machine, but it must
# never become READY merely because it is not a FAIL.
publish_blockers = list(blockers)
for required_id in sorted(auto_required | {"eval_live", "M6_15_day_self_use"}):
    if check_ok(required_id):
        continue
    if any(b.startswith(required_id + ":") for b in publish_blockers):
        continue
    state = next((c["result"] for c in checks if c["id"] == required_id), "MISSING")
    publish_blockers.append(f"{required_id}:{state}")
ready = len(publish_blockers) == 0

# hashes
root = Path(sys.argv[2]).resolve().parent.parent  # artifacts/readiness -> repo? 
# status path is ART/status.json; root is ART.parent.parent if ART=repo/artifacts/readiness
art_dir = Path(sys.argv[2]).resolve().parent
repo = art_dir.parent.parent if art_dir.name == "readiness" else art_dir.parent
lock_p = repo / "SOURCE_LOCK.json"
lock_sha = hashlib.sha256(lock_p.read_bytes()).hexdigest() if lock_p.is_file() else None
reconcile_p = art_dir / "reconcile.json"
reconcile = json.loads(reconcile_p.read_text()) if reconcile_p.is_file() else {}
bin_sha = None
for cand in [repo / "agent" / "target" / "release" / "lumen", Path.home() / ".local" / "bin" / "lumen"]:
    if cand.is_file():
        bin_sha = hashlib.sha256(cand.read_bytes()).hexdigest()
        break

status = {
    "schema_version": 1,
    "generated_at": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
    "ready": ready,
    "state": "READY" if ready else "BLOCKED",
    "can_tool_call": can_tool,
    "l0_pass": l0,
    "engineering_complete": eng_ok,
    "source_lock_sha256": lock_sha,
    "binary_sha256": bin_sha,
    "blockers": publish_blockers,
    "checks": checks,
    "reconcile_pass": bool(reconcile.get("pass")),
    "reconciled_at": reconcile.get("generated_at"),
    "note": "ready=true only when every publish gate is PASS, including live eval and 15 productivity days. SKIP never counts as publish-ready. engineering_complete=true when required automated gates pass.",
}
Path(sys.argv[2]).write_text(json.dumps(status, indent=2) + "\n")

eng = {
    "schema_version": 1,
    "check_id": "engineering_complete",
    "pass": eng_ok,
    "meaning": "All automatable FINAL-2.0 gates pass; publish ready still requires M6_15_day_self_use",
    "auto_blockers": auto_blockers,
    "can_tool_call": can_tool,
    "source_lock_sha256": lock_sha,
    "binary_sha256": bin_sha,
    "generated_at": status["generated_at"],
}
Path(sys.argv[3]).write_text(json.dumps(eng, indent=2) + "\n")

print()
print(f"state={status['state']} ready={status['ready']} can_tool_call={status['can_tool_call']} engineering_complete={eng_ok}")
print("blockers:", publish_blockers if publish_blockers else "[]")
print("auto_blockers:", auto_blockers if auto_blockers else "[]")
print("wrote", sys.argv[2])
print("wrote", sys.argv[3], "pass=", eng_ok)
# exit 0 always so artifacts are the source of truth (same as prior design)
sys.exit(0)
PY2
