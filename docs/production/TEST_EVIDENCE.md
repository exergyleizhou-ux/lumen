# Lumen Release-Candidate Test Evidence

## Final local gate (2026-07-13)

Repository: `feat/lumen-production-runtime`; gate input HEAD `ae2eee5`.

| Gate | Result | Observed duration |
| --- | --- | ---: |
| `gofmt` over `main...HEAD` Go files | PASS; no branch formatting diff | <1s |
| `go test -race ./...` | PASS across 116 packages | approximately 30s |
| `go vet ./...` | PASS | 2s |
| `go build ./...` | PASS | 5s |
| `go test ./... -count=3` | PASS, three uncached executions requested | approximately 20s |
| `git diff --check main...HEAD` | PASS | <1s |

Go reports packages without tests separately; the meaningful failure count for
each command above is zero. The `-count=3` command reruns test binaries three
times rather than reporting three independent package records, so this document
does not invent a test-case count from package output.

## Scope covered by committed tests

The branch includes deterministic and race coverage for JWT rejection, owner
non-enumeration, 100-Run tenant concurrency, guarded paths/symlinks, controller
isolation, durable state/event replay, Postgres CAS/idempotency, approval
consume/crash/reapproval, usage and quota leases, Artifact compensation and
restart replay, evidence redaction and limits, request size/origin/CSP handling,
Code/Lab snapshots, readiness, and detached Run cancel/terminal races.

Phase 9 local-stack acceptance used Oasis-migrated Postgres/control plane, a real
five-minute Oasis token, object/workspace storage, and loopback Caddy. It proved
unauthenticated rejection, authenticated Code/Lab reads, readiness, exact-origin
SSE with durable event IDs, and explicit terminal failure for a deliberately
invalid provider credential. It did not call a real production model.

## External live-evaluation blocker

The committed 10-Code/10-Lab `controlled-v1` report validates the deterministic
evaluation harness only. A live Qwen or DeepSeek capability baseline was not run
because it requires an operator-approved provider key, current prices, and paid
network calls. No fixture metric is represented as real-model performance. See
`MODEL_EVAL.md` for the exact four-part opt-in and command.

## Release-candidate worktree

`git status --short` is empty at final handoff. Five unrelated user-owned
`cmd/lumen` changes were preserved verbatim in the local stash named
`preserve user cmd UI changes before Lumen production RC handoff`; the stash is
not part of this branch and can be restored later with `git stash pop`.
