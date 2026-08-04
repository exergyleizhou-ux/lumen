#!/usr/bin/env bash
# Prepare and publish a Lumen release through the tag-triggered GitHub workflow.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VERSION_TOOL="$ROOT/scripts/release_version.py"
CHANGELOG_TOOL="$ROOT/scripts/update-changelog.py"
RELEASE_BRANCH="${LUMEN_RELEASE_BRANCH:-main}"
REMOTE="${LUMEN_RELEASE_REMOTE:-origin}"
DRY_RUN=0
NO_PUSH=0
UNSIGNED_TAG=0

usage() {
  cat <<'EOF'
Usage: scripts/release.sh [--dry-run] [--no-push] [--unsigned-tag] BUMP

BUMP is patch, minor, major, prerelease, or an explicit SemVer (with optional v).

  --dry-run       Validate version state and print the next version only.
  --no-push       Prepare the release commit and tag without pushing them.
  --unsigned-tag  Create an annotated tag. Allowed only together with --no-push.

Formal releases create a signed tag and atomically push the release commit and
tag to origin. The tag triggers .github/workflows/release.yml, which builds,
checksums, signs, and publishes all four native artifacts.
EOF
}

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

BUMP=""
while (($#)); do
  case "$1" in
    --dry-run) DRY_RUN=1 ;;
    --no-push) NO_PUSH=1 ;;
    --unsigned-tag) UNSIGNED_TAG=1 ;;
    -h|--help) usage; exit 0 ;;
    --*) fail "unknown option: $1" ;;
    *)
      [[ -z "$BUMP" ]] || fail "only one bump may be specified"
      BUMP="$1"
      ;;
  esac
  shift
done
[[ -n "$BUMP" ]] || { usage >&2; exit 2; }
((UNSIGNED_TAG == 0 || NO_PUSH == 1)) || fail "--unsigned-tag is allowed only with --no-push"

command -v python3 >/dev/null || fail "python3 is required"
CURRENT="$(python3 "$VERSION_TOOL" --root "$ROOT" check)"
NEXT="$(python3 "$VERSION_TOOL" --root "$ROOT" next "$BUMP")"
TAG="v$NEXT"
echo "Release plan: $CURRENT -> $NEXT ($TAG)"
if ((DRY_RUN)); then
  exit 0
fi

for command_name in git cargo; do
  command -v "$command_name" >/dev/null || fail "$command_name is required"
done
[[ -z "$(git -C "$ROOT" status --porcelain --untracked-files=all)" ]] \
  || fail "release preparation requires a clean working tree"
BRANCH="$(git -C "$ROOT" symbolic-ref --quiet --short HEAD)" \
  || fail "release preparation requires a branch, not detached HEAD"
[[ "$BRANCH" == "$RELEASE_BRANCH" ]] \
  || fail "release must run on $RELEASE_BRANCH (current branch: $BRANCH)"
git -C "$ROOT" remote get-url "$REMOTE" >/dev/null 2>&1 \
  || fail "missing release remote: $REMOTE"
REMOTE_URL="$(git -C "$ROOT" remote get-url "$REMOTE")"
[[ "$REMOTE_URL" =~ github\.com[:/][^/]+/lumen(\.git)?$ ]] \
  || fail "$REMOTE does not point to a GitHub lumen repository: $REMOTE_URL"
git -C "$ROOT" fetch --prune "$REMOTE" "$RELEASE_BRANCH" --tags
REMOTE_HEAD="$(git -C "$ROOT" rev-parse "$REMOTE/$RELEASE_BRANCH")"
LOCAL_HEAD="$(git -C "$ROOT" rev-parse HEAD)"
[[ "$LOCAL_HEAD" == "$REMOTE_HEAD" ]] \
  || fail "HEAD must exactly match $REMOTE/$RELEASE_BRANCH before release"
if git -C "$ROOT" show-ref --verify --quiet "refs/tags/$TAG" \
  || git -C "$ROOT" ls-remote --exit-code --tags "$REMOTE" "refs/tags/$TAG" >/dev/null 2>&1; then
  fail "release tag already exists: $TAG"
fi

# ── Phase A: clean source candidate (HEAD = A) ───────────────────────────────
# source-lock.sh refuses a dirty tree by design: the lock records the commit
# the source was built from, so that commit must already exist. The version/
# changelog/Cargo bump therefore becomes its own explicit-paths commit FIRST;
# the source lock and readiness evidence are collected on that clean tree and
# committed as an evidence-only suffix (B) afterwards. The release tag always
# points at A (the build source), never at B.
if [[ "$CURRENT" != "$NEXT" ]]; then
  python3 "$VERSION_TOOL" --root "$ROOT" set "$NEXT" >/dev/null
  python3 "$CHANGELOG_TOOL" --root "$ROOT" "$NEXT"
  python3 "$VERSION_TOOL" --root "$ROOT" check >/dev/null
  git -C "$ROOT" diff --check
  (cd "$ROOT/agent" && cargo check --locked --package xai-grok-pager-bin --features release-dist)

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
  while IFS= read -r changed_path; do
    case "$changed_path" in
      VERSION|CHANGELOG.md|agent/Cargo.lock|\
      agent/crates/codegen/xai-grok-pager/Cargo.toml|\
      agent/crates/codegen/xai-grok-pager-bin/Cargo.toml|\
      agent/crates/codegen/xai-grok-shell/Cargo.toml|\
      agent/crates/codegen/xai-grok-tools/Cargo.toml|\
      agent/crates/codegen/xai-grok-tools-api/Cargo.toml|\
      agent/crates/codegen/xai-grok-update/Cargo.toml|\
      agent/crates/codegen/xai-grok-workspace/Cargo.toml) ;;
      *) fail "release source candidate changed an unexpected path: $changed_path" ;;
    esac
  done < <(
    {
      git -C "$ROOT" diff --name-only
      git -C "$ROOT" ls-files --others --exclude-standard
    } | sort -u
  )
  git -C "$ROOT" add -- "${SOURCE_PATHS[@]}"
  git -C "$ROOT" diff --cached --quiet && fail "version bump produced no staged changes"
  git -C "$ROOT" commit -m "chore(release): prepare $TAG source candidate"
