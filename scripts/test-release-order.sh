#!/usr/bin/env bash
# S13 regression: release ordering must not bump VERSION before source-lock.
#
# Background: scripts/release.sh used to run
#   release_version.py set NEXT -> update-changelog.py NEXT -> source-lock.sh
# but source-lock.sh refuses ANY dirty tree by design (the lock records the
# commit the source was built from, so that commit must already exist), so
# every formal release died inside its own helper.
#
# This test runs on a throwaway repository and locks in the corrected
# discipline:
#   1. OLD ORDER (version bump on the worktree, then source-lock) MUST fail.
#   2. NEW ORDER (version bump committed as source candidate A, then
#      source-lock on the clean tree, then an evidence-only commit B) MUST
#      pass, the lock must name A, B must contain only evidence files, and
#      the release tag must be able to point at A rather than B.
#   3. The evidence whitelist must reject any source change in B.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d "${TMPDIR:-/tmp}/lumen-release-order-XXXXXX")"
trap 'rm -rf "$TMP"' EXIT
FIXTURE="$TMP/repo"

# ---------------------------------------------------------------------------
# Fixture: a minimal monorepo that source-lock.sh can run against.
# ---------------------------------------------------------------------------
mkdir -p \
  "$FIXTURE/scripts" \
  "$FIXTURE/docs/masterplan" \
  "$FIXTURE/agent/crates/codegen/xai-grok-shell-base/src/util" \
  "$FIXTURE/agent/crates/codegen/xai-grok-models" \
  "$FIXTURE/agent/crates/codegen/lumen-guard/src" \
  "$FIXTURE/agent/crates/codegen/lumen-discipline/src"

cp \
  "$ROOT/scripts/check-version-consistency.sh" \
  "$ROOT/scripts/invalidate-release-readiness.py" \
  "$ROOT/scripts/release_version.py" \
  "$ROOT/scripts/source-lock.sh" \
  "$ROOT/scripts/update-changelog.py" \
  "$FIXTURE/scripts/"

# Every critical path source-lock.sh requires must exist. Content is
# recomputed by the lock itself, so placeholders suffice for the ordering
# invariant (the hashes are recorded, not pre-validated).
critical_placeholder_files=(
  "$FIXTURE/.gitleaksignore"
  "$FIXTURE/agent/crates/codegen/xai-grok-shell-base/src/util/event_id.rs"
  "$FIXTURE/agent/crates/codegen/xai-grok-models/default_models.json"
  "$FIXTURE/agent/crates/codegen/lumen-guard/src/lib.rs"
  "$FIXTURE/agent/crates/codegen/lumen-discipline/src/lib.rs"
  "$FIXTURE/scripts/assert-defaults.sh"
  "$FIXTURE/scripts/check-binary-tuple.sh"
  "$FIXTURE/scripts/eval-coding.sh"
  "$FIXTURE/scripts/eval-coding-live.sh"
  "$FIXTURE/scripts/generate-sbom.sh"
  "$FIXTURE/scripts/install-local.sh"
  "$FIXTURE/scripts/onboarding-gate.sh"
  "$FIXTURE/scripts/productivity-gate.sh"
  "$FIXTURE/scripts/reconcile-evidence.sh"
  "$FIXTURE/scripts/smoke-deepseek.sh"
  "$FIXTURE/scripts/smoke-deepseek-agent.sh"
  "$FIXTURE/scripts/smoke-deepseek-l2.sh"
  "$FIXTURE/scripts/smoke-deepseek-l3.sh"
  "$FIXTURE/scripts/smoke-deepseek-l4.sh"
  "$FIXTURE/scripts/smoke-deepseek-l5.sh"
  "$FIXTURE/scripts/smoke-r0-min.sh"
  "$FIXTURE/scripts/smoke-r0.sh"
  "$FIXTURE/scripts/smoke-verify.sh"
  "$FIXTURE/scripts/test-readiness-contract.sh"
  "$FIXTURE/scripts/test-onboarding-gate.sh"
  "$FIXTURE/scripts/verify-readiness.sh"
  "$FIXTURE/scripts/probe-local.sh"
  "$FIXTURE/scripts/doctor-verticals.sh"
  "$FIXTURE/docs/LUMEN-NEXTGEN-EXECUTION-BOOK-2026-08-01.md"
  "$FIXTURE/docs/masterplan/M5-onboarding-evidence.template.json"
  "$FIXTURE/docs/masterplan/00A-来源锁与运行合同.md"
)
for path in "${critical_placeholder_files[@]}"; do
  printf 'fixture placeholder\n' >"$path"
