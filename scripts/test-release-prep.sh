#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d "${TMPDIR:-/tmp}/lumen-release-prep-XXXXXX")"
trap 'rm -rf "$TMP"' EXIT
FIXTURE="$TMP/repo"
mkdir -p "$FIXTURE/scripts" "$FIXTURE/agent/crates/codegen"
cp \
  "$ROOT/scripts/check-version-consistency.sh" \
  "$ROOT/scripts/invalidate-release-readiness.py" \
  "$ROOT/scripts/release_version.py" \
  "$ROOT/scripts/update-changelog.py" \
  "$FIXTURE/scripts/"
printf '1.2.3-alpha.4\n' >"$FIXTURE/VERSION"

packages=(
  xai-grok-version xai-grok-pager xai-grok-pager-bin xai-grok-shell xai-grok-tools
  xai-grok-tools-api xai-grok-update xai-grok-workspace
)
for package in "${packages[@]}"; do
  mkdir -p "$FIXTURE/agent/crates/codegen/$package"
  version='1.2.3-alpha.4'
  # Upstream protocol/client identity is intentionally independent from the
  # shipped Lumen release. Keep it different in the fixture so a future edit
  # cannot silently put it back into the release-version authority set.
  if [[ "$package" == 'xai-grok-version' ]]; then
    version='9.9.9'
  fi
  printf '[package]\nname = "%s"\nversion = "%s"\n' "$package" "$version" \
    >"$FIXTURE/agent/crates/codegen/$package/Cargo.toml"
done
{
  for package in "${packages[@]}"; do
    version='1.2.3-alpha.4'
    if [[ "$package" == 'xai-grok-version' ]]; then
      version='9.9.9'
    fi
    printf '[[package]]\nname = "%s"\nversion = "%s"\n\n' "$package" "$version"
  done
} >"$FIXTURE/agent/Cargo.lock"
cat >"$FIXTURE/CHANGELOG.md" <<'EOF'
# Changelog

## [Unreleased]

### Added

- Hand-written pending note.
EOF

git -C "$FIXTURE" init -q -b main
git -C "$FIXTURE" config user.email fixture@example.invalid
git -C "$FIXTURE" config user.name Fixture
git -C "$FIXTURE" add .
git -C "$FIXTURE" commit -qm 'feat: initial release fixture'
git -C "$FIXTURE" commit --allow-empty -qm 'fix(release): keep versions synchronized'

[[ "$(python3 "$FIXTURE/scripts/release_version.py" --root "$FIXTURE" check)" == 1.2.3-alpha.4 ]]
[[ "$(python3 "$FIXTURE/scripts/release_version.py" --root "$FIXTURE" next patch)" == 1.2.3 ]]
[[ "$(python3 "$FIXTURE/scripts/release_version.py" --root "$FIXTURE" next prerelease)" == 1.2.3-alpha.5 ]]
python3 "$FIXTURE/scripts/release_version.py" --root "$FIXTURE" set 1.2.3 >/dev/null
[[ "$(python3 "$FIXTURE/scripts/release_version.py" --root "$FIXTURE" check)" == 1.2.3 ]]
[[ "$(grep -R 'version = "1.2.3"' "$FIXTURE/agent/crates/codegen" -l | wc -l | tr -d ' ')" == 7 ]]
[[ "$(grep -c 'version = "1.2.3"' "$FIXTURE/agent/Cargo.lock")" == 7 ]]
grep -Fqx 'version = "9.9.9"' "$FIXTURE/agent/crates/codegen/xai-grok-version/Cargo.toml"

mkdir -p "$FIXTURE/artifacts/readiness"
printf '{"schema_version":1,"lumen_version":"1.2.3-alpha.4"}\n' >"$FIXTURE/SOURCE_LOCK.json"
printf '{"schema_version":1,"version":"1.2.3-alpha.4","pass":true}\n' \
  >"$FIXTURE/artifacts/readiness/engineering_complete.json"
printf '{"schema_version":1,"version":"1.2.3-alpha.4","ready":true}\n' \
  >"$FIXTURE/artifacts/readiness/status.json"
if python3 "$FIXTURE/scripts/invalidate-release-readiness.py" --root "$FIXTURE" \
  >"$TMP/stale-source-lock.out" 2>&1
then
  echo "FAIL: readiness invalidation accepted a stale SOURCE_LOCK version" >&2
  exit 1
fi
grep -Fq 'SOURCE_LOCK.json must be refreshed' "$TMP/stale-source-lock.out"
python3 - "$FIXTURE/artifacts/readiness/status.json" <<'PY'
import json
import sys

assert json.load(open(sys.argv[1]))["ready"] is True
PY
printf '{"schema_version":1,"lumen_version":"1.2.3"}\n' >"$FIXTURE/SOURCE_LOCK.json"
python3 "$FIXTURE/scripts/invalidate-release-readiness.py" --root "$FIXTURE"
"$FIXTURE/scripts/check-version-consistency.sh" "$FIXTURE"
python3 - \
  "$FIXTURE/artifacts/readiness/status.json" \
  "$FIXTURE/artifacts/readiness/engineering_complete.json" <<'PY'
import json
import sys

status = json.load(open(sys.argv[1]))
engineering = json.load(open(sys.argv[2]))
assert status["version"] == "1.2.3"
assert status["ready"] is False
assert status["state"] == "BLOCKED"
assert status["engineering_complete"] is False
assert status["checks"] == []
assert engineering["version"] == "1.2.3"
assert engineering["pass"] is False
assert status["source_lock_sha256"] == engineering["source_lock_sha256"]
assert status["blockers"] == engineering["auto_blockers"]
assert status["blockers"][0].startswith("release_version_changed:")
PY

python3 "$FIXTURE/scripts/update-changelog.py" --root "$FIXTURE" --date 2026-07-18 1.2.3
grep -Fq '## [1.2.3] - 2026-07-18' "$FIXTURE/CHANGELOG.md"
grep -Fq -- '- Hand-written pending note.' "$FIXTURE/CHANGELOG.md"
grep -Fq -- '- keep versions synchronized (`' "$FIXTURE/CHANGELOG.md"
[[ "$(grep -c '^### Added$' "$FIXTURE/CHANGELOG.md")" == 1 ]]
if python3 "$FIXTURE/scripts/update-changelog.py" --root "$FIXTURE" --date 2026-07-18 1.2.3 \
  >"$TMP/duplicate.out" 2>&1; then
  echo "FAIL: duplicate changelog version was accepted" >&2
  exit 1
fi
grep -Fq 'already contains version 1.2.3' "$TMP/duplicate.out"

echo "OK: release version and changelog fixtures passed"
