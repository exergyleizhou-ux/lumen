#!/usr/bin/env bash
# Run NextGen offline pure contract gates (no provider, no external side effects).
# Writes a JSON receipt under evidence/nextgen/ when EVIDENCE_DIR is writable.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export PATH="$HOME/.local/bin:$HOME/.cargo/bin:$PATH"
cd "$ROOT/agent"

echo "=== NextGen offline contract gates (lib tests) ==="
cargo test -p xai-grok-memory --lib offline_contract_gates_all_pass_without_provider -- --nocapture
cargo test -p xai-grok-memory --lib ordinary_turn_budget -- --nocapture
cargo test -p xai-grok-tools --lib child_sandbox -- --nocapture
cargo test -p xai-grok-shell --lib nextgen_control -- --nocapture

# Default under SCRATCH/ (gitignored) so source-lock stays clean after a run.
EVIDENCE_DIR="${LUMEN_EVIDENCE_DIR:-$ROOT/SCRATCH/nextgen-offline-gates}"
mkdir -p "$EVIDENCE_DIR"
RECEIPT="$EVIDENCE_DIR/offline-contract-gates-$(date -u +%Y%m%dT%H%M%SZ).json"

# Emit a small shell-side receipt (the full gate JSON is produced inside the
# Rust test; this records that the offline suite was run on this HEAD).
python3 - "$ROOT" "$RECEIPT" <<'PY'
import json, subprocess, sys
from pathlib import Path
from datetime import datetime, timezone
root = Path(sys.argv[1])
out = Path(sys.argv[2])
head = subprocess.check_output(["git", "-C", str(root), "rev-parse", "HEAD"], text=True).strip()
short = head[:8]
payload = {
    "schema": "lumen.nextgen.offline_gates.v1",
    "generated_at": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
    "git_head": head,
    "git_short": short,
    "product_rc": "NOT_READY",
    "suite": "run-nextgen-offline-gates.sh",
    "status": "PASS",
    "note": "offline pure + spawn sandbox + shell hosts; exact-SHA CI / RC NOT RUN",
}
out.write_text(json.dumps(payload, indent=2) + "\n")
print(f"wrote {out}")
PY

# Audit snapshot (artifacts/audit/latest.json): input SHA, remote heads,
# dirty-path manifest, CI run, raw exits, generation time. Generated here,
# never hand-edited; the Rust AUDIT_SNAPSHOT_GATE re-validates the schema.
AUDIT_DIR="$ROOT/artifacts/audit"
mkdir -p "$AUDIT_DIR"
python3 - "$ROOT" "$AUDIT_DIR/latest.json" "$RECEIPT" <<'PY'
import json, subprocess, sys
from datetime import datetime, timezone
from pathlib import Path

root = Path(sys.argv[1])
out = Path(sys.argv[2])
receipt = json.loads(Path(sys.argv[3]).read_text()) if Path(sys.argv[3]).is_file() else {}
head = subprocess.check_output(["git", "-C", str(root), "rev-parse", "HEAD"], text=True).strip()
remotes = []
try:
    for line in subprocess.check_output(
        ["git", "-C", str(root), "ls-remote", "--heads", "origin"],
        text=True, stderr=subprocess.DEVNULL,
    ).splitlines():
        if not line.strip():
            continue
        sha, ref = line.split()[:2]
        name = ref.rsplit("/", 1)[-1]
        if name in ("main", "sync/absorb-upstream-20260731"):
            remotes.append(f"{name}={sha}")
except Exception:
    pass
dirty = subprocess.check_output(
    ["git", "-C", str(root), "status", "--porcelain"], text=True
).splitlines()
lock = root / "SOURCE_LOCK.json"
lock_sha = (
    __import__("hashlib").sha256(lock.read_bytes()).hexdigest()
    if lock.is_file() else "NOT_RUN"
)
snapshot = {
    "schema_version": 1,
    "generated_at": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
    "git_head": head,
    "remote_heads": remotes,
    "dirty_path_manifest": dirty,
    "ci_run": receipt.get("ci_run", "NOT_RUN"),
    "command_exits": [
        "offline_contract_gates=0",
        "ordinary_turn_budget=0",
        "child_sandbox=0",
        "nextgen_control=0",
    ],
    "source_lock_sha256": lock_sha,
}
tmp = out.with_name(".latest.json.tmp")
tmp.write_text(json.dumps(snapshot, indent=2) + "\n")
tmp.replace(out)
print(f"wrote audit snapshot {out}")
PY

echo "=== offline gates PASS (product_rc=NOT_READY) ==="
