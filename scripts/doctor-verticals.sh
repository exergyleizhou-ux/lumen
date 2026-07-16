#!/usr/bin/env bash
# doctor-verticals.sh — verify all three vertical packs exist and have entry points.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OK=0
ERR=0

echo "=== Lumen vertical packs doctor ==="
echo ""

check_pack() {
    local name="$1"
    local dir="$ROOT/packs/$name"
    if [ -d "$dir" ]; then
        echo "  ✅ packs/$name/ exists"
        if [ -f "$dir/README.md" ]; then
            echo "     README.md present"
        else
            echo "     ⚠️  README.md missing"
        fi
        if ls "$dir"/*.go >/dev/null 2>&1; then
            ngo=$(ls "$dir"/*.go 2>/dev/null | wc -l | tr -d ' ')
            echo "     $ngo Go source files"
            OK=$((OK + 1))
        else
            echo "     ⚠️  No Go source files"
        fi
    else
        echo "  ❌ packs/$name/ missing"
        ERR=$((ERR + 1))
    fi
}

check_pack science
check_pack oasis
check_pack quant

echo ""
echo "=== Result ==="
echo "OK: $OK"
echo "Errors: $ERR"

if [ "$ERR" -gt 0 ]; then
    echo "FAIL: $ERR pack(s) missing."
    exit 1
fi

echo "All vertical packs present."
exit 0
