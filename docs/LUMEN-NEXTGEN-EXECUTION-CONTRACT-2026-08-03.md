# Lumen 2 — NextGen 最终执行合同书（差距→切片，2026-08-03 版）

**性质：** 本文件是 `docs/LUMEN-NEXTGEN-EXECUTION-BOOK-2026-08-01.md`（规范性总纲）与
`docs/nextgen/INVARIANTS.md`（硬规则）的**当前差距→可执行切片合同**。它不改变总纲语义；
它把"最终目标"翻译成：形态要求、差距表、按依赖排序的切片卡、每片必测/证据/Gate/停止条件。
所有【拟建】类型在提交前都不是现有 API。

**阅读顺序：** 先读 §0（最终形态）→ §1（当前基线）→ §2（差距）→ §3（切片合同）→ §4（纪律）→ §5（验收总闸）。

---

## 0. 最终目标的形态要求

### 0.1 一句话

> **Lumen 2 = 一个默认轻量、需要时才升级为"受治理任务树"的编码 Agent：主 Agent 说了算；
> 子 Agent 只能收缩能力、只能提案；事实靠 evidence + root 验收；预算与操作有账本；
> 崩溃要么可派生恢复、要么 Frozen；UI 只投影权威状态；短对话不拖一整棵树；
> 长任务才进 Kairos。全程没有第二套 runtime，也没有"模型说完成了"当完成。**

### 0.2 用户能看到的形态（产品面）

```text
Human / TUI / ACP
        ↓
主 Agent（depth 0）—— 唯一 execution / permission / evidence / completion authority
 ├─ Code 子代理（depth 1）—— 干活，但不能提权
 │    ├─ Research / Review / Test（depth 2）—— 默认只读/更窄
 │    └─ Evidence leaf（depth 3）—— 只读、不可再 spawn、不可写、不可联网
 ↓
每个结论 = evidence + root 验收才算事实（child 只能 Proposed）
崩溃/取消/晚到事件可恢复，不靠 PID、日志、模型口头说 done
```

具体用户可感知要求（每一条都是验收项）：

| 形态 | 要求 |
|------|------|
| 启动 | 显示 Lumen 品牌字标 + 产品版本 `2.0.0-alpha.x (commit)`；绝不再显示上游 `0.2.116` |
| 短任务（默认） | 不创建树/不写长期记忆/不调 Advisor/不等待 daemon（INV-23） |
| 升级提示 | 一旦需要并行/子 Agent/恢复/定时任务，先展示将升级为治理树的 scope/模型 pin/权限/预算/可取消性 |
| 树视图/ACP | 显示真实 owner/phase/budget/evidence；UI 状态全部来自 actor/journal 投影，不用文案伪造完成 |
| 子 Agent 行为 | 并行不乱写同一路径；取消干净；leaf 无法 spawn/write/network/bypass |
| 恢复 | 进程重启后 journal/read model 重放；external-effect 节点保持 Frozen 直至人工批准 |
| 顾问 | 主模型可按检查点咨询独立模型；咨询不能改事实、不能宣布完成（Advisor 只做第八道审阅） |
| 长期任务 | Kairos 可 claim/heartbeat/complete/fail/freeze/take_over；人能看、能冻结、能取消、能带证据继续 |
| 发布 | `v2.0.0-rc.1` 只在 R0 + exact-SHA CI + source/evidence tuple + 人工门全部闭合后出现 |

### 0.3 系统形态（Harness Kernel，工程面）

唯一 authority：**Rust `SessionActor`**（INV-1）。不存在第二编排 runtime；child、Advisor、
daemon、UI、worktree、PID、stdout 都不能自证成功或接受事实。

十二个平面必须共同闭环（总纲 §0.1.1）：

