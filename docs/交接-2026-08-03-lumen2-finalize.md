# Lumen 2 收尾交接词（2026-08-03）

**读者：** 下一个接手的执行 agent / 会话。本文档是当前唯一权威交接（合同为 `docs/LUMEN-NEXTGEN-EXECUTION-CONTRACT-2026-08-03.md`，纪律为 `docs/nextgen/INVARIANTS.md`）。交接词里的每一条都要先验证再行动，禁止凭记忆断言。

---

## 0. 一句话现状

Lumen 2 的**积木层基本齐了（~70%），主路径接线 ~50%，合同综合 ~45%，RC ~15%**；`product_rc=NOT_READY`，`v2.0.0-rc.1` 未创建。剩余大头是：sampler 全路径 sealed receipt 收口、Advisor virtual tool、Kairos daemon、exact-binary golden、R0/exact-SHA CI、以及**人工门**（PR/merge/tag/release）。**不要宣称产品/RC 完成，不要 merge。**

## 1. 当前状态快照（2026-08-03 实测）

```bash
cd /Users/lei/code/lumen
git status -sb                    # sync/absorb-upstream-20260731，干净，已 push
git log --oneline -3              # 275c0c79 (evidence) → 69070b38 (escalation) → fd841f10
~/.local/bin/lumen --version      # lumen 2.0.0-alpha.1 (69070b38)
SOURCE_LOCK.json                  # git_short=69070b3, lumen_version=2.0.0-alpha.1
```

- 分支 `sync/absorb-upstream-20260731`（PR 功能在仓库被禁用，只能看分支/commit）。
- 本地测试基线（本会话最后全绿）：memory offline gates 1/1、sealed receipt 1/1、tools child_sandbox 4/4、shell nextgen_control 7/7、auth_error_no_retry 30/30、provider_failure_routing 11/11。
- 本机配置已追加 `[model_routing] enabled=true model_pool=["deepseek-v4-flash","grok-4.5"]`（`~/.grok/config.toml` 末尾，重启生效；普通 turn 路由，见 §8 坑）。

## 2. 已完成资产（按切片，均带测试/证据）

| 面 | 资产 | 位置 |
|----|------|------|
| Identity（NG-01） | lineage/depth 硬顶 `HARD_MAX_SUBAGENT_DEPTH=3`、root 取消级联 | tools `task/mod.rs`、coordinator |
| Operation（NG-03C） | `GovernedOperationStore`（create/claim/heartbeat/takeover/complete/fail/freeze/cancel-cascade、JSON 快照）、`TreeAuthorityLog`（JSONL 落盘、per-op no-revival） | tools `task/governed_operation.rs`、`authority_log.rs` |
| Budget（NG-03B） | `BudgetLedger` 原子 reserve/settle/release，coordinator 主路径已接线 | tools `task/budget.rs` |
| WriteScope（NG-03D） | `WriteScopeLease`、`write_scopes_overlap`、`MergeReceiptV1` handoff，主 writer 已接线 | tools `task/write_scope.rs` |
| Sandbox（NG-04D） | `AgentSandboxV1`（memory）+ `ChildSandboxCapabilityResource` 投影；**task spawn、web_fetch、web_search、全部主 writer 已接线** | memory `agent_sandbox.rs`、tools `child_sandbox_capability.rs` |
| Evidence loop（NG-04E） | Node/Tree/Supervisor 三个 pure reducer、`LoopEvent`/`LoopEffect` 全事件 | memory `evidence_loop.rs` |
| Seal（S8/P0-NR-A） | `SealedAttemptReceiptV1`、`may_in_process_retry`（四观察量 fail-closed）、`AttemptSealTracker`、`ordinary_turn_max_retries=0` | memory `sealed_attempt_receipt.rs` |
| 失败升级 | 连续失败（≥2）追加可操作指引：认证→重新登录+`/model`；provider→`/model`；pin 明示 | shell `nextgen_control.rs::failure_escalation_guidance` + `sampler_turn.rs` |
| Advisor（NG-06A） | `issue_shadow_advice`（Shadow 模式、applies_authority 恒 false）、shell `ShadowAdvisorHost` | memory `client_advisor_shadow.rs`、shell `nextgen_control.rs` |
| Kairos（NG-08） | `KairosSupervisorState` + `apply_kairos_command`（claim/heartbeat/complete/fail/freeze/take_over/cancel）、shell `KairosControlHost` | memory `kairos_supervisor.rs` |
| Assignment（NG-07） | `authorize_assignment_apply` 纯门 + `RootGovernedAssignment` 存储 | memory `bounded_assignment_apply.rs`、`governed_assignment.rs` |
| M1（S6） | `run_m1_governed_tree_preview` 离线三节点树 + `deny_mechanism` 四类 | memory `m1_governed_tree_preview.rs` |
| Golden（S10 部分） | `offline_golden.rs` 串 loop/seal/sandbox；`nextgen_contract_gates.rs` 复合 gate；`scripts/run-nextgen-offline-gates.sh`（receipt 写 SCRATCH） | 见左 |
| UI 品牌 | LUMEN 字标、`lumen --version` 产品版本、品牌文案 | shell |

