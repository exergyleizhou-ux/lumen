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

echo "=== offline gates PASS (product_rc=NOT_READY) ==="
