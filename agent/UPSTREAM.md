# Upstream pin

- Source: xai-org/grok-build
- Local import: ~/Desktop/grok-build-main
- Import date: 2026-07-16
- Remote tip observed: `ba76b0a` (upstream/main, 2026-07)
- Policy: **PINNED** snapshot. Security / correctness cherry-picks only.
  No auto-merge of upstream feature dumps. Never overwrite Expert dual,
  lumen-guard, DeepSeek defaults, or Lumen product defaults (Grok catalog
  default model stays ours).

## Cherry-picks applied (dialectical)

| Date | Upstream area | What we took | What we refused |
|------|---------------|--------------|-----------------|
| 2026-07-20 | `dispatch_locks` cancel/prompt race | Rename `prompt_intake_locks` → `dispatch_locks`; cancel holds lock; regression test `cancel_never_overtakes_in_flight_prompt_intake` | Full shell/hooks/pager dumps; model catalog; Expert surfaces |
| 2026-07-20 | OSC 52 clipboard kill switch | `osc52_disabled()` + route wiring; env `GROK_CLIPBOARD_NO_OSC52` **and** Lumen alias `LUMEN_CLIPBOARD_NO_OSC52` | Pure rename-only churn; pager rewrite |
| 2026-07-20 | `/summarize` pager alias | `RecapCommand::aliases() = ["summarize"]` + registry test | Compaction strategy changes; aggressive summarize defaults |
| 2026-07-20 | marketplace `require_sha` | `ensure_pinned` / `hoist_pin_slots` / `UnpinnedRemoteRefused`; `load_require_sha` + env (GROK + Lumen alias); gate on `install_from_remote_url` | Full `clone_operands` / `install_from_source` signature rewrite; auth/OAuth rewrites |
| 2026-07-20 | auth recovery (survey) | **SKIP** — `xai-grok-auth` sources match upstream line-for-line at pin tip | OAuth/mcp recovery dumps already covered on our side |

## How to port more later

1. `git fetch upstream`
2. Diff only the security/correctness file (not whole tree)
3. Port by hand into `agent/crates/...` (our tree has `agent/` prefix)
4. Keep Expert / lumen-guard / DeepSeek / defaults untouched
5. Add a row above + focused unit tests
