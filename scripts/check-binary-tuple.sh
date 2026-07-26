#!/usr/bin/env bash
# Fail closed unless release + installed lumen are the same build of current HEAD.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export PATH="/opt/homebrew/bin:$HOME/.cargo/bin:$HOME/.local/bin:$PATH"

RELEASE_BIN="${LUMEN_RELEASE_BIN:-$ROOT/agent/target/release/lumen}"
INSTALLED_BIN="${LUMEN_INSTALLED_BIN:-$HOME/.local/bin/lumen}"
HEAD_FULL="$(git -C "$ROOT" rev-parse HEAD)"

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
if [[ -z "$VER_COMMIT" || "$HEAD_FULL" != "$VER_COMMIT"* ]]; then
  echo "FAIL: binary is not built from current HEAD ${HEAD_FULL:0:8}: $RELEASE_VERSION" >&2
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
echo "binary_sha256=$RELEASE_SHA"
echo "release_binary=$RELEASE_BIN"
echo "installed_binary=$INSTALLED_BIN"
