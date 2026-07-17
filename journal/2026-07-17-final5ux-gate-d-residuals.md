# FINAL-5UX Gate D residuals (`codex/final-5ux-gate-d`)

Honest inventory after Gate D UI + runtime refresh. **Not** a ready claim.

## Explicitly blocked (missing runtime / human gates)

| Residual | Why blocked |
|----------|-------------|
| Interactive readiness wizard (provider → probe → trust UI) | No TUI runtime wizard surface; would be fake without multi-step probe API + isolation. CLI `scripts/probe-local.sh` remains external. |
| Startup auto-probe without live tool_call | Until a first real ACP tool_call or an external `apply_truth_probe`, capability stays `Unknown` (honest). |
| Provider-reported cache auto-feed | No stable TUI event carrying `CacheSource::ProviderReported` metrics; `note_truth_cache` API ready for a future sink. |
| Auto verification pass from agent goals | Goal verify fields are display-only; wiring without command/run_id would fabricate Passed. Manual/`note_truth_verification_passed` only. |
| Full interactive PTY color matrix 80×24 / 120×40 / 180×50 × truecolor/256/16/NO_COLOR | Environment/harness not driven in this pass; unit width matrix covers load-bearing truth-bar copy only. |
| Exhaustive Grok Build string deletion in docs/billing SuperGrok commerce | Non-goal; legal/upstream/billing/xAI product paths retained. |
| M5 onboarding + M6 productivity | Human gates; `ready` must stay false. |

## Implemented in this branch (do not re-list as open)

- Shared `Arc<TruthSnapshot>` + `truth_bar` / redacted `/status` / dashboard parity
- Runtime refresh: model switch, live tool_call, edit→stale verify, permission sync
- Chat-only edit block on permission path
- Static welcome logo + idle `TickDemand::None`
- Contract-gated `install_truth_snapshot`
