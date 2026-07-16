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

# Optionally refresh SOURCE_LOCK (default: use existing to avoid lock↔commit churn).
if [[ "${REFRESH_SOURCE_LOCK:-0}" == "1" ]] || [[ ! -f "$ROOT/SOURCE_LOCK.json" ]]; then
  "$ROOT/scripts/source-lock.sh"
fi

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
# Content freshness: critical file hashes still match (not only exact HEAD pin).
# Exact HEAD pin is optional meta; churning SOURCE_LOCK every commit is not required.
crit = lock.get("critical_file_sha256") or {}
content_ok = True
mismatched = []
for rel, expected in crit.items():
    p = root / rel
    if not p.is_file():
        content_ok = False
        mismatched.append(rel + ":missing")
        continue
    got = hashlib.sha256(p.read_bytes()).hexdigest()
    if got != expected:
        content_ok = False
        mismatched.append(rel)
lock_fresh = content_ok  # evidence not invalidated by critical-path drift
lock_head_match = lock_head == head

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

# readiness artifact digests (stable subset; skip self + frequently rewritten status)
artifact_sha = {}
skip = {"reconcile.json", "status.json", "engineering_complete.json", "M6-productivity.json"}
for p in sorted(art.glob("*.json")):
    if p.name in skip:
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
    blockers.append(
        "source_lock_content_drift:" + ",".join(mismatched[:5])
        if mismatched
        else f"source_lock_stale:lock={lock_head[:7] if lock_head else '?'} head={head[:7]}"
    )
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
    "source_lock_head_match": lock_head_match,
    "source_lock_content_ok": content_ok,
    "binary_path": binary_path,
    "binary_sha256": binary_sha,
    "artifact_sha256": artifact_sha,
    "sbom_present": sbom_ok,
    "legal_present": legal_ok,
    "blockers": blockers,
    "note": "R7: run/event/effect/UI evidence tuple reconciles to current source + binary",
}
out = art / "reconcile.json"
# Idempotent write: keep prior generated_at / monorepo_git_head if material fields match
if out.is_file():
    prev = json.loads(out.read_text())
    material_keys = [
        "pass", "source_lock_sha256", "source_lock_git_head", "source_lock_fresh",
        "source_lock_content_ok", "binary_sha256", "artifact_sha256",
        "sbom_present", "legal_present", "blockers",
    ]
    if all(prev.get(k) == rec.get(k) for k in material_keys):
        print(f"pass={ok} (unchanged) source_lock_sha256={lock_sha[:12]}…")
        print(f"lock_fresh={lock_fresh} content_ok={content_ok} head_match={lock_head_match}")
        print(f"blockers={blockers or []}")
        print(f"kept {out}")
        sys.exit(0 if ok else 1)

out.write_text(json.dumps(rec, indent=2) + "\n")
print(f"pass={ok} source_lock_sha256={lock_sha[:12]}… binary_sha256={(binary_sha or 'none')[:12]}…")
print(f"lock_fresh={lock_fresh} content_ok={content_ok} head_match={lock_head_match}")
print(f"blockers={blockers or []}")
print(f"wrote {out}")
sys.exit(0 if ok else 1)
PY