| # | 平面 | 必须成立 |
|---|------|----------|
| 1 | Identity & authority | 每次运行有 root-owned 身份；所有托管对象有 operation identity |
| 2 | Capability ceiling | 只缩不扩：root policy ∩ parent ∩ role ∩ approval |
| 3 | TreeBudget | 原子 check-and-reserve；恰好 release 一次；usage 不可得记 unknown 不记零 |
| 4 | SharedWorkingLedger | child 只能 Proposed；Accepted 必须 evidence + root |
| 5 | LongTermMemory | 只有 root 显式 promote；evidence 保留、幂等、可追溯 |
| 6 | AgentSandbox | 每节点 context/memory/tool/process/resource 隔离合同，可撤销 |
| 7 | GovernedEvidenceLoop | checkpoint/stop/escalate 由 actor 决定，不是模型 prose |
| 8 | ExpertConsultation | 已有独立第二意见（保留） |
| 9 | ClientAdvisor | 本地虚拟工具、先 shadow 后受限咨询；usage receipt 独立 |
| 10 | KairosSupervisor | 长期任务治理建立在 NG-03C operation API 之上 |
| 11 | Delivery/outbox | 权威事件不可静默 drop；未知即 Frozen |
| 12 | 产品证明 | exact-binary 零 provider golden path 全过才算 local-ready |

### 0.4 终局验收（总纲 §0.1.6 原文意译）

只有下列零 provider、零外部副作用的 **exact-binary** 场景全过，才可称 Harness Kernel v1 就绪：

```text
root 创建 envelope + immutable objective + accepted snapshot
  → depth-1 code 收到严格更小的 grant 和 budget 切片
  → depth-2 research/review/test 在隔离 scope 运行
  → depth-3 evidence leaf 无法 spawn/write/network/bypass
  → child 以 fixture artifact hash 追加 Proposed facts
  → root 拒绝一个 stale/conflict/unproven claim，接受一个 verified claim
  → verifier 发出 typed PASS/FAILED/SKIPPED/ERROR，无虚假投递
  → 一个分支被取消；late terminal 被 reconcile 而不是复活
  → crash/restart 重放 journal/read model；external-effect 节点保持 Frozen
  → 状态 UI 显示真实 owner/phase/budget/evidence；重建 binary 无 provider 调用
```

过这条只是 **Harness Kernel local-ready**；仍不等于 exact-SHA CI、Windows、release、真实 provider、
24h soak 或无人值守自动 commit。

### 0.5 版本梯子（发布形态）

| 身份 | 条件 | 现状 |
|------|------|------|
| `2.0.0-alpha.1` | 进行中源码 | **当前** |
| `v2.0.0-rc.1` | clean source A + evidence B + exact-SHA CI + SBOM/readiness + 人工门 | 未到 |
| 正式 2.0 | RC 稳定 + 产品门 + 发布事务 | 未到 |

上游 `xai-grok-version`（0.2.116）只是 Grok Build 协议/客户端身份，**永远不能**决定 Lumen 产品
version/tag/release（发布身份边界，总纲 §1）。

---

## 1. 当前基线（2026-08-03 审计快照；执行前必须重测）

```bash
cd /Users/lei/code/lumen
git status -sb                                  # 应在 sync/absorb-upstream-20260731，干净
git log --oneline -6                            # 最新应到 bc64ea36（evidence）
~/.local/bin/lumen --version                    # 应显示 2.0.0-alpha.1 (ce70a75f)
```

| 项 | 值 |
|----|-----|
| 分支 | `sync/absorb-upstream-20260731`（PR 功能在仓库被禁用；只能看分支/commit） |
| 功能源候选 | `ce70a75f`（UI 品牌：LUMEN 字标 + 产品版本 + 品牌文案） |
| 证据锁 HEAD | `bc64ea36` |
| 本机二进制 | `2.0.0-alpha.1 (ce70a75f)`（~/.local/bin/lumen） |
| 本地测试基线 | tools task 模块 227 ✓；pager lib 8003 ✓；pager-minimal 83 ✓；memory compose/golden 16 ✓ |
| 已知红 | 无（本轮修掉 auth 陈旧断言）；CI exact-SHA 全绿**未声明** |

已接线的 NextGen 资产（截至本快照）：
- lineage/depth 硬顶（HARD_MAX_SUBAGENT_DEPTH=3）、root 取消级联
- `GovernedOperationStore`：durable op create/claim/heartbeat/takeover/complete/fail/freeze/cancel-cascade、
  idempotency、foreign-tree 拒绝、JSON snapshot 落盘、fail-closed
