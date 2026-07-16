#!/usr/bin/env bash
# Refresh SOURCE_LOCK.json for the monorepo (FINAL-2.0 S0).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
export PATH="/opt/homebrew/bin:$HOME/.local/bin:$PATH"

python3 <<'PY'
import hashlib, json, subprocess
from datetime import datetime, timezone
from pathlib import Path

root = Path(".")
head = subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip()
paths = [
    "agent/crates/codegen/xai-grok-models/default_models.json",
    "agent/crates/codegen/lumen-guard/src/lib.rs",
    "agent/crates/codegen/lumen-discipline/src/lib.rs",
    "scripts/eval-coding.sh",
    "scripts/smoke-deepseek-agent.sh",
    "docs/masterplan/09-FINAL-2.0-执行路径.md",
    "docs/masterplan/00A-来源锁与运行合同.md",
]
files = {}
h = hashlib.sha256()
for rel in paths:
    p = root / rel
    if not p.exists():
        continue
    b = p.read_bytes()
    files[rel] = hashlib.sha256(b).hexdigest()
    h.update(rel.encode())
    h.update(b)

lock = {
    "schema_version": 1,
    "generated_at": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
    "monorepo": {"git_head": head, "git_short": head[:7]},
    "upstream_pin": {
        "doc": "agent/UPSTREAM.md",
        "source": "xai-org/grok-build (local Desktop pin)",
        "policy": "PINNED; security-only cherry-picks",
    },
    "masterplan_authority": {
        "desktop": "Lumen Masterplan FINAL-2.0 - 生产级执行方案.docx",
        "baseline": "Lumen Masterplan.docx FINAL-1.1",
        "repo": "docs/masterplan/",
    },
    "critical_file_sha256": files,
    "aggregate_critical_sha256": h.hexdigest(),
}
Path("SOURCE_LOCK.json").write_text(json.dumps(lock, indent=2) + "\n")
print("OK: wrote SOURCE_LOCK.json", head[:7])
PY
