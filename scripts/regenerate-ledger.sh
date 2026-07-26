#!/usr/bin/env bash
# regenerate-ledger.sh — auto-generate CURRENT_STATE_LEDGER.md from live git state.
# Called by CI on every push to main (fetch-depth: 0 required so origin/* refs exist).
# Content is deliberately timestamp-free and HEAD-free so the CI "commit if
# changed" step only commits when a fact actually changed (no bot noise).
set -euo pipefail

REPO="${1:-$(git rev-parse --show-toplevel)}"
cd "$REPO"

WORKTREE_COUNT=$(git worktree list --porcelain | grep -c '^worktree ' || echo 0)

# Branch inventory: remote branches on origin (the shared truth), not local
# branches — in a CI checkout only main exists locally, which used to make
# this table permanently empty.
MAIN_REF="origin/main"
git rev-parse --verify -q "$MAIN_REF" >/dev/null || MAIN_REF="main"

BRANCH_TOTAL=0
NOT_IN_MAIN=0
BRANCH_TABLE=""
while IFS= read -r b; do
  case "$b" in
    ""|origin|origin/HEAD*|origin/main) continue ;;
  esac
  BRANCH_TOTAL=$((BRANCH_TOTAL + 1))
  if git merge-base --is-ancestor "$b" "$MAIN_REF" 2>/dev/null; then
    :
  else
    NOT_IN_MAIN=$((NOT_IN_MAIN + 1))
    head_short=$(git rev-parse --short "$b" 2>/dev/null || echo "N/A")
    ahead=$(git rev-list --count "$MAIN_REF".."$b" 2>/dev/null || echo "?")
    BRANCH_TABLE="${BRANCH_TABLE}| $b | $head_short | $ahead |\n"
  fi
done < <(git branch -r --format='%(refname:short)' 2>/dev/null)
IN_MAIN=$((BRANCH_TOTAL - NOT_IN_MAIN))

VERSION_STR=$(tr -d ' \n' < VERSION 2>/dev/null || echo "unknown")

READINESS_LINE="no artifacts/readiness/status.json"
if [ -f artifacts/readiness/status.json ]; then
  READINESS_LINE=$(python3 - <<'PY' 2>/dev/null || echo "status.json unreadable"
import json
s = json.load(open('artifacts/readiness/status.json'))
state = s.get("state", "?")
ready = s.get("ready")
blockers = s.get("blockers") or []
eng = s.get("engineering_complete")
print(f"state={state} ready={ready} engineering_complete={eng} blockers={len(blockers)}")
PY
)
fi

LAST_HUMAN_COMMIT=$(git log --format='%h %s' --author-date-order \
  --invert-grep --grep='auto-regenerate CURRENT_STATE_LEDGER' -1 2>/dev/null || echo "?")

cat > CURRENT_STATE_LEDGER.md << EOF
# Lumen Current State Ledger

**Auto-generated** by CI \`regenerate-ledger.sh\` on push to main. All facts
below are computed from git and readiness artifacts — nothing is hand-written.
For the generation date, see the last ledger commit in \`git log\`.

---

## ⚡ Executive Summary

| Question | Answer |
|---|---|
| Version (root VERSION) | **$VERSION_STR** |
| Readiness | $READINESS_LINE |
| Active git worktrees (this checkout) | $WORKTREE_COUNT |
| Remote branches on origin (excl. main) | **$BRANCH_TOTAL** |
| … fully merged into main | $IN_MAIN |
| … NOT in main | **$NOT_IN_MAIN** |
| Last non-bot commit | $LAST_HUMAN_COMMIT |

---

## Remote Branches NOT in Main

| Branch | HEAD | Commits ahead |
|---|---|---|
$(echo -e "$BRANCH_TABLE")

Branches listed here either carry unmerged work or are stale (e.g. the
archived 2026-06 Go-era branches). See docs/go-era-branch-map.md for the
Go-branch → Rust-backlog mapping.

---

*This file is auto-generated. Do not edit manually.*
EOF

echo "Ledger regenerated: version=$VERSION_STR branches=$BRANCH_TOTAL not_in_main=$NOT_IN_MAIN"