- `BudgetLedger`（NG-03B）+ **coordinator 主路径接线**：spawn 前原子 reserve，失败回滚，finish/cancel
  settle+release 恰好一次，wall-time expire 关闭账本
- `TreeAuthorityLog`（NG-03C 接线）：SpawnReserved/SpawnClaimed/Terminal*/Cancelled，per-op no-revival，
  **JSONL 落盘**（task-tree-authority/{hex-root}.jsonl，重启 reload）
- memory 层：`CanonicalRecord`/payload_hash、`LifecycleJournal`（NG-00 全量磁盘 JSONL）、
  `EffectRecoveryClass`/`crash_action_for`（K4）、`ClaimAuthority`、`ContextManifestV1` admission、
  `WorkingMemoryLedger`（accepted-only 注入、derived_from 传播/无环、Progress 非 claim）
- `compose_ng03c`：BudgetLedger × GovernedOperationStore × LifecycleJournal 组合集成测试
- UI：LUMEN 盲文字标、`LUMEN_PRODUCT_VERSION`（= `lumen --version`）、品牌文案 Lumen

---

## 2. 差距总表（维度 → 现在 → 目标）

| 维度 | 现在 | 目标 | 主要缺口 |
|------|------|------|----------|
| Identity & authority（NG-01） | 75% | 100% | background/workflow 全部进统一 owner/operation identity；跨进程 lease/outbox/reconcile |
| Capability ceiling（NG-02） | 50% | 100% | 通用 grant/TTL/revoke token；spawn 前 actor 事务签发 |
| Tool contract（NG-02A） | 40% | 100% | dispatch 全路径强制；secret/artifact redaction 入 journal/UI |
| Tree budget（NG-03/03B） | 70% | 100% | token 预留额（现 reserve=0）；daily-cost/artifact 限额；legacy exhausted-set 收编 |
| Operation lease/journal/outbox（NG-03C） | 72% | 100% | S1 schema bridge + S2 outbox 原子快照已落地；待 Kairos API + outbox consumer/reconcile |
| WriteScope（NG-03D） | 70% | 100% | S3：overlap 纯函数 + spawn 前拒绝 + MergeReceipt 根侧 handoff；待 worktree auto-handoff 接线与 dirty-target fixture |
| Flow control（NG-03E） | 15% | 100% | bounded queue、DeliveryObservation、backpressure、fair share |
| WorkingLedger/claim（NG-04） | 60% | 100% | 全入口强制 accepted-only；rebase/conflict 语义 |
| ContextManifest（NG-04C） | 60% | 100% | 全入口 enforce；压缩/恢复重建 hash 一致 |
| derived_from（NG-04A） | 60% | 100% | 全图 enforcement（revoke 传播到消费方） |
| Sandbox（NG-04D） | 15% | 100% | AgentSandboxV1 统一签发；handoff packet；consumer enforcement |
| Evidence loop（NG-04E） | 15% | 100% | Node/Tree/Supervisor reducer；收敛/stop/escalate 合同 |
| 模型选择/Expert（NG-05） | 40% | 100% | P4b 唯一允许路线；provider health + no-replay failover 全审计 |
| Advisor（NG-06/06A） | 25% | 100% | ClientAdvisor virtual tool；shadow→受限咨询；usage receipt |
| Assignment（NG-07） | 10% | 100% | root-approved bounded assignment；Applied 全条件 |
| Kairos（NG-08） | 15% | 100% | KairosSupervisor 状态机；operator freeze surface；local proof |
| exact-binary golden（NG-09A/B） | 10% | 100% | 三层 shadow golden + regression corpus + bounded assignment 扩展 |
| R0/CI/release（R0、NG-10） | 25% | 100% | exact-SHA CI；A/B tuple；二阶段 release transaction；updater 隔离 |
| 产品品牌/UI 身份 | 75% | 100% | 细节打磨（欢迎屏、状态栏、菜单） |

**综合（保守加权）：约 38%**。积木层 ~50%；主路径接线 ~35%；Harness local-ready ~22%；RC ~15%。

---

## 3. 执行合同：切片卡

### 通用实施卡模板（每片必须含；缺 non-goals/负例/命令/停止条件/证据包任一，不得交给辅助模型）

