#!/usr/bin/env bash
# Master /goal verification — coding agent evidence + science full-verify + security gates.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
export GOTOOLCHAIN=local
export LUMEN_REPO_ROOT="$ROOT"

SCRATCH="${SCRATCH:-$ROOT/.goal-verify-scratch}"
mkdir -p "$SCRATCH"
FAIL=0

echo "▸ goal-all-verify scratch=$SCRATCH"

run_gate() {
  local name="$1"
  shift
  if ! "$@" > "$SCRATCH/${name}.log" 2>&1; then
    echo "✗ ${name} failed — see $SCRATCH/${name}.log" >&2
    FAIL=1
  else
    echo "✓ ${name}"
  fi
}

echo "▸ build bin/lumen"
CGO_ENABLED=0 go build -o "$SCRATCH/lumen" ./cmd/lumen

echo "▸ goal evidence (AC1 dogfood + AC3 eval baseline6)"
export LUMEN_GOAL_SCRATCH="$SCRATCH"
if ! CGO_ENABLED=0 go test -count=1 -timeout 180s -run '^TestGoalEvidence$' ./cmd/lumen -v > "$SCRATCH/goal-evidence.log" 2>&1; then
  echo "✗ TestGoalEvidence failed" >&2
  FAIL=1
else
  echo "✓ TestGoalEvidence"
fi

run_gate bash-sandbox go test -count=1 -v ./internal/tool/builtin/ -run 'BashSandbox'
run_gate mcp-injection go test -count=1 -v ./internal/tool/builtin/ -run 'MCP|Untrusted|mcp|WrapMCP'
run_gate sqlite-session env LUMEN_SQLITE_STORE=on go test -count=1 -v ./internal/lumenstore/... ./internal/audit/... ./internal/agent/... -run 'SQLite|Migrate|SessionMessages|MutationMatrix|DualWrite|AutoMigrate|DropLast|Concurrent|CompactRewrites|CompactSQLite|EmptyJSONL|UnreadableJSONL|ReconcileLoad'
run_gate provider-live go test -count=1 -short ./internal/provider/anthro/... ./internal/provider/gemini/... -run 'Live|Smoke'
run_gate eval-struct go test -count=1 ./internal/eval/... -run 'WellFormed|Integration'

echo "▸ eval task inventory"
TASKS_N=$("$SCRATCH/lumen" eval -list 2>/dev/null | head -1 | awk '{print $1}')
BASE_N=$("$SCRATCH/lumen" eval -tasks evals/baseline6 -list 2>/dev/null | head -1 | awk '{print $1}')
{
  echo "tasks=$TASKS_N baseline6=$BASE_N"
  "$SCRATCH/lumen" eval -list
  "$SCRATCH/lumen" eval -tasks evals/baseline6 -list
} > "$SCRATCH/eval-list.log" 2>&1 || true
TOTAL=$(( ${TASKS_N:-0} + ${BASE_N:-0} ))
if [[ "$TOTAL" -lt 14 ]]; then
  echo "✗ eval inventory total=$TOTAL want >=14" >&2
  FAIL=1
else
  echo "✓ eval inventory total=$TOTAL"
fi

run_gate branding-grep bash -c 'git grep -i "csswitch\|cc-switch" -- README.md HANDOFF.md CHANGELOG.md docs/ cmd/ internal/science/gui/static/ desktop/ && exit 1 || exit 0'

echo "▸ science full-verify"
if ! SCRATCH="$SCRATCH/science" bash scripts/science/full-verify.sh >> "$SCRATCH/science-full-verify.log" 2>&1; then
  echo "✗ science full-verify failed" >&2
  FAIL=1
else
  echo "✓ science full-verify"
fi

echo "▸ make check"
if ! env -u LUMEN_GOAL_SCRATCH make check > "$SCRATCH/make-check.log" 2>&1; then
  echo "✗ make check failed" >&2
  FAIL=1
else
  echo "✓ make check"
fi

if [[ "$FAIL" -ne 0 ]]; then
  echo "✗ goal-all-verify FAIL" >&2
  exit 1
fi
echo "✓ goal-all-verify PASS"