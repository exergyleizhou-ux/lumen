#!/usr/bin/env bash
# Lumen Lab L3 smoke — quick health checks for release readiness.
# Run against a running Lab instance.
# Usage: ./scripts/science/lab-l3-smoke.sh [base_url]
set -euo pipefail

BASE="${1:-http://127.0.0.1:18992}"
PASS=0
FAIL=0

check() {
  local label="$1"; shift
  if "$@" >/dev/null 2>&1; then
    echo "  PASS $label"
    PASS=$((PASS+1))
  else
    echo "  FAIL $label"
    FAIL=$((FAIL+1))
  fi
}

echo "=== Lumen Lab L3 Smoke ($BASE) ==="
echo ""

# 1. Health
HEALTH=$(curl -sS "$BASE/api/lab/health" 2>/dev/null || echo "{}")
check "health.status==ok" python3 -c "import json,sys; d=json.load(sys.stdin); assert d.get('status')=='ok'" <<< "$HEALTH"
check "ketcher.same_origin" python3 -c "import json,sys; d=json.load(sys.stdin); assert d['ketcher']['same_origin']" <<< "$HEALTH"
check "jupyter.available" python3 -c "import json,sys; d=json.load(sys.stdin); assert d['jupyter']['available']" <<< "$HEALTH"
check "langgraph key exists" python3 -c "import json,sys; d=json.load(sys.stdin); assert 'langgraph' in d" <<< "$HEALTH"
check "onlyoffice key exists" python3 -c "import json,sys; d=json.load(sys.stdin); oo=d.get('onlyoffice',{}); assert 'configured' in oo; assert 'edit' in oo" <<< "$HEALTH"

# 2. Static files
check "office-editor.html 200" curl -sS -o /dev/null -w '%{http_code}' "$BASE/office-editor.html" | grep -q 200
check "ketcher / 200" curl -sS -o /dev/null -w '%{http_code}' "$BASE/ketcher/" | grep -q '200\|302'

# 3. OnlyOffice callback route exists (POST empty body should not 500)
CBRESP=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$BASE/api/lab/onlyoffice/callback?project_id=x&path=t.docx" -H 'Content-Type: application/json' -d '{}' 2>/dev/null || echo "000")
if [ "$CBRESP" != "000" ] && [ "$CBRESP" != "500" ]; then
  echo "  PASS callback route ($CBRESP)"
  PASS=$((PASS+1))
else
  echo "  FAIL callback route ($CBRESP)"
  FAIL=$((FAIL+1))
fi

# 4. Notebook
NBRESP=$(curl -sS -X POST "$BASE/api/lab/notebooks?project_id=default" -H 'Content-Type: application/json' -d '{"name":"smoke-l3.ipynb"}' 2>/dev/null || echo "{}")
check "notebook create" python3 -c "import json,sys; d=json.load(sys.stdin); assert 'name' in d" <<< "$NBRESP"

# 5. OnlyOffice: if DS configured, check editor page loads api.js
OO_URL=$(python3 -c "import json; print(json.loads('$HEALTH').get('onlyoffice',{}).get('url',''))" 2>/dev/null || echo "")
if [ -n "$OO_URL" ]; then
  DSAPI="${OO_URL%/}/web-apps/apps/api/documents/api.js"
  check "DS api.js reachable" curl -sS -o /dev/null -w '%{http_code}' "$DSAPI" | grep -q 200
else
  echo "  SKIP DS api.js (not configured)"
fi

echo ""
echo "=== Results: $PASS passed, $FAIL failed ==="
[ "$FAIL" -eq 0 ] || exit 1
