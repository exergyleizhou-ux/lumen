#!/usr/bin/env bash
# Build the current source commit and atomically install `lumen` to ~/.local/bin.
#
# NOTE for callers: piping this script (e.g. `install-local.sh | tail -2`)
# discards its exit code — a refused dirty-tree build then looks like success
# and the caller silently proceeds with a STALE binary. Either check
# ${PIPESTATUS[0]}, set `set -o pipefail` in the caller, or don't pipe.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export PATH="/opt/homebrew/bin:$HOME/.cargo/bin:$HOME/.local/bin:$PATH"
export PROTOC="${PROTOC:-/opt/homebrew/bin/protoc}"

# A binary stamped with HEAD cannot truthfully identify uncommitted source.
# Ignored scratch output is harmless, but tracked or untracked source can alter
# Cargo auto-discovery/build-script inputs and is rejected unless explicitly
# overridden.
if [[ "${LUMEN_ALLOW_DIRTY:-0}" != "1" ]]; then
  if [[ -n "$(git -C "$ROOT" status --porcelain --untracked-files=all)" ]]; then
    echo "FAIL: refusing to build/install from a dirty tree." >&2
    echo "Commit source first, or set LUMEN_ALLOW_DIRTY=1 explicitly." >&2
    git -C "$ROOT" status --short >&2
    exit 1
  fi
fi

BIN_SRC="$ROOT/agent/target/release/lumen"
DEST_DIR="${LUMEN_INSTALL_DIR:-$HOME/.local/bin}"
DEST="$DEST_DIR/lumen"
HEAD_FULL="$(git -C "$ROOT" rev-parse HEAD)"

# Release evidence is deliberately committed after the source candidate it
# describes.  When that suffix is limited to lock/SBOM/readiness artifacts,
# install the already-built locked candidate instead of rebuilding HEAD and
# stamping the evidence commit into the executable.  The same critical-file
# checks used by the tuple gate prevent this from accepting an arbitrary stale
# lock.
EXPECTED_SOURCE="$HEAD_FULL"
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
fi
SOURCE_COMMIT="$(git -C "$ROOT" rev-parse --short "$EXPECTED_SOURCE")"

if [[ "${LUMEN_SKIP_BUILD:-0}" != "1" ]]; then
  if [[ "$EXPECTED_SOURCE" != "$HEAD_FULL" ]]; then
    echo "FAIL: current HEAD is evidence for locked source $SOURCE_COMMIT; refusing to rebuild and stamp the evidence commit." >&2
    echo "Build the source candidate before source-lock, then run LUMEN_SKIP_BUILD=1 scripts/install-local.sh." >&2
    exit 1
  fi
  echo "Building release lumen from source commit $SOURCE_COMMIT..."
  (cd "$ROOT/agent" && CARGO_BUILD_JOBS="${CARGO_BUILD_JOBS:-2}" cargo build -p xai-grok-pager-bin --release)
else
  echo "LUMEN_SKIP_BUILD=1: verifying existing release binary against $SOURCE_COMMIT..."
fi
test -x "$BIN_SRC"

VERSION_LINE="$($BIN_SRC --version)"
case "$VERSION_LINE" in
  *"($SOURCE_COMMIT)"*) ;;
  *)
    echo "FAIL: release binary is stale: expected commit $SOURCE_COMMIT, got: $VERSION_LINE" >&2
    echo "Unset LUMEN_SKIP_BUILD and rebuild." >&2
    exit 1
    ;;
esac

mkdir -p "$DEST_DIR"
TMP_DEST="$DEST.tmp.$$"
trap 'rm -f "$TMP_DEST"' EXIT
cp "$BIN_SRC" "$TMP_DEST"
chmod +x "$TMP_DEST"
# ad-hoc code-sign the INSTALLED COPY ONLY so macOS taskgated won't kill it.
# Signing cargo's own output (target/release/lumen) mutates a build artifact:
# the next `cargo build`/built-binary e2e re-links it, the signature vanishes,
# and the release/installed pair silently diverges mid-verification — exactly
# what binary_tuple_post exists to catch. Keep target/ cargo-pristine.
codesign --force --sign - "$TMP_DEST" 2>/dev/null || true
mv -f "$TMP_DEST" "$DEST"
trap - EXIT

DEST_SHA="$(shasum -a 256 "$DEST" | awk '{print $1}')"

echo "Installed: $DEST"
echo "source_commit=$SOURCE_COMMIT"
echo "binary_sha256=$DEST_SHA"
"$DEST" --version
echo ""
echo "Ensure PATH includes: $DEST_DIR"
echo "Set:  export DEEPSEEK_API_KEY=..."
echo "Then: lumen"
echo ""
echo "Productivity diary template: journal/TEMPLATE-productivity-day.md"