```text
Slice ID / status / owner
Goal / non-goals
Prerequisite gate
Allowed paths / forbidden paths
Existing code/tests to read first
Proposed schema/API + compatibility
Persistence/migration/rollback
Steps
Positive cases / negative-fault cases
Exact commands + expected exit
Evidence packet
Exit Gate
Stop condition
```

### 切片排序原则（依赖图，总纲 §4）

```
R0-00/01 ──► S1 ──► S2 ──► S3 ──► S4
                     │        └──► S5 ──► S6(M1) ──► S7 ──► S10(NG-09A) ──► S11 ──► S12(NG-08)
S0(纪律) ─┘                                   └──► S8 ──► S9
S10 之后才可 S13(NG-10) ──► S14(R0-02..05 / RC)
```

- **S0** 每片都要做（不是阶段）。
- **S1–S5** 无 provider、无外部副作用，可直接开工。
- **S6（M1）** 是最早可给人看的产品证明，依赖 S1/S2/S5。
- **S10（NG-09A）** 是把所有积木串成完整 golden 的验收主轴。
- **S13/S14** 是发布事务，只在前置 gates 全过后进行。

---

### S0 — 纪律切片（每片内嵌，不单独排期）

- 每次源码改动：跑被改 crate 的相关测试 + 全量基线（tools task 模块、pager lib、memory golden）。
- 提交顺序：源码 commit → `bash scripts/source-lock.sh` → 证据 commit → `git push`。脏树提交/构建被拒是特性。
- 不给辅助模型派发缺 non-goals/负例/命令/停止条件/证据包的切片。
- 测试必须驱动真实入口（coordinator spawn 路径、pager 渲染函数、memory reducer），禁止硬编码期望值糊测试。
- 本地绿 ≠ exact-SHA CI 绿 ≠ 产品完成；CI 未跑写 `NOT RUN`。

---

### S1 — NG-03C-5：coordinator authority log 与 memory LifecycleJournal 统一（schema bridge）

**状态：** 已实施（源候选 `b48e01cf`，证据锁 `980dba01`；本会话复核 + future-schema 反例）。
前置：无（现资产：`authority_log.rs`、`lifecycle_journal.rs`）。
**目标：** 同一 authority 事件只存在一种 schema 语义。tools 不能依赖 memory（crate 环），
所以把 memory `LifecycleJournal` 的事件 envelope（NG-00 canonical、sequence/causal-parent/no-revival/payload_hash）
提升为**唯一权威 schema**，coordinator log 以兼容投影存在或直接复用同 schema 的局部实现；
二者的事件 id/kind 映射有单测锁死。
**已落地：** tools `AuthorityEventKind::{lifecycle_kind_str,from_lifecycle_kind_str,as_str}` +
`schema_version`；memory `authority_projection::{project_authority_event,project_authority_trail}`
（evidence：`op:`/`coord_kind:`/`reservation:`）；compose
`authority_log_outbox_and_lifecycle_projection_compose`。无 tools→memory 依赖。
**允许路径：** `agent/crates/codegen/xai-grok-tools/src/implementations/grok_build/task/authority_log.rs`、
`agent/crates/codegen/xai-grok-memory/src/{authority_projection,lifecycle_journal,compose_ng03c}.rs`。
**禁止：** 新增第三种事件格式；把聊天/artifact 混入日志（INV-21）。
**必测：** 事件 kind 映射双向一致（终端可逆；collapsed phase 靠 `coord_kind`）；序列单调；
no-revival per-op；JSONL round-trip；future `schema_version` fail-closed。
**命令：** `cargo test -p xai-grok-tools --lib -- authority_log`、
`cargo test -p xai-grok-memory --lib -- authority_projection lifecycle_journal compose_ng03c`。
**Gate：** `AUTHORITY_SCHEMA_UNIFIED_GATE=PASS`（本地；exact-SHA CI `NOT RUN`）。
**停止：** 若需要 tools 依赖 memory 才能证明，停下改方案（保持 crate 环为零）。

---

### S2 — NG-03C-6：outbox 原子落盘与 delivery observation