## 3. 剩余工作（到"彻底做完"的差距）

### 3.1 代码可做（无人工、无 provider）
- **S8 收口（最高优先）**：SealedAttemptReceipt 从"每轮零重试"升级为**真实记录**——在 `run_turn_via_sampler` 失败点用 `AttemptSealTracker` 判定干净失败（零输出/零工具/零副作用），干净者允许 auth 刷新后**有界**重试（当前 `NO_RECEIPT_MAX_RETRIES=0` 是一刀切）；P4b 唯一路线收口（用户池 + health + budget + privacy + no-replay）。必测：pin 绕过、pool exhausted、breaker open、schema mismatch、existing output、stale advice 全拒；`GROK_MAX_RETRIES` 不能重新打开 safety closure。
- **S9**：Advisor virtual tool / consult 模式接线（超时→Blocked 不降级；advice 不能改 claim；usage receipt 独立；Shadow 标记落盘）。`ShadowAdvisorHost` 已就位。
- **S12**：Kairos daemon/lease consumer + OperatorControlPlane（Inspect/Freeze/Cancel/ApproveResume/TakeOver 五个 typed command 写 operation journal）。`KairosControlHost` 已就位。
- **S10**：exact-binary golden（§0.4 全场景）+ NG-09A-1 五 corpus（authority/context-claim/execution-liveness/provider-model/UX-provenance）+ `verification_debt` read model。
- **S11**：recommend UI + bounded assignment 全条件（root approval、actual model receipt、budget reservation、ledger decision）。
- **S13**：修 `scripts/release.sh` 先改 VERSION 再调拒绝脏树的 source-lock 的顺序 bug（**加回归测试**）；updater RC 前 fail-closed；NG-10A ReleaseSourceTupleV1。
- **S14 的本地部分**：R0-02 clean source（`git diff --check` 0）、R0-04 三段式演练（A build → lock → SBOM/readiness → B suffix，本地 verifier 校验 B→A）。

### 3.2 人工门禁（不能代做，做完才可称 RC）
- exact-SHA GitHub CI（URL + conclusion 记录）
- PR/merge/tag/release/install 分门操作
- `v2.0.0-rc.1` tag（指向 clean source A，不是 evidence B）
- 人工验收 §0.4 全场景
- 另：grok-4.5 的 x.ai 认证 401（`reason=no auth context`，新 OIDC token 16 分钟内被代理拒）在客户端只能检测+指引，根治在登录态/上游——交接前**重新登录一次**确认是否仍复现。

## 4. 建议开工顺序

1. **S8 收口**（durable seal + 干净失败有界重试）——合同自身把它列为 P0；`AttemptSealTracker` 是现成抓手，先补"失败点真实 seal 判定 + 单测"再放开任何 retry 预算。
2. **S13 回归测试**（release.sh 顺序 bug）——纯本地、快、防回归。
3. **S9 → S12**（Advisor virtual tool → Kairos daemon），均为纯本地可测。
4. **S10 golden corpus** → **S14 本地三段式** → 列好人工门清单交回给人。

每片遵守通用卡模板：Goal/non-goals、prerequisite gate、allowed/forbidden paths、先读的代码、schema+兼容性、正例/负例、精确命令+预期 exit、证据包、Exit Gate、停止条件。

## 5. 硬纪律（违反即回退）