done

printf '1.2.3-alpha.4\n' >"$FIXTURE/VERSION"

packages=(
  xai-grok-version xai-grok-pager xai-grok-pager-bin xai-grok-shell xai-grok-tools
  xai-grok-tools-api xai-grok-update xai-grok-workspace
)
for package in "${packages[@]}"; do
  mkdir -p "$FIXTURE/agent/crates/codegen/$package"
  version='1.2.3-alpha.4'
  # Upstream protocol/client identity is independent from the Lumen release.
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

# ---------------------------------------------------------------------------
# Part 1 — the old order is locked as a failure: a version bump on the
# worktree followed by source-lock.sh on that dirty tree must be rejected.
# ---------------------------------------------------------------------------
python3 "$FIXTURE/scripts/release_version.py" --root "$FIXTURE" set 1.2.3 >/dev/null
python3 "$FIXTURE/scripts/update-changelog.py" --root "$FIXTURE" --date 2026-07-18 1.2.3 >/dev/null
if "$FIXTURE/scripts/source-lock.sh" >"$TMP/old-order.out" 2>&1; then
  echo "FAIL: old order (version bump before source-lock on a dirty tree) unexpectedly succeeded" >&2
  exit 1
fi
grep -Fq 'refuses a dirty source tree' "$TMP/old-order.out"
echo "OK: old order rejected — source-lock refuses a dirty tree after the version bump"

# ---------------------------------------------------------------------------
# Part 2 — the fixed order passes on the same fixture.
# ---------------------------------------------------------------------------
git -C "$FIXTURE" reset --hard -q
git -C "$FIXTURE" clean -fdx -q
python3 "$FIXTURE/scripts/release_version.py" --root "$FIXTURE" set 1.2.3 >/dev/null
python3 "$FIXTURE/scripts/update-changelog.py" --root "$FIXTURE" --date 2026-07-18 1.2.3 >/dev/null
python3 "$FIXTURE/scripts/release_version.py" --root "$FIXTURE" check >/dev/null

# Phase A: commit the source candidate on explicit paths, then require a
# clean tree (source-lock's precondition).
SOURCE_PATHS=(
  VERSION
  CHANGELOG.md
  agent/Cargo.lock
  agent/crates/codegen/xai-grok-pager/Cargo.toml
  agent/crates/codegen/xai-grok-pager-bin/Cargo.toml
  agent/crates/codegen/xai-grok-shell/Cargo.toml
  agent/crates/codegen/xai-grok-tools/Cargo.toml
  agent/crates/codegen/xai-grok-tools-api/Cargo.toml
  agent/crates/codegen/xai-grok-update/Cargo.toml
  agent/crates/codegen/xai-grok-workspace/Cargo.toml
)
git -C "$FIXTURE" add -- "${SOURCE_PATHS[@]}"
git -C "$FIXTURE" diff --cached --quiet && {
  echo "FAIL: version bump produced no staged changes" >&2
  exit 1
}
git -C "$FIXTURE" commit -qm 'chore(release): prepare v1.2.3 source candidate'
SOURCE_COMMIT="$(git -C "$FIXTURE" rev-parse HEAD)"
if [[ -n "$(git -C "$FIXTURE" status --porcelain --untracked-files=all)" ]]; then
  echo "FAIL: source candidate left a dirty tree before source-lock" >&2
  exit 1
fi

# Phase B: source-lock on the clean tree, then readiness evidence, then an
# evidence-only commit.
"$FIXTURE/scripts/source-lock.sh" >"$TMP/fixed-order.out" 2>&1
grep -Fq 'OK: wrote SOURCE_LOCK.json' "$TMP/fixed-order.out"
python3 - "$FIXTURE" "$SOURCE_COMMIT" <<'PY'
import json
import sys
from pathlib import Path

