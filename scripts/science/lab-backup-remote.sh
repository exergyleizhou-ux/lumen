#!/usr/bin/env bash
# Remote backup: pull VPS lab data to local machine.
# Usage: ./scripts/science/lab-backup-remote.sh
set -euo pipefail

HOST="${LUMEN_DEPLOY_HOST:-118.31.47.129}"
KEY="${LUMEN_DEPLOY_KEY:-$HOME/.ssh/oasis_deploy}"
BACKUP_DIR="${HOME}/.lumen/backups"
KEEP="${LUMEN_BACKUP_KEEP:-7}"

mkdir -p "$BACKUP_DIR"
STAMP="$(date -u +%Y%m%d-%H%M%S)"
OUT="$BACKUP_DIR/vps-science-$STAMP.tgz"

echo "[backup-remote] pulling from $HOST ..."
ssh -i "$KEY" -o ConnectTimeout=15 "root@${HOST}" \
  'tar -czf - -C /root/.lumen science 2>/dev/null' > "$OUT"

SIZE=$(ls -lh "$OUT" | awk '{print $5}')
echo "[backup-remote] $OUT ($SIZE)"

# Prune old backups
ls -t "$BACKUP_DIR"/vps-science-*.tgz 2>/dev/null | tail -n +$((KEEP+1)) | xargs rm -f 2>/dev/null || true
echo "[backup-remote] kept last $KEEP backups"
