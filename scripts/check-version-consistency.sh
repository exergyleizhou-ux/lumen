#!/usr/bin/env bash
# check-version-consistency.sh — ensure VERSION, Cargo.toml, SOURCE_LOCK, and readiness agree.
# Called by CI. Exit 1 on mismatch.
set -euo pipefail

REPO="${1:-$(git rev-parse --show-toplevel)}"
cd "$REPO"
FAIL=0

# 1. VERSION file
if [ ! -f VERSION ]; then echo "FAIL: VERSION file missing"; exit 1; fi
VER_FILE=$(cat VERSION | tr -d '\n' | tr -d ' ')
echo "VERSION file:        $VER_FILE"

# 2. Cargo.toml version (from xai-grok-version crate)
CARGO_VER=$(grep '^version = ' agent/crates/codegen/xai-grok-version/Cargo.toml | head -1 | sed 's/version = "\(.*\)"/\1/' | tr -d ' ')
echo "Cargo.toml version:  $CARGO_VER"

# 3. SOURCE_LOCK version
if [ -f SOURCE_LOCK.json ]; then
  LOCK_VER=$(python3 -c "import json; print(json.load(open('SOURCE_LOCK.json')).get('lumen_version',''))" 2>/dev/null || echo "PARSE_ERROR")
  echo "SOURCE_LOCK version: $LOCK_VER"
else
  LOCK_VER="MISSING"
fi

# 4. Readiness version
if [ -f artifacts/readiness/engineering_complete.json ]; then
  READY_VER=$(python3 -c "import json; print(json.load(open('artifacts/readiness/engineering_complete.json')).get('version',''))" 2>/dev/null || echo "PARSE_ERROR")
  echo "Readiness version:   $READY_VER"
else
  READY_VER="MISSING"
fi

# 5. status.json must not carry a stale version, and must not claim READY
#    while engineering_complete disagrees (this exact combination shipped a
#    forged READY on 2026-07-25).
if [ -f artifacts/readiness/status.json ]; then
  STATUS_VER=$(python3 -c "import json; print(json.load(open('artifacts/readiness/status.json')).get('version',''))" 2>/dev/null || echo "PARSE_ERROR")
  echo "status.json version: ${STATUS_VER:-<absent>}"
  if [ -n "$STATUS_VER" ] && [ "$STATUS_VER" != "PARSE_ERROR" ] && [ "$STATUS_VER" != "$VER_FILE" ]; then
    echo "FAIL: status.json version ($STATUS_VER) != VERSION ($VER_FILE)"
    FAIL=1
  fi
  CONTRADICTION=$(python3 - <<'PY' 2>/dev/null || echo "PARSE_ERROR"
import json
status = json.load(open('artifacts/readiness/status.json'))
try:
    eng = json.load(open('artifacts/readiness/engineering_complete.json'))
except Exception:
    eng = {}
s_ready = bool(status.get("ready"))
e_ok = eng.get("pass")
print("CONTRADICTION" if (s_ready and e_ok is False) else "OK")
PY
)
  if [ "$CONTRADICTION" = "CONTRADICTION" ]; then
    echo "FAIL: status.json claims ready=true while engineering_complete.json says pass=false"
    FAIL=1
  fi
fi

# Check consistency
if [ "$VER_FILE" != "$CARGO_VER" ]; then
  echo "FAIL: VERSION ($VER_FILE) != Cargo.toml ($CARGO_VER)"
  FAIL=1
fi

if [ "$LOCK_VER" != "MISSING" ] && [ "$LOCK_VER" != "$VER_FILE" ] && [ "$LOCK_VER" != "PARSE_ERROR" ]; then
  echo "FAIL: SOURCE_LOCK lumen_version ($LOCK_VER) != VERSION ($VER_FILE) — run scripts/source-lock.sh"
  FAIL=1
fi

if [ -n "$READY_VER" ] && [ "$READY_VER" != "MISSING" ] && [ "$READY_VER" != "$VER_FILE" ] && [ "$READY_VER" != "PARSE_ERROR" ]; then
  echo "FAIL: readiness version ($READY_VER) != VERSION ($VER_FILE)"
  FAIL=1
fi

if [ "$FAIL" -eq 1 ]; then
  echo "Version consistency check FAILED"
  exit 1
fi
echo "PASS: all version files consistent at $VER_FILE"
