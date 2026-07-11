#!/usr/bin/env bash
# Release evidence: capture health, git rev, date for audit trail.
# Usage: ./scripts/science/lab-release-evidence.sh [base_url]
set -euo pipefail

BASE="${1:-https://demo.oasisdata2026.xyz/lumen-lab}"
STAMP="$(date -u +%Y%m%d-%H%M%S)"
REPO="$(cd "$(dirname "$0")/../.." && pwd)"
EVIDENCE_DIR="${HOME}/.lumen/reports/lab-release-$STAMP"

mkdir -p "$EVIDENCE_DIR"

echo "[evidence] capturing to $EVIDENCE_DIR ..."

# Health snapshot
curl -sS "$BASE/api/lab/health" | python3 -m json.tool > "$EVIDENCE_DIR/health.json" 2>/dev/null || echo '{}' > "$EVIDENCE_DIR/health.json"

# Git rev
(cd "$REPO" && git rev-parse HEAD) > "$EVIDENCE_DIR/git-rev.txt"
(cd "$REPO" && git log --oneline -5) > "$EVIDENCE_DIR/git-log.txt"
(cd "$REPO" && cat VERSION) > "$EVIDENCE_DIR/version.txt"

# Date
date -u > "$EVIDENCE_DIR/timestamp.txt"

echo "[evidence] done: $(ls "$EVIDENCE_DIR")"
