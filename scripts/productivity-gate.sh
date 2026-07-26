#!/usr/bin/env bash
# M6 productivity gate: count real journal/YYYY-MM-DD.md productivity days.
# Exit 0 only when ≥15 days marked as productivity days (是).
# Does NOT fabricate journals.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
JOURNAL="$ROOT/journal"
MIN="${PRODUCTIVITY_MIN_DAYS:-15}"
ART="$ROOT/artifacts/readiness"
mkdir -p "$ART"

count=0
rejected=0
days_file="$(mktemp)"
rejected_file="$(mktemp)"
trap 'rm -f "$days_file" "$rejected_file"' EXIT

# Anti-forgery contract: a journal only counts if it was committed close to the
# day it claims to describe. A file first added to git more than GRACE days
# after its filename date is a backfill and never counts. Files whose filename
# date predates the repository's first commit are backfills by definition.
# Uncommitted files count only while their filename date is within GRACE days
# of today (a journal being written right now).
GRACE_DAYS="${PRODUCTIVITY_GRACE_DAYS:-2}"
REPO_BIRTH=$(cd "$ROOT" && git log --reverse --format=%ad --date=format:%Y-%m-%d 2>/dev/null | head -1 || echo "")

journal_date_ok() {
  local file="$1" fdate="$2"
  local added
  added=$(cd "$ROOT" && git log --diff-filter=A --format=%ad --date=format:%Y-%m-%d -- "${file#"$ROOT"/}" 2>/dev/null | tail -1 || echo "")
  python3 - "$fdate" "$added" "$REPO_BIRTH" "$GRACE_DAYS" <<'PY'
import sys
from datetime import date, timedelta
fdate_s, added_s, birth_s, grace_s = sys.argv[1:5]
def parse(s):
    try:
        y, m, d = s.split("-")
        return date(int(y), int(m), int(d))
    except Exception:
        return None
fdate, added, birth = parse(fdate_s), parse(added_s), parse(birth_s)
grace = timedelta(days=int(grace_s))
if fdate is None:
    raise SystemExit(1)
if birth is not None and fdate < birth:
    raise SystemExit(1)  # claims a day before the repo existed
if added is None:
    # not committed yet: only acceptable for a journal being written now
    raise SystemExit(0 if date.today() - fdate <= grace else 1)
raise SystemExit(0 if added - fdate <= grace else 1)
PY
}

shopt -s nullglob
for f in "$JOURNAL"/[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9].md; do
  base=$(basename "$f")
  fdate="${base%.md}"
  # Marked productivity day: checked "- [x] 是" near 生产力日 section, or explicit "是"
  if grep -Eq '^\s*-\s*\[[xX]\]\s*是' "$f" || grep -Eqi '今日算生产力日[？?]?\s*是|算生产力日[？?]?\s*是' "$f"; then
    if journal_date_ok "$f" "$fdate"; then
      count=$((count + 1))
      echo "$base" >>"$days_file"
    else
      rejected=$((rejected + 1))
      echo "$base (first git add >${GRACE_DAYS}d after filename date, or predates repo)" >>"$rejected_file"
    fi
  fi
done

echo "=== productivity-gate ==="
echo "journal_dir=$JOURNAL"
echo "count=$count"
echo "min=$MIN"
echo "grace_days=$GRACE_DAYS"
if [[ $count -gt 0 ]]; then
  echo "days:"
  sed 's/^/  /' "$days_file"
fi
if [[ $rejected -gt 0 ]]; then
  echo "rejected_backfills:"
  sed 's/^/  /' "$rejected_file"
fi

python3 - "$ART/M6-productivity.json" "$count" "$MIN" "$days_file" "$rejected_file" <<'PY'
import json, sys
from datetime import datetime, timezone
from pathlib import Path
out, count, mn, days_path, rejected_path = sys.argv[1], int(sys.argv[2]), int(sys.argv[3]), sys.argv[4], sys.argv[5]
days = Path(days_path).read_text().splitlines() if Path(days_path).stat().st_size else []
rejected = Path(rejected_path).read_text().splitlines() if Path(rejected_path).stat().st_size else []
art = {
    "schema_version": 1,
    "check_id": "M6_15_day_self_use",
    "pass": count >= mn,
    "count": count,
    "min": mn,
    "days": days,
    "rejected_backfills": rejected,
    "generated_at": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
}
Path(out).write_text(json.dumps(art, indent=2) + "\n")
print("wrote", out, "pass=", art["pass"])
PY

if [[ "$count" -ge "$MIN" ]]; then
  echo "OK: productivity gate passed ($count ≥ $MIN)"
  exit 0
fi
echo "BLOCKED: productivity days $count < $MIN (use journal/TEMPLATE-productivity-day.md — do not fabricate)"
exit 1
