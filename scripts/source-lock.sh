#!/usr/bin/env bash
# Refresh SOURCE_LOCK.json for the monorepo's current execution contract.
#
# ORDERING: the lock records the source commit used to build and collect
# evidence. Committing the lock creates a new evidence-only commit, so a
# committed lock cannot name its own commit. Verifiers therefore accept a lock
# commit that is an ancestor of HEAD only when every intervening change is a
# lock, SBOM, or readiness-evidence file. Any source change remains a failure.
# The workflow is:
#
#   1. commit all real changes            (HEAD = X)
#   2. scripts/install-local.sh           (binary stamped X, needs clean tree)
#   3. scripts/source-lock.sh             (lock records X from *binary stamp*)
#   4. scripts/verify-readiness.sh        (evidence applies to X)
#   5. commit lock/SBOM/readiness only    (verifier accepts evidence-only suffix)
#
# THRASH GUARD: never record HEAD when the installed/release binary is still
# stamped with an earlier source commit. Lock the binary stamp instead (when
# the suffix is evidence/docs-only). Recording HEAD after an evidence-only
# commit while the binary still names the source is what made tuple fail
# repeatedly in the A5–A12 land.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
export PATH="/opt/homebrew/bin:$HOME/.local/bin:$PATH"

# A source lock is evidence about an already committed candidate, never a way
# to bless whatever happens to be in a developer's worktree. This rejects
# staged, unstaged, and untracked inputs before the script writes its own lock
# file; the prescribed evidence-only commit happens after this command.
dirty_status="$(git status --porcelain=v1)"
if [[ -n "$dirty_status" ]]; then
  echo "FAIL: source-lock refuses a dirty source tree; commit the source candidate first" >&2
  printf '%s\n' "$dirty_status" >&2
  exit 1
fi

python3 <<'PY'
import hashlib, json, re, subprocess
from datetime import datetime, timezone
from pathlib import Path

root = Path(".")
head = subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip()

def binary_stamp(path: Path) -> str | None:
    if not path.is_file():
        return None
    try:
        ver = subprocess.check_output([str(path), "--version"], text=True).strip()
    except (OSError, subprocess.CalledProcessError):
        return None
    m = re.search(r"\(([0-9a-f]{7,40})\)", ver)
    if not m:
        return None
    short = m.group(1)
    try:
        return subprocess.check_output(
            ["git", "rev-parse", short], text=True
        ).strip()
    except subprocess.CalledProcessError:
        return None

installed = Path.home() / ".local/bin/lumen"
release = root / "agent/target/release/lumen"
stamp = binary_stamp(installed) or binary_stamp(release)
if not stamp:
    raise SystemExit(
        "FAIL: source-lock requires an installed or release lumen binary "
        "(run scripts/install-local.sh first)"
    )

allowed_prefix = (
    "SOURCE_LOCK.json",
    "SBOM.spdx.json",
    "artifacts/readiness/",
    "docs/",
    "CURRENT_STATE_LEDGER.md",
)

if stamp == head:
    locked_source = head
elif subprocess.run(
    ["git", "merge-base", "--is-ancestor", stamp, head],
    stdout=subprocess.DEVNULL,
    stderr=subprocess.DEVNULL,
).returncode == 0:
    suffix = subprocess.check_output(
        ["git", "-c", "core.quotepath=false", "diff", "--name-only", f"{stamp}..{head}"],
        text=True,
    ).splitlines()
    bad = [p for p in suffix if not p.startswith(allowed_prefix)]
    if bad:
        raise SystemExit(
            "FAIL: binary stamp %s is behind HEAD but suffix has non-evidence paths: %s\n"
            "Rebuild/install at HEAD, or finish source commits before source-lock."
            % (stamp[:8], ", ".join(bad[:8]))
        )
    # Evidence/docs-only suffix: lock the *binary* source, never the evidence HEAD.
    locked_source = stamp
    print(
        "note: locking binary stamp %s (HEAD %s is evidence-only suffix)"
        % (stamp[:7], head[:7])
    )
else:
    raise SystemExit(
        "FAIL: binary stamp %s is not an ancestor of HEAD %s; reinstall from current source"
        % (stamp[:8], head[:8])
    )

version = Path("VERSION").read_text().strip() if Path("VERSION").is_file() else None
paths = [
    ".gitleaksignore",
    "agent/crates/codegen/xai-grok-shell-base/src/util/event_id.rs",
    "agent/crates/codegen/xai-grok-models/default_models.json",
    "agent/crates/codegen/lumen-guard/src/lib.rs",
    "agent/crates/codegen/lumen-discipline/src/lib.rs",
    "scripts/assert-defaults.sh",
    "scripts/check-binary-tuple.sh",
    "scripts/eval-coding.sh",
    "scripts/eval-coding-live.sh",
    "scripts/generate-sbom.sh",
    "scripts/install-local.sh",
    "scripts/onboarding-gate.sh",
    "scripts/productivity-gate.sh",
    "scripts/reconcile-evidence.sh",
    "scripts/source-lock.sh",
    "scripts/smoke-deepseek.sh",
    "scripts/smoke-deepseek-agent.sh",
    "scripts/smoke-deepseek-l2.sh",
    "scripts/smoke-deepseek-l3.sh",
    "scripts/smoke-deepseek-l4.sh",
    "scripts/smoke-deepseek-l5.sh",
    "scripts/smoke-r0-min.sh",
    "scripts/smoke-r0.sh",
    "scripts/smoke-verify.sh",
    "scripts/test-readiness-contract.sh",
    "scripts/test-onboarding-gate.sh",
    "scripts/verify-readiness.sh",
    "scripts/probe-local.sh",
    "scripts/doctor-verticals.sh",
    "docs/LUMEN-NEXTGEN-EXECUTION-BOOK-2026-08-01.md",
    "docs/masterplan/M5-onboarding-evidence.template.json",
    "docs/masterplan/00A-来源锁与运行合同.md",
]
files = {}
h = hashlib.sha256()
missing = []
for rel in paths:
    p = root / rel
    if not p.is_file():
        missing.append(rel)
        continue
    b = p.read_bytes()
    files[rel] = hashlib.sha256(b).hexdigest()
    h.update(rel.encode())
    h.update(b)
if missing:
    raise SystemExit("FAIL: critical source-lock files missing: " + ", ".join(missing))

lock = {
    "schema_version": 1,
    "generated_at": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
    "monorepo": {"git_head": locked_source, "git_short": locked_source[:7]},
    "lumen_version": version,
    "upstream_pin": {
        "doc": "agent/UPSTREAM.md",
        "source": "xai-org/grok-build (local Desktop pin)",
        "policy": "PINNED; security-only cherry-picks",
    },
    "execution_authority": {
        "repo": "docs/LUMEN-NEXTGEN-EXECUTION-BOOK-2026-08-01.md",
        "evidence_window": "2026-07-27..2026-08-01",
        "policy": "Current NextGen execution contract; historical plans are not authority",
    },
    "critical_file_sha256": files,
    "aggregate_critical_sha256": h.hexdigest(),
}
Path("SOURCE_LOCK.json").write_text(json.dumps(lock, indent=2) + "\n")
print("OK: wrote SOURCE_LOCK.json", locked_source[:7])
if locked_source != head:
    print("OK: HEAD", head[:7], "is evidence suffix; binary/lock stay on", locked_source[:7])
PY