else
  # Re-release after a failed workflow: the version is already bumped on main.
  # Skip the bump commit; current HEAD is the source candidate A.
  echo "note: version already at $NEXT; reusing current HEAD as source candidate"
  git -C "$ROOT" diff --check
fi
SOURCE_COMMIT="$(git -C "$ROOT" rev-parse HEAD)"
[[ -z "$(git -C "$ROOT" status --porcelain --untracked-files=all)" ]] \
  || fail "source candidate must leave a clean tree for source-lock"

# Thrash-safe source-lock records the *installed binary stamp*, not HEAD.
# Rebuild+install the source candidate so the lock names A (never a stale
# pre-bump stamp, and never an evidence-only HEAD).
echo "Building and installing release source candidate $SOURCE_COMMIT..."
"$ROOT/scripts/install-local.sh" \
  || fail "install-local failed for source candidate $SOURCE_COMMIT"
INSTALLED_VER="$("$HOME/.local/bin/lumen" --version 2>/dev/null || true)"
case "$INSTALLED_VER" in
  *"($(git -C "$ROOT" rev-parse --short "$SOURCE_COMMIT"))"*) ;;
  *)
    # Short length may widen; prefix-match full sha via extracted stamp.
    STAMP="$(printf '%s' "$INSTALLED_VER" | sed -nE 's/.*\(([0-9a-f]{7,40})\).*/\1/p')"
    [[ -n "$STAMP" && "$SOURCE_COMMIT" == "$STAMP"* ]] \
      || fail "installed binary not stamped with source A: $INSTALLED_VER (want $SOURCE_COMMIT)"
    ;;
esac

# ── Phase B: evidence-only suffix on clean A ─────────────────────────────────
# The lock names A; readiness evidence is regenerated for the bumped version;
# the only files that may change now are lock/readiness evidence, and B must
# never carry source, version, Cargo, or runtime changes.
"$ROOT/scripts/source-lock.sh"
python3 "$ROOT/scripts/invalidate-release-readiness.py" --root "$ROOT"
"$ROOT/scripts/check-version-consistency.sh" "$ROOT"
git -C "$ROOT" diff --check
"$ROOT/scripts/test-release-prep.sh"
RUSTUP_HOME="${RUSTUP_HOME:-$HOME/.rustup}" \
  CARGO_HOME="${CARGO_HOME:-$HOME/.cargo}" \
  "$ROOT/scripts/test-release-contract.sh"

EVIDENCE_PATHS=(
  SOURCE_LOCK.json
  artifacts/readiness/engineering_complete.json
  artifacts/readiness/status.json
)
while IFS= read -r changed_path; do
  case "$changed_path" in
    SOURCE_LOCK.json|\
    artifacts/readiness/engineering_complete.json|\
    artifacts/readiness/status.json) ;;
    *) fail "release evidence changed an unexpected path: $changed_path" ;;
  esac
done < <(
  {
    git -C "$ROOT" diff --name-only
    git -C "$ROOT" ls-files --others --exclude-standard
  } | sort -u
)
git -C "$ROOT" add -- "${EVIDENCE_PATHS[@]}"
git -C "$ROOT" diff --cached --quiet && fail "release evidence produced no staged changes"
git -C "$ROOT" commit -m "chore(release): evidence for $TAG"

# ── Tag: names the build source A, never the evidence suffix B ──────────────
if ((UNSIGNED_TAG)); then
  git -C "$ROOT" tag -a "$TAG" -m "Lumen $TAG" "$SOURCE_COMMIT"
else
  git -C "$ROOT" tag -s "$TAG" -m "Lumen $TAG" "$SOURCE_COMMIT"
  git -C "$ROOT" tag -v "$TAG"
fi
TAGGED_COMMIT="$(git -C "$ROOT" rev-parse "$TAG^{commit}")"
[[ "$TAGGED_COMMIT" == "$SOURCE_COMMIT" ]] \
  || fail "release tag $TAG peeled to $TAGGED_COMMIT, not source commit $SOURCE_COMMIT"

if ((NO_PUSH)); then
  echo "OK: prepared $TAG locally; no remote changes were made"
  exit 0
fi
git -C "$ROOT" push --atomic "$REMOTE" "HEAD:$RELEASE_BRANCH" "refs/tags/$TAG"
echo "OK: pushed $TAG; GitHub Actions will build and publish the four-platform release"
