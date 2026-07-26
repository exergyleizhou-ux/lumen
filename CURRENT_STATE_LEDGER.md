# Lumen Current State Ledger

**Auto-generated** by CI `regenerate-ledger.sh` on push to main. All facts
below are computed from git and readiness artifacts — nothing is hand-written.
For the generation date, see the last ledger commit in `git log`.

---

## ⚡ Executive Summary

| Question | Answer |
|---|---|
| Version (root VERSION) | **0.1.250** |
| Readiness | state=BLOCKED ready=False engineering_complete=False blockers=5 |
| Active git worktrees (this checkout) | 1 |
| Remote branches on origin (excl. main) | **71** |
| … fully merged into main | 6 |
| … NOT in main | **65** |
| Last non-bot commit | f9b61ee fix(eval): reverse gate runs in a sandbox copy; document the SOURCE_LOCK ordering deadlock |

---

## Remote Branches NOT in Main

| Branch | HEAD | Commits ahead |
|---|---|---|
| origin/archive/go-main | dd8d71c | 632 |
| origin/cherry/upstream-p0-dispatch-osc52 | 0290445 | 2 |
| origin/cherry/upstream-p1-20260720 | 5743bb6 | 2 |
| origin/chore/delete-phantom-vision-docs | 61ac753 | 262 |
| origin/chore/honest-docs | 8f14c74 | 243 |
| origin/chore/release-pipeline | e7d1415 | 240 |
| origin/codex/science-fusion-full | 99d18c5 | 32 |
| origin/docs/parallel-sessions-plan | abeabeb | 249 |
| origin/docs/plan-local-first | 633656d | 250 |
| origin/docs/v7-review-landing | 213b05a | 265 |
| origin/feat/eval-harness | da83981 | 247 |
| origin/feat/eval-json-repeat-latency | 4cf87a3 | 253 |
| origin/feat/eval-more-tasks | 69e2222 | 255 |
| origin/feat/oasis-publish | a618c81 | 187 |
| origin/feat/onlyoffice-local-langgraph | 0b6de86 | 605 |
| origin/feat/provider-aware-cost | 3fd28f6 | 241 |
| origin/feat/tool-profile-core | c9b746b | 248 |
| origin/feat/verify-multilang-activation | b03cbdc | 196 |
| origin/fix/agent-verify-label-and-repeat-guard | 4cd1d9e | 230 |
| origin/fix/anthro-tool-block-wire-format | 75bcb91 | 234 |
| origin/fix/apply-compaction-and-skills-config | 335c326 | 213 |
| origin/fix/bash-nonzero-exit-is-error | f4c785d | 212 |
| origin/fix/bash-scrub-secret-env | e8bf0d9 | 244 |
| origin/fix/cleanup-dedup-stats-rewound | 5703752 | 233 |
| origin/fix/compact-truncate-rune-safe | e60aa49 | 224 |
| origin/fix/compaction-summary-budget-and-dead-knob | bc5f41f | 231 |
| origin/fix/config-tools-shared-store | fb2090e | 222 |
| origin/fix/cost-accuracy-cache-aware | 5be3fb7 | 223 |
| origin/fix/default-model-resolution | b8ffc3d | 227 |
| origin/fix/diff-truncation-show-count | 3cd8162 | 216 |
| origin/fix/doctor-nongo-no-hardfail | fbfcca4 | 220 |
| origin/fix/editverify-lint-caveat-and-modern-js | 5baffa8 | 219 |
| origin/fix/editverify-skip-not-verified | 6e0eceb | 245 |
| origin/fix/gemini-block-and-cancel-robustness | 32bb6fe | 221 |
| origin/fix/guard-destructive-gaps | 35f09bd | 191 |
| origin/fix/guard-home-data-dir-rm | 02f6022 | 246 |
| origin/fix/guard-pipe-to-shell | 32b95c3 | 190 |
| origin/fix/guard-sensitive-write-paths | 29d72e7 | 192 |
| origin/fix/guard-strip-hidden-chars | 45be208 | 189 |
| origin/fix/lineedit-wrapped-cursor | a4575b0 | 242 |
| origin/fix/mcp-client-registry-race-and-leak | 44539a5 | 215 |
| origin/fix/oasis-author-toolchain | 6a40592 | 185 |
| origin/fix/paste-flood-and-silent-turns | 03ead36 | 198 |
| origin/fix/paste-lifecycle-asker-goroutine | b94e610 | 205 |
| origin/fix/preview-resolves-path-like-execute | c28ec4f | 235 |
| origin/fix/render-highlight-correctness | 19410f9 | 211 |
| origin/fix/render-markdown-correctness | fa9cbea | 210 |
| origin/fix/render-underscore-italic | 578a088 | 226 |
| origin/fix/render-verify-result-in-run | 6a1ed31 | 214 |
| origin/fix/stream-recovery-preserve-partial | 24aa86e | 229 |
| origin/fix/terminal-sink-parallel-checkmark | e369dbb | 217 |
| origin/fix/timeline-seed-turn-counter | d133952 | 225 |
| origin/fix/token-cost-cumulative-basis | 52650d0 | 239 |
| origin/fix/token-estimate-images-schemas | 00afd7c | 228 |
| origin/fix/tui-chat-scroll-and-statusbar-overflow | 610a65f | 218 |
| origin/fix/tui-spinner-budget-deadcode | 059850e | 232 |
| origin/fix/tui-tool-row-coalesce-and-verify-skip | 01fc5a1 | 237 |
| origin/fix/verify-monorepo-subdir-root | 99e3d7d | 236 |
| origin/fix/wizard-fresh-install-scaffold | 4f79f23 | 238 |
| origin/s3-pr1-threat-model | f3adf75 | 251 |
| origin/s3-pr2-guard-property | ee12bee | 252 |
| origin/s3-pr3-sandbox-runner | 4192fe6 | 255 |
| origin/s3-pr4-audit-jsonl | 2cc86bc | 258 |
| origin/s3-pr5-injection-ssrf | 677b679 | 260 |
| origin/test/eval-tasks-wellformed | 5d33767 | 263 |

Branches listed here either carry unmerged work or are stale (e.g. the
archived 2026-06 Go-era branches). See docs/go-era-branch-map.md for the
Go-branch → Rust-backlog mapping.

---

*This file is auto-generated. Do not edit manually.*
