# Lumen NextGen — Normative Invariants

**规范依据：** `docs/LUMEN-NEXTGEN-EXECUTION-BOOK-2026-08-01.md`（§2 authority 边界、§24 产品前提、§22 拒绝清单）
**性质：** `INV-*` 是规范性硬规则；本书/各 phase 状态与 Git/CI 数字只能由生成审计快照更新。任何违反 `INV-*` 的实现改动必须被拒绝，除非先修订本文件并附带迁移/回退证明。

## Authority

- **INV-1** Rust `SessionActor` 是唯一 execution / permission / evidence / completion authority；不存在第二编排 runtime。child、Advisor、daemon、UI、worktree 文件、PID、stdout 均不能自证成功或接受事实。
- **INV-2** child 只能 `Proposed`（own branch）；`HostVerified/Accepted/Rejected/Conflicted/Superseded/Revoked` 转移唯一写者是 root SessionActor，且需 evidence 非空、grant 未撤销、snapshot 未 stale。
- **INV-3** child 不继承 root 的 yolo/bypass/PermissionHandle 提权、不继承 MCP 工具面（depth≥1 默认 deny）、不读取 sibling scratch/未接受 proposal/raw root chat/secret。leaf（depth=HARD_MAX）不可 spawn、不可写、不可联网。
- **INV-4** capability 只能单调收缩：effective = root policy ∩ parent ceiling ∩ role ∩ operation approval；任何路径不得扩张。无 grant/admission 的 governed child 一律拒绝（fail-closed）。
- **INV-5** RootBypassPermission 必含 issuer=root、exact scope、human-visible reason、expires_at、nonce+audit_id、可撤销、不可继承；缺失任一字段即无效。

## Memory / Facts

- **INV-6** 每节点可读的当前任务事实仅为 `TaskContract + ContextManifest + AcceptedSnapshot`；Proposed/Conflicted/Revoked/foreign/scope 外 claim 不得注入任何模型输入。
- **INV-7** 长期记忆只能由 root 显式 `/memory promote` 提升（evidence 保留、幂等、可追溯）；child 永无 promotion 权。
- **INV-8** accepted 而无非空 `evidence_ref` 的记录在 reload 时 fail-closed；torn/foreign/unknown-schema 记录进入 `NeedsRecoveryReview`/`Frozen`，禁止自动 promotion。

## Evidence / Loop / Provider

- **INV-9** terminal success 仅在 evidence hash、verify receipt、scope/provenance、root acceptance 可重算时成立；`Some(Pass)` 只证明 edit delivery。`CompletionCandidate` 不是 success。
- **INV-10** 无进展（连续相同 progress fingerprint）、未知 effect、未知 delivery、queue/effect/lease 不确定时只能 `Blocked`/`RecoveryRequired`/`Frozen`，禁止自动 retry/fallback/replay；loop repair 必须引用上轮 failure receipt 且受 repair cap 约束。
- **INV-11** provider 仅在 sealed `NoOutput + NoToolCall + NotAttempted + NoExternalEffect` receipt 下可切换/重试；已输出、thought、tool delta、effect unknown 一律 partial failure，不重放。`GROK_MAX_RETRIES` 等 env/config 不得重新打开 safety closure（actor policy 是上限）。
- **INV-12** 强类型 admission 减少绕过，但 actor 每次 dispatch 仍须重验 grant/lease/cancel；stale epoch、revoked grant、过期 manifest 不得用于新 dispatch。

## 资源 / 并行 / 副作用

- **INV-13** 并行写入只允许 root-signed、path-scoped、可撤销的 `WriteScopeLease`；child 不得自行 commit/push/merge；stale base / overlap / dirty target 不自动合并。
- **INV-14** 每个可托管 child/terminal/monitor/scheduler fire/workflow run 有 root-owned operation identity（lease/heartbeat/receipt）；PID、tmux、内存 registry、日志文本不得充当恢复状态。
- **INV-15** ExternalEffect 无 idempotency receipt 时崩溃后只能 `Frozen`；未知 publish/effect outcome 先 remote reconcile，不自动重发。
- **INV-16** 预算/usage 缺失记 unknown，不记零；reserve 在 success/fail/cancel/timeout 恰好 release 一次；usage 可验证才扣减。

## 数据 / 隐私 / 流控

