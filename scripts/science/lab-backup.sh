#!/usr/bin/env bash
# Backup Lab projects tree (config + workspaces). Offline-friendly.
# Usage: ./scripts/science/lab-backup.sh [dest_dir]
set -euo pipefail

SRC="${LUMEN_SCIENCE_DIR:-$HOME/.lumen/science}"
DEST="${1:-$HOME/.lumen/backups}"
STAMP=$(date -u +%Y%m%dT%H%M%SZ)
OUT="$DEST/science-lab-$STAMP.tgz"

mkdir -p "$DEST"
if [[ ! -d "$SRC" ]]; then
  echo "source missing: $SRC" >&2
  exit 1
fi

tar -czf "$OUT" -C "$(dirname "$SRC")" "$(basename "$SRC")"
echo "wrote $OUT ($(du -h "$OUT" | awk '{print $1}'))"
# keep last 10
ls -1t "$DEST"/science-lab-*.tgz 2>/dev/null | tail -n +11 | xargs -r rm -f