**状态：** 已实施（源候选 `b48e01cf`+本会话补测；证据锁随后续 source-lock）。前置：S1。
**目标：** `GovernedOperationStore` 目前整库 JSON snapshot；改为 op + outbox 的**原子追加**（JSONL 或
事务文件），使 terminal receipt / delivery observation（Delivered/Uncertain）与 state transition 同一次
落盘。投递未知 → `RecoveryRequired`/`Frozen`（INV-18），不假装已投递、不自动重放。
**已落地（单 store 原子性，显式范围）：** snapshot v2 `{schema_version,ops,outbox}`，
每次 create/claim/heartbeat/complete/fail/freeze/cancel/takeover 与 `OutboxRecordV1` 同一次
tempfile+rename；`list_outbox()`；legacy v1 裸数组仍可读；future schema fail-closed +
`PersistenceUnavailable`。**未接线：** outbox consumer/投递 worker、Kairos reconcile loop。
**允许路径：** `.../task/governed_operation.rs`、`coordinator.rs` 的 `observe_spawn_delivery`。
**必测：** state+outbox 同 sequence 同快照；reload 后 outbox 完整；
`mark_outbox_delivered`/`uncertain`；future schema 拒绝导入与 mutation。
**Gate：** `OUTBOX_ATOMIC_GATE=PASS`（本地单 store 范围；exact-SHA CI `NOT RUN`）。
**停止：** 跨 adapter 原子事务不在本片；已缩小到单 store 原子性并显式声明。

---

### S3 — NG-03D：WriteScopeLease v1（并行写防冲突）

**状态：** 已实施（核心 gate；不授权自动 merge）。前置：S1、S2。
**目标：** 每个写入 node 只能在 root 签发、限时、可审计的 write scope 内工作；**spawn 前**拒绝 overlap，
不是事后让模型猜合并。child 不自行 commit/push/merge。
**已落地：**
- `write_scopes_overlap` 纯函数（parent/child 前缀、disjoint、empty=legacy 不冲突、escape fail-closed）
- coordinator `governed_write_scope_conflict` 调用该纯函数；spawn 真实入口测试
  `governed_write_scopes_reject_overlap_but_allow_disjoint_live_children`
- `WriteScopeLease` + `enforce_write_scope_if_present`（symlink 解析、绝对 worktree root）
- 生产 writer（search_replace/edit/apply_patch 等）经 enforce 拒 scope 外写
- `MergeReceiptV1` + `evaluate_merge_handoff`：缺 root decision / stale base / empty change /
  revoked lease fail-closed；conflict 列表 → Conflict，不自动 Applied
**未接线：** worktree apply 自动生成 receipt 的 host 路径、dirty-target 与 worktree restore 全套 fixture、
child git commit 硬拒（依赖 shell/git policy 层）。
**Gate：** `WRITE_SCOPE_GATE=PASS`（本地 overlap + merge handoff + writer enforce + coordinator spawn；
exact-SHA CI `NOT RUN`）。
**停止：** 本切片不授权自动 merge/auto commit。

---

### S4 — NG-03E：Flow control、delivery uncertainty 与 liveness

**状态：** 部分实施（合同类型 + 首条生产接线）。前置：S2。
**目标：** 无界 channel 改为有界 + `DeliveryObservationV1`（Enqueued/Coalesced/Dropped/ReceiverClosed/Unknown +
QueuePressure）。UI token 可 coalesce；tool signal/grant/cancel/lease/terminal receipt/claim/evidence 不可静默 drop。
**已落地：** `delivery_observation::{DeliveryObservationV1,observe_std_sync_try_send}`（真实
`sync_channel` Full/Disconnected 单测）；`cache_epoch` durable evidence observer 经该合同观测
try_send，满/闭均 fail-closed 标 unavailable（INV-18）。
**未接线：** 全 actor/sampler mailbox 有界化、QueuePressure、token flood load fixture、
two-tree fairness、shutdown drain。
**Gate：** `FLOW_CONTROL_GATE=PARTIAL`（合同+cache_epoch；全路径 `NOT RUN`）。
**停止：** 一次 benchmark 通过不等于 daemon soak。

---

### S5 — NG-04D-1/2：AgentSandboxV1 schema + accepted-only 能力