- **INV-17** credential/raw secret/protected path/受限 artifact 不进入 manifest、ledger、status、preview；只允许 `SecretRef`/redacted hash。redaction miss 或 retention owner 不明 fail-closed。
- **INV-18** authority event（tool signal、grant/cancel、lease、terminal receipt、claim/evidence）不可静默 drop；`try_send`/closed channel/sequence gap 必须产生 delivery observation，之后状态 `RecoveryRequired`/`Frozen`。UI 可 coalesce 非权威内容。
- **INV-19** unknown schema / partial migration / rollback 到旧 binary：新 contract consumer 关闭，旧 binary 只读明确兼容记录，其余 `Frozen(UnknownSchema)`；GC/delete 以 tombstone + deletion receipt 执行，不删唯一 evidence/journal。

## 编码 / 事件（NG-00）

- **INV-20** 所有 hash-bearing record 使用唯一 canonical encoding（revision 在 preimage 内）；跨 revision 静默比较/混写禁止，revision 变更等价于显式 migration/rehash 事务。
- **INV-21** 统一的是逻辑 authority event 与因果顺序（tree_id/tree_sequence/causal_parent/actor_owner/event_kind/payload_hash/durability/encoding_revision），不是单一物理日志；禁止把聊天、大 artifact、effect 汇入一份巨型文件。
- **INV-22** clock/filesystem/process/network/random 均以 typed observation/input event 进入 reducer；不每秒持久化 TimeTick（deadline/heartbeat/host sleep-wake 只在 policy 边界产生事件）；macOS 唤醒后 lease/effect 先 reconcile。

## 产品边界

- **INV-23** 默认短任务（`interactive_single_turn`）不创建 tree/ledger/daemon 持久化、不调用 Advisor；升级为 governed profile 是一次性不可降级 admission，失败即 `Blocked(AdmissionUpgradeFailed)`。
- **INV-24** 未授权 provider/billable 调用、deploy、release、自动 merge/push/tag、24h autonomy 宣称均需单独 human/live authority；`NOT RUN` 不隐藏。

## 派生与失效（NG-04A）

- **INV-25** `derived_from` 由 actor 从 manifest+AcceptedSnapshot 推出；child 只能在该集合内收窄，不能扩展或伪造来源；source 必须存在且同 tree，dangling/foreign 一律拒绝。
- **INV-26** `derived_from` 图必须无环：环成员没有稳定标定，一律 `Frozen`（绝不因存储状态为 Accepted 而视为真相）；失效传播是 K2 意义上的纯函数（同 justification 集必得同 affected 集）。
- **INV-27** 撤销/revoke 的后果集由 justification 图传播确定（间接 consumer 不遗漏）；无关分支不受影响；传播只读，不自动改写任何 claim。

## 效果与恢复（NG-03C/K4）

- **INV-28** 每个效果的恢复类由外部世界能力决定（Pure / Idempotent / Queryable / Opaque），不是按严重程度分类；`Opaque` 效果的崩溃恢复唯一动作是 `Frozen`，无人值守模式不得授予 `Opaque` permit。
- **INV-29** 无 `CompensationReadyReceipt`（补偿自身为 Idempotent/Queryable 且有 receipt）的写不得标记为可补偿；补偿由人决定、机器执行、逐路径验证；`Opaque` 的"补偿"不叫补偿。
- **INV-30** `RecoveryRequired` 必须带 procedure_id，且该 procedure 有确定性收敛 fixture；无 procedure 的 RecoveryRequired 立即升级 `Frozen`。

## 观察、时钟与存储

- **INV-31** replay 只消费已记录 observation event；replay 模式下 adapter 被隔离，绝不重放未记录输入或重新执行外部效果。
- **INV-32** in-process 时序判定只用 monotonic clock；wall clock 仅用于展示与跨进程记录。
- **INV-33** authority journal 写入前必须有空间 reservation；`ENOSPC` 是带 receipt 的失败，不是 torn record，不得触发 repair 裁剪。
- **INV-34** `ArchivedNeedsReview` 释放 lease/reservation/write-scope/worktree/process，但保留全部 evidence；`LegacyUnpermitted` dispatch 使该 node assurance 封顶 `HarnessPolicyOnly`，且该计数单调递减。
- **INV-35** `Progress` 不是 claim：它可从义务状态推导，只作为投影/审计存在，永不进入 AcceptedSnapshot 或注入模型输入；`confidence` 不驱动任何决策分支，禁止新 consumer（既有字段仅作遗留元数据）。

## 完成判定

- **INV-36** "全维度 100%" / "Harness Kernel local-ready" 的机器可判定基准是 `CURRENT_STATE_LEDGER.md`（CI 自动生成）与 `artifacts/readiness/status.json`：`ready=true` 且 `state=READY`。发布、tag、CI 绿或离线 gate 全 PASS 都不能替代 readiness 聚合；`release_version_changed` 之类的自动 blocker 出现时，必须先重跑 `verify-readiness.sh` 再宣称完成，禁止以"已发布"绕过。
