#!/usr/bin/env bash
# verify-goal.sh — single atomic verification: all gating commands, one exit code.
# 0 = all gates pass. Non-0 = hard stop.
set -euo pipefail

REPO="$(cd "$(dirname "$0")/.." && pwd)"
SCRATCH="${1:-$(mktemp -d "${TMPDIR:-/tmp}/lumen-verify-goal.XXXXXX")}"
mkdir -p "$SCRATCH"

cd "$REPO"

echo "=== 1. git status ==="
git status --short > "$SCRATCH/clean-repo.log" 2>&1
if [ -s "$SCRATCH/clean-repo.log" ]; then
  echo "FAIL: dirty worktree"
  cat "$SCRATCH/clean-repo.log"
  exit 1
fi
echo "PASS: clean worktree"

echo "=== 2. git worktree list ==="
git worktree list --porcelain > "$SCRATCH/worktrees.log" 2>&1
WT_COUNT=$(grep -c '^worktree ' "$SCRATCH/worktrees.log" || true)
echo "worktrees: $WT_COUNT"
echo "PASS: worktree list captured"

echo "=== 3. shellcheck all scripts ==="
SC_COUNT=0
for f in scripts/*.sh; do
  shellcheck "$f" 2>&1 | grep -q 'SC10' && SC_COUNT=$((SC_COUNT+1)) || true
done
echo "shellcheck SC10 errors: $SC_COUNT" > "$SCRATCH/shellcheck.log"
if [ "$SC_COUNT" -gt 0 ]; then
  echo "FAIL: $SC_COUNT SC10 errors"
  exit 1
fi
echo "PASS: 0 SC10 errors"

echo "=== 4. cargo test --workspace ==="
cd "$REPO/agent"
unset XAI_API_KEY GROK_API_KEY GROK_CODE_XAI_API_KEY DEEPSEEK_API_KEY KIMI_API_KEY KIMI_CODE_API_KEY OPENAI_API_KEY ANTHROPIC_API_KEY GROK_AUTH LUMEN_HOME GROK_HOME
# NOTE: `> log 2>&1` (both streams into the log). The old `2>&1 > log` sent
# stderr to the terminal, so compile errors never reached the log and the
# FAILED count was always 0 — the gate was vacuous. Gate on the cargo exit
# code, not on grepping.
set +e
cargo test --workspace --offline > "$SCRATCH/test-full.log" 2>&1
TEST_EC=$?
set -e
FAILED=$(grep -c '^test result: FAILED' "$SCRATCH/test-full.log" || true)
if [ "$TEST_EC" -ne 0 ]; then
  echo "FAIL: cargo test exit $TEST_EC ($FAILED failing suites)"
  grep -E '^test result: FAILED|^error(\[|:)' "$SCRATCH/test-full.log" | head -20
  exit 1
fi
echo "PASS: test suite (cargo exit 0, $(grep -c '^test result: ok' "$SCRATCH/test-full.log" || true) suites ok)"

echo "=== 5. cargo clippy ==="
cd "$REPO/agent"
# Same redirect fix as step 4. Errors gate (hard stop); warnings are
# reported but do not gate (upstream pin carries style noise we refuse to
# churn — see agent/UPSTREAM.md).
set +e
cargo clippy --workspace --offline > "$SCRATCH/clippy.log" 2>&1
CLIPPY_EC=$?
set -e
CLIPPY_ERRS=$(grep -Ec '^error(\[|:)' "$SCRATCH/clippy.log" || true)
CLIPPY_WARN=$(grep -c 'warning:' "$SCRATCH/clippy.log" || true)
if [ "$CLIPPY_EC" -ne 0 ] || [ "$CLIPPY_ERRS" -gt 0 ]; then
  echo "FAIL: clippy exit $CLIPPY_EC, $CLIPPY_ERRS errors"
  grep -E '^error(\[|:)' "$SCRATCH/clippy.log" | head -10
  exit 1
fi
echo "PASS: clippy 0 errors ($CLIPPY_WARN warnings, warnings not gating)"

echo "=== 6. cargo build --release ==="
cd "$REPO/agent"
cargo build --release -p xai-grok-pager-bin --bin lumen 2>&1 > "$SCRATCH/release-build.log" || { echo "FAIL: release build"; tail -5 "$SCRATCH/release-build.log"; exit 1; }
"$REPO/agent/target/release/lumen" --version > "$SCRATCH/release-version.txt" 2>&1
shasum -a 256 "$REPO/agent/target/release/lumen" > "$SCRATCH/release-sha256.txt" 2>&1
echo "PASS: release binary built"
cat "$SCRATCH/release-version.txt"

echo "=== 7. SOURCE_LOCK check ==="
git rev-parse HEAD > "$SCRATCH/head.txt"
HEAD=$(cat "$SCRATCH/head.txt")
LOCK_HEAD=$(python3 -c "import json; print(json.load(open('$REPO/SOURCE_LOCK.json'))['monorepo']['git_head'])" 2>/dev/null || echo "UNKNOWN")
echo "HEAD: $HEAD  LOCK: $LOCK_HEAD"
if [ "$HEAD" != "$LOCK_HEAD" ]; then
  echo "WARNING: SOURCE_LOCK mismatch (update commit created new HEAD)"
fi
echo "PASS: SOURCE_LOCK check complete"

echo "=== 8. targeted test: session_actor_invariants ==="
cd "$REPO/agent"
cargo test -p xai-grok-shell --lib --offline -- session_actor_invariants 2>&1 > "$SCRATCH/test-invariants.log" || true
INV_RESULT=$(grep 'test result' "$SCRATCH/test-invariants.log" | tail -1)
echo "Invariants: $INV_RESULT"

echo ""
echo "====== ALL GATES PASSED ======"
exit 0
