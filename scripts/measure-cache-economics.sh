#!/usr/bin/env bash
# measure-cache-economics.sh — report what the provider ACTUALLY did with our
# prompt cache, and how stable our prefix actually was.
#
# Why this exists: Lumen invested heavily in prefix-cache discipline (discipline
# state never enters the system prefix, shape capture, miss attribution, epoch
# evidence). Until 2026-07-27 nobody had ever measured the outcome. The repo
# could prove "our prefix was byte-stable" from cache_request_evidence.jsonl,
# but the provider-reported hit/miss lived only in unified.jsonl and was never
# aggregated. Two halves of one number, never joined.
#
# Measured on 2026-07-27 (first baseline, this machine):
#   provider-reported cache hit rate   95.9%   (40 turns, 1.22M prompt tokens)
#   prefix stability                   99.9%   (1902 follow-up requests,
#                                                2 breaks, both full_compaction)
#   model_elapsed_ms                   median 2131, p95 7347
#
# Both halves matter and they answer different questions:
#   - prefix stability  = did WE keep our side byte-identical (our fault if not)
#   - provider hit rate = did the PROVIDER actually reuse it (the outcome)
# A high prefix stability with a low hit rate would mean the provider is not
# honouring the cache; the reverse would be impossible. Tracking only one is
# how you end up optimising something that already works.
#
# Usage: scripts/measure-cache-economics.sh [--json]
set -euo pipefail

GROK_HOME_DIR="${GROK_HOME:-$HOME/.grok}"
LOG="$GROK_HOME_DIR/logs/unified.jsonl"
SESSIONS="$GROK_HOME_DIR/sessions"
JSON_OUT=0
[[ "${1:-}" == "--json" ]] && JSON_OUT=1

python3 - "$LOG" "$SESSIONS" "$JSON_OUT" <<'PY'
import json, sys, glob, collections
from pathlib import Path

log_path, sessions_dir, want_json = Path(sys.argv[1]), Path(sys.argv[2]), sys.argv[3] == "1"

# ---- provider-reported outcome -------------------------------------------
turns = []
if log_path.is_file():
    with log_path.open(errors="ignore") as fh:
        for line in fh:
            if "provider_cache_hit_tokens" not in line:
                continue
            try:
                ctx = (json.loads(line).get("ctx") or {})
            except json.JSONDecodeError:
                continue
            if "provider_cache_hit_tokens" in ctx:
                turns.append(ctx)

hit = sum(t.get("provider_cache_hit_tokens") or 0 for t in turns)
miss = sum(t.get("provider_cache_miss_tokens") or 0 for t in turns)
prompt = sum(t.get("prompt_tokens") or 0 for t in turns)
accounting = collections.Counter(t.get("provider_cache_accounting") for t in turns)
hit_rate = hit / (hit + miss) if (hit + miss) else None

def pct(values, p):
    if not values:
        return None
    ordered = sorted(values)
    return ordered[min(len(ordered) - 1, int(len(ordered) * p))]

elapsed = [t["model_elapsed_ms"] for t in turns if t.get("model_elapsed_ms")]
itl = [t["itl_p50_ms"] for t in turns if t.get("itl_p50_ms")]

# ---- our own side: was the prefix byte-stable -----------------------------
stable = broken = 0
reasons = collections.Counter()
for f in glob.glob(str(sessions_dir / "*" / "*" / "cache_request_evidence.jsonl")):
    recs = []
    with open(f, errors="ignore") as fh:
        for line in fh:
            line = line.strip()
            if not line:
                continue
            try:
                recs.append(json.loads(line))
            except json.JSONDecodeError:
                pass
    for r in recs[1:]:                       # first request cannot "break" a prefix
        mutations = r.get("mutation_reasons") or []
        if mutations:
            broken += 1
            reasons.update(mutations)
        else:
            stable += 1

followups = stable + broken
report = {
    "schema_version": 1,
    "provider": {
        "turns": len(turns),
        "cache_hit_tokens": hit,
        "cache_miss_tokens": miss,
        "prompt_tokens": prompt,
        "hit_rate": round(hit_rate, 4) if hit_rate is not None else None,
        "accounting": dict(accounting),
    },
    "prefix": {
        "followup_requests": followups,
        "stable": stable,
        "broken": broken,
        "stability": round(stable / followups, 4) if followups else None,
        "break_reasons": dict(reasons),
    },
    "latency_ms": {
        "model_elapsed_median": pct(elapsed, 0.5),
        "model_elapsed_p95": pct(elapsed, 0.95),
        "itl_p50_median": pct(itl, 0.5),
    },
}

if want_json:
    print(json.dumps(report, indent=2))
    raise SystemExit(0)

print("=== cache economics ===")
if not turns:
    print("no provider-reported turns found in", log_path)
else:
    print(f"turns with provider accounting : {len(turns)}  ({dict(accounting)})")
    print(f"prompt tokens                  : {prompt:,}")
    print(f"  cache hit                    : {hit:,}")
    print(f"  cache miss                   : {miss:,}")
    print(f"PROVIDER HIT RATE              : {hit_rate*100:.1f}%")
print()
if followups:
    print(f"prefix follow-up requests      : {followups}")
    print(f"PREFIX STABILITY               : {stable/followups*100:.1f}%  ({broken} broken)")
    for reason, count in reasons.most_common():
        print(f"  break: {reason} x{count}")
else:
    print("no prefix evidence found under", sessions_dir)
print()
print(f"model_elapsed_ms  median={report['latency_ms']['model_elapsed_median']} "
      f"p95={report['latency_ms']['model_elapsed_p95']}")
print(f"itl_p50_ms        median={report['latency_ms']['itl_p50_median']}")
PY
