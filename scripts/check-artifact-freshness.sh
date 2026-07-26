#!/usr/bin/env bash
# check-artifact-freshness.sh — repo-only staleness gate (no built binary needed).
# Recomputes the SOURCE_LOCK critical-file hashes over the working tree and
# fails when any tracked critical file drifted from SOURCE_LOCK.json — i.e.
# someone changed a gate/script/manifest without refreshing the lock in the
# same change. Full binary/evidence reconciliation stays in
# reconcile-evidence.sh; this is the cheap CI-friendly subset.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

python3 <<'PY'
import hashlib, json, sys
from pathlib import Path

try:
    lock = json.loads(Path("SOURCE_LOCK.json").read_text())
except (OSError, json.JSONDecodeError) as exc:
    print(f"FAIL: SOURCE_LOCK.json missing/unparseable: {type(exc).__name__}")
    raise SystemExit(1)

critical = lock.get("critical_file_sha256")
if not isinstance(critical, dict) or not critical:
    print("FAIL: SOURCE_LOCK.json has no critical_file_sha256 map")
    raise SystemExit(1)

drift = []
for rel, expected in critical.items():
    p = Path(rel)
    if not p.is_file():
        drift.append(f"{rel} (missing)")
        continue
    actual = hashlib.sha256(p.read_bytes()).hexdigest()
    if actual != expected:
        drift.append(rel)

if drift:
    print("FAIL: critical files drifted from SOURCE_LOCK.json — run scripts/source-lock.sh and commit the refreshed lock:")
    for d in drift:
        print(f"  - {d}")
    raise SystemExit(1)

print(f"PASS: {len(critical)} critical files match SOURCE_LOCK.json")
PY
