#!/usr/bin/env bash
# Pre-release gate: run all automated tests before deploy.
# Usage: ./scripts/science/pre-release.sh
set -euo pipefail
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$REPO_ROOT"

echo "=== pre-release: go test ==="
go test ./internal/science/lab/... -count=1

echo ""
echo "=== pre-release: labui_test.mjs ==="
node --test internal/science/lab/static/labui_test.mjs

echo ""
echo "=== pre-release: ok ==="
