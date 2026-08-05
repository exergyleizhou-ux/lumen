# Lumen Current State Ledger

**Auto-generated** by CI `regenerate-ledger.sh` on push to main. All facts
below are computed from git and readiness artifacts — nothing is hand-written.
For the generation date, see the last ledger commit in `git log`.

---

## ⚡ Executive Summary

| Question | Answer |
|---|---|
| Version (root VERSION) | **2.0.0** |
| Readiness | state=BLOCKED ready=False engineering_complete=False blockers=3 |
| Active git worktrees (this checkout) | 1 |
| Remote branches on origin (excl. main) | **75** |
| … fully merged into main | 9 |
| … NOT in main | **66** |
| Last non-bot commit | 064a6d4e Merge remote-tracking branch 'origin/main' |

---

## Remote Branches NOT in Main

| Branch | HEAD | Commits ahead |
|---|---|---|
| origin/archive/go-main | dd8d71cb | 632 |
| origin/cherry/upstream-p0-dispatch-osc52 | 0290445c | 2 |
| origin/cherry/upstream-p1-20260720 | 5743bb6a | 2 |
| origin/chore/delete-phantom-vision-docs | 61ac7534 | 262 |
| origin/chore/honest-docs | 8f14c747 | 243 |
| origin/chore/release-pipeline | e7d1415a | 240 |
| origin/codex/science-fusion-full | 99d18c52 | 32 |
| origin/docs/parallel-sessions-plan | abeabeb2 | 249 |
| origin/docs/plan-local-first | 633656d1 | 250 |
| origin/docs/v7-review-landing | 213b05a1 | 265 |
| origin/feat/eval-harness | da839814 | 247 |
| origin/feat/eval-json-repeat-latency | 4cf87a33 | 253 |
| origin/feat/eval-more-tasks | 69e2222c | 255 |
| origin/feat/oasis-publish | a618c816 | 187 |
| origin/feat/onlyoffice-local-langgraph | 0b6de865 | 605 |
| origin/feat/provider-aware-cost | 3fd28f61 | 241 |
| origin/feat/tool-profile-core | c9b746bd | 248 |
| origin/feat/verify-multilang-activation | b03cbdc9 | 196 |
| origin/fix/agent-verify-label-and-repeat-guard | 4cd1d9e5 | 230 |
| origin/fix/anthro-tool-block-wire-format | 75bcb91d | 234 |
| origin/fix/apply-compaction-and-skills-config | 335c3269 | 213 |
| origin/fix/bash-nonzero-exit-is-error | f4c785db | 212 |
| origin/fix/bash-scrub-secret-env | e8bf0d97 | 244 |
| origin/fix/cleanup-dedup-stats-rewound | 57037528 | 233 |
| origin/fix/compact-truncate-rune-safe | e60aa498 | 224 |
| origin/fix/compaction-summary-budget-and-dead-knob | bc5f41fb | 231 |
| origin/fix/config-tools-shared-store | fb2090e1 | 222 |
| origin/fix/cost-accuracy-cache-aware | 5be3fb7e | 223 |
| origin/fix/default-model-resolution | b8ffc3d2 | 227 |
| origin/fix/diff-truncation-show-count | 3cd81620 | 216 |
| origin/fix/doctor-nongo-no-hardfail | fbfcca46 | 220 |
| origin/fix/editverify-lint-caveat-and-modern-js | 5baffa8d | 219 |
| origin/fix/editverify-skip-not-verified | 6e0eceb4 | 245 |
| origin/fix/gemini-block-and-cancel-robustness | 32bb6fef | 221 |
| origin/fix/guard-destructive-gaps | 35f09bd2 | 191 |
| origin/fix/guard-home-data-dir-rm | 02f60221 | 246 |
| origin/fix/guard-pipe-to-shell | 32b95c32 | 190 |
| origin/fix/guard-sensitive-write-paths | 29d72e7e | 192 |
| origin/fix/guard-strip-hidden-chars | 45be2087 | 189 |
| origin/fix/lineedit-wrapped-cursor | a4575b03 | 242 |
| origin/fix/mcp-client-registry-race-and-leak | 44539a57 | 215 |
| origin/fix/oasis-author-toolchain | 6a40592f | 185 |
| origin/fix/paste-flood-and-silent-turns | 03ead366 | 198 |
| origin/fix/paste-lifecycle-asker-goroutine | b94e6101 | 205 |
| origin/fix/preview-resolves-path-like-execute | c28ec4f7 | 235 |
| origin/fix/render-highlight-correctness | 19410f9b | 211 |
| origin/fix/render-markdown-correctness | fa9cbea0 | 210 |
| origin/fix/render-underscore-italic | 578a0882 | 226 |
| origin/fix/render-verify-result-in-run | 6a1ed310 | 214 |
| origin/fix/stream-recovery-preserve-partial | 24aa86e7 | 229 |
| origin/fix/terminal-sink-parallel-checkmark | e369dbb6 | 217 |
| origin/fix/timeline-seed-turn-counter | d133952e | 225 |
| origin/fix/token-cost-cumulative-basis | 52650d0d | 239 |
| origin/fix/token-estimate-images-schemas | 00afd7cb | 228 |
| origin/fix/tui-chat-scroll-and-statusbar-overflow | 610a65fd | 218 |
| origin/fix/tui-spinner-budget-deadcode | 059850e8 | 232 |
| origin/fix/tui-tool-row-coalesce-and-verify-skip | 01fc5a1b | 237 |
| origin/fix/verify-monorepo-subdir-root | 99e3d7de | 236 |
| origin/fix/wizard-fresh-install-scaffold | 4f79f23c | 238 |
| origin/s3-pr1-threat-model | f3adf755 | 251 |
| origin/s3-pr2-guard-property | ee12bee6 | 252 |
| origin/s3-pr3-sandbox-runner | 4192fe6f | 255 |
| origin/s3-pr4-audit-jsonl | 2cc86bc0 | 258 |
| origin/s3-pr5-injection-ssrf | 677b679e | 260 |
| origin/test/eval-tasks-wellformed | 5d33767f | 263 |
| origin/windows-fix-ps-scripts | 5a80be98 | 15 |

Branches listed here either carry unmerged work or are stale (e.g. the
archived 2026-06 Go-era branches). See docs/go-era-branch-map.md for the
Go-branch → Rust-backlog mapping.

---

*This file is auto-generated. Do not edit manually.*