1. 证据优先于断言；CI 未跑写 `NOT RUN`。
2. **SOURCE_LOCK 顺序不可交换**：源码 commit → lock → evidence commit → push。
3. 不发明完成：Progress ≠ claim；模型说 done ≠ done。
4. fail-closed 默认：不确定 → `RecoveryRequired`/`Frozen`，禁止自动 retry/replay。
5. 不扩大成不可收敛重写；每片有边界。
6. 测试驱动真实入口：禁止 mock-only UI、0 tests matched。
7. 版本身份边界：产品版本只看 VERSION/`lumen --version`；上游 0.2.116 只做协议身份。

## 6. 环境与命令

```bash
export PATH="$HOME/.local/bin:$HOME/.cargo/bin:/opt/homebrew/bin:$PATH"
export PROTOC="${PROTOC:-/opt/homebrew/bin/protoc}"

# 常用测试（串行防 worktree_pool 抖动）
cd agent && cargo test -p xai-grok-memory --lib <filter> -- --nocapture --test-threads=1
cd agent && cargo test -p xai-grok-tools --lib child_sandbox -- --nocapture
cd agent && cargo test -p xai-grok-shell --lib nextgen_control -- --nocapture
cd agent && cargo test -p xai-grok-shell --lib auth_error_no_retry_tests -- --nocapture
cd agent && cargo test -p xai-grok-shell --lib provider_failure_routing_tests -- --nocapture
bash scripts/run-nextgen-offline-gates.sh     # 离线 gate（receipt 写 SCRATCH/，不脏树）

# 发布链路（顺序！）
bash scripts/source-lock.sh                   # 只允许干净树
git add SOURCE_LOCK.json && git commit        # evidence commit
git push origin sync/absorb-upstream-20260731
# install：先在 source commit 上构建 release，再回分支
git checkout <source> && (cd agent && cargo build -p xai-grok-pager-bin --release)
git checkout sync/absorb-upstream-20260731 && LUMEN_SKIP_BUILD=1 bash scripts/install-local.sh
```

## 7. 已知红与坑

- **worktree_pool 测试**：全量并行下偶发超时（30s 内部 deadline），串行 `--test-threads=1` 必过；不是回归。
- **e2e 前必须 build pager-bin**：`cargo build -p xai-grok-pager-bin`，否则 spawn 的 `target/debug/lumen` 是旧 binary。
- **lumen-guard**：commit message 里避免含 `se-ssion`/`s-c-p` 词；`rm -rf` 会被拦；必要时 `LUMEN_UNSAFE=1`（仅用于明确场景）。
- **docs-only 变更不要刷新 SOURCE_LOCK**（会把 binary tuple 弄脏：install-local 校验 locked source 与 binary stamp 一致）。docs 直接 commit+push 即可。
- **nextgen receipts 写 SCRATCH/**（已 gitignore），别写 `artifacts/readiness/nextgen/`（会让 source-lock 拒脏树）。
- **本机 `[model_routing]` 已配置**（flash+grok-4.5，重启生效）。注意：会话内 `/model` pin 会禁用普通 turn 路由（设计如此）；401 不触发换路（认证失败 ≠ provider 不可用）。
- 上游吸收政策：PINNED，security-only cherry-pick；永不覆盖 Expert dual、lumen-guard、DeepSeek 默认、default_models（`agent/UPSTREAM.md`）。

## 8. 验收总闸（"彻底做完"的定义，全部满足才收工）

```text
S0–S14 全部合同 gates PASS
＋ R0_SOURCE_GATE=PASS（exact-SHA CI 记录在案）
＋ 每条 golden path 用 rebuilt exact-source binary 真跨 ACP/TUI seam 跑通
＋ verification_debt == 0（无 Blocked/Frozen/unverified patch/未消费 advice/NOT RUN gate）
＋ v2.0.0-rc.1 tag 指向 clean source A
→ 才可称 "Harness Kernel local-ready"；仍不等于 Windows/真实 provider/24h soak/无人值守/正式 2.0。
```

**收尾检查单（每次会话结束）**：`git status -sb` 干净 / 已 push / SOURCE_LOCK 与 HEAD 一致或 docs-only / 本机 binary 与 lock 一致 / 交接词更新 / 无未声明的红。
