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
