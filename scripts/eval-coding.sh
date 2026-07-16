#!/usr/bin/env bash
# eval-coding.sh — Coding eval harness for ≥20 tasks (Masterplan M4).
# Usage:
#   ./scripts/eval-coding.sh                    # verify all broken tasks (harness-only)
#   ./scripts/eval-coding.sh --model deepseek   # run agent on each task (requires API key)
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TASKS="$ROOT/evals/tasks"

PASS=0
FAIL=0
SKIP=0
TOTAL=0

echo "=== lumen coding eval harness ==="
echo "Tasks dir: $TASKS"
echo ""

for task_dir in "$TASKS"/*/; do
    name=$(basename "$task_dir")
    ws="$task_dir/workspace"
    TOTAL=$((TOTAL + 1))

    if [ ! -d "$ws" ]; then
        echo "  SKIP $name: no workspace/"
        SKIP=$((SKIP + 1))
        continue
    fi

    # Detect language and run appropriate test command
    passed=false
    if [ -f "$ws/go.mod" ]; then
        out=$(cd "$ws" && timeout 30 go test ./... 2>&1) && out_ok=true || out_ok=false
        if $out_ok; then
            echo "  PASS $name (go test)"
            PASS=$((PASS + 1))
            passed=true
        else
            # Expected: tests should FAIL on broken workspace
            if echo "$out" | grep -q "FAIL"; then
                echo "  BROKEN $name (go test) — OK: harness detects failure"
                FAIL=$((FAIL + 1))
            else
                echo "  BUILD-ERR $name (go test) — build error: $(echo "$out" | head -1)"
                FAIL=$((FAIL + 1))
            fi
        fi
    elif ls "$ws"/*.py >/dev/null 2>&1 || [ -f "$ws/pytest.ini" ]; then
        if command -v pytest &>/dev/null; then
            out=$(cd "$ws" && python3 -m pytest -q 2>&1) && out_ok=true || out_ok=false
            if $out_ok; then
                echo "  PASS $name (pytest)"
                PASS=$((PASS + 1))
                passed=true
            else
                echo "  BROKEN $name (pytest) — OK: harness detects failure"
                FAIL=$((FAIL + 1))
            fi
        else
            echo "  SKIP $name (pytest not found)"
            SKIP=$((SKIP + 1))
        fi
    elif [ -f "$ws/package.json" ]; then
        if command -v npx &>/dev/null; then
            out=$(cd "$ws" && npx vitest run 2>&1) && out_ok=true || out_ok=false
            if $out_ok; then
                echo "  PASS $name (vitest)"
                PASS=$((PASS + 1))
                passed=true
            else
                echo "  BROKEN $name (vitest) — OK: harness detects failure"
                FAIL=$((FAIL + 1))
            fi
        else
            echo "  SKIP $name (npx not found)"
            SKIP=$((SKIP + 1))
        fi
    else
        echo "  SKIP $name: unknown project type"
        SKIP=$((SKIP + 1))
    fi
done

echo ""
echo "=== Results ==="
echo "Total: $TOTAL"
echo "BROKEN (expected): $FAIL"
echo "Pass (unexpected fix): $PASS"
echo "Skipped: $SKIP"

# All broken tasks should show as BROKEN — that proves the harness works.
# If any task PASSes, the workspace may already be fixed (shouldn't happen).
if [ "$PASS" -gt 0 ]; then
    echo "WARNING: $PASS tasks passed — workspaces may already be fixed."
fi

echo "Harness verification complete."
exit 0
