# R0 upstream a4221165 review — 2026-08-02

## Scope and current snapshot

This is a point-in-time review record, not an approval to merge upstream.

| Field | Value |
|---|---|
| Candidate source commit | `9ae4762aeaeb74a57c7428dd4912304de441ce70` |
| Candidate branch | `sync/absorb-upstream-20260731` |
| Candidate parent source | `2e778682203db788048ee1b2df74554d3b08b64b` |
| GitHub `origin/main` | `2f47a9ad84e94b20291a1ad3d6b005ccbd3885f4` |
| Upstream reviewed | `xai-org/grok-build@a4221165824e5b1f5c4c10b7459f65e78dd6448d` |
| Shared upstream baseline | `dd04f397b1d02f2272b092555669dfba1f01bc85` |
| Fetch result | `HEAD...upstream/main = 530 / 1` commits (Lumen / upstream) |
| Upstream delta | 165 files, `+15,161/-1,969` relative to `dd04f397` |
| Working tree at review | clean |

The upstream commit is a monorepo synchronization drop, not a cohesive patch.
Its own summary spans session release, model overload handling, compaction,
background subagents, SDK liveness, PTY cleanup, permissions, pager views,
and authentication retries. It must therefore be split by safety contract;
`git merge upstream/main` remains prohibited by `agent/UPSTREAM.md`.

## Non-negotiable local contracts consulted first

1. `docs/LUMEN-NEXTGEN-EXECUTION-BOOK-2026-08-01.md`: SessionActor is the
   sole authority; source synchronization and NextGen contract acceptance are
   separate gates.
2. `docs/lumen-upstream-assumption-collisions.md`: Lumen defaults must not
   let test traffic escape, weaken guard semantics, or silently disappear at a
   cross-crate seam.
3. P0 candidate `2e778682`: a failed inference has no durable
   `ProviderAttemptReceipt`; the same request is terminal and may not be
   retried, compacted-and-resubmitted, reauthenticated-and-resubmitted, or
   rerouted-and-resubmitted in process.

## Disposition by upstream area

| Upstream area | Disposition | Reason and required proof before reconsidering |
|---|---|---|
| `SamplingError::Auth` wire provenance plus auth retry budget | **Reject for current candidate** | a422's outcome is a successful recovery followed by an automatic re-submit. That directly violates P0's no-replay rule. Reconsider only after a versioned `ProviderAttemptReceipt` proves no output/effect and has a negative replay matrix. |
| Error-triggered compaction / `SamplerTurnOutcome::CompactAndResubmit` | **Reject for current candidate** | a422 keeps the outer `continue` path. P0 deliberately makes context overflow terminal rather than rewrites context and replays the request. |
| `is_context_length_error` text for `Current message … exceeds budget` | **Defer as an isolated parser candidate** | Useful classification, but it must be ported without connecting it to compaction/retry. It needs positive parser fixtures and a regression proving `handle_sampling_failure` still leaves conversation/context window unchanged. |
| Overload (`529`, stream overload) and `/btw` retry | **Defer** | The classifier can be valuable for a later *new task* routing/advice decision. Automatic `/btw` retry is outside the P0 receipt contract and must not be imported with the classifier. |
| Carry background tasks/subagents through compaction | **Defer, high conflict** | This overlaps TaskTree lineage, reviewed working ledger, tree budgets, and the pending ContextManifest/claim state machine. Importing it now risks losing parent/claim provenance. Require a task-tree recovery golden path first. |
| Session release, attached-client idle withholding, activity cleanup | **Defer, high conflict** | Lumen already has lifecycle changes (`16ddc314`, `2a3a9913`) and scheduler leases. Reconcile against the unified activity owner model before porting any upstream release code. |
| SDK round-trip liveness / large computer-hub changes | **Defer** | A cross-process liveness contract needs operation leases, heartbeat identity, timeout semantics, and fault-injection proof; the upstream patch is not a drop-in helper. |
| PTY reaping until registry removal | **Defer for targeted audit** | Potentially valuable hygiene, but it changes process ownership. It needs a test proving no descendant or registry leak across cancellation and shutdown. |
| `.grok/sandbox.toml` protected editing | **Defer for guard comparison** | Lumen has a stronger `lumen-guard` and explicit permission policy. First prove the upstream path neither weakens existing deny rules nor creates a bypass. |
| Pager/dashboard/session-delete/UI changes | **Reject for this R0 slice** | Broad UI rewrite is outside the source-safety integration slice and is not needed to close NextGen authority contracts. |
| Skill watcher and leader-soak refinements | **Defer** | These should be evaluated after the current CI and lifecycle contracts establish an exact source baseline. |

## Evidence run on the candidate

All commands below used the candidate source (runtime code at `2e778682` plus
its evidence-only source-lock suffix `9ae4762a`) and preserved raw exit status.

| Gate | Result |
|---|---|
| `bash scripts/check-artifact-freshness.sh` | PASS, 32 critical files match |
| `bash scripts/check-version-consistency.sh` | PASS, all version fields `2.0.0-alpha.1` |
| `bash scripts/test-readiness-contract.sh` | PASS |
| `cargo check -p xai-grok-shell` | PASS, exit 0 (49.82s); two pre-existing dead-code warnings |
| `cargo test -p xai-grok-shell --lib prefetch_env_ -- --nocapture` | PASS, 2 passed / 0 failed / 6,285 filtered |
| `cargo test -p xai-grok-shell --lib parse_output_issuer_claim_does_not_grant_xai_auth -- --nocapture` | PASS, 1 passed / 0 failed / 6,286 filtered |

These are only grouped R0 checks. They do not prove the full shell suite,
GitHub CI, release, provider behavior, M5/M6, live evaluation, or a 24-hour
daemon run.

## GitHub state at review

PR [#134](https://github.com/exergyleizhou-ux/lumen/pull/134) is mergeable but
not accepted. The older `2e778682` CI run had an Offline-gates failure caused
only by a stale `SOURCE_LOCK` hash for the execution book. `9ae4762a` refreshes
that lock, and its exact-SHA CI run `30732653905` was pending when this review
was written. A pending or a later green check is not a merge or release gate.

## Next R0 action

1. Wait for and inspect CI run `30732653905` by exact SHA; repair only an
   observed failure.
2. Build the R0-00 per-path manifest before any upstream port.
3. If a422's `exceeds budget` parser is selected, implement it as a standalone
   no-replay-safe patch with the listed negative test, not a cherry-pick.
4. Do not update the upstream pin or merge upstream until an accepted point
   integration has exact tests and an explicit conflict decision.
