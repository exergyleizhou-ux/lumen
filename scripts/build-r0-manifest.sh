#!/usr/bin/env bash
set -euo pipefail

candidate=${1:?usage: $0 <candidate-sha> [baseline-sha] [upstream-ref]}
baseline=${2:-dd04f397b1d02f2272b092555669dfba1f01bc85}
upstream_ref=${3:-upstream/main}
repo_root=$(git rev-parse --show-toplevel)
cd "$repo_root"

origin_sha=$(git rev-parse origin/main)
upstream_sha=$(git rev-parse "$upstream_ref")
merge_base=$(git merge-base "$baseline" "$upstream_ref")
out="artifacts/r0/$candidate"
mkdir -p "$out"

paths=$(git diff --name-only "$baseline" "$upstream_ref")
{
  printf '{\n'
  printf '  "candidate": "%s",\n' "$candidate"
  printf '  "baseline": "%s",\n' "$baseline"
  printf '  "origin_main": "%s",\n' "$origin_sha"
  printf '  "upstream_ref": "%s",\n' "$upstream_ref"
  printf '  "upstream_sha": "%s",\n' "$upstream_sha"
  printf '  "merge_base": "%s",\n' "$merge_base"
  printf '  "path_count": %s,\n' "$(printf '%s\n' "$paths" | sed '/^$/d' | wc -l | tr -d ' ')"
  printf '  "paths": [\n'
  first=1
  while IFS= read -r path; do
    [ -n "$path" ] || continue
    if [ "$first" -eq 0 ]; then printf ',\n'; fi
    first=0
    if [ -f "$path" ]; then hash=$(shasum -a 256 "$path" | awk '{print $1}'); else hash=null; fi
    group=R0-A
    case "$path" in
      docs/*|scripts/*|*.md) group=R0-C ;;
      agent/crates/*/src/*|agent/crates/*/tests/*) group=R0-A ;;
      *) group=R0-B ;;
    esac
    printf '    {"path":"%s","group":"%s","candidate":false,"sha256":%s,"owner":"lumen-integration","protected":false}' "$path" "$group" "${hash:+\"$hash\"}"
  done <<< "$paths"
  printf '\n  ],\n'
  printf '  "disposition": "review-only; no upstream path approved for runtime import",\n'
  printf '  "generated_by": "scripts/build-r0-manifest.sh"\n'
  printf '}\n'
} > "$out/manifest.json"

printf '%s\n' "wrote $out/manifest.json ($(wc -l < "$out/manifest.json" | tr -d ' ') lines)"
