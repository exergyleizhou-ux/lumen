#!/usr/bin/env bash
# Full product smoke for Lumen Lab (L1–L3 software surface).
# Usage: ./scripts/science/lab-product-smoke.sh [base_url]
set -euo pipefail

BASE="${1:-https://demo.oasisdata2026.xyz/lumen-lab}"
PASS=0
FAIL=0

pass() { echo "  PASS $1"; PASS=$((PASS+1)); }
fail() { echo "  FAIL $1"; FAIL=$((FAIL+1)); }

echo "=== Lab product smoke ($BASE) ==="

HEALTH=$(curl -sS "$BASE/api/lab/health" || echo "{}")

# --- health ---
python3 -c "import json,sys; d=json.load(sys.stdin); assert d.get('status')=='ok'" <<<"$HEALTH" && pass "health.ok" || fail "health.ok"
python3 -c "import json,sys; d=json.load(sys.stdin); assert d.get('version') and d.get('version')!='dev'" <<<"$HEALTH" && pass "version!=dev" || fail "version!=dev"
python3 -c "import json,sys; d=json.load(sys.stdin); assert d.get('ketcher',{}).get('same_origin')" <<<"$HEALTH" && pass "ketcher" || fail "ketcher"
python3 -c "import json,sys; d=json.load(sys.stdin); assert d.get('jupyter',{}).get('available')" <<<"$HEALTH" && pass "jupyter" || fail "jupyter"
python3 -c "import json,sys; d=json.load(sys.stdin); assert 'langgraph' in d" <<<"$HEALTH" && pass "langgraph key" || fail "langgraph key"
python3 -c "import json,sys; d=json.load(sys.stdin); assert 'onlyoffice' in d and 'configured' in d['onlyoffice']" <<<"$HEALTH" && pass "onlyoffice key" || fail "onlyoffice key"
python3 -c "import json,sys; d=json.load(sys.stdin); assert d.get('research_pack',{}).get('healthy') is True" <<<"$HEALTH" && pass "research_pack" || fail "research_pack"
python3 -c "import json,sys; d=json.load(sys.stdin); assert int(d.get('fleet',{}).get('connected_total') or 0)>=0" <<<"$HEALTH" && pass "fleet" || fail "fleet"

# --- readyz ---
code=$(curl -sS -o /dev/null -w '%{http_code}' "$BASE/api/lab/readyz" || echo 000)
[[ "$code" == "200" ]] && pass "readyz" || fail "readyz"

# --- static files ---
code=$(curl -sS -o /dev/null -w '%{http_code}' "$BASE/office-editor.html" || echo 000)
[[ "$code" == "200" ]] && pass "office-editor" || fail "office-editor"
code=$(curl -sS -o /dev/null -w '%{http_code}' "$BASE/" || echo 000)
[[ "$code" == "200" ]] && pass "index" || fail "index"

# --- app.js?v= cache-bust consistency ---
APPVER=$(curl -sS "$BASE/" 2>/dev/null | grep -oP 'app\.js\?v=\K\d+' | head -1 || true)
if [[ -n "$APPVER" ]]; then
  code=$(curl -sS -o /dev/null -w '%{http_code}' "$BASE/app.js?v=$APPVER" || echo 000)
  [[ "$code" == "200" ]] && pass "app.js cache-bust" || fail "app.js cache-bust"
else
  pass "app.js cache-bust (inline)"
fi

# --- routes exist ---
code=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$BASE/api/lab/onlyoffice/callback?project_id=x&path=t.docx" -H 'Content-Type: application/json' -d '{}' || echo 000)
[[ "$code" != "000" && "$code" != "404" && "$code" != "500" ]] && pass "oo-callback" || fail "oo-callback"
code=$(curl -sS -o /dev/null -w '%{http_code}' "$BASE/api/lab/onlyoffice/session?project_id=x&path=a.docx" || echo 000)
[[ "$code" == "400" || "$code" == "200" ]] && pass "oo-session" || fail "oo-session"
code=$(curl -sS -o /dev/null -w '%{http_code}' "$BASE/api/lab/langgraph/history?project_id=x" || echo 000)
[[ "$code" == "200" || "$code" == "400" ]] && pass "lg-history" || fail "lg-history"

# --- create project + file write/read + notebook + langgraph ---
PROJ=$(curl -sS -X POST "$BASE/api/lab/projects" -H 'Content-Type: application/json' -d '{"title":"product-smoke"}' || echo '{}')
SLUG=$(python3 -c "import json,sys; d=json.loads(sys.argv[1]); print(d.get('slug',''))" "$PROJ" 2>/dev/null || true)
if [[ -n "$SLUG" ]]; then
  pass "project-create"

  # File write + content read-back
  code=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$BASE/api/lab/files/write?project_id=$SLUG" -H 'Content-Type: application/json' -d '{"path":"notes/smoke.md","content":"# Smoke content"}' || echo 000)
  [[ "$code" == "200" ]] && pass "file-write" || fail "file-write"
  CONTENT=$(curl -sS "$BASE/api/lab/files/content?project_id=$SLUG&path=notes/smoke.md" 2>/dev/null || echo '{}')
  echo "$CONTENT" | python3 -c "import json,sys; d=json.load(sys.stdin); assert 'Smoke content' in d.get('content','')" && pass "file-content" || fail "file-content"

  # Notebook
  code=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$BASE/api/lab/notebooks?project_id=$SLUG" -H 'Content-Type: application/json' -d '{"name":"smoke.ipynb"}' || echo 000)
  [[ "$code" == "200" ]] && pass "notebook-create" || fail "notebook-create"

  # LangGraph history
  code=$(curl -sS -o /dev/null -w '%{http_code}' "$BASE/api/lab/langgraph/history?project_id=$SLUG" || echo 000)
  [[ "$code" == "200" ]] && pass "lg-history-project" || fail "lg-history-project"

  # LangGraph run (if available)
  LANGGRAPH_AVAIL=$(python3 -c "import json,sys; print(json.load(sys.stdin).get('langgraph',{}).get('available',False))" <<<"$HEALTH")
  if [[ "$LANGGRAPH_AVAIL" == "True" ]]; then
    LGRESP=$(curl -sS -X POST "$BASE/api/lab/langgraph/run" -H 'Content-Type: application/json' -d "{\"project_id\":\"$SLUG\",\"prompt\":\"smoke test\"}" 2>/dev/null || echo '{}')
    echo "$LGRESP" | python3 -c "import json,sys; d=json.load(sys.stdin); assert d.get('ok')" && pass "lg-run" || fail "lg-run"
  else
    pass "lg-run (skip: not available)"
  fi
else
  fail "project-create"
fi

echo ""
echo "=== Results: $PASS passed, $FAIL failed ==="
[[ "$FAIL" -eq 0 ]]
