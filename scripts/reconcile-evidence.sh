#!/usr/bin/env bash
# R7 + source tuple reconcile: SOURCE_LOCK freshness, binary hash, readiness hashes.
# Writes artifacts/readiness/reconcile.json and patches status fields when present.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export PATH="/opt/homebrew/bin:$HOME/.cargo/bin:$HOME/.local/bin:$PATH"
ART="$ROOT/artifacts/readiness"
mkdir -p "$ART"
cd "$ROOT"

echo "=== reconcile-evidence ==="

# Refresh SOURCE_LOCK to current HEAD
"$ROOT/scripts/source-lock.sh"

python3 - "$ROOT" "$ART" <<'PY'
import hashlib, json, subprocess, sys
from datetime import datetime, timezone
from pathlib import Path

root = Path(sys.argv[1])
art = Path(sys.argv[2])
now = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
head = subprocess.check_output(["git", "rev-parse", "HEAD"], text=True, cwd=root).strip()

lock_path = root / "SOURCE_LOCK.json"
assert lock_path.is_file(), "SOURCE_LOCK.json missing"
lock = json.loads(lock_path.read_text())
lock_sha = hashlib.sha256(lock_path.read_bytes()).hexdigest()
lock_head = (lock.get("monorepo") or {}).get("git_head") or ""
lock_fresh = lock_head == head
if not lock_fresh and lock_head:
    # Allow a single meta commit after lock generation that only refreshes
    # SOURCE_LOCK / readiness artifacts (avoids infinite lock-refresh loop).
    try:
        parent = subprocess.check_output(
            ["git", "rev-parse", "HEAD^"], text=True, cwd=root
        ).strip()
        if parent == lock_head:
            lock_fresh = True
    except Exception:
        pass

# binary candidates
bin_candidates = [
    root / "agent" / "target" / "release" / "lumen",
    Path.home() / ".local" / "bin" / "lumen",
]
binary_path = None
binary_sha = None
for c in bin_candidates:
    if c.is_file():
        binary_path = str(c)
        binary_sha = hashlib.sha256(c.read_bytes()).hexdigest()
        break

# readiness artifact digests
artifact_sha = {}
for p in sorted(art.glob("*.json")):
    if p.name == "reconcile.json":
        continue
    artifact_sha[p.name] = hashlib.sha256(p.read_bytes()).hexdigest()

# required evidence files
required = [
    "status.json",
    "R0-min.json",
    "L1-tool-calls.json",
]
missing = [r for r in required if not (art / r).is_file()]

# SBOM / LEGAL
sbom = root / "SBOM.spdx.json"
legal = root / "LEGAL.md"
sbom_ok = sbom.is_file() and sbom.stat().st_size > 100
legal_ok = legal.is_file() and legal.stat().st_size > 200

blockers = []
if not lock_fresh:
    blockers.append(f"source_lock_stale:lock={lock_head[:7] if lock_head else '?'} head={head[:7]}")
if not binary_sha:
    blockers.append("binary_missing")
if missing:
    blockers.append("readiness_missing:" + ",".join(missing))
if not sbom_ok:
    blockers.append("sbom_missing")
if not legal_ok:
    blockers.append("legal_too_short")

ok = len(blockers) == 0
rec = {
    "schema_version": 1,
    "check_id": "reconcile_evidence",
    "pass": ok,
    "generated_at": now,
    "monorepo_git_head": head,
    "source_lock_sha256": lock_sha,
    "source_lock_git_head": lock_head,
    "source_lock_fresh": lock_fresh,
    "binary_path": binary_path,
    "binary_sha256": binary_sha,
    "artifact_sha256": artifact_sha,
    "sbom_present": sbom_ok,
    "legal_present": legal_ok,
    "blockers": blockers,
    "note": "R7: run/event/effect/UI evidence tuple reconciles to current source + binary",
}
(art / "reconcile.json").write_text(json.dumps(rec, indent=2) + "\n")

# Patch status.json with sha fields if it exists
status_path = art / "status.json"
if status_path.is_file():
    status = json.loads(status_path.read_text())
    status["source_lock_sha256"] = lock_sha
    status["binary_sha256"] = binary_sha
    status["reconcile_pass"] = ok
    status["reconciled_at"] = now
    status_path.write_text(json.dumps(status, indent=2) + "\n")

print(f"pass={ok} source_lock_sha256={lock_sha[:12]}… binary_sha256={(binary_sha or 'none')[:12]}…")
print(f"lock_fresh={lock_fresh} blockers={blockers or []}")
print(f"wrote {art / 'reconcile.json'}")
if not ok:
    sys.exit(1)
sys.exit(0)
PY