**状态：** 待开工（Draft）。前置：S1。
**目标：** `AgentSandboxV1` DTO（canonical hash、expiry/revoke reason）；每节点只能读 AcceptedSnapshot、
写自己 branch 的 Proposed；sibling/foreign/scope 外一律拒绝（INV-6）。
**允许路径：** `xai-grok-memory/src/{context_manifest,governed_assignment,task_ledger}.rs`、
`xai-grok-tools/.../task/{types,coordinator,write_scope}.rs`、`xai-grok-shell/src/agent/subagent/*`。
**禁止：** caller 提供 parent/depth/permission/bypass；顺手重构 workspace/global memory。
**必测：** two siblings 同 snapshot 异 scratch；root 接受后 child rebase 才可见；handoff
foreign/stale/malformed/oversize/secret 全拒；depth-3 leaf 不能 spawn/write/network/bypass。
**Gate：** `SANDBOX_SCHEMA_GATE=PASS` + `SANDBOX_MEMORY_GATE=PASS`。

---

### S6 — M1：Governed Tree Preview（最早产品证明）

**状态：** 待开工。前置：S1、S2、S5 + 现有 NG-00/01/02/04A-C。
**目标：** exact rebuilt TUI/ACP binary 打开 offline fixture 显示三节点树（root→read-only child→
read-only grandchild）；grandchild 尝试 spawn/write/network/unknown ToolKind/读 sibling scratch 均
**typed deny**；root 接受一项带 artifact receipt 的 proposal 后，child 只在明确 rebase 后看到新 snapshot。
**禁止：** provider 调用、写文件、联网、Advisor、Kairos、自动 repair、promotion。
**必测：** 每条 deny 记录 `deny_mechanism: CapabilityCeiling | ToolFilter | SandboxEnforcement | LineageDepth`
（防证据挪用）；输出 `M1_GOVERNED_TREE_PREVIEW_GATE=PASS` 带 binary hash、fixture hash、tree projection、
deny reason、raw counts、source/evidence tuple。
**停止：** 出现 `0 tests matched`、mock-only UI 或绕过 SessionActor 的 projection 即 FAIL。
**意义：** M1 通过只说明基础产品可见；不说明三层写入、自动化、OS sandbox 认证或 24h daemon。

---

### S7 — NG-04E：Governed Evidence Loop 与收敛合同

**状态：** 待开工（Draft）。前置：S1–S6。
**目标：** Node/Tree/Supervisor 三个 pure reducer + typed fake event source；progress 仅因 obligation
discharge/refute/获批 refine/新 evidence 推进；repair 引用上轮 failure receipt；completion 只
`CompletionCandidate` 且 verify/host/root 三层后才 terminal；连续 no-progress、scope/budget/model 越界 →
`NeedsParentDecision`；不确定 → `RecoveryRequired`/`Frozen`。
**允许路径：** memory 新 module（复用 `governed_operation.rs`/`offline_golden.rs` 模式）；
tools `coordinator*.rs`；shell 仅 `acp_session_impl/run_loop.rs`、`session/expert.rs`、`session/workflow/*`。
**禁止：** workflow/Expert/coordinator 各存一套 loop state（authority 拆裂）。
**必测：** obligation/action-cycle/evidence-yield、repair limit、budget/deadline、rebase/conflict、
cancel/late event、verification failure、closed channel、intent-before-effect crash window 的 ± 矩阵。
**Gate：** `LOOP_CONVERGENCE_GATE=PASS`。

---

### S8 — NG-05：ProviderHealth、P0-NR-A 收口审计与 P4b 唯一路线

**状态：** P4a 已落地；收口待做。前置：S1。
**目标：** 审计所有同轮重投路径是否带 sealed receipt（NoOutput+NoToolCall+NotAttempted+NoExternalEffect）；
输出已发/thought/tool delta/effect unknown 一律 partial failure 不重放（INV-11）；P4b 成为普通 turn 与
后台任务唯一允许路线（用户池 + health + budget + privacy + no-replay）。
**必测：** pin 被绕过、pool exhausted、breaker open、schema mismatch、existing output、stale advice 全拒；
`GROK_MAX_RETRIES` 等 env 不能重新打开 safety closure。
**Gate：** `P0_NR_A_FULL_AUDIT_GATE=PASS`（含 P0-NR-A 负例矩阵复跑）。

