# Lumen Science Phase C delivery report

**Date:** 2026-07-23 (Asia/Shanghai)
**Worktree:** `/Users/lei/code/lumen/.worktrees/science-kernel`
**Branch/base:** `science/kernel` from `1ed0f9ce`
**Authority:** Rust Lumen is the sole execution, approval, verification, and
durable-state authority.

## Verdict

Phase C local implementation and fixture-backed product verification are
complete through C3. The public ChEMBL connector now also has an explicit L5
probe. The real-host SSH proof is **Blocked**, not failed or
silently waived: it requires a user-provided, explicitly authorized host,
account, host-key fingerprint, and disposable transfer data. Nothing was
deployed, merged, pushed, rebased, or tagged.

## C0, C1, and C2 evidence

- **C0 — durable Rust kernel and authenticated reads:** `14e1da9e`,
  `07559f01`, and `8a86f171` establish atomic durable runs/events/artifacts,
  authenticated owner-scoped result reads, restart/replay, and fail-closed
  persistence.
- **C1 — formal actor/permission product path:** `167e4e83` and `daebf471`
  route CSV work through the sole `SessionActor`, production permission
  bridge, workspace tool execution, and durable terminal states. The current
  complete L4 gate re-proves CSV allow, ACP cancellation, and approval timeout.
- **C2 — scientific files and connector reads:** `58704ec5`, `1b11422c`,
  `a4ad1e4a`, and `1ed0f9ce` add descriptor admission, content-sniffed previews,
  CSV/FASTA import, and PubMed/ChEMBL fixture-backed fetch pipelines with
  evidence, citations, and replayable artifacts. The current complete L4 gate
  re-proves import and connector fetch.

### Public connector L5 evidence

The explicit ignored `live_probe_chembl_real_search` probe passed on
2026-07-23: public `aspirin` retrieval returned 52 hits and CHEMBL25/ASPIRIN
as the first record. The complete redaction-safe evidence is
`outputs/evidence/gp5_chembl_live_probe.log`. This is a read-only public API
probe; ordinary test runs remain zero-network.

### B3 async correction

Commit `a4ad1e4a` corrected three vacuous CSV e2e bodies by adding the missing
terminal `.await`. It also moved evidence roots inside the product-enforced
workspace boundary and corrected ACP cancellation semantics. The present e2e
run takes 9.07 seconds; no 0.00-second async pass is accepted as evidence.

## C3 real SCP transport

C3 uses `/usr/bin/scp`, not a new network crate or second runtime. Admission
binds project, owner, target, timeout, egress, host-key fingerprint, and an
operation SHA-256. Raw hostname, paths, identity, key material, command line,
stdout, and stderr remain process-local. Execution recomputes the operation
digest and independently verifies the known-hosts key blob against the
approved fingerprint before starting SCP.

The debug-only fixture maps the allowlisted DNS-shaped
`fixture.lumen.test` to a temporary local `sshd`. Per-test identity, host keys,
known-hosts, and SSH config remain inside the temporary session workspace; no
`~/.ssh` state is read or written.

### Four-path L4 evidence

| Path | Durable result | Artifact invariant | Result |
|---|---|---|---|
| put | `Succeeded` | redacted transfer artifact registered | passed |
| get | `Succeeded` | bytes round-trip and artifact registered | passed |
| timeout | `TimedOut` | reopened store has zero artifacts | passed |
| cancel | `Cancelled` | reopened store has zero artifacts | passed |

These paths execute through ACP stdio, `MvpAgent` facade, `SessionActor`, the
production permission bridge, real child-process execution, and reopened
Science storage. Timeout and cancellation kill and reap the child.

## Verification gates

| Gate | Result | Evidence |
|---|---|---|
| Science unit/doc tests | 57 passed, 0 failed, 1 ignored | `outputs/evidence/gc3_science_rerun.log` |
| Shell library | 5669 passed, 0 failed, 13 ignored; 92.42s | `outputs/evidence/gc3_shell_lib_rerun2.log` |
| Science strict clippy | passed with `-D warnings` | `outputs/evidence/gc3_clippy_rerun.log` |
| Pager build | passed | `outputs/evidence/gc3_pager_build_final.log` |
| Complete Science L4 e2e | 7 passed, 0 failed; 9.07s | `outputs/evidence/gc3_science_e2e.log` |

After the C3 report, the Phase-C extension added the explicit ChEMBL L5
probe and S5 completion fencing. Its default science gate is **59 passed, 0
failed, 2 ignored** (`outputs/evidence/gp5_science.log`); the two ignored
tests are the only network probes. The post-P5 rebuild initially exposed and
then corrected a persistence-barrier type error in `verify_science_goal`.
The complete post-fix gates are:

