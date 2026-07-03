#!/usr/bin/env bash
# goal:d6aa846b round9 — verification manifest (plan.md steps 1–2)
# Run from repo root: bash scripts/goal-d6aa846b-verify.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCRATCH="${SCRATCH:-/var/folders/dn/_prdhdnn5l53lb71bhtx_n5w0000gn/T/grok-goal-d6aa846b7e98/implementer}"
BASE="${BASE:-https://demo.oasisdata2026.xyz}"
mkdir -p "$SCRATCH"

echo "▸ step 1: go test"
cd "$ROOT"
GOTOOLCHAIN=local go test -count=1 -v ./internal/server/... 2>&1 | tee "$SCRATCH/go-test-workflow.log"
echo "EXIT:$?" | tee -a "$SCRATCH/go-test-workflow.log"
GOTOOLCHAIN=local go test -count=1 ./internal/server/... ./internal/control/... 2>&1 | tee "$SCRATCH/go-test.log"
echo "EXIT:$?" | tee -a "$SCRATCH/go-test.log"

echo "▸ step 2: API smoke (curl)"
python3 << PY
import json, subprocess, re
BASE = "$BASE"
results = {"base": BASE, "via": "curl", "round": 9}

def curl(method, path, data=None):
    cmd = ["curl", "-s", "-w", "\n__HTTP__%{http_code}", "-X", method, BASE+path, "-H", "Content-Type: application/json"]
    if data is not None:
        cmd += ["-d", json.dumps(data)]
    out = subprocess.check_output(cmd, text=True)
    m = re.search(r"__HTTP__(\d+)$", out, re.M)
    status = int(m.group(1)) if m else 0
    body = out[:m.start()].strip() if m else out.strip()
    try:
        parsed = json.loads(body)
    except Exception:
        parsed = body
    return status, parsed

for k, d in [
    ("command_workflow_demo", {"command": "/workflow test task"}),
    ("command_workflow_bad_key", {"command": "/workflow auth fail", "api_key": "sk-bad", "provider": "deepseek"}),
    ("command_execute", {"command": "/execute"}),
    ("command_reject", {"command": "/reject"}),
    ("command_ultra", {"command": "/ultra test"}),
    ("command_goal", {"command": "/goal test"}),
    ("command_help", {"command": "/help"}),
]:
    st, body = curl("POST", "/lumen/v1/command", d)
    results[k] = {"status": st, "body": body}

st, body = curl("GET", "/lumen/v1/skills")
results["skills"] = {"status": st, "count": len(body.get("skills", [])) if isinstance(body, dict) else 0}
st, body = curl("POST", "/lumen/v1/memories", {"action": "save", "entry": {"name": "smoke-test", "content": "r9"}})
results["memories_save"] = {"status": st, "body": body}
st, body = curl("POST", "/lumen/v1/memories", {"action": "delete", "name": "smoke-test"})
results["memories_delete"] = {"status": st, "body": body}
st, body = curl("PUT", "/lumen/v1/mode", {"mode": "bypass"})
results["mode_bypass"] = {"status": st, "body": body}
st, body = curl("GET", "/lumen/v1/sessions")
if isinstance(body, dict) and body.get("sessions"):
    sname = body["sessions"][0]["name"]
    st2, body2 = curl("POST", "/lumen/v1/sessions/resume", {"name": sname})
    results["sessions_resume"] = {"status": st2, "body": body2}
for p in ["/workspace/lumen", "/workspace/lumen-science"]:
    out = subprocess.check_output(["curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", BASE+p], text=True)
    results["http"+p.replace("/","_")] = int(out)
for label, payload in [
    ("sse_demo_no_key", '{"action":"workflow","prompt":"demo"}'),
    ("sse_auth_fail", '{"action":"workflow","prompt":"fail","api_key":"sk-bad","provider":"deepseek"}'),
]:
    out = subprocess.check_output(["curl", "-sN", "-m", "20", "-X", "POST", BASE+"/lumen/v1/workflow", "-H", "Content-Type: application/json", "-d", payload], text=True)
    results[label] = {"has_plan_ready": "plan_ready" in out, "has_demo": "[Demo mode]" in out, "has_error": "error" in out.lower() or "authentication" in out.lower(), "snippet": out[:1200]}
with open("$SCRATCH/api-smoke.json", "w") as f:
    json.dump(results, f, indent=2, ensure_ascii=False)
print("api-smoke written")
PY

echo "▸ step 2b: workflow-sse-smoke.txt"
{
  echo "=== command bad key (expect 400) ==="
  curl -s -X POST "$BASE/lumen/v1/command" -H "Content-Type: application/json" \
    -d '{"command":"/workflow fail","api_key":"sk-bad","provider":"deepseek"}'
  echo ""
  echo "=== SSE demo ==="
  curl -sN -m 15 -X POST "$BASE/lumen/v1/workflow" -H "Content-Type: application/json" -d '{"action":"workflow","prompt":"demo"}' | head -10
  echo ""
  echo "=== SSE auth fail ==="
  curl -sN -m 15 -X POST "$BASE/lumen/v1/workflow" -H "Content-Type: application/json" -d '{"action":"workflow","prompt":"fail","api_key":"sk-bad","provider":"deepseek"}' | head -10
} > "$SCRATCH/workflow-sse-smoke.txt"

echo "▸ step 2c: command-plan-fail local curl e2e"
bash "$ROOT/scripts/goal-d6aa846b-command-plan-fail.sh" "$SCRATCH/command-plan-fail-curl.txt"

echo "✓ verification artifacts in $SCRATCH"