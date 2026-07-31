#!/bin/bash
# sync-upstream.sh — absorb the latest xai-org/grok-build into lumen.
#
# Mechanism (see IMPORT_LEDGER.md / SOURCE_LOCK.json for the policy):
#   lumen's history was imported from the pre-publication monorepo snapshot,
#   so upstream and lumen share NO git ancestry. A local graft makes the
#   lumen root commit a child of the public "Publish" commit, and a mirror
#   branch materializes upstream's tree under agent/ so a real 3-way merge
#   can run with a meaningful base (merge-base == upstream publish).
#
# Usage:
#   scripts/sync-upstream.sh            # fetch + mirror + merge into current branch
#   scripts/sync-upstream.sh --fetch    # only fetch + update the mirror
#   scripts/sync-upstream.sh --abort    # abort an in-progress merge (git merge --abort)
#
# Resolution policy after conflicts appear (per SYNC notes):
#   - red zone (session/acp_session_impl, mvp_agent, sampler/stream, chat-state):
#     upstream wins the architecture; lumen behavior deltas (DeepSeek BYOK,
#     lumen-discipline/guard/verify hooks, expert, science commands, cache
#     epochs) are re-applied as patches on top.
#   - everything else: upstream wins unless the ours-side block carries
#     lumen-specific logic (deepseek / env_key / base_url / lumen-* / fail-closed).
#   - Cargo.lock: never hand-merge; take one side and run `cargo check` to
#     re-resolve.
set -euo pipefail
cd "$(dirname "$0")/.."

MIRROR_BRANCH="mirror/upstream-main"
UPSTREAM_REMOTE="upstream"

echo "==> fetch $UPSTREAM_REMOTE"
git fetch "$UPSTREAM_REMOTE" --prune

echo "==> updating mirror branch $MIRROR_BRANCH"
CUR_MIRROR=$(git rev-parse "$MIRROR_BRANCH" 2>/dev/null || echo none)
git checkout -q "$MIRROR_BRANCH" 2>/dev/null || git checkout -q -b "$MIRROR_BRANCH"
# materialize upstream main under agent/ with a fresh index
TMP_INDEX=$(mktemp)
export GIT_INDEX_FILE="$TMP_INDEX"
git read-tree --empty
git read-tree --prefix=agent/ "$UPSTREAM_REMOTE/main"
TREE=$(git write-tree)
NEW_MIRROR=$(git commit-tree "$TREE" -p "$CUR_MIRROR" -m "mirror: upstream $UPSTREAM_REMOTE/main ($(git rev-parse --short $UPSTREAM_REMOTE/main)) at agent/ layout")
unset GIT_INDEX_FILE
rm -f "$TMP_INDEX"
git update-ref "refs/heads/$MIRROR_BRANCH" "$NEW_MIRROR"
# keep the graft (mirror commit -> upstream/main) so merge-base stays meaningful
git replace --graft "$NEW_MIRROR" "$UPSTREAM_REMOTE/main" 2>/dev/null || true
git checkout -q -

if [ "${1:-}" = "--fetch" ]; then
    echo "mirror updated: $NEW_MIRROR"
    exit 0
fi

echo "==> merging $MIRROR_BRANCH into $(git branch --show-current)"
git merge "$MIRROR_BRANCH" --no-commit -m "merge: absorb upstream main $(git rev-parse --short $UPSTREAM_REMOTE/main)"

UNMERGED=$(git status --short | grep -cE '^(UU|AA|UD|DU|UA|AU|DD|D) ' || true)
echo ""
echo "==> $UNMERGED paths need resolution."
echo "    Policy: red-zone = upstream floor + lumen behavior patch;"
echo "    elsewhere upstream wins unless ours carries lumen/BYOK logic."
echo "    After resolving: git add <files> && git commit"
