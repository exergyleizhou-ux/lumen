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
  # Fail closed: a READY claim must be AFFIRMATIVELY backed by a parseable
  # engineering_complete.json with pass=true. Deleting or corrupting either
  # file must never silence this check.
  CONTRADICTION=$(python3 - <<'PY' 2>/dev/null || echo "FORGED"
import json
status = json.load(open('artifacts/readiness/status.json'))
if not bool(status.get("ready")):
    print("OK")
else:
    try:
        eng = json.load(open('artifacts/readiness/engineering_complete.json'))
    except Exception:
        print("FORGED")  # ready=true with missing/unreadable evidence
        raise SystemExit(0)
    print("OK" if eng.get("pass") is True else "FORGED")
PY
)
  if [ "$CONTRADICTION" != "OK" ]; then
    echo "FAIL: status.json claims ready=true without affirmative engineering_complete pass=true evidence"
    FAIL=1
  fi
fi

# Check consistency
if [ "$VER_FILE" != "$CARGO_VER" ]; then
  echo "FAIL: VERSION ($VER_FILE) != Cargo.toml ($CARGO_VER)"
  FAIL=1
fi

# SOURCE_LOCK is a tracked provenance anchor: it must exist, parse, carry the
# version, and match. All failure modes are hard failures.
if [ "$LOCK_VER" = "MISSING" ] || [ "$LOCK_VER" = "PARSE_ERROR" ] || [ -z "$LOCK_VER" ]; then
  echo "FAIL: SOURCE_LOCK.json ${LOCK_VER:-empty lumen_version} — provenance anchor must exist and parse; run scripts/source-lock.sh"
  FAIL=1
elif [ "$LOCK_VER" != "$VER_FILE" ]; then
  echo "FAIL: SOURCE_LOCK lumen_version ($LOCK_VER) != VERSION ($VER_FILE) — run scripts/source-lock.sh"
  FAIL=1
fi

if [ "$READY_VER" != "MISSING" ] && [ "$READY_VER" != "PARSE_ERROR" ] && [ "$READY_VER" != "$VER_FILE" ]; then
  # Empty means the file exists but carries no version stamp — since
  # verify-readiness now always stamps one, absence is itself drift.
  echo "FAIL: readiness version (${READY_VER:-unstamped}) != VERSION ($VER_FILE)"
  FAIL=1
fi

if [ "$FAIL" -eq 1 ]; then
  echo "Version consistency check FAILED"
  exit 1
fi
echo "PASS: all version files consistent at $VER_FILE"
