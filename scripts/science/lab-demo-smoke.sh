#!/usr/bin/env bash
# Verify the public (or local) demo path used in docs/lab/DEMO.md
# Usage: ./scripts/science/lab-demo-smoke.sh [base_url]
set -euo pipefail

BASE="${1:-https://demo.oasisdata2026.xyz/lumen-lab}"
PASS=0
FAIL=0
pass() { echo "  PASS $1"; PASS=$((PASS+1)); }
fail() { echo "  FAIL $1 — $2"; FAIL=$((FAIL+1)); }

echo "=== Lab demo smoke ($BASE) ==="
echo "  (matches docs/lab/DEMO.md checklist)"
echo ""

H=$(curl -sS "$BASE/api/lab/health" || echo "{}")

python3 -c "import json,sys; d=json.load(sys.stdin); assert d.get('status')=='ok'" <<<"$H" \
  && pass "health ok" || fail "health" "status not ok"
python3 -c "import json,sys; d=json.load(sys.stdin); assert d.get('version') and d.get('version')!='dev'" <<<"$H" \
  && pass "version shipped" || fail "version" "still dev or missing"
python3 -c "import json,sys; d=json.load(sys.stdin); assert d.get('ketcher',{}).get('same_origin')" <<<"$H" \
  && pass "ketcher same-origin (demo step 5)" || fail "ketcher" "not same_origin"
python3 -c "import json,sys; d=json.load(sys.stdin); assert d.get('jupyter',{}).get('available')" <<<"$H" \
  && pass "jupyter available (demo step 4)" || fail "jupyter" "unavailable"
python3 -c "import json,sys; d=json.load(sys.stdin); assert d.get('langgraph',{}).get('available')" <<<"$H" \
  && pass "langgraph available (demo step 6)" || fail "langgraph" "unavailable"
python3 -c "import json,sys; d=json.load(sys.stdin); oo=d.get('onlyoffice') or {}; assert 'configured' in oo" <<<"$H" \
  && pass "onlyoffice honest flag (demo step 1)" || fail "onlyoffice" "missing key"

# UI shell
code=$(curl -sS -o /dev/null -w '%{http_code}' "$BASE/" || echo 000)
[[ "$code" == "200" ]] && pass "index 200" || fail "index" "http $code"
echo "$BASE" | grep -q . 
IDX=$(curl -sS "$BASE/" || true)
echo "$IDX" | grep -q 'Lumen Science Lab' && pass "welcome branding" || fail "welcome" "missing Lumen Science Lab title"
echo "$IDX" | grep -q '产品导览' && pass "demo chip present" || fail "demo chip" "missing 产品导览"

# Create project + notebook + langgraph (core demo path)
PROJ=$(curl -sS -X POST "$BASE/api/lab/projects" -H 'Content-Type: application/json' -d '{"title":"demo-script"}' || echo '{}')
SLUG=$(python3 -c "import json,sys; print(json.loads(sys.argv[1]).get('slug',''))" "$PROJ" 2>/dev/null || true)
if [[ -z "$SLUG" ]]; then
  fail "project" "create failed"
else
  pass "project create"
  code=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$BASE/api/lab/notebooks?project_id=$SLUG" \
    -H 'Content-Type: application/json' -d '{"name":"demo.ipynb"}' || echo 000)
  [[ "$code" == "200" ]] && pass "notebook create" || fail "notebook" "http $code"
  LG=$(curl -sS -X POST "$BASE/api/lab/langgraph/run" -H 'Content-Type: application/json' \
    -d "{\"project_id\":\"$SLUG\",\"prompt\":\"demo smoke: list workspace and suggest one next step\"}" || echo '{}')
  echo "$LG" | python3 -c "import json,sys; d=json.load(sys.stdin); assert d.get('ok') and d.get('result')" \
    && pass "langgraph run" || fail "langgraph run" "not ok"
fi

echo ""
echo "=== Demo smoke: $PASS passed, $FAIL failed ==="
echo "Script: docs/lab/DEMO.md"
[[ "$FAIL" -eq 0 ]]
