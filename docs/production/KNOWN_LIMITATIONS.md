# Lumen Production Candidate: Known Limitations

1. **No public production action was authorized.** The branch has not been
   pushed, merged to `main`, exposed publicly, or run against production data.
   Production TLS, DNS, private routing, backups, monitoring, and secret-manager
   integration therefore remain operator gates.
2. **Live model quality is unmeasured.** The deterministic 20-task report is a
   controlled fixture. Qwen/DeepSeek live evaluation is blocked on a real
   provider credential, approved spend/network access, and current price inputs.
3. **The local hosted smoke used an intentionally invalid provider credential.**
   It proves explicit failure and durable proxy/SSE behavior, not successful
   model inference. A real-token Code edit/verification and Lab Artifact flow is
   required during the authorized staging/production change.
4. **Observability is platform-owned.** Lumen intentionally does not expose an
   unauthenticated application `/metrics`; production dashboards and alerts must
   consume private Oasis/platform exporters and be validated after deployment.
5. **Schema rollback is conditional.** Application-image rollback is prepared,
   but destructive database down-migration is unsafe after incompatible writes.
   Operators must prefer a forward fix or approved snapshot restoration.
6. **Local mode is intentionally less restrictive.** Desktop SQLite and local
   provider configuration remain supported. They are not a substitute for the
   hosted JWT, Postgres, object store, quota, and platform-provider boundaries.
7. **Unrelated user changes are preserved outside the release candidate.** Five
   pre-existing modified `cmd/lumen` files are outside this feature branch's
   `main...HEAD` Go diff. They are preserved verbatim in the local stash named
   `preserve user cmd UI changes before Lumen production RC handoff`, leaving
   the release-candidate worktree clean. Restore them with `git stash pop` only
   when returning to that separate UI task.
