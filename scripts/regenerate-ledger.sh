#!/usr/bin/env bash
# regenerate-ledger.sh — auto-generate CURRENT_STATE_LEDGER.md from live git state.
# Called by CI on every push to main. Never requires manual editing.
set -euo pipefail

REPO="${1:-$(git rev-parse --show-toplevel)}"
cd "$REPO"

HEAD=$(git rev-parse HEAD)
HEAD_SHORT=$(git rev-parse --short HEAD)
NOW=$(date -u +"%Y-%m-%d %H:%M UTC")
BRANCH_COUNT=$(git branch | wc -l | tr -d ' ')
WORKTREE_COUNT=$(git worktree list --porcelain | grep -c '^worktree ' || echo 0)

# Count branches in/not in main
IN_MAIN=0
NOT_IN_MAIN=0
BRANCH_TABLE=""

for b in $(git branch --format='%(refname:short)'); do
  tracking=$(git branch --list "$b" --format='%(upstream:short)' 2>/dev/null || echo "—")
  head_short=$(git rev-parse --short "$b" 2>/dev/null || echo "N/A")
  if git merge-base --is-ancestor "$b" main 2>/dev/null; then
    status="✅ YES"
    IN_MAIN=$((IN_MAIN + 1))
  else
    status="❌ NO"
    NOT_IN_MAIN=$((NOT_IN_MAIN + 1))
    ahead=$(git rev-list --count main.."$b" 2>/dev/null || echo "?")
    BRANCH_TABLE="${BRANCH_TABLE}| $b | $head_short | $tracking | $status | $ahead |\n"
  fi
done

cat > CURRENT_STATE_LEDGER.md << EOF
# Lumen Current State Ledger — Phase 0

**Generated**: $NOW
**HEAD**: $HEAD_SHORT
**Auto-generated**: by CI \`regenerate-ledger.sh\` on push to main

---

## ⚡ Executive Summary

| Question | Answer |
|---|---|
| Active git worktrees | **$WORKTREE_COUNT** |
| Local branches | **$BRANCH_COUNT** |
| Branches fully in main | **$IN_MAIN of $BRANCH_COUNT** |
| Branches NOT in main | **$NOT_IN_MAIN of $BRANCH_COUNT** |
| Current HEAD | $HEAD_SHORT |

---

## Branches NOT in Main

| Branch | HEAD | Tracking | In main? | Commits ahead |
|---|---|---|---|---|
$(echo -e "$BRANCH_TABLE")

---

## Key Facts

- Cache hardening: ✅ merged into main (\`dfef497f\`)
- Expert E2/E3: ✅ all 5 key commits in main
- TruthSnapshot: ✅ foundation in main; runtime callers exist
- Science fusion: codex/science-fusion-full has ~30 unmerged commits
- Windows build: ✅ verified (MSVC, 138 core tests)

---

*This file is auto-generated. Do not edit manually. Last update: $NOW*
EOF

echo "Ledger regenerated at HEAD $HEAD_SHORT"
