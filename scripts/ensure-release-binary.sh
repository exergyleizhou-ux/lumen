#!/usr/bin/env bash
# Ensure agent/target/release/lumen exists and is stamped with the expected
# source commit, without ever stamping an evidence or docs-only commit into
# the binary.
#
# Expected source resolution (identical semantics to check-binary-tuple.sh):
#   * SOURCE_LOCK's locked commit, when it is an ancestor of HEAD and the
#     suffix locked..HEAD touches only lock/SBOM/readiness/docs/ledger paths
#     with no critical-file drift — the binary must stay stamped with the
#     source, not the evidence suffix;
#   * otherwise HEAD itself.
#
# Build policy:
#   * binary present and stamped with expected source  -> no-op
#   * LUMEN_SKIP_BUILD=1                               -> verify only, no build
#   * expected source == HEAD                          -> in-place release build
#   * expected source != HEAD (evidence/docs suffix)   -> detached-worktree
#     build of the locked source, binary copied over target/release/lumen
#
# Used by the L2/L3/L4/L5 smoke scripts so their rebuild paths cannot break
# the release/installed binary tuple (P3 unification).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export PATH="/opt/homebrew/bin:$HOME/.cargo/bin:$HOME/.local/bin:$PATH"

BIN="${LUMEN_RELEASE_BIN:-${LUMEN_BINARY:-$ROOT/agent/target/release/lumen}}"

HEAD_FULL="$(git -C "$ROOT" rev-parse HEAD)"

EXPECTED_SOURCE="$(python3 - "$ROOT" "$HEAD_FULL" <<'PY'
import hashlib, json, subprocess, sys
from pathlib import Path

root = Path(sys.argv[1])
head = sys.argv[2]
try:
    lock = json.loads((root / "SOURCE_LOCK.json").read_text())
    locked = ((lock.get("monorepo") or {}).get("git_head") or "")
    if len(locked) != 40:
        raise ValueError("missing locked source")
    subprocess.run(
        ["git", "-C", str(root), "merge-base", "--is-ancestor", locked, head],
        check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
    )
    suffix = subprocess.check_output(
        ["git", "-C", str(root), "-c", "core.quotepath=false",
         "diff", "--name-only", f"{locked}..{head}"],
        text=True,
    ).splitlines()
    allowed = ("SOURCE_LOCK.json", "SBOM.spdx.json", "artifacts/readiness/", "artifacts/audit/",
               "docs/", "CURRENT_STATE_LEDGER.md")
    if any(not path.startswith(allowed) for path in suffix):
        raise ValueError("non-evidence suffix")
    critical = lock.get("critical_file_sha256")
    if not isinstance(critical, dict) or not critical:
        raise ValueError("missing critical hashes")
    for relative, expected in critical.items():
        path = root / relative
        if not isinstance(expected, str) or not path.is_file():
            raise ValueError("missing critical file")
        if hashlib.sha256(path.read_bytes()).hexdigest() != expected:
            raise ValueError("critical content drift")
    print(locked)
except (OSError, ValueError, json.JSONDecodeError, subprocess.CalledProcessError):
    print(head)
PY
)"

stamp_matches() {
  [[ -x "$BIN" ]] || return 1
  local ver commit
  ver="$("$BIN" --version 2>/dev/null)" || return 1
  commit="$(printf '%s' "$ver" | sed -nE 's/.*\(([0-9a-f]{7,40})\).*/\1/p')"
  [[ -n "$commit" && "$EXPECTED_SOURCE" == "$commit"* ]]
}

if stamp_matches; then
  echo "release binary ok: $("$BIN" --version)"
  exit 0
fi

if [[ "${LUMEN_SKIP_BUILD:-0}" == "1" ]]; then
  echo "FAIL: LUMEN_SKIP_BUILD=1 but $BIN is missing or not stamped with ${EXPECTED_SOURCE:0:8}" >&2
  echo "Build the source candidate first (see scripts/install-local.sh)." >&2
  exit 1
fi

if [[ "$EXPECTED_SOURCE" == "$HEAD_FULL" ]]; then
  echo "building release lumen from current HEAD $HEAD_FULL..."
  (cd "$ROOT/agent" && CARGO_BUILD_JOBS="${CARGO_BUILD_JOBS:-2}" \
    cargo build -p xai-grok-pager-bin --release)
  stamp_matches || {
    echo "FAIL: rebuilt binary still not stamped with $HEAD_FULL" >&2
    exit 1
  }
  exit 0
fi

# HEAD is an evidence/docs-only suffix of the locked source: build the locked
# source in a detached worktree so the stamp stays the locked source.
TMP="$(mktemp -d "${TMPDIR:-/tmp}/lumen-locked-build-XXXXXX")"
cleanup() { git -C "$ROOT" worktree remove --force "$TMP" 2>/dev/null || rm -rf -- "$TMP"; }
trap cleanup EXIT
echo "building locked source ${EXPECTED_SOURCE:0:8} in temporary worktree (HEAD is evidence/docs suffix)..."
git -C "$ROOT" worktree add --detach "$TMP" "$EXPECTED_SOURCE" >/dev/null
(cd "$TMP/agent" && CARGO_BUILD_JOBS="${CARGO_BUILD_JOBS:-2}" \
  cargo build -p xai-grok-pager-bin --release)
mkdir -p "$(dirname "$BIN")"
cp "$TMP/agent/target/release/lumen" "$BIN"
stamp_matches || {
  echo "FAIL: worktree build of ${EXPECTED_SOURCE:0:8} produced an unexpected stamp" >&2
  exit 1
}
echo "release binary rebuilt from locked source ${EXPECTED_SOURCE:0:8}"