| Gate | Result | Evidence |
|---|---|---|
| Shell library | 5674 passed, 0 failed, 13 ignored; 56.35s | `outputs/evidence/gp5_shell_lib_final.log` |
| Science strict clippy | passed with `-D warnings` | `outputs/evidence/gp5_science_clippy_final.log` |
| Pager build | passed; 2m02s | `outputs/evidence/gp5_pager_build_final.log` |
| Complete Science L4 e2e | 7 passed, 0 failed; 8.89s | `outputs/evidence/gp5_science_e2e_final.log` |

The shell-lib investigation found that test-only `MvpAgent` constructors with
`remote_settings=None` entered the production remote-prefetch fallback and
waited on network I/O. Test fixtures now supply an explicit empty remote
settings snapshot, making the gate deterministic and offline.

## Dependency and provenance audit

- C3 changes no `Cargo.toml` or `Cargo.lock`; added crate dependencies: **0**.
- External boundary: system OpenSSH `/usr/bin/scp`.
- Provenance: `third_party/provenance/transport-openssh.md`.
- Documentation: `docs/science/SSH_SCP_CONNECTOR_V1.md`.

## Main-worktree protection proof

The main worktree remains at `c3649f9b80c0a40fd5507b709387437ebf5bc87d`
with its pre-existing user modifications only:

```text
 M agent/crates/codegen/lumen-guard/src/bash.rs
 M agent/crates/codegen/xai-grok-shell/src/agent/config.rs
```

Neither protected file is part of the C3 worktree diff.

## Real-host proof

**Blocked pending explicit user authorization.** No default connection was
attempted and no ambient SSH credentials were inspected. Closing this item
requires the user to name an authorized non-loopback target and approve the
exact put/get test scope. This blocked item limits external-host evidence; it
does not invalidate the local real-OpenSSH fixture proof above.

## Post-C extensions and remaining decision list

- **Implemented locally:** `7d877055` wires S5 Goal × Expert ×
  HostVerification fencing into the sole Rust `SessionActor`; a consultant
  verdict is advisory only, while durable run/approval/artifact/evidence/
  provenance verification is required for Goal completion.
- **Implemented and live-verified:** explicit ChEMBL L5 probe above.
- **Audited but not admitted:** PDF/DOCX/XLSX/PPTX converters and Notebook
  execution. `docs/science/FORMAT_CONVERTER_SUPPLY_CHAIN_AUDIT.md` records the
  missing reproducibility, dependency-license, and runtime-isolation evidence;
  no unpinned system tool or Python converter has been added.

1. Keep Rust Lumen and `SessionActor` as the only execution and permission
   authority; do not introduce an Open Science Agent/ACP runtime.
2. Keep the production capability to bounded SCP put/get; remote shell, port
   forwarding, retries, and background recovery remain out of scope.
3. Keep system OpenSSH as the transport boundary for v1; add no network crate.
4. Keep the loopback SSH mapping debug-only and unavailable to production
   admission.
5. Keep real-host validation human-authorized and `Blocked` until exact host,
   identity, fingerprint, and disposable data are supplied.
6. Require successful transfers to register redacted artifacts and require
   timeout/cancel/failure to register none.
7. Preserve no-auto-resume: reopen converts non-terminal transport work to
   `Interrupted` and never retries a stale ticket.
8. Treat this report as local build/test evidence, not deployment or release
   evidence.

## Evidence hashes

```text
77b41f5d44acf8d1d005a3877783db3d1a50971636453b177a87299bade224c7  gc3_shell_lib_rerun2.log
ca68bc2262d80f6a578b119b7339dee253b8b89f1be72de0c7a87bbc74fd970e  gc3_science_rerun.log
ca76a6e7e24c4e73a893d51ad1d0441bb9e338dd95d6ffde271810cf0e86564e  gc3_clippy_rerun.log
627bd3cf969152d16796ce3dc80d170d930e0d6c00d7e67b3283d294b3f9f385  gc3_pager_build_final.log
a076f80b6eac0f1f22ade1ed312ebd5f3e9438ac818d95d5b1aeb5cb3e7c8b98  gc3_science_e2e.log
670d685228e58de0eb50ad1f136a65852b3077251640b189e29974159525a79e  transport-openssh.md
7d5b9714e99d7ec909e5c4fc53480b37b4e979c9222a22dda00b2a518153d725  gp5_chembl_live_probe.log
8617246d92227261f56a8623936122e99cdfde645555f49672a4226a0d1f42d4  gp5_shell_lib_final.log
dea27ada1a7760f5b296ecd0ef82decc17c13382930dd169125f8bd56f55b539  gp5_pager_build_final.log
61aee28e64f522773e75bc629f41ad38a195a1b29e0a438bc63166545887bd9a  gp5_science_e2e_final.log
45f75b19e5718ce74d75d27dc949e2b0cc21908a1e53dca4d31c24e03ebe8e09  gp5_science_clippy_final.log
```
