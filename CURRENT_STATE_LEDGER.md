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
| Active git worktrees (this checkout) | 6 |
| Remote branches on origin (excl. main) | **77** |
| … fully merged into main | 9 |
| … NOT in main | **68** |
| Last non-bot commit | a4230435 chore(evidence): readiness for 9a133a95, third 1h soak bound to locked build |

---

## Remote Branches NOT in Main

| Branch | HEAD | Commits ahead |
|---|---|---|
| origin/archive/go-main | dd8d71cb | 10 |
| origin/cherry/upstream-p0-dispatch-osc52 | 0290445c | 2 |
| origin/cherry/upstream-p1-20260720 | 5743bb6a | 2 |
| origin/chore/delete-phantom-vision-docs | 61ac7534 | 96 |
| origin/chore/honest-docs | 8f14c747 | 77 |
| origin/chore/release-pipeline | e7d1415a | 74 |
| origin/codex/science-fusion-full | 99d18c52 | 32 |
| origin/docs/parallel-sessions-plan | abeabeb2 | 83 |
| origin/docs/plan-local-first | 633656d1 | 84 |
| origin/docs/v7-review-landing | 213b05a1 | 99 |
| origin/feat/eval-harness | da839814 | 81 |
| origin/feat/eval-json-repeat-latency | 4cf87a33 | 87 |
| origin/feat/eval-more-tasks | 69e2222c | 89 |
| origin/feat/oasis-publish | a618c816 | 21 |
| origin/feat/onlyoffice-local-langgraph | 0b6de865 | 10 |
| origin/feat/provider-aware-cost | 3fd28f61 | 75 |
| origin/feat/tool-profile-core | c9b746bd | 82 |
| origin/feat/verify-multilang-activation | b03cbdc9 | 30 |
| origin/fix/agent-verify-label-and-repeat-guard | 4cd1d9e5 | 64 |
| origin/fix/anthro-tool-block-wire-format | 75bcb91d | 68 |
| origin/fix/apply-compaction-and-skills-config | 335c3269 | 47 |
| origin/fix/bash-nonzero-exit-is-error | f4c785db | 46 |
| origin/fix/bash-scrub-secret-env | e8bf0d97 | 78 |
| origin/fix/cleanup-dedup-stats-rewound | 57037528 | 67 |
| origin/fix/compact-truncate-rune-safe | e60aa498 | 58 |
| origin/fix/compaction-summary-budget-and-dead-knob | bc5f41fb | 65 |
| origin/fix/config-tools-shared-store | fb2090e1 | 56 |
| origin/fix/cost-accuracy-cache-aware | 5be3fb7e | 57 |
| origin/fix/default-model-resolution | b8ffc3d2 | 61 |
| origin/fix/diff-truncation-show-count | 3cd81620 | 50 |
| origin/fix/doctor-nongo-no-hardfail | fbfcca46 | 54 |
| origin/fix/editverify-lint-caveat-and-modern-js | 5baffa8d | 53 |
| origin/fix/editverify-skip-not-verified | 6e0eceb4 | 79 |
| origin/fix/gemini-block-and-cancel-robustness | 32bb6fef | 55 |
| origin/fix/guard-destructive-gaps | 35f09bd2 | 25 |
| origin/fix/guard-home-data-dir-rm | 02f60221 | 80 |
| origin/fix/guard-pipe-to-shell | 32b95c32 | 24 |
| origin/fix/guard-sensitive-write-paths | 29d72e7e | 26 |
| origin/fix/guard-strip-hidden-chars | 45be2087 | 23 |
| origin/fix/lineedit-wrapped-cursor | a4575b03 | 76 |
| origin/fix/mcp-client-registry-race-and-leak | 44539a57 | 49 |
| origin/fix/oasis-author-toolchain | 6a40592f | 19 |
| origin/fix/paste-flood-and-silent-turns | 03ead366 | 32 |
| origin/fix/paste-lifecycle-asker-goroutine | b94e6101 | 39 |
| origin/fix/preview-resolves-path-like-execute | c28ec4f7 | 69 |
| origin/fix/render-highlight-correctness | 19410f9b | 45 |
| origin/fix/render-markdown-correctness | fa9cbea0 | 44 |
| origin/fix/render-underscore-italic | 578a0882 | 60 |
| origin/fix/render-verify-result-in-run | 6a1ed310 | 48 |
| origin/fix/stream-recovery-preserve-partial | 24aa86e7 | 63 |
| origin/fix/terminal-sink-parallel-checkmark | e369dbb6 | 51 |
| origin/fix/timeline-seed-turn-counter | d133952e | 59 |
| origin/fix/token-cost-cumulative-basis | 52650d0d | 73 |
| origin/fix/token-estimate-images-schemas | 00afd7cb | 62 |
| origin/fix/tui-chat-scroll-and-statusbar-overflow | 610a65fd | 52 |
| origin/fix/tui-spinner-budget-deadcode | 059850e8 | 66 |
| origin/fix/tui-tool-row-coalesce-and-verify-skip | 01fc5a1b | 71 |
| origin/fix/verify-monorepo-subdir-root | 99e3d7de | 70 |
| origin/fix/wizard-fresh-install-scaffold | 4f79f23c | 72 |
| origin/s3-pr1-threat-model | f3adf755 | 85 |
| origin/s3-pr2-guard-property | ee12bee6 | 86 |
| origin/s3-pr3-sandbox-runner | 4192fe6f | 89 |
| origin/s3-pr4-audit-jsonl | 2cc86bc0 | 92 |
| origin/s3-pr5-injection-ssrf | 677b679e | 94 |
| origin/test/eval-tasks-wellformed | 5d33767f | 97 |
| origin/windows-fix-ps-scripts | 5a80be98 | 15 |
| upstream | a4221165 | 1 |
| upstream/main | a4221165 | 1 |

Branches listed here either carry unmerged work or are stale (e.g. the
archived 2026-06 Go-era branches). See docs/go-era-branch-map.md for the
Go-branch → Rust-backlog mapping.

---

*This file is auto-generated. Do not edit manually.*
