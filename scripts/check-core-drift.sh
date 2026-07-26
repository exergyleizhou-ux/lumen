#!/usr/bin/env bash
# check-core-drift.sh — detect divergence between this repo's Rust core and a
# sibling checkout that carries a copy of it (today: ~/code/lumen-science).
#
# Why: on 2026-07-27 a file-by-file comparison found 130 core files diverged
# between the two repos, and NONE of the eight security fixes landed in lumen
# on 2026-07-26 existed in the sibling copy — including a silent-RCE folder
# trust hole, on the very line that runs external research code. Manual sync
# has a measured success rate of 0/8, so it needs a gate rather than a habit.
#
# This is a REPORTING gate by default: it prints the drift and exits 0 unless
# CORE_DRIFT_STRICT=1, because until the two repos agree on a single upstream
# (pin instead of copy) some divergence is expected and blocking every commit
# on it would just get the gate disabled. What it must never do is let the
# divergence stay invisible.
#
# Usage:
#   scripts/check-core-drift.sh                      # report
#   CORE_DRIFT_STRICT=1 scripts/check-core-drift.sh  # fail on any drift
#   CORE_DRIFT_SIBLING=/path/to/repo scripts/check-core-drift.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SIBLING="${CORE_DRIFT_SIBLING:-$HOME/code/lumen-science}"
STRICT="${CORE_DRIFT_STRICT:-0}"

if [[ ! -d "$SIBLING/agent/crates" ]]; then
  echo "SKIP: no sibling core at $SIBLING (set CORE_DRIFT_SIBLING to override)"
  exit 0
fi

echo "=== core drift: $(basename "$ROOT") vs $(basename "$SIBLING") ==="

diverged=0
missing=0
report="$(mktemp)"
trap 'rm -f "$report"' EXIT

# xai-grok-science is the science lane's own crate: it is expected to differ
# and is not part of the shared core.
while IFS= read -r rel; do
  ours="$ROOT/agent/crates/$rel"
  theirs="$SIBLING/agent/crates/$rel"
  if [[ ! -f "$theirs" ]]; then
    missing=$((missing + 1))
    echo "MISSING $rel" >>"$report"
  elif ! cmp -s "$ours" "$theirs"; then
    diverged=$((diverged + 1))
    echo "DIVERGED $rel" >>"$report"
  fi
done < <(cd "$ROOT/agent/crates" && find . -name "*.rs" -not -path "*/xai-grok-science/*" | sed 's|^\./||' | sort)

total=$((diverged + missing))
echo "diverged=$diverged missing_in_sibling=$missing total=$total"

if [[ $total -gt 0 ]]; then
  echo
  echo "most affected crates:"
  awk '{print $2}' "$report" | awk -F/ '{print $2}' | sort | uniq -c | sort -rn | head -8 | sed 's/^/  /'
fi

# The security-relevant markers: each is a fix that must not be missing from a
# copy of the core, because each one is exploitable in its absence.
echo
echo "security fixes present in the sibling core:"
check_marker() {
  local label="$1" needle="$2" rel="$3" expect="${4:-present}"
  local f="$SIBLING/agent/crates/$rel"
  local found=no
  if [[ "$expect" == "absent" ]]; then
    # An "absent" marker asks whether the SHIPPED code still names the thing,
    # so scan only the production half of the file. A fix of this shape lands
    # with a regression test that names the very string being searched for
    # (`assert!(!is_safe_command("cargo check"))`), so grepping the whole file
    # reports MISSING exactly when the fix is properly tested.
    #
    # That false positive is not academic: it fired against lumen's own core,
    # which is byte-identical to the source of truth for this very fix, and it
    # cost a sync cycle chasing an item that was already done. A gate that
    # cannot go green teaches people to ignore it.
    # Done in a single awk rather than `awk | grep -q`: with `pipefail` set,
    # grep -q exits on the first hit, awk dies of SIGPIPE, and the pipeline
    # reports failure — which this checker would read as "marker absent", i.e.
    # green. Whether that happens depends on how much awk got into the 64K pipe
    # buffer first, so the pipeline form is a coin flip that fails open.
    [[ -f "$f" ]] && awk -v needle="$needle" '
      /^#\[cfg\(test\)\]/ { exit }
      index($0, needle)   { hit = 1; exit }
      END                 { exit !hit }
    ' "$f" && found=yes
  else
    [[ -f "$f" ]] && grep -q "$needle" "$f" && found=yes
  fi
  if [[ "$expect" == "absent" ]]; then
    # marker that must be GONE (e.g. an entry removed from an allowlist)
    if [[ "$found" == "yes" ]]; then
      echo "  MISSING  $label"
      return 1
    fi
    echo "  ok       $label"
    return 0
  fi
  if [[ "$found" == "yes" ]]; then
    echo "  ok       $label"
    return 0
  fi
  echo "  MISSING  $label"
  return 1
}

sec_missing=0
check_marker "folder-trust [permission] marker (silent RCE)" "has_permission" \
  "codegen/xai-grok-workspace/src/folder_trust.rs" || sec_missing=$((sec_missing + 1))
check_marker "marketplace git operand validation" "validate_git_url" \
  "codegen/xai-grok-plugin-marketplace/src/git.rs" || sec_missing=$((sec_missing + 1))
check_marker "cargo check NOT auto-safe" '"cargo check"' \
  "codegen/xai-grok-workspace/src/permission/manager.rs" absent || sec_missing=$((sec_missing + 1))
check_marker "chained-command allow containment" "evaluate_bash_access" \
  "codegen/xai-grok-workspace/src/permission/policy.rs" || sec_missing=$((sec_missing + 1))
check_marker "guard strict evaluation / UNSAFE audit" "check_bash_strict" \
  "codegen/lumen-guard/src/lib.rs" || sec_missing=$((sec_missing + 1))
check_marker "BYOK hermetic-test containment" "LUMEN_INFERENCE_BASE_URL" \
  "codegen/xai-grok-shell/src/agent/config.rs" || sec_missing=$((sec_missing + 1))
check_marker "verify multi-language activation" "allow_package_runner" \
  "codegen/lumen-verify/src/config.rs" || sec_missing=$((sec_missing + 1))
check_marker "session cwd reaches the tool registry" "session.cwd()" \
  "codegen/xai-grok-workspace/src/workspace_ops.rs" || sec_missing=$((sec_missing + 1))

echo
if [[ $sec_missing -gt 0 ]]; then
  echo "RESULT: $sec_missing/8 security fixes absent from the sibling core."
  echo "        See docs/core-drift-risk-20260727.md for the sync plan."
else
  echo "RESULT: all 8 tracked security fixes present in the sibling core."
fi

if [[ "$STRICT" == "1" && $total -gt 0 ]]; then
  echo "FAIL: CORE_DRIFT_STRICT=1 and $total files diverge"
  exit 1
fi
if [[ $sec_missing -gt 0 ]]; then
  # Security gaps are always an error, strict or not: an exploitable hole in a
  # copy of the core is not "expected divergence".
  exit 1
fi
exit 0
