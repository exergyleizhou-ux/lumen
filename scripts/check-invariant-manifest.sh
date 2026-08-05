#!/usr/bin/env bash
# Executable invariant manifest check (DEBT-028 W2c-2).
#
# INVARIANTS.md is normative prose; INVARIANTS.toml is the machine-readable
# registry. This script makes "invariant coverage" computable:
#
#   1. every INV-* in INVARIANTS.md has an [[invariant]] entry;
#   2. every manifest entry exists in INVARIANTS.md (no ghost entries);
#   3. every entry declares a kernel (K1..K5) and a witness
#      (type_level | model_check | property | corpus) with a target;
#   4. `enforced` entries whose witness is only `corpus` must explain why a
#      stronger witness is impossible (why_not_stronger);
#   5. the witness target symbol/file exists in the source tree.
#
# Non-zero exit on any violation — the build refuses a rotting registry.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
MD="$ROOT/docs/nextgen/INVARIANTS.md"
TOML="$ROOT/docs/nextgen/INVARIANTS.toml"

python3 - "$MD" "$TOML" "$ROOT" <<'PY'
import re, sys
from pathlib import Path

md = Path(sys.argv[1]).read_text()
toml = Path(sys.argv[2]).read_text()
root = Path(sys.argv[3])

md_ids = {int(x) for x in re.findall(r"\*\*INV-(\d+)\*\*", md)}
failures = []
counts = {"type_level": 0, "model_check": 0, "property": 0, "corpus": 0}
covered = set()

entry_re = re.compile(
    r"\[\[invariant\]\]\s*"
    r'id\s*=\s*"(?P<id>INV-\d+)"\s*'
    r'statement\s*=\s*(?P<stmt>\'(?:[^\'\\]|\\.)*\'|"(?:[^"\\]|\\.)*")\s*'
    r'kernel\s*=\s*"(?P<kernel>K[1-5])"\s*'
    r'witness\s*=\s*\{\s*kind\s*=\s*"(?P<kind>type_level|model_check|property|corpus)"\s*,\s*'
    r'target\s*=\s*"(?P<target>[^"]+)"\s*\}\s*'
    r'gate\s*=\s*"(?P<gate>[^"]+)"\s*'
    r'status\s*=\s*"(?P<status>enforced|draft)"\s*'
    r'(?:why_not_stronger\s*=\s*"(?P<why>[^"]*)")?'
)
for m in entry_re.finditer(toml):
    inv = m.group("id")
    kernel = m.group("kernel")
    kind = m.group("kind")
    target = m.group("target")
    gate = m.group("gate")
    status = m.group("status")
    why = m.group("why") or ""
    covered.add(int(inv.split("-")[1]))
    counts[kind] += 1
    if int(inv.split("-")[1]) not in md_ids:
        failures.append(f"{inv}: manifest entry not present in INVARIANTS.md (ghost entry)")
    if status == "enforced" and kind == "corpus" and not why:
        failures.append(f"{inv}: enforced with corpus witness but no why_not_stronger explanation")
    # Witness target existence: module/symbol or script file.
    if not (root / target).exists() and not (root / "agent/crates/codegen/xai-grok-memory/src").glob(f"*{target.split(':')[0]}*"):
        # best-effort: search source trees for the target token
        hits = list((root / "agent/crates").rglob(f"*{target.split(':')[0]}*"))
        if not hits and not (root / "scripts").glob(target) and not (root / "scripts").glob("*" + target + "*"):
            failures.append(f"{inv}: witness target '{target}' not found in source tree")

missing = sorted(md_ids - covered)
if missing:
    failures.append(f"missing manifest entries for INV-" + ", INV-".join(str(i) for i in missing))

if failures:
    print("INVARIANT MANIFEST FAIL:")
    for f in failures:
        print(f"  - {f}")
    sys.exit(1)

total = len(covered)
print(f"INVARIANT MANIFEST PASS: {total} invariants covered "
      f"(type_level={counts['type_level']}, model_check={counts['model_check']}, "
      f"property={counts['property']}, corpus={counts['corpus']})")
PY