---

### S9 — NG-06/NG-06A：AdvisorPolicy shadow + ClientAdvisor virtual tool

**状态：** shadow 已落地；ClientAdvisor Draft。前置：S8。
**目标：** 主模型可按检查点请求咨询独立模型：SessionActor 生成最小 capsule、执行本地 policy、
审计/限额/可取消；structured report + 独立 usage receipt；**永远不能**接受事实、切换模型或宣布完成。
**必测：** Advisor 超时/不可用 → Blocked 而非降级；advice 不能改 claim 状态；usage receipt 独立可审计；
shadow 模式所有 advice 落盘 `Shadow` 标记。
**Gate：** `ADVISOR_SHADOW_GATE=PASS`（ClientAdvisor 部分：`CLIENT_ADVISOR_GATE=PASS`）。
**停止：** 不复制供应商私有 header/flag；不把 prompt 当安全控制。

---

### S10 — NG-09A：三层 shadow-only offline golden path + Harness regression corpus

**状态：** Not started。前置：S1–S9 中除 S11/S12 外的全部（总纲：刻意不等 NG-07）。
**目标：** 用零 provider、零外部副作用的 rebuilt binary 证明 shadow-only 边界一起工作（§0.4 场景）；
同时交付 `NG-09A-1` 版本化 scenario corpus（5 个 corpus：authority / context-claim / execution-liveness /
provider-model / UX-provenance），每条含 input hash、fixture hash、expected transitions、negative mutation、
exact binary hash、raw exits；维护 `verification_debt` read model。
**必测：** §0.4 全场景；corpus 任一 policy/schema 变更先跑覆盖 corpus；新增功能必须带一个 negative mutation。
**Gate：** `HARNESS_REGRESSION_GATE=PASS` + `NG09A_SHADOW_GOLDEN_GATE=PASS`。
**停止：** 需要 live key/network/bypass 或 Applied advice 才能证明，说明 fixture/边界错误。

---

### S11 — NG-07：recommend 与 bounded assignment

**状态：** Not started。前置：S10（+ NG-01..06）。
**目标：** 仅当 new child/new turn、no output、no user pin、allowlisted/compatible、health 可用、
budget reserve 成功、privacy 允许、capability 不变、root approval 完整时可 Applied。
**禁止：** SetDefaultModel、替换 stream、静默 cross-provider、无限 spend、Advisor PASS→completion。
**反例：** user pin、private endpoint、budget exhausted、breaker open、existing output、stale advice。
**Exit：** 每个 Applied advice 有 root approval、actual model receipt、budget reservation、ledger decision。
**Gate：** `BOUNDED_ASSIGNMENT_GATE=PASS`。

---

### S12 — NG-08：KairosSupervisor local proof + OperatorControlPlane v1

**状态：** scheduler 局部有；统一状态机 Draft。前置：S2（operation API）、S10。
**目标：** KairosSupervisor 状态机（Draft→AwaitingScheduleApproval→…→Succeeded/Failed/DeadLetter/
Frozen/TakenOver/RecoveryRequired），接口只做 claim_run/heartbeat/complete/fail/freeze/take_over；
所有 tool/model/process 仍经 root actor。OperatorControlPlane：Inspect/Freeze/Cancel/ApproveResume/TakeOver
五个 typed command，全部写 operation journal；UI 只是 command client。
**必测：** fake clock、lease race、two supervisor、dispatch crash、duplicate outbox、expired approval、
root cancel、no replay、freeze during tool signal、restart after freeze、exact binary start/ready/crash/
reconcile/stop。
**Gate：** `OPERATOR_CONTROL_GATE=PASS` → `KAIROS_LOCAL_GATE=PASS`（仍不称 24h autonomous）。
**停止：** 外部副作用无 idempotency receipt 时永远 Frozen。

---

### S13 — NG-10：Delivery provenance、升级与 release transaction