root = Path(sys.argv[1])
source = sys.argv[2]
lock = json.loads((root / "SOURCE_LOCK.json").read_text())
assert lock["monorepo"]["git_head"] == source, (
    f"lock must name source commit A ({source}), got {lock['monorepo']['git_head']}"
)
assert lock["lumen_version"] == "1.2.3", lock.get("lumen_version")
print("OK: lock names source commit A and the bumped version")
PY
python3 "$FIXTURE/scripts/invalidate-release-readiness.py" --root "$FIXTURE"
"$FIXTURE/scripts/check-version-consistency.sh" "$FIXTURE" >/dev/null

EVIDENCE_PATHS=(
  SOURCE_LOCK.json
  artifacts/readiness/engineering_complete.json
  artifacts/readiness/status.json
)
git -C "$FIXTURE" add -- "${EVIDENCE_PATHS[@]}"
git -C "$FIXTURE" commit -qm 'chore(release): evidence for v1.2.3'
EVIDENCE_COMMIT="$(git -C "$FIXTURE" rev-parse HEAD)"

# B must be a direct child of A and carry only evidence files.
[[ "$(git -C "$FIXTURE" rev-parse "$EVIDENCE_COMMIT^")" == "$SOURCE_COMMIT" ]] || {
  echo "FAIL: evidence commit B is not a direct child of source commit A" >&2
  exit 1
}
suffix="$(git -C "$FIXTURE" diff --name-only "$SOURCE_COMMIT".."$EVIDENCE_COMMIT" | sort)"
expected="artifacts/readiness/engineering_complete.json
artifacts/readiness/status.json
SOURCE_LOCK.json"
[[ "$suffix" == "$expected" ]] || {
  echo "FAIL: evidence suffix B contains unexpected paths:" >&2
  printf '%s\n' "$suffix" >&2
  exit 1
}
# Verifier shape (install-local.sh / verify-readiness contract): the lock is
# an ancestor of HEAD and every intervening change is evidence-only.
git -C "$FIXTURE" merge-base --is-ancestor "$SOURCE_COMMIT" HEAD || {
  echo "FAIL: locked source A is not an ancestor of HEAD" >&2
  exit 1
}
echo "OK: evidence suffix B is a direct, evidence-only child of A"

# The release tag must be able to name A (the build source), never B.
git -C "$FIXTURE" tag -a v1.2.3 -m "Lumen v1.2.3" "$SOURCE_COMMIT"
[[ "$(git -C "$FIXTURE" rev-parse v1.2.3^{commit})" == "$SOURCE_COMMIT" ]] || {
  echo "FAIL: tag v1.2.3 does not peel to source commit A" >&2
  exit 1
}
[[ "$SOURCE_COMMIT" != "$EVIDENCE_COMMIT" ]] || {
  echo "FAIL: source commit and evidence commit are the same object" >&2
  exit 1
}
echo "OK: release tag v1.2.3 points at source commit A, not evidence commit B"

# ---------------------------------------------------------------------------
# Part 3 — the evidence whitelist (release.sh Phase B guard) must reject any
# source change trying to sneak into the evidence commit.
# ---------------------------------------------------------------------------
reject_if_not_evidence() {
  local changed_path="$1"
  case "$changed_path" in
    SOURCE_LOCK.json|\
    artifacts/readiness/engineering_complete.json|\
    artifacts/readiness/status.json) return 0 ;;
    *) return 1 ;;
  esac
}
for allowed in SOURCE_LOCK.json artifacts/readiness/engineering_complete.json artifacts/readiness/status.json; do
  reject_if_not_evidence "$allowed" || {
    echo "FAIL: evidence whitelist rejected allowed path $allowed" >&2
    exit 1
  }
done
for smuggled in VERSION CHANGELOG.md agent/Cargo.lock agent/crates/codegen/xai-grok-pager/Cargo.toml; do
  if reject_if_not_evidence "$smuggled"; then
    echo "FAIL: evidence whitelist accepted a source path: $smuggled" >&2
    exit 1
  fi
done
echo "OK: evidence whitelist accepts lock/readiness files and rejects source changes"

echo "OK: release ordering regression suite passed"
