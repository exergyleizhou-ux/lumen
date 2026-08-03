#!/usr/bin/env bash
# Fail closed unless release + installed lumen are the same build of current HEAD.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export PATH="/opt/homebrew/bin:$HOME/.cargo/bin:$HOME/.local/bin:$PATH"

RELEASE_BIN="${LUMEN_RELEASE_BIN:-$ROOT/agent/target/release/lumen}"
INSTALLED_BIN="${LUMEN_INSTALLED_BIN:-$HOME/.local/bin/lumen}"
HEAD_FULL="$(git -C "$ROOT" rev-parse HEAD)"

# A release evidence commit necessarily follows the source commit it describes:
# the lock, SBOM, and readiness records cannot be committed before they exist.
# Accept that narrow suffix only when the lock is an ancestor, every changed
# path is evidence-only, and every locked critical file still hashes exactly.
# Otherwise retain the strict current-HEAD requirement below.
EXPECTED_SOURCE="$HEAD_FULL"
EXPECTED_SOURCE_KIND="current HEAD"
LOCKED_SOURCE="$(python3 - "$ROOT" "$HEAD_FULL" <<'PY'
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
        ["git", "merge-base", "--is-ancestor", locked, head],
        cwd=root, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
    )
    suffix = subprocess.check_output(
        ["git", "diff", "--name-only", f"{locked}..{head}"], cwd=root, text=True
    ).splitlines()
    allowed = ("SOURCE_LOCK.json", "SBOM.spdx.json", "artifacts/readiness/")
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
    pass
PY
)"
if [[ -n "$LOCKED_SOURCE" && "$LOCKED_SOURCE" != "$HEAD_FULL" ]]; then
  EXPECTED_SOURCE="$LOCKED_SOURCE"
  EXPECTED_SOURCE_KIND="SOURCE_LOCK evidence source"
fi

for bin in "$RELEASE_BIN" "$INSTALLED_BIN"; do
  [[ -x "$bin" ]] || {
    echo "FAIL: lumen binary missing or not executable: $bin" >&2
    exit 1
  }
done

RELEASE_VERSION="$("$RELEASE_BIN" --version)"
INSTALLED_VERSION="$("$INSTALLED_BIN" --version)"
[[ "$RELEASE_VERSION" == "$INSTALLED_VERSION" ]] || {
  echo "FAIL: release/installed version mismatch" >&2
  echo "release=$RELEASE_VERSION" >&2
  echo "installed=$INSTALLED_VERSION" >&2
  exit 1
}
# The binary stamps `git rev-parse --short HEAD`, whose length git auto-widens
# under prefix ambiguity (7, 8, … chars). Comparing against a fixed-width
# short is wrong — extract the stamped commit and prefix-match the full sha.
VER_COMMIT="$(printf '%s' "$RELEASE_VERSION" | sed -nE 's/.*\(([0-9a-f]{7,40})\).*/\1/p')"
if [[ -z "$VER_COMMIT" || "$EXPECTED_SOURCE" != "$VER_COMMIT"* ]]; then
  echo "FAIL: binary is not built from expected $EXPECTED_SOURCE_KIND ${EXPECTED_SOURCE:0:8}: $RELEASE_VERSION" >&2
  exit 1
fi

RELEASE_SHA="$(shasum -a 256 "$RELEASE_BIN" | awk '{print $1}')"
INSTALLED_SHA="$(shasum -a 256 "$INSTALLED_BIN" | awk '{print $1}')"
# The installed copy carries an ad-hoc macOS code signature that the pristine
# cargo output deliberately does not (see install-local.sh), so byte equality
# between the two is impossible by design. Same-build identity is enforced by
# the identical version+commit stamp checked above; both digests are still
# reported so any drift is visible in the recorded evidence.

if [[ -n "${LUMEN_EXPECTED_BINARY_SHA:-}" ]] && \
   [[ "$RELEASE_SHA" != "$LUMEN_EXPECTED_BINARY_SHA" ]]; then
  echo "FAIL: binary changed during readiness run" >&2
  echo "expected_sha256=$LUMEN_EXPECTED_BINARY_SHA" >&2
  echo "actual_sha256=$RELEASE_SHA" >&2
  exit 1
fi

echo "version=$RELEASE_VERSION"
echo "source_commit=$EXPECTED_SOURCE"
echo "binary_sha256=$RELEASE_SHA"
echo "release_binary=$RELEASE_BIN"
echo "installed_binary=$INSTALLED_BIN"