**状态：** 基础资产存在（source-lock / install-local / verify-readiness / release.yml），**未在当前
candidate 上重新证明**。前置：R0_SOURCE_GATE、S10、S11、S12。
**目标：** `NG-10A ReleaseSourceTupleV1`（source A / evidence B / tag 指向 A）；二阶段 release
transaction（A clean source → B evidence suffix → V 独立验证 → T 人工授权 tag/release/install）；
修复 `scripts/release.sh` 先改 VERSION 再调拒绝脏树的 source-lock 的顺序 bug（加回归测试）；
updater（shell/PowerShell/xai-grok-update）在 RC 前只能 fail-closed 或迁移到签名 Lumen tuple。
**必测：** B 夹带 runtime/version/Cargo lock 必须失败；tag 指向 B 必须失败；binary stamp 与 A 不符必须失败；
干净机安装/upgrade/rollback 是单独 receipt。
**Gate：** `NG10_RELEASE_FOUNDATION_GATE=PASS`。
**非目标：** 本 phase 不授权当前 tag/push/publish/provider live/macOS notarization。

---

### S14 — R0-02..05 + exact-SHA CI + v2.0.0-rc.1

**状态：** 未闭合。前置：S0–S13 的合同 gates + R0-00/01。
**目标：**
- R0-02 clean source candidate（explicit paths commit；`git diff --check` 0）
- R0-03 integration candidate（fetch origin；conflict-tests.json）
- R0-04 source/binary/evidence 三段式（A build → lock → SBOM/readiness → B suffix；verifier 校验 B→A）
- R0-05 PR/merge/tag/release/install 分门（PR≠merge≠tag≠release≠install）
- 对 source candidate 跑 exact-SHA GitHub CI，明确记录 URL + conclusion
- 全部闭合后才创建 `v2.0.0-rc.1`（tag 指向 A）

**证据：** commits.json、integration-decision.md、SOURCE_LOCK、SBOM、readiness、每 contract gate receipt、
exact-SHA CI URL、rollback 记录。
**Gate：** `R0_SOURCE_GATE=PASS` + 各 `<CONTRACT>_GATE=PASS`（R0 的绿不能替代 contract gate，反之亦然）。
**停止：** origin 再前进、CI 失败、冲突无可验证裁决、B 不是 A 的 allowlisted evidence suffix——任何一项
出现即停。

---

## 4. 纪律（硬规则，违反即回退）

1. **证据优先于断言**：任何"完成/绿"声明必须有命令输出；CI 未跑写 `NOT RUN`。
2. **SOURCE_LOCK 是证据不是摆设**：源码 commit → lock → evidence commit → push，顺序不可交换。
3. **不发明完成**：`Progress` 不是 claim；`confidence` 不驱动决策；模型说 done ≠ done。
4. **fail-closed 是默认**：任何不确定（sequence gap、unknown owner、missing receipt、effect unknown）→
   `RecoveryRequired`/`Frozen`，禁止自动 retry/replay。
5. **不扩大成不可收敛重写**（总纲 §3.4.7）：每片有边界，越界先停。
6. **测试驱动真实入口**：禁止 mock-only UI、0 tests matched、硬编码期望。
7. **版本身份边界**：产品版本只看 VERSION/`lumen --version`；上游 0.2.116 只做协议身份。

## 5. 验收总闸（最终"完成"的定义）

```text
S0–S14 全部合同 gates PASS
＋ R0_SOURCE_GATE=PASS（exact-SHA CI 记录在案）
＋ 每条 golden path 用 rebuilt exact-source binary 真跨 ACP/TUI seam 跑通
＋ verification_debt == 0（无 Blocked/Frozen/unverified patch/未消费 advice/NOT RUN gate）
＋ v2.0.0-rc.1 tag 指向 clean source A（不是 evidence B）
→ 此时才可称 "Harness Kernel local-ready"；
   仍不等于：Windows、真实 provider、24h soak、无人值守自动 commit、正式 2.0 发布。
```

**本合同的当下第一步（建议开工顺序）：**
1. S1（authority schema 统一）—— 快、无 provider、把两套日志钉死；
2. S6（M1）—— 最早能给人看的产品证明；
3. S10（NG-09A）—— 验收主轴，把积木串成完整链路。
