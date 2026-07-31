# IMPORT_LEDGER

| When | Source | Destination | Policy |
|------|--------|-------------|--------|
| 2026-07-16 | ~/Desktop/grok-build-main | agent/ | Full pin; exclude target/.git |
| 2026-07-31 | xai-org/grok-build @ dd04f397 (14 days / 15 syncs / 0.2.102→0.2.116) | agent/ | Milestone absorb via graft+mirror 3-way merge (branch `sync/absorb-upstream-20260731`); red-zone: upstream floor + lumen behavior patches (BYOK/lumen-discipline/expert/science/cache-epoch); 273 conflicts resolved; skills dir kept (lumen product asset); xAI-specific infra (signed_policy/extra-ca/otel) absorbed as-is, flagged in report |
| 2026-07-16 | ~/lumen evals/tasks 01-08 | evals/tasks/ | Tier1 coding eval |
| 2026-07-16 | new tasks 09-20 | evals/tasks/ | Tier2/3 |
| 2026-07-16 | ~/lumen internal science/oasis/quant | packs/ | Verticals |
| 2026-07-16 | FINAL-2.0 desktop doc | docs/masterplan/00A,09 | Contract extract (no full re-import) |

Secrets: never import `.env`, `*.pem`, API keys into git.
Refresh lock: `./scripts/source-lock.sh`
