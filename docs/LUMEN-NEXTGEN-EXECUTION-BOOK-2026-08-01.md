# Lumen 2 — Governed Agent Runtime

## NextGen 最终可执行总纲

**日期：** 2026-08-03（北京时间；本次为增量修订，不使用旧 `FINAL-*` 规划）
**性质：** Lumen 后续实施的唯一排序、依赖、验收与交接总纲；不是功能完成、CI 通过、发布、安装或 live/provider 证明。
**范围：** Rust Lumen coding agent；macOS-first；不做 Windows 专项、未授权 provider/billable 调用、deploy 或 release。
**方法参考：** 同日 Lumen Science 执行书只提供阶段与证据结构；它不是 Lumen Core 的 API、代码或发布依赖。
**证据窗口：** 当前源码、当前 GitHub、当前工作树与 2026-08-01 起的 NextGen 决策。窗口外的旧 Lumen 规划、恢复资料和旧聊天不读取、不参与需求、优先级或完成判断；本轮用户提供的 Claude Advisor 截图仅作为问题清单，绝不作为其服务端 API 或 Lumen 源码事实。

本书先冻结事实，再规定每项的文件接缝、数据合同、迁移、反例、命令、退出门和回退。下文标为【拟建】的类型、crate、配置或命令，在真正提交前都不是现有 API。

### 计划新鲜度与证据优先级

1. **P0 当前事实：** 当前 worktree/HEAD、`git ls-remote` 的 GitHub branch head、原始 command exit、当前源码测试；它们可推翻本文任何旧数字或路径。
2. **P1 当前设计：** 2026-08-01 起由本书和已提交 NextGen contract 定义的产品目标；变更必须有 issue/slice、negative test、commit 和本节校准更新。
3. **P2 非权威材料：** 窗口外 Lumen 文档/聊天一律不读取；本轮素材仓中明确登记的当日外部材料只提供问题清单或模式灵感，不能证明当前 API、版本、CI、GitHub 状态或完成度。

任何实施者先读 P0，再读 P1；P2 与 P0/P1 冲突时直接弃用。不得为了“沿用计划”覆盖当前代码、把历史测试计数挪作新 HEAD 的绿，或从旧文件恢复已被否决的设计。

### 本轮实施校准（2026-08-03 的审计快照；R0 前必须重新实测）

本书不是以旧规划或 implementer 口述来“报绿”。下列值是本次**编辑前的审计锚点**，不是会随本书
提交自动更新的 source candidate：本地及 GitHub `sync/absorb-upstream-20260731` 均为
`8b74b3618fde1a235b37c2b5bb4a2472aac9b1e9`，`origin/main=2f47a9ad84e94b20291a1ad3d6b005ccbd3885f4`，
候选相对 main 为 ahead 194 / behind 0，PR #134 open、merge state `UNSTABLE`。本次 GitHub 读取中
`Expert v2 gate` 与 `Offline gates` 已成功，`Lumen crates (guard/discipline/verify tests + clippy)` 仍在运行。
这不是 exact-SHA 全绿，更不是 merge、release 或产品完成。工作树另有 5 个刻意隔离、未跟踪本候选的旧
`SBOM.spdx.json`/`artifacts/readiness/*.json` 修改；它们与当前 source tuple 不同源，禁止混入任何 NextGen
提交或作为 readiness 证据。

任何 phase 的输入事实只允许由下列命令重新产生，并写入该 phase receipt；本节的 SHA、PR 或计数若与
实测不同立即作废，不为维护文档而改写 runtime：

~~~zsh
cd /Users/lei/code/lumen
git rev-parse HEAD
git status --short
git rev-list --left-right --count origin/main...HEAD
git ls-remote --heads origin main sync/absorb-upstream-20260731
gh pr view 134 --repo exergyleizhou-ux/lumen --json headRefOid,state,statusCheckRollup,url
~~~

`637b5825` 及其前的 runtime checkpoint 仍是历史锚点；本书之后的当前 runtime baseline 是 `8b74b361`。
它已引入 ContextManifest、ClaimAuthority、governed assignment/operation、write scope、离线 golden
fixture、真实 nested lineage、spawn ingress 全局上限与独立 control ingress。下列历史增量仍有效，但不能再被
写成“当前唯一 runtime HEAD”：

1. 根 Session 也获得同一 task-tree ledger 的受控 review port；child 仍无 promotion
   authority（`85e1a4c8`）。
2. 只有 root 的显式 `/memory promote` 才能把已接受的 Fact/Decision 提升到 workspace
   长期记忆；提升带稳定 marker、保留 evidence、可重复执行不重复写入（`ab01c47d3`）。
3. 已接受但没有非空 `evidence_ref` 的手工/旧损坏 JSONL 会在 reload 时 fail-closed，不能
   注入 child prompt、不能 promotion、也不会触发 panic（`d844e7505`）。
4. 每个 child prompt 现在都有协调器生成的 task-tree contract：root/direct-parent/depth/path、
   有效能力上限、是否 leaf；它强调 Proposal/evidence/root acceptance，而执行权限仍由
   coordinator 和 tool filter 强制（`ecefdc7b1`）。
5. client disconnect 的 idle unload 改为 actor mailbox 内的 `UnloadIfIdle`，并由与 prompt intake
   相同的 dispatch lock 覆盖决策至 resident-map detach；不再是 `IsBusy` 后另发 `Shutdown` 的
   check-then-act race（`16ddc314f`）。这只关闭 foreground/queued-prompt/parked-plan-approval 的
   竞态。
6. actor 的 bounded activity snapshot 现在还会保留 owner-scoped background terminal/monitor、direct
   child、scheduler active-run lease 与 pending interaction；adapter probe timeout fail-closed，恢复清单
   也不会把 child terminal 误记到 root（`2a3a9913`）。它不等于跨 adapter transaction、event journal 或
   24h recovery。
7. root 用户可执行 `/memory repair-ledger`，但它只能在先 fsync 保存 exact raw tail backup 后裁剪最后
   一条 torn record；child、non-root 和 middle corruption 一律 fail-closed（`637b5825`）。这只是
   ledger file repair，不是完整 recovery journal、read-model rebuild 或 task recovery。

本轮 `8b74b361` 本地核心证据包括 memory/tools/shell 的 targeted check、task coordinator 197 passed、
governed-assignment shell 3 passed、memory 333 passed、rustfmt 与 `git diff --check`；它们都是局部 raw exit，
不等于完整 suite、GitHub exact-SHA CI、24h daemon、release 或 provider live proof。`SOURCE_LOCK` 仍锁
`0fae4c7b`、readiness 仍对应旧 source 且为 `BLOCKED`；它们与当前候选不同源，既不证明当前候选、也不允许
被“刷新文档”掩盖。每次源码改变后，R0 receipt 必须更新；本节快照不替代 receipt，旧 CI 不可挪用。

### 发布身份与版本边界

- **产品代际名：** `Lumen 2 — Governed Agent Runtime`；`Lumen NextGen` 是实施代号。
- **当前开发版本：** `2.0.0-alpha.1`。它清晰标识 Lumen 2 的进行中实现；它不是已验收、已合并或可发布的宣称。
- **首个候选标签：** `v2.0.0-rc.1`，只有 clean source candidate、exact-SHA CI、source lock、SBOM、readiness 和人工门全部闭合后才能创建。
- **上游身份：** `xai-grok-version` 是 Grok Build 协议/客户端身份，可独立为 `0.2.116`；它绝不能决定 Lumen 的产品 version、tag 或 release。

因此，在 R0 完成以前，`2.0.0-alpha.1` 只表示进行中的 Lumen 2 源码；`0.2.116` 仍只是上游组件身份。二者都不能提前伪装为 RC、正式发布或验收完成。

---

## 0. 最终目标：受治理的执行树

Lumen NextGen 不是“更多 Agent”，而是更多 Agent 后仍可控、可追责、可恢复。

~~~text
Human / TUI / ACP
        ↓
SessionActor — 唯一 execution / permission / evidence / completion authority
 ├─ TaskTree — logical parent、lineage、取消与汇总
 ├─ Capability Ceiling — 子代只能收缩的能力上限
 ├─ TreeBudget — 并行节点、工具、时间、token、成本的原子账本
 ├─ SharedWorkingLedger — 当前任务树的已验证事实
 ├─ LongTermMemory — 从 Accepted facts 受控提升的长期知识
 ├─ AgentSandbox — 每节点的上下文、记忆、工具、进程与资源隔离合同
 ├─ GovernedEvidenceLoop — 检查点、证据、停止与升级的受控闭环
 ├─ ExpertConsultation v1 — 已有独立第二意见
 ├─ ClientAdvisor — 本地虚拟工具、先影子，后受限咨询/分配建议
 └─ KairosSupervisor — lease/recovery 的长期任务治理
        ↓
受控 tool / process / provider / filesystem adapters
        ↓
artifact + evidence + provenance + replay receipt
~~~

标准三级 profile：

~~~text
Main Agent               depth 0，唯一 authority
└── Code Agent           depth 1，受限 workstream lead
    ├── Research Agent   depth 2，默认只读
    ├── Review Agent     depth 2，默认只读
    └── Test/Evidence    depth 2，受限 runner
        └── Evidence leaf depth 3，只读、不可 spawn
~~~

max_depth=3 的意思是 root 深度 0 后允许 1、2、3 三代子节点；深度 3 必须硬拒下一次 spawn。它不表示默认开启三层，也不表示 child 继承 root 权力。

### 0.0.1 本版需求可追溯矩阵（只采纳最近两天确认的结论）

| 已确认的方向或灵感 | 本书中的工程化落点 | 不允许的误读 | 可验收事实 |
|---|---|---|---|
| Main → Code → Research/Review/Test → Evidence leaf | NG-01 lineage、NG-02 ceiling、NG-03 reservation、NG-09A exact-binary path | 配置 `max_depth=3` 就等于产品完成 | 每层 parent/path、scope、budget、取消与 UI 投影一致；leaf 无法 spawn。 |
| 并行是嵌套 Agent 的执行底座 | 一个 root-owned process/lease/budget/evidence 合同覆盖 child、terminal、monitor、scheduler | 同一 worktree 自由并发写入；tmux/PID 充当恢复状态 | 并发 reservation、late event、orphan、cancel、lease takeover 都有反例。 |
| 子 Agent 会跑偏/幻觉 | NG-04 ledger + NG-04C ContextManifest + root acceptance；Advisor 只做第八道审阅 | 用 Advisor、长 prompt、summary 或 sibling 投票代替事实门 | child 只能 Proposed；只有 evidence + host/root review 才成为 Accepted。 |
| 双模记忆与 Agent 沙箱 | Session/Scratch 与 SharedWorkingLedger/LongTermMemory 分层；每节点只能读 Accepted snapshot、写自己 branch 的 Proposed | 让 child 共写 MEMORY.md、看 sibling 草稿，或把 chat summary 当权威 | foreign/stale/unproven fact 不进入 child；proposal 不能跨 branch；promotion 可追溯且幂等。 |
| Expert/Advisor 与用户模型池 | NG-05/06/07：pin、pool、priority、privacy、health、budget、independence、root approval | Advisor 直接改正在输出的模型、接受事实或宣布完成 | every selection/advice/applied assignment 有 receipt；已输出绝不重放。 |
| Claude 类 Advisor 产品体验 | NG-06A ClientAdvisor virtual tool：按检查点咨询独立模型、结构化 report、独立 usage receipt | 复制 server tool type、供应商私有 header/flag，或把 prompt 当安全控制 | 主模型可请求咨询；SessionActor 生成最小 capsule、执行本地 policy、审计/限额/可取消。 |
| DeepSeek Flash / Grok / DeepSeek Pro 的可选主力组合 | 用户 allowlist + 可改 priority；`auto` 仅在用户池内、仅对新任务推荐 | 根据“谁更聪明”绕过用户 pool、BYOK 或隐私边界 | pin、quota exhausted、pool exhausted、failure-domain 与 no-replay 负例。 |
| Kairos/daemon/24h 自动化 | NG-03C/NG-08：operation identity、lease、outbox、reconcile、freeze | 多开一个 daemon Agent、靠日志/PID 断言可恢复 | crash/takeover/duplicate/outbox/external-effect Frozen 演练。 |
| Claude 类 harness、CrewAI、公开文章/PDF 的经验 | 仅作为风险清单：authority、tool boundary、context rebuild、maker/checker、events | 导入专有实现/flag，或把外部描述当 API 与完成证据 | 每项只以 Lumen source、tests 与 receipts 定案。 |
| Lumen Science 当前执行书 | 借用 source gate 与 contract gate 分离、shadow-first proof 的结构 | 把 Science domain/API/发布范围抄入 Lumen Core | Lumen 自己的 Core gates、Rust source、exact binary 证明。 |

这张表是本版防遗漏清单。任何新增能力必须先补一行，再指定一个 phase、一个 authority owner、一个失败
模式和一个证据 gate；没有这些字段的想法只能停在 RFC，不能交给模型或进入 runtime。

### 0.0.2 素材仓盘点、采用边界与弃用规则

本表记录本轮实际讨论过的素材，目的是防遗漏，而不是赋予它们相同权威。只有第一行和第二行可
证明 Lumen 当前状态；其余都只能提供问题、模式或验收方法。任何从素材得到的结论，仍须在 Lumen
中落到 source anchor、负例测试和 evidence receipt。

| 素材仓 | 本轮可提取的内容 | 纳入的 NextGen 结论 | 明确不做 |
|---|---|---|---|
| 当前 Lumen `/Users/lei/code/lumen`、当前 GitHub `exergyleizhou-ux/lumen`、上游 `xai-org/grok-build` | 当前 API、提交、分支、测试、上游吸收边界 | P0 事实源；SessionActor/ACP/Grok Build 是唯一运行时底座 | 以 README、旧审计或别的分支冒充当前事实。 |
| Lumen Science 执行书 `/Users/lei/code/lumen-science/docs/science/5.0/LUMEN_SCIENCE_NEXTGEN_FINAL_EXECUTION_BOOK_2026-08-01.md` | source gate 与 contract gate 分离；shadow-first 的验收顺序 | R0 contract receipt、NG-09A 再 NG-07/NG-09B | 搬运 Science domain、connector、设备或发布合同。 |
| `Claude-Code-Source-Analysis-zh-v260411.pdf`、`Claude-Code-Complete-Guide-zh-v260411.pdf` | harness、tool/permission/context、独立验证和压缩风险清单 | 十二平面 Harness、context rebuild、maker/checker 分层 | 将二手描述、内部目录、flag 或专有行为当 Lumen API。 |
| `Loop-Engineering橙皮书-v260615.pdf` | 可观测 loop、检查点、反馈与停止条件 | typed lifecycle event、receipt、bounded retry、freeze | 用无限 loop、stdout 文本或自评替代状态机。 |
| `ultraworkers/claw-code` | 多 Agent 状态、事件/任务阶段、harness 的反例检查 | boot/ready/prompt-accepted/running/blocked/terminal 事件与 outbox/reconcile 要求 | 获取、导入、反编译或分发未授权 Claude Code 源码。 |
| `crewAIInc/crewAI` | crew/flow/role 协作的产品启发 | task contract、role narrowing、可观察协作；但 authority 保持 Rust SessionActor | 新增第二编排 runtime、让 role prompt 成为权限边界。 |
| 用户提供的两张任务树/权限截图 | 真实 UX 诉求：树展示、正在做什么、bypass 可见、分层 delegation | truthful task-tree projection、root-only bypass、depth=3 leaf hard deny | 将截图里的 bypass 下放、把 UI 文案当事实源。 |
| 窗口外的 Lumen 文档/聊天/恢复归档 | 不在本轮读取范围 | 无；仅在文件存在层面标为 out-of-scope | 恢复淘汰架构、旧版本号、旧测试数或旧完成判断。 |

每次新增外部材料或新灵感，都必须附：来源日期、可采用的一条工程结论、对应 phase、反例、以及
不采用的内容。没有这五项，不得改动 runtime。

---

## 0.1 Lumen Harness Kernel v1：不是功能清单，而是统一运行时

**核心判断：** 模型决定“能提出什么”，Harness 决定“提出的东西能否安全、持续、可验证地变成真实结果”。
因此 Lumen 2 不以“又接入一个模型、又多一个 Agent、又多一个命令”为架构单位；它以每一次受治理运行
(`GovernedRun`) 为单位。任何新功能若无法进入这个合同，只能是实验 adapter，不能成为产品 authority。

这比历史材料中描述的 Claude Code harness 更进一步的地方，不是工具数量，而是把多模型、三层任务树、
共享事实、长期任务和证据链放进一个 Rust authority boundary。它保留 Claude 类 harness 的正确经验：
专用工具优先、权限是硬门、压缩不能替代状态、记忆不是当前事实、验证者独立；但不复制其专有实现、
内部 flag 或产品专属协议。

### 0.1.1 十二个平面必须共同闭环

| 平面 | 回答的问题 | Lumen authority / 当前资产 | 尚未完成的硬门 |
|---|---|---|---|
| 1. Identity & authority | 这次运行归谁、谁有最终权力？ | SessionActor、root/session/node identity、TaskTree lineage | 所有 background/workflow 同样进入统一 owner/operation identity。 |
| 2. Capability & policy | 允许什么、资源范围多大、何时撤销？ | permission、Capability Ceiling、restricted child、unknown MCP deny | RootBypass TTL/revoke/audit 与全 adapter 的同一 policy revision。 |
| 3. Execution & tools | 模型如何作用于文件、shell、MCP、process？ | typed tools、tool dispatch、process scope、ACP | 每一个副作用都有 operation receipt 与 idempotency class。 |
| 4. Context | 此刻模型能看见什么，压缩后如何不跑偏？ | system prompt、summary/JSONL、task contract、tool result shaping | ContextManifest、immutable user objective 引用、compact/recovery rebuild proof。 |
| 5. Memory | 什么可跨节点/跨会话记住，什么只能暂存？ | task-tree Proposed/Accepted ledger、root-only promotion、workspace memory | versioned read model、cross-worktree recovery/CAS/conflict proof。 |
| 6. Verification & evidence | 谁能说“完成”，凭什么？ | VerifyAfterEditOutcome、HostVerification、artifacts/provenance | 所有 terminal success 都引用 evidence/verify receipt；offline golden path。 |
| 7. Parallel & resource | 哪些能并行、如何隔离、花多少、谁取消？ | lineage、depth=3、tree token/tool/time limit、worktree capability | atomic budget reservation/release、daily cost/artifact cap、conflict/merge contract。 |
| 8. Model & provider | 用谁、何时可换、没额度怎么办？ | user pool/priority、role pin、Expert shadow、no-replay rules | normal turn/background `ProviderAttemptReceipt` 与 root-approved assignment。 |
| 9. Lifecycle & Kairos | 长任务如何 start/stop/retry/recover，进程何时可回收？ | scheduler lease/heartbeat/backoff/dead-letter、workflow/leader、atomic unload seam | generic operation lease、event journal/outbox/reconcile、24h no-side-effect proof。 |
| 10. Operator UX & audit | 人如何看到真相、冻结、接管、复盘？ | ACP/TUI/Pager tree、logs、status、artifact paths | truth-preserving status projection、freeze/approve/replay surface、exact-source evidence packet。 |
| 11. Data, secret & retention | 哪些数据可见、可持久化、多久可保留、何时可删？ | credential provider、terminal env exclusions、Expert secret-free/redacted evidence | common SecretRef/redaction policy、artifact retention/GC receipt、manifest/ledger/log 全链路 leak tests。 |
| 12. Flow control & liveness | 高并发、慢 consumer、队列满、事件丢失时如何不挂死或假成功？ | actor mailbox、sampler event stream、activity snapshot 的 fail-closed 思路 | bounded queues/backpressure policy、delivery-loss receipt、watchdog/heartbeat/queue-pressure projection。 |

**禁止替代：** prompt 不能替代 policy；session summary 不能替代 task ledger；Advisor 不能替代
acceptance；terminal stdout 不能替代 evidence；tmux/PID 不能替代 lease；“模型说完成了”不能替代
terminal receipt。任何功能少一个平面都只能标 `Draft/Experimental`，不能标自治完成。

### 0.1.2 唯一受治理运行信封【拟建】

后续 NG-01 至 NG-09B 的 DTO 不能各自携带半套身份。它们都投影自下列版本化合同；字段可拆分为
crate-local 类型，但语义不得分叉：

~~~rust
pub struct GovernedRunEnvelopeV1 {
    pub run_id: RunId,
    pub task_tree_id: TaskTreeId,
    pub node_id: TaskNodeId,
    pub root_session_id: SessionId,
    pub immediate_parent_id: Option<TaskNodeId>,
    pub lineage_path: Vec<TaskNodeId>,
    pub immutable_assignment_hash: Sha256,
    pub accepted_snapshot_hash: Sha256,
    pub context_manifest_hash: Sha256,
    pub capability_grant_id: GrantId,
    pub policy_revision: PolicyRevision,
    pub budget_reservation_id: ReservationId,
    pub model_selection_receipt: ModelSelectionReceipt,
    pub lease_id: Option<LeaseId>,
    pub operation_class: ReadOnly | ReversibleWrite | ExternalEffect,
    pub idempotency_key: Option<IdempotencyKey>,
    pub evidence_sink: ArtifactSink,
    pub created_at: Timestamp,
    pub deadline: Timestamp,
}
~~~

**硬语义：**

1. `PromptAccepted` 之前必须存在 immutable assignment、grant、policy、budget 和 context snapshot；
   任何缺项是 `Blocked`，不以 best effort 继续。
2. child 可读取的是 `AcceptedSnapshot`，可写的是 bounded `Proposed`；不能把 sibling scratch、原始 root chat、
   secret 或自由文本控制指令带入信封。
3. `ExternalEffect` 没有 idempotency receipt 时，崩溃后只能 `Frozen`，不可自动 replay；首个模型文字块、
   tool call 或未知观察也禁止 provider 重放。
4. terminal success 只在 evidence hash、verify receipt、scope/provenance、root acceptance 都可重算时成立。
   `Some(Pass)` 证明 edit delivery，不单独证明整个 task success。
5. TUI、CLI、Pager、daemon log 都只读这个合同和事件 journal；它们不能创造 `Running`、`Succeeded` 或
   `Accepted` 状态。

### 0.1.3 Lumen、PDF 中的 Claude harness 与早期 Lumen 2 计划的差异

| 维度 | PDF 描述的历史 Claude Code | Lumen 原有基础 | Lumen Harness Kernel v1 的加强 |
|---|---|---|---|
| 中心 | 单一产品 harness：prompt/tool/permission/context/memory | Rust SessionActor + ACP + Grok Build tool runtime | 把 SessionActor 固化为多模型、多节点、证据/完成的唯一 authority。 |
| 多 Agent | team/leader/worktree/permission bubbling 的产品形态 | child/session、lineage、depth、取消与 pager tree | parent identity、capability monotonicity、tree budget、ledger snapshot、leaf hard deny 一起成立。 |
| 记忆 | 偏好/引用为主，提醒旧事实需重验 | JSONL/summary/workspace/global memory | 将当前任务事实升级为 append-only ledger；长记忆只能 root evidence promotion。 |
| 验证 | 提醒“实际验证”、独立 checker 的设计经验 | typed `VerifyAfterEditOutcome`、Expert/HostVerification | 将自评、checker 文本、host evidence、root acceptance 分成不可混淆的四层。 |
| 自动化 | hooks、flags、background/Kairos 方向 | scheduler lease/retry、workflow、leader | 事件 journal + owner/lease/outbox/reconcile + freeze；不新增第二执行 runtime。 |
| 模型 | 产品绑定的模型/权限/上下文策略 | BYOK、catalog、Expert pool/priority | 用户池和 pin 优先；Advisor 仅推荐；no-output/no-effect 才可切换。 |
| 完成定义 | 依赖具体产品行为与当时实现 | 多个成熟部件，但跨域验收不完整 | exact-source binary + offline golden path + CI/release 分门；本地 check 绝不等于产品完成。 |

**因此这是加强且换代，不是推倒重写：** 保留 Lumen 已经成熟的 Rust tool/runtime、SessionActor、
Grok Build、Expert、Goal、ACP、scheduler、typed verification；补上它们之间原来缺少的共同身份、
状态、证据、资源和恢复合同。模型越强，这一层越重要，因为并行数、工具权限、成本和副作用都会同步放大。

### 0.1.4 终局验收：Harness 不是“看起来齐”，而是一次完整受控运行

只有下列零 provider、零外部副作用的 exact-binary 场景全过，才可称 Harness Kernel v1 已就绪：

~~~text
root creates envelope + immutable objective + accepted snapshot
  → depth-1 code node receives a strictly smaller grant and budget slice
  → depth-2 research/review/test nodes run in isolated scopes
  → depth-3 evidence leaf cannot spawn, write, network, or bypass
  → children append proposed facts with fixture artifact hashes
  → root rejects one stale/conflicting/unproven claim and accepts one verified claim
  → verifier emits typed PASS/FAILED/SKIPPED/ERROR without false delivery
  → one branch is cancelled; late terminal event is reconciled, not revived
  → crash/restart replays journal/read model; external-effect node stays Frozen
  → status UI reports real owner/phase/budget/evidence; rebuilt binary has no provider call
~~~

这个场景通过后只是 **Harness Kernel local-ready**；仍不等于 GitHub exact-SHA CI、Windows、release、
真实 provider、24 小时 soak 或无人值守自动 commit 已完成。

### 0.1.5 开发态开关与短任务快路径：减少开销，不减少治理

治理层已经很厚；若每一句短问答都启动 TaskTree、持久 ledger、daemon、树面板和多份 artifact，产品会
变慢且把研发态复杂性转嫁给用户。解决办法不是提供绕过开关，而是在同一 SessionActor authority 下提供
**按能力升级、一次性不可降级的运行 profile**。以下名称、配置和 DTO 都是【拟建】，在实施前不是现有
API：

| profile | 默认适用 | 允许的运行资源 | 何时升级 | 绝不放宽 |
|---|---|---|---|---|
| `interactive_single_turn`（默认） | 短根任务、一个模型、无 child、无后台/恢复诉求 | 单一 actor turn；可在内存中构造同一 schema 的 ephemeral admission snapshot；不创建 tree/lease/ledger UI | child spawn、compact/resume、计划任务、显式 `/tree`、需要 durable evidence/approval 任一发生时 | SessionActor、现有 tool permission、user pin、no-replay、typed verification、未授权 external effect。 |
| `governed_tree_development` | 开发/离线 fixture、三层能力验收 | TaskTree、ceiling、budget、ledger、ContextManifest、fault injection 与树 UX | 只能由 root 用户显式开启；首次 spawn 前必须 durable admit | bypass、child write scope、unknown MCP、预算、model assignment、provider replay。 |
| `kairos_local` | 已通过前置 gates 的无副作用本地恢复演练 | operation lease、journal/outbox、fake clock、operator freeze | 只在 NG-08 gate 下 | live provider、外部副作用自动 replay、24h autonomy 宣称。 |

**语义同一性：** fast path 不是第二个 execution runtime。它使用与 `ContextManifestV1` 同字段语义的
ephemeral admission snapshot；一旦升级，actor 在 `PromptAccepted` 前把 immutable assignment、当前
AcceptedSnapshot、policy/grant 与 budget/lease（如适用）原子持久化为 manifest。升级失败则当前 turn
`Blocked(AdmissionUpgradeFailed)`，不能“先 spawn 再补账”。已进入 governed profile 的 run 不因 UI
隐藏或性能原因退回无 manifest 状态。

**开发开关的边界：** 开关只允许启用额外的 observability、schema validation、fixture/fault injection、
tree projection 和实验性 consumer；不能关闭 permission checks、evidence requirement、no-replay、budget
reservation、root-only bypass 或 hash verification。`warn` 模式只记录“本可升级而未升级”的诊断，不能让
本应 fail-closed 的状态继续执行；`enforce` 模式仅在可证明兼容后成为默认。每个开关都要有 owner、默认
值、expiry、telemetry/receipt、删除条件和 negative test，避免 development flag 永久成为隐蔽后门。

**用户体验验收：** 默认短任务不显示空任务树、不等待 daemon、不写入长期记忆，也不要求用户理解
claim；用户一旦请求并行/子 Agent/恢复/定时任务，TUI/ACP 必须先显示“将升级为受治理任务树”的 scope、
模型 pin/池、权限、预算与可取消性。UI 状态始终来源于 actor/journal，不能用 profile 文案伪造完成。

### 0.1.6 红队补洞：并行写入、秘密、流控、人工接管与评估必须显式闭环

本书原有 Tree/Grant/Ledger/Manifest/Lease 主线是正确的，但若省略下面五项，系统仍会在真实长任务中
“看起来受治理、实际在旁路失控”。它们不是新产品愿望，而是把已经存在的 worktree、secret redaction、
unbounded event channel、scheduler lease 和离线 golden path 接成闭环：

| 漏洞面 | 必须新增/收紧的合同 | 首次落点 | 完成定义 |
|---|---|---|---|
| 并行写冲突 | `WriteScopeLeaseV1`：base commit、worktree id、allowed path globs、writer node、expiry、apply/merge receipt | NG-03D | 两个 child 不能写同一 scope；stale base/overlap/dirty target 不自动 merge。 |
| secret 与 artifact 泄漏 | `SecretRef`/redaction class/retention policy：manifest、ledger、status、preview 只放 safe reference/hash | NG-02A + NG-04C | credential/raw secret 永不进 journal/UI；redaction miss 或 retention owner 不明 fail-closed。 |
| 队列满和事件丢失 | `DeliveryObservationV1`：bounded queue policy、drop/closed marker、attempt/operation 不确定性 | P4b/NG-03E | slow consumer/closed channel 不能假装已投递、也不能自动 replay；queue pressure 可见。 |
| 人工接管 | actor-owned `Inspect/Freeze/Cancel/Approve/Resume/TakeOver` commands，全部写 operation journal | NG-03C/NG-08 | UI 是 command client；无 root/operator approval 的 resume、takeover、external effect 一律 Frozen。 |
| 质量回归/验证债 | versioned offline scenario corpus，语义 assertions 而非快照文本；每一次 policy/schema 改动可 diff | NG-09A/09B | safety/authority invariant coverage、mutation/fault cases、性能预算与 exact-binary product trace 同时可复现。 |

这些合同各自只增加一个 authority owner：write scope、operation、artifact、queue 和 approval 都由
SessionActor/其 durable store 决定；它们不得由 model、Advisor、TUI、PID 或 worktree 文件自行宣布。这样
增强的是当前 Lumen 的 Rust harness，而不是引入一个难以维护的“多 Agent 平行控制系统”。

### 0.1.7 跨版本、迁移与删除：宁可冻结，不可猜测兼容

TaskTree、grant、claim、manifest、operation、receipt、write lease 与 delivery observation 都会跨 crash、
resume、升级和 rollback 存活。因此每一个持久合同必须在首次 PR 中同时写清 **read-new / write-new /
read-old / rollback** 四件事；不能等产品数据出现后再猜迁移语义。

| 情况 | 唯一安全行为 | 禁止行为 |
|---|---|---|
| 旧版本记录、可完全映射 | 只读兼容投影；首次 root-approved rewrite 生成新 record 并保留旧 hash/ref | 静默就地改 JSON、复用旧 hash 或把 legacy 当 V1 已验证。 |
| 未知未来 schema / 缺 migration | `Frozen(UnknownSchema)`，只允许 inspect/export/人工升级 | best-effort 默认值、丢字段后继续 dispatch 或自动 retry。 |
| migration 中 crash | journal 记录 prepare/apply/verify；read model 从最后已验证 sequence 重建 | 只写一半索引、删除旧 journal、让 child 读取半迁移数据。 |
| rollback 到旧 binary | 新 contract consumer 关闭；旧 binary 只能读明确兼容记录，其他保持 Frozen | 把较新 grant/receipt/manifest 降级成自由文本或静默执行。 |
| retention/delete/GC | 由 root/operator policy 以 tombstone + deletion receipt 执行，保留最小审计 hash | GC 直接删除唯一 evidence、worktree 或 journal。 |

**共同 gate：** 每个 `<CONTRACT>_GATE` 都要加 schema matrix（旧→新、新→旧、unknown、partial migration、
corrupt/torn、rollback）和 fixture hash。任何 contract 缺这张矩阵只能标 `Experimental`，不得作为 Kairos、
auto assignment 或 24h 的恢复依据。

---

## 1. 当前真相冻结

### 1.1 精确基线

| 项目 | 当前实测事实 | 本书处理 |
|---|---|---|
| 本地工作树 | 本次编辑前直接复核：`/Users/lei/code/lumen`；分支 `sync/absorb-upstream-20260731`；HEAD=`8b74b3618fde1a235b37c2b5bb4a2472aac9b1e9`；仅有本执行书与 5 个旧 SBOM/readiness 证据文件未提交。后者不属于 runtime candidate。 | 每个 phase 开始都必须重读 HEAD/status；HEAD 才是待审完整 source candidate，`SOURCE_LOCK` 只是 R0-04 以后生成的其一证据。 |
| GitHub main | origin/main=2f47a9ad84e94b20291a1ad3d6b005ccbd3885f4 | 是本地候选祖先；禁止直接把本地分支叫作已合并 main。 |
| 分叉量 | 本次 `origin/main...HEAD = ahead 194 / behind 0`；PR #134 head 是 `8b74b361`；Expert v2 与 Offline gates 成功，Lumen crates 仍 in progress，merge=`UNSTABLE` | 提交数、部分成功或 in-progress CI 不是验收证据；待 exact SHA 全部完成后再做 R0 分组审查与人工 merge。 |
| 工作树 | 每次 R0 source candidate 前必须重新实测 clean/dirty；任何未分类路径都不进入候选 | R0 manifest 必须逐路径归属，不能沿用旧计数或旧 evidence。 |
| 上游吸收 | f9cf565d → 818d6488 → a556d74b → b09b929f → e7afd15b；上游 pin dd04f397 | 已在本机，尚未进入 GitHub main。 |
| 版本 | 当前开发 VERSION 为 2.0.0-alpha.1；Lumen 2 首候选目标为 2.0.0-rc.1 | alpha、RC、tag、release 与同步分门；未过 R0 不得创建 RC/tag。 |
| 当前证据错位 | current runtime=8b74b361；历史 checkpoint=637b5825；SOURCE_LOCK source=0fae4c7b；readiness head=9e719020 且 state=BLOCKED | 四者不是同一 candidate。旧 lock/SBOM/readiness、旧 binary 或旧 CI 全部不得证明当前 HEAD。 |
| GitHub CI | 只承认 PR 上与 source candidate 对应的 exact-SHA run | 未完成、失败或其他 SHA 的 run 都不能被说成当前全绿。 |
| readiness | 只承认与 source candidate 同源的 lock、SBOM、binary 与 readiness | 旧 evidence 不证明后续源码。 |
| 发布门 | L5 soak、binary tuple post、M5、M6、eval_live、reconcile 未闭合或失败 | R0 不解除这些门。 |

### 1.2 来源锁的真实含义

SOURCE_LOCK 的 source SHA 与关键文件 hash 必须每次从当前候选实测读取；历史 SHA 不证明后续工作树。R0 的强制顺序：

1. 所有源码和文档合同先形成 clean source candidate commit；
2. 从该 commit 构建并记录 binary hash；
3. 再生成 source lock、SBOM、readiness；
4. 之后仅允许 lock/SBOM/readiness/evidence 组成 evidence-only suffix；
5. 任意源码变化都回到第 1 步。

### 1.3 当前资产与缺口

| 域 | 已有资产 | 不能误报为完成 |
|---|---|---|
| 子 Agent | 真实 lineage、三级硬拒、根取消、Pager 递归树、树级 token/tool/time 限额 | exact CI、跨进程恢复和完整产品 golden path。 |
| tools/context | `ToolKind` taxonomy、canonical metadata、MCP descriptors、tool definition snapshot、现有 result truncation/recap | every-call ToolContract、unknown-MCP child deny、artifact/redaction/result budget 与 manifest catalog binding。 |
| Expert | Fast/Vision/Deep/Dual、双 proposal、单 writer、HostVerification、shadow advice、用户 pool/priority；ordinary turn reroute 仅为未验证 candidate；全新 root scheduler iteration 的请求前 pool 选择 | root-approved assignment、sealed no-replay receipt、一般后台 workflow/subagent 的完整 routing 与 provider 额度证据。 |
| memory | global/workspace、SQLite/FTS/vector、JSONL/summary、task-tree Proposed/Accepted ledger、root-only `/memory promote` | 跨 worktree/recovery 的完整产品证明、claim 状态机/read-model gate。 |
| 进程 | scheduler 已有 task-scoped durable run lease/takeover、workflow、leader、background terminal、子任务 heartbeat/孤儿收口、workflow budget/recovery | workflow/general process 的跨进程 operation lease/takeover、统一 activity 原子聚合、24h daemon golden path。 |
| 验证 | VerifyAfterEditOutcome；Some(Pass) 才算 edit delivery | 全任务或 release 成功。 |
| provider | catalog、BYOK、role pin、Expert pool health skip、ordinary reroute candidate、全新 root scheduler preflight routing evidence | sealed ProviderAttemptReceipt/no-replay fault matrix、可复核 failover receipt、真实额度证明。 |

### 1.4 状态词典

Not started：无实现。
Draft：RFC/contract 已写。
Implementing：工作树修改中。
Verified locally：exact source 定向测试通过。
CI pending：等待 GitHub exact SHA。
Accepted：Exit Gate 和证据包完整。
BLOCKED：外部或前置门未满足。
NOT RUN：没有执行，不能拿旧证据替代。

---

## 2. 不可谈判的 authority 边界

| 组件 | 可以做 | 不可以做 |
|---|---|---|
| SessionActor | grant/deny、tree acceptance、预算、artifact/evidence/provenance、cancel/recovery、terminal completion | 让 child、daemon、Advisor 自证成功。 |
| TaskTree | logical parent、lineage、depth、root process scope、树状态 | 相信 caller 给的 depth/parent，或以 process scope 替代父节点。 |
| Capability Ceiling | root policy ∩ parent ceiling ∩ role ∩ operation approval | 传递 yolo、bypass、raw PermissionHandle 或 unknown MCP。 |
| TreeBudget | reserve/release 节点、工具、时间、token、成本、artifact 配额 | 让 child 各自超额或伪造 usage。 |
| SharedWorkingLedger | facts/evidence/assumption/conflict/root decision | child 直接 Accepted，或自由文本变成控制命令。 |
| LongTermMemory | 提升已验收稳定知识 | 取代当前任务事实。 |
| Expert / Advisor | 独立意见、模型建议、风险和拒绝码 | 写文件、批准权限、接受 claim、完成任务。 |
| Kairos | lease、heartbeat、reconcile、retry eligibility | 第二执行 actor 或盲目复放副作用。 |

### 2.1 Root-only bypass

【拟建】RootBypassPermission 必含：

~~~text
issuer = root SessionActor
scope = exact action + resource scope
reason = human-visible
expires_at = mandatory
nonce + audit_id = mandatory
revocable = yes
child inheritance = forbidden
~~~

它不等于现有 always-approve、PermissionHandle、yolo 或环境变量。它不能映射到 child、Advisor、Kairos 或 MCP tool。

---

## 3. 子 Agent 幻觉：系统硬控制，不是 Advisor 职责

Advisor 只能提出反证和风险。它不能将 child 的文本升级为事实，也不能防止 sibling 相互附和。主控制面必须强制：

1. child 输入仅含 immutable TaskContract、CapabilityCeiling、AcceptedLedgerSnapshot、assignment、BudgetSlice、artifact reference 和 schema；
2. child 不继承整个 root chat、不读取 sibling 未接受草稿、不接收 secret、裸路径或控制指令；
3. child 输出只能是 size-bounded Proposal 或 evidence artifact；“完成/测试通过/文件存在”一律不可信；
4. 缺 artifact hash、tool receipt、可重算 derivation 或 host verification 的结论只能是 Proposed、Hypothesis、Inconclusive；
5. sibling 只读 versioned Accepted snapshot，不能写对方 branch 或自动合并摘要；
6. root 校验 scope、hash、provenance、tests、verify outcome、冲突后才可 Accepted；
7. Advisor 仅是可选第八道审阅，没有 acceptance 权。

### 3.1 【拟建】claim 状态机：事实门，而非另一份聊天摘要

`SharedWorkingLedger` 的每条 claim 必须是不可变记录，不是可编辑 Markdown，也不是模型之间的
共享指令通道。它解决的是“分支 Agent 后来不知道什么已被证明、什么只是猜测”，而不是保存全部思维。
这也是 ContextManifest 可以安全引用的唯一任务事实来源。

~~~text
Draft (仅 root 本地构造，不持久化)
  → Proposed (author node append-only)
  → EvidenceAttached (artifact/receipt 已绑定)
  → HostVerified (独立 host 仅验证证据可重算)
  → Accepted | Rejected | Inconclusive | Conflicted

Accepted → Superseded | Revoked
Conflicted → Accepted | Rejected | Inconclusive     (须由新的 resolution record 指向)
任何状态 → Frozen                                  (journal/hash/authority 不确定)
~~~

**状态不是权力。** `HostVerified` 只证明 evidence artifact、命令、scope 与 hash 能够重算；它不自动
证明结论为真。只有 root SessionActor 在 scope、grant、policy、evidence、conflict、预算与必要的
`VerifyAfterEditOutcome` 都通过后，才可追加 `Accepted` resolution。Advisor report、搜索摘要、child
self-report、网页片段和 sibling 投票默认最多 `Proposed`；`Accepted` 也不等于 terminal success。

#### 3.1.1 唯一允许的转移与写者

| 转移 | 唯一写者 | 先决条件 | 禁止行为 |
|---|---|---|---|
| `Draft → Proposed` | root 或该 node 的 child actor | manifest、branch scope、size limit、author identity 有效 | child 为其他 branch/root 写入；携带自由执行指令。 |
| `Proposed → EvidenceAttached` | 原 author node 或 root | artifact/command/receipt hashes 已写入；证据在该 grant scope 内 | 用模型文字、“看起来通过”、未落盘 URL 代替 artifact。 |
| `EvidenceAttached → HostVerified` | host verifier / deterministic checker 产出 receipt；状态附加仍由 root actor journal | fixture/command/raw exit/provenance 可重算 | verifier 直接 `Accepted`，或把 check warning 写成 PASS。 |
| `HostVerified → Accepted` | root SessionActor | grant 未撤销、snapshot 未 stale、无未解决 conflict、evidence 非空 | child、Advisor、daemon、UI 或命令输出接受事实。 |
| `* → Rejected/Inconclusive/Conflicted` | root SessionActor | reason code 与 competing claim/evidence 有引用 | 删除原 claim、静默覆盖、sibling 自动合并。 |
| `Accepted → Superseded/Revoked` | root SessionActor | 新 claim/resolution 或 evidence invalidation；保留引用链 | 就地改 content、复用旧 hash、把 revoked claim 继续注入。 |
| `* → Frozen` | recovery/actor | sequence gap、foreign tree、hash 不匹配、owner 不明 | best-effort 恢复、自动 promotion 或自动重试。 |

#### 3.1.2 claim 的最小身份与拒绝规则

每个持久化 claim 至少绑定 `task_tree_id`、`claim_id`、`author_node_id`、`branch_id`、单调
`sequence`、`revision`、`kind`、canonical `content_hash`、`evidence_refs`、`provenance_refs`、
`policy_revision`、`manifest_hash`、创建时间和可选 review deadline。修订永远是新记录，并经
`supersedes` 指向旧 claim；read model 只能由 hash-linked journal 重建。

根/child 在任何时刻只能向模型注入同一 `AcceptedSnapshot(tree_id, end_sequence, end_hash,
accepted_claim_set_hash)`。`Proposed`、`Conflicted`、`Inconclusive`、`Revoked`、foreign tree、scope
外 artifact、过期 grant 或 manifest 不匹配 claim 都不得进入 child 输入。这样即使多个 child 同时
工作，它们也共享的是已确认事实，不是互相放大的猜测。

### 3.2 child heartbeat

~~~text
task_tree_id / node_id / parent_id / state revision
current objective / last evidence ref / next bounded step
remaining budget / grant expiry / blocker or uncertainty
~~~

输入缺失、与 Accepted facts 冲突、下一步扩大 scope、重复无新证据、tool result 无法支持结论时，child 必须 Blocked 或 NeedParentDecision。

### 3.3 外部材料的批判性校准（2026-08-02）

本节只把三份用户提供的中文二手解读和公开 `claw-code` 仓库当作**问题清单**，不把它们描述的
Claude Code 内部目录、功能旗标、模型行为、数量或“泄露源码”当成 Lumen 的事实、API 或可复制实现。
不得获取、导入、反编译或分发未获授权的专有源码。每一条可采用结论都必须由 Lumen 自己的测试和证据包
证明。

| 外部观察 | Lumen 的可迁移结论 | 明确不采用 |
|---|---|---|
| 记忆提取与主工作分离，且持久记忆可能被注入污染 | `SharedWorkingLedger` 是当前树事实的唯一来源；child 只写 Proposed，root 以 evidence 接受；长期记忆只能显式提升 | 将 child 对话摘要、网页摘要或模型自评直接写入 `MEMORY.md`；把历史代码位置当作长期记忆 |
| maker/checker 应独立，完成不是作者自己宣布 | `VerifyAfterEditOutcome=Some(Pass)`、host receipt 和 root acceptance 分别处理交付、证据和任务完成 | 用另一个模型的“看起来没问题”替代可复跑验证；让 Advisor 接受 claim 或宣布完成 |
| 提示词/项目说明会在压缩中丢失，机器不变量需在生命周期边界执行 | 将不可绕过的约束实现为 actor/tool policy/typed state；在 compact/recovery 后从 immutable contract 与 Accepted snapshot 重建输入 | 用更长 system prompt、child reminder 或 session summary 承担权限、预算、完成门 |
| 并行需要 worktree/隔离，调度需要状态和下一轮入口 | 任务节点必须有 lineage、owner、scope、lease、budget reservation、artifact/evidence 和 terminal receipt；仅独立写任务才分 worktree | 同一工作树多 child 自由写入、日志抓取作为唯一状态、以 tmux PID 代替 durable owner |
| 公共 agent harness 的 roadmap 强调 boot/ready/prompt-accepted/running/blocked/terminal 事件与 outbox | Kairos 采用事件优先的 typed lifecycle 与 outbox/reconciliation；TUI/CLI 是 projection，不是事实源 | 把“agent 已启动”、终端输出、或内存 registry 当作任务已接受/已完成；把本仓库自称的 parity 表当作质量证明 |

**Lumen 专属的硬结论：** child 幻觉不能靠 Advisor 解决；Advisor 只能提高发现问题的概率。
真正的反跑偏设施是不可变 assignment、versioned Accepted snapshot、窄输出 schema、evidence hash、
root acceptance、process/budget ownership，以及在每一层失败时 fail-closed。以上结论与 NG-01 至
NG-04 的 authority 设计一致，不创建第二个控制面。

#### 3.3.1 本轮 PDF/公开材料的最终采纳与反驳

三份用户给出的 PDF 以及公开 agent 框架材料的价值在于把问题分层，不在于提供可复制实现。经过本轮
文本与版式复核后，Lumen 只采纳下列可落地结论：

| 外部观察 | Lumen 的具体采纳 | 为什么不能照搬 |
|---|---|---|
| harness 管一次运行；loop 管发现、交付、验证、持久化、调度的多轮闭环 | `GovernedRunEnvelopeV1`/ToolContract/Manifest 是 harness；Kairos 只能在 NG-03C durable operation API 之上做 loop | Lumen 不新增第二个 agent runtime；所有实际 tool/model/terminal 仍回到 SessionActor。 |
| 子 Agent 有独立上下文，worktree 让并行写入隔离 | `TaskContract + AcceptedSnapshot` 给独立上下文；只有 declared write scope 才可申请 isolated worktree/operation | “独立上下文”不授权访问 root chat、sibling 草稿或共享工作树任写。 |
| maker/checker 分离能降低自评偏差 | HostVerification/typed test receipt 可构成独立 evidence；Advisor 可作为额外审阅 | reviewer 不拥有 claim acceptance、permission、completion 或 model switch 权。 |
| Hooks/提示可在压缩后提醒规则 | compact/resume 重新 render manifest，关键不变量在 actor/tool policy 中强制 | 不把 prompt/hook 当 security boundary；压缩后注入的文字不能改变 grant/budget/claim。 |
| 工具描述和大输出会吞噬 context，专用工具比万能 shell 易控制 | NG-02A 的 ToolContract、bounded redacted preview、artifact ref、deferred visibility | 不添加未经分类的工具，也不以截断文本替代完整 artifact/evidence。 |
| loop 需要状态、验证与停止；否则只是在更快地空转 | Kairos 有 lease/outbox/reconcile/freeze、dead-letter 与 operator handoff；NG-09 先做零副作用 golden path | 不以 cron、tmux、PID、stdout 或“模型说 done”宣称 24h 自动化。 |

故本书的判断不是“做成 Claude 的功能集合”，而是：以 Rust Lumen 当前 authority 为中心，把这些经验
逐项翻译成 schema、owner、negative test、receipt 和 stop condition。任何外部材料的版本号、内部路径、
产品 flag 或数字都不进入 Lumen source truth。

#### 3.3.2 【拟建】统一生命周期事件合同

NG-03/NG-08 不再允许多个模块以自由文本或日志推断进程状态。任何可托管的 child、terminal、monitor、
scheduler fire 或 workflow 都必须由其 owner 发出下列最小事件；事件 journal 是事实，UI/日志是投影。

~~~rust
pub struct GovernedLifecycleEventV1 {
    pub event_id: EventId,
    pub task_tree_id: TaskTreeId,
    pub node_id: TaskNodeId,
    pub owner_session_id: SessionId,
    pub sequence: u64,
    pub causal_parent: Option<EventId>,
    pub kind: Booting | Ready | PromptAccepted | Running | Blocked
            | Checkpointed | TerminalSucceeded | TerminalFailed
            | Cancelled | Reconciled | Frozen,
    pub source: Actor | Scheduler | TerminalAdapter | WorkflowAdapter,
    pub lease_id: Option<LeaseId>,
    pub contract_hash: Sha256,
    pub policy_revision: PolicyRevision,
    pub evidence_refs: Vec<ArtifactRef>,
    pub occurred_at: Timestamp,
}
~~~

规则：`PromptAccepted` 只能由 SessionActor 在 immutable contract、ceiling、budget reservation 和
lease（如适用）都存在后发出；`TerminalSucceeded` 必须携带 verify/evidence receipt；任何 sequence
gap、owner/lease 不匹配、未知 terminal reason 都归入 `RecoveryRequired` 或 `Frozen`，不能合成成功。
`Blocked` 是一等终态候选，必须带 reason code（budget、grant、evidence、provider、operator、recovery）。

**首个实现切片（NG-03A-1）：** 不新增平行 daemon。先在现有 shell actor 中建立
`SessionCommand::UnloadIfIdle` 的原子 check-and-act seam，并使 activity 的 read model 覆盖 foreground、
background terminal、monitor、scheduler fire、background subagent、active lease、pending approval。只有
这个 seam 的 race/late-event 反例通过，才将 lifecycle event journal 接入 Kairos。

**NG-03A-1 必测反例：**

1. actor 收到 `UnloadIfIdle` 前后插入 prompt，不能丢 prompt 或错误 shutdown；
2. 任一 background terminal、monitor、scheduler fire、subagent、lease 或 approval 存在，必须拒绝 unload；
3. shutdown 后的 late completion 只记录为 stale/reconciled，不能复活 session 或发布完成；
4. 两个 unload 请求竞争时，至多一个获得 shutdown ownership；
5. 事件 sequence 缺口、重复 terminal event、foreign owner、expired lease 均 fail-closed。

**可观测性而非日志猜测：** operator/status API 必须显示 tree/node、phase、owner、lease、last evidence、
budget、blocked reason 和下一次可安全动作。它不能展示模型思维链，也不能因一段 stdout 包含 “done” 显示完成。

---

### 3.4 2026-08-03 增补：半透 Agent 沙箱、客户端 Advisor 与 Governed Evidence Loop

本节取代“共享更多上下文即可协作”的误解。多 Agent 的正确目标是：**共享已验证事实，隔离未验证
推理和执行能力；每轮可停止、可验证、可恢复，而不是更快地空转。** `AgentSandbox`、`ClientAdvisor` 和
`GovernedEvidenceLoop` 都是 SessionActor 的 consumer；它们绝不形成第二个 execution authority。

#### 3.4.1 AgentSandboxV1：不是只有 worktree 的沙箱

worktree/容器只能限制文件系统，不能限制模型看到什么、把什么写入记忆、调用什么工具、耗尽多少预算，
也不能给取消/恢复留下可信状态。因此每一个 root/child/advisor/kairos 节点都必须持有 actor-issued、
可撤销的沙箱合同。child 不持有 raw root handle、PermissionHandle、bypass token 或 sibling capability。

~~~rust
pub struct AgentSandboxV1 {
    pub sandbox_id: SandboxId,
    pub task_tree_id: TaskTreeId,
    pub node_id: TaskNodeId,
    pub immediate_parent_id: Option<TaskNodeId>,
    pub depth: u8,
    pub context_manifest_hash: ManifestHash,
    pub accepted_snapshot: AcceptedSnapshotRef,
    pub branch_id: BranchId,
    pub memory_capability: ReadAcceptedSnapshot | ProposeOwnBranch | RootResolve,
    pub capability_grant_id: GrantId,
    pub tool_contract_hashes: Vec<Sha256>,
    pub filesystem_scope: ReadOnlyRoots | WriteScopeLeaseRef,
    pub process_scope: ProcessScopeRef,
    pub network_class: NetworkDeny | ExplicitAdapterAllowlist,
    pub budget_slice: BudgetReservationId,
    pub inbox_policy: InboxPolicyRef,
    pub issued_at: Timestamp,
    pub expires_at: Timestamp,
    pub state: Active | Revoked | Expired | Frozen,
}
~~~

**信息流硬规则：**

1. 每个 child 读取的当前任务记忆仅为 `TaskContract + ContextManifest + AcceptedSnapshot`；可有自己的
   `BranchScratchpad`，但它永不自动外流、永不 promotion。
2. sibling 不读取彼此 scratch、chat、未接受 proposal、裸路径、secret 或“下一步命令”。他们只读取同一
   版本的 Accepted snapshot；任何新 snapshot 在下一次明确 `rebase`/admission 时才可见。
3. child 只可 append 自己 branch 的 `Proposed`/`EvidenceAttached` claim 和有界 `HandoffPacket`；直接
   向 sibling 发自由文本控制消息、修改 Accepted、修改长记忆、修改其他 branch 一律 deny。
4. root 或 direct parent 可看 child 的 handoff，但查看不等于接受；root actor 仍是唯一能 transition
   `HostVerified/Accepted/Rejected/Conflicted` 的 authority。
5. 外部网页、tool output、Advisor 文本和模型自评均以 `UntrustedEvidence` 进入 artifact，不能作为
   instruction 或可执行配置进入 manifest。
6. scope/grant/budget/manifest 被撤销或版本不再匹配时，sandbox 立即 `Frozen`；late tool/terminal 只可
   reconciliation，不能复活 sandbox。

`HandoffPacketV1` 的内容固定为 `from_node / snapshot_hash / proposed_claim_refs / evidence_refs /
uncertainties / next_bounded_step / terminal_or_blocked_reason`。不得携带完整 chain-of-thought、自由 shell
命令、secret、raw chat 或“请无条件相信我”的 prose。每包有大小上限、schema version、content hash 和
delivery receipt；已过期 snapshot 的包只可提示 rebase，不能合并。

**实施切片 `NG-04D Sandbox + Handoff`：** 先在 memory/tools 侧实现纯 DTO、canonical serializer、deny
reasons 和 property tests；随后在 child admission、tool dispatch、branch claim append 三处接入；最后才为
受隔离写任务映射现有 `WriteScopeLease`/worktree。不得先引入容器、第二 daemon 或新的 agent runtime。

**负例：** sibling scratch read、child cross-branch append、expired sandbox dispatch、proposal-as-instruction、
secret in handoff、same worktree dual writer、manifest/snapshot mismatch、cancel 后 late terminal、unclassified
MCP tool、depth-3 spawn。`SANDBOX_ISOLATION_GATE=PASS` 需要上述反例、正向 Accepted sharing、rebase、
cancel/revoke 与 exact-binary tree projection；它不证明 OS/container escape 防护或跨机隔离。

#### 3.4.2 GovernedEvidenceLoopV1：Loop 是状态机，不是无限提示词

Lumen 的 loop 分三层，且每层都有唯一状态、预算和停止条件：

| 层 | 驱动者 | 单轮输入/输出 | 允许继续 | 必须停止或升级 |
|---|---|---|---|---|
| node loop | 该 node 的 AgentSandbox | accepted snapshot、assignment → receipt/proposal/evidence/checkpoint | 出现新证据且未超 scope/budget | 无新证据、scope 扩大、验证失败不收敛、需要 parent 决定 |
| tree loop | root SessionActor | branch checkpoints/claims → accept/reject/rebase/cancel | 无冲突且 reservation 有余量 | conflict、snapshot stale、write overlap、root cancel、tree budget exhausted |
| supervisor loop | Kairos/operation consumer | lease/heartbeat/outbox → reconcile/freeze/operator action | receipt 可判定且 retry class 安全 | delivery/effect/owner unknown、lease gap、deadline 或 policy revoke |

~~~rust
pub struct LoopContractV1 {
    pub loop_id: LoopId,
    pub task_tree_id: TaskTreeId,
    pub node_id: TaskNodeId,
    pub sandbox_id: SandboxId,
    pub context_manifest_hash: ManifestHash,
    pub accepted_snapshot_hash: Sha256,
    pub allowed_actions: Vec<LoopActionClass>,
    pub checkpoint_policy: CheckpointPolicyV1,
    pub verification_policy: VerificationPolicyRef,
    pub stop_conditions: Vec<StopCondition>,
    pub budget_reservation_id: ReservationId,
    pub deadline: Timestamp,
}

pub struct LoopCheckpointV1 {
    pub loop_id: LoopId,
    pub iteration: u32,
    pub input_snapshot_hash: Sha256,
    pub action_receipts: Vec<ReceiptRef>,
    pub evidence_refs: Vec<ArtifactRef>,
    pub proposed_claim_refs: Vec<ClaimId>,
    pub state: Progressed | NeedsEvidence | NeedsParentDecision | Blocked
             | BudgetExhausted | Cancelled | Frozen | CompletionCandidate,
    pub reason_codes: Vec<LoopReasonCode>,
}
~~~

`CompletionCandidate` 永远不是 success；它只允许进入 typed verification、HostVerification 和 root
acceptance。相反，`NeedsEvidence`、`NeedsParentDecision`、`Blocked`、`Frozen` 都是可见的正常结果，禁止
模型用“再试一次”绕过。repair 只能创建新 iteration，必须引用上一轮失败 receipt，并受 repair count、
deadline、token/tool/artifact 预算限制。

**检查点触发：** 首次副作用前；每次 tool/evidence 后；连续两次无新 evidence；scope/预算/模型变更前；
验证失败；终端候选；cancel/revoke/queue uncertainty。只有 deterministic policy 能判定 transition；模型、
Advisor 和 UI 只能提供输入。这样 harness 管一次受治理运行，loop 管可观测的多轮闭环，但二者共用同一
SessionActor authority。

**实施切片 `NG-04E Governed Evidence Loop`：** 先做无 provider 的 pure state reducer + fake clock +
bounded iteration property tests；再将 checkpoint 写入 operation journal/ledger；最后映射现有 Expert repair、
workflow 以及 tree UI。禁止以 cron、tmux、stdout、普通 retry 或“模型自评完成”替代 checkpoint。
`LOOP_CONVERGENCE_GATE=PASS` 需：progress、no-progress、repair limit、stale snapshot、cancel/late event、
verification failed、queue unknown、budget expiry 的完整状态矩阵；同时要求 exact-binary offline trace。

#### 3.4.3 ClientAdvisorV1：复现功能体验，不复制供应商服务端

Claude 类产品的可取经验是“在重要节点让独立审查模型介入、让用户可见开关/模型/用量、结果回到当前
任务”。Lumen 的实现不能依赖 provider 的私有 `server_tool_use`、schema type、header 或模型名；应当把
它实现为注册在本地 ToolRegistry、由 SessionActor dispatch 的虚拟工具 `lumen_advisor_consult`。

~~~text
primary model invokes virtual tool (or root checkpoint requires it)
  → SessionActor validates mode / user pool / pin / privacy / budget / deadline
  → build redacted AdvisorContextCapsuleV1 from manifest + AcceptedSnapshot + allowed artifacts
  → call one independently configured, user-allowed provider/model
  → persist AdviceReportV1 + AdvisorUsageReceiptV1 as artifacts
  → return bounded structured tool result to primary/root
  → root policy decides Continue | NeedEvidence | NeedParentDecision | Freeze
~~~

**模式与触发：**

| mode | 行为 | 默认 | 不允许的越权 |
|---|---|---|---|
| `off` | 不注册虚拟工具、不发 provider request | 是 | 不得有隐式咨询 |
| `shadow` | 只记录按策略本应咨询/会选哪个模型 | 可用于评估 | 不调用 provider、不改变模型 |
| `on_demand` | root 或主模型明确请求一种受限 review | 用户显式开启 | 任意 prompt 泄漏/无限咨询 |
| `checkpoint` | 仅在 plan、failure convergence、scope/budget escalation、completion candidate 触发 | 经 shadow corpus 后 | advisor 自行创建检查点或阻塞执行 |

`AdvisorRequestV1` 只能选择 `PlanReview | EvidenceGapReview | FailureConvergenceReview |
ScopeOrBudgetEscalationReview | CompletionCandidateReview`，不能塞任意自由 prompt。`AdviceReportV1` 必须包含
`risk_findings / counterevidence / missing_evidence / suggested_verification / escalation_recommendation /
confidence / report_hash`，且明确标记为 advisory。`AdvisorUsageReceiptV1` 必须有 user pool、实际 model/provider
reference、manifest/snapshot/input/output hash、token/cost 若 provider 可报、deadline、cancel/timeout/deny
reason、是否被 root 采纳；unknown usage 记 unknown，不能记零。

**绝对边界：** Advisor 无 filesystem/shell/MCP/write scope、无 claim acceptance、无 bypass、无 model switch、
无 terminal success、无 direct child spawn。它可建议一个新任务候选模型，但 Applied assignment 仍只可在
`NG-07` 的 new admission/no output/no effect/root approval 条件下发生。Advisor 不可用或超时返回
`Unavailable`；除非 deterministic policy 已把该 checkpoint 定义为 mandatory，否则不重放主任务、不偷偷换
模型；mandatory 也只会得到 `Blocked(AdvisorUnavailable)`，不会自动放宽。

**缓存与提示词：** 可把虚拟工具 descriptor 作为稳定 ToolCatalog 的尾部 feature segment，降低开关对静态
前缀的扰动；但所有 provider 的 cache 行为都必须由实际 provider receipt 证明。开关、模型、schema 或
redaction policy 改变必须产生新的 tool catalog/manifest hash，禁止声称“绝不影响缓存”。system prompt 可
提醒何时咨询，但实际 admission、次数、数据最小化、权限和完成门只在 actor/tool policy 强制。

**实施切片 `NG-06A ClientAdvisor`：**

1. `AdvisorContextCapsuleV1` pure builder：redaction、artifact allowlist、stable hash、size cap、foreign/
   proposed/secret/path denial；零 network。
2. `AdviceRequest/Report/UsageReceipt` schema、tool registry descriptor、`off/shadow` status/UI projection；
   默认 `off`，不改现有 Expert 行为。
3. mock provider adapter：timeout/cancel/receiver closed/usage unavailable/invalid schema/independent failure
   domain 的 negative matrix；只在 fixture 使用，禁止 billable call。
4. `on_demand` local virtual tool：root/SessionActor policy、budget reservation、bounded result artifact、
   explicit cancellation；结果仍不能改变 claim/assignment。
5. `checkpoint` shadow corpus，证明每一次自动触发都有 receipt、bounded rate、停止条件和 privacy decision；
   通过独立审阅后才可 gated rollout。

`CLIENT_ADVISOR_GATE=PASS` 必须证明 off 无 provider attempt、shadow 无 provider attempt、on-demand 的
capsule 最小化、Advisor 不能接受 claim/执行 tool/切换已输出 stream、cancel/timeout/unknown usage 真实可见、
cache/manifest drift fail-closed。它不证明任何供应商实际质量、实时价格或 live availability。

#### 3.4.4 产品 UX 与开发态轻量路径

默认短任务维持 `interactive_single_turn`：不显示空树、不调用 Advisor、不创建长期记忆或 daemon。用户明确
请求并行、子 Agent、恢复、定时任务或 checkpoint Advisor 时，UI 先展示即将升级的 profile、sandbox scope、
模型池/pin、权限、预算、snapshot revision 和可取消性。运行中至少投影：树节点、当前 loop checkpoint、
Accepted snapshot revision、proposal/verification 状态、queue pressure、Advisor 状态和花费真相、Blocked/Frozen
reason。所有 UI 是 actor/journal projection，不可编辑状态、不可隐藏 delivery uncertainty。

## 4. 依赖图与并行纪律

~~~mermaid
flowchart LR
  P0["P0-NR-A unsafe resubmit closed"] --> R0["R0 source + GitHub sync"]
  R0 --> T1["NG-01 TaskTree"]
  T1 --> C2["NG-02 Capability Ceiling"]
  C2 --> TC["NG-02A ToolContract + result boundary"]
  T1 --> X4C["NG-04C ContextManifest"]
  C2 --> B3["NG-03 TreeBudget + lifecycle"]
  B3 --> O3C["NG-03C operation lease/outbox"]
  O3C --> W3D["NG-03D WriteScopeLease"]
  O3C --> F3E["NG-03E flow control"]
  C2 --> L4["NG-04 WorkingLedger"]
  L4 --> X4C
  TC --> X4C
  X4C --> S4D["NG-04D AgentSandbox + Handoff"]
  C2 --> S4D
  O3C --> S4D
  S4D --> E4E["NG-04E Governed Evidence Loop"]
  F3E --> E4E
  R0 --> F5["NG-05 receipt + provider health"]
  F5 --> A6["NG-06 Advisor policy shadow"]
  X4C --> A6
  A6 --> CA6A["NG-06A ClientAdvisor virtual tool"]
  S4D --> CA6A
  E4E --> CA6A
  W3D --> G9A["NG-09A shadow-only offline golden path"]
  F3E --> G9A
  E4E --> G9A
  CA6A --> G9A
  G9A --> A7["NG-07 bounded assignment"]
  A7 --> G9B["NG-09B assignment golden-path extension"]
  O3C --> K8["NG-08 Kairos local"]
  F3E --> K8
  L4 --> K8
  X4C --> K8
  G9B --> G10["NG-10 release hardening"]
  K8 --> G10
~~~

**本轮最短开工纪律：** 在 `P0-NR-A=PASS` 与 `R0_SOURCE_GATE=PASS` 前，只允许只读 inventory、RFC、
fixture/test 设计和当前 P0 修复；不得并行实施 NG-01/02/03/04/04C/05/06/07/08/09 的 runtime consumer。
这避免在 ahead 194（开工时必须再实测）、source lock/readiness 失配的候选上同时叠加多个不可拆分的架构改变。

R0 之后才允许并行的**无重叠设计/测试准备**：NG-01 DTO/UI inventory、NG-04 claim schema RFC、NG-04C
ContextManifest fixture RFC、NG-05 mock transport matrix、Kairos fake-clock harness。每个 runtime contract 仍
一次只允许一个 writer；同一 coordinator/schema/permission/source-lock 路径绝不并行写。

绝不提前：没有 Tree/Ceiling/Budget/Sandbox 不开三层；没有 accepted snapshot/manifest 不把压缩摘要当 child
contract；没有 sealed receipt/no-replay 不做自动 routing；没有 ledger scope 不共写记忆；没有 Loop checkpoint
不让模型无限 repair；没有 ClientAdvisor gate 不发“自动咨询”；没有 lease/crash proof 不称 24h；没有 R0 exact
SHA/CI 不把本地当正式基础。

---

## 5. 通用实施卡

每个 phase/PR/交接都必须有：

~~~text
Phase ID / status / owner
Goal / non-goals
Exact input SHA / source-lock / RFC revision
Prerequisite gate
Allowed paths / forbidden paths/actions
Existing code/tests to read first
Proposed type/schema and compatibility version
Persistence/migration/rollback
Implementation steps
Positive cases
Negative or fault-injection cases
Exact commands + expected exit semantics
Evidence packet
Exit Gate
Stop condition
Handoff
~~~

缺 non-goals、负例、命令、停止条件或证据包任一项，任务不得交给辅助模型实施。

---

# Part I — R0 source and GitHub synchronization

## 6. R0-00：冻结与逐路径归属

**Owner：** Codex/Lumen integration owner。
**目标：** 每个脏路径有唯一 disposition，别人可重现起点。

**输入：** 第 1 节基线、CODE_REVIEW_HANDOFF.md、SOURCE_LOCK/SBOM/readiness。
**【拟建】产物：**

~~~text
artifacts/r0/<candidate>/manifest.json
artifacts/r0/<candidate>/scope-review.md
artifacts/r0/<candidate>/remote-snapshot.json
~~~

manifest 至少记录 start/end HEAD、merge base、origin SHA、fetch time、每个 path、组别、候选与否、SHA-256、owner、理由、protected 标记。

**步骤：**

1. 记录 cwd/top-level/branch/HEAD/remotes/divergence/status/process；
2. 归为 R0-A 上游/恢复、R0-B safety/evidence、R0-C 文档迁移、protected-not-for-commit；
3. 再 fetch origin；若 SHA 变化，manifest 作废重建；
4. 不动 runtime。

**负例：** 未分类 path、误提交 CODE_REVIEW_HANDOFF、git add -A、远端变化仍合并、reset/clean/stash。
**Exit：** 每一项有唯一 disposition。
**Stop：** 需猜测归属或丢弃内容。

## 7. R0-01：分组验证，不做整包假绿

| 组 | 范围 | 必核对 |
|---|---|---|
| R0-A | upstream mirror/absorb、Lumen 恢复、compaction、JSONL、Expert、BYOK | 每个显式 Lumen 分歧与相应 test。 |
| R0-B | prefetch hermetics、source-lock/reconcile/readiness contract | endpoint 不逃逸；evidence suffix 不掩盖 source drift。 |
| R0-C | 执行书/入口/索引/lock migration | 当前链接不指向已删除旧规划。 |

最低命令：

~~~zsh
cd /Users/lei/code/lumen
git diff --check
cd agent
cargo check -p xai-grok-shell
cargo test -p xai-grok-shell --lib prefetch_env_
cargo test -p xai-grok-shell --lib parse_output_issuer_claim_does_not_grant_xai_auth
cd ..
./scripts/test-readiness-contract.sh
~~~

**负门：** filter=0、grep pipeline 丢 cargo exit、只跑 check 宣称 suite、复用旧 CI。
**证据：** diff hash、argv、raw exit、passed/failed/ignored/filtered、HEAD、NOT RUN。
**Exit：** 每组独立 PASS/FAIL/BLOCKED/NOT RUN。

## 8. R0-02 至 R0-05：clean source、集成、证据、发布分门

### R0-02 clean source candidate

只使用 git add -- exact paths；每个 commit 只服务一合同；记录 full SHA、parent、tests、rollback SHA。candidate 必须干净后才能 build/lock/SBOM/readiness。

禁止：git add -A、reset、clean、stash、rebase、force push、在未归属内容上 merge。
Exit：可重建 candidate；git diff --check merge-base..candidate 为 0。

### R0-03 integration candidate

前置：R0-02 后再 fetch origin。
产物：commits.json、integration-decision.md、conflict-tests.json。
每个冲突记录 base/ours/theirs、选择、理由、测试、reviewer。
Stop：origin 再前进、CI 失败、冲突无可验证裁决。

### R0-04 source/binary/evidence 三段式

1. 从 clean source candidate `A` 构建并记录 binary_sha256；
2. source lock 锁 `A`；
3. 生成 SBOM/readiness/evidence；
4. 只允许 lock/SBOM/readiness/evidence 文件形成 `A` 的 suffix `B`；
5. verifier 检查 `B → A` 的每一个路径、binary stamp 与 evidence hash。

新增负例：source-lock 前 tracked/staged dirty 必须失败；任意源代码混入 evidence suffix 必须失败；binary SHA/
source stamp 不同必须失败；B 被误作为 binary source、A/B relation 不为 declared evidence suffix 必须失败。

### R0-05 PR/merge/tag/release/install

| 门 | 所需证据 | 不代表 |
|---|---|---|
| PR | exact integration SHA、required CI、review | 已合并/发布。 |
| Merge | GitHub main 指向审过 source/evidence | 已有 tag。 |
| Tag | tag 指向验证的 clean source `A`，而非 evidence suffix `B` | assets 完备或 A/B transaction 已验证。 |
| Release | assets/checksum/SBOM/signature/manifest | 干净机可安装。 |
| Install | 隔离环境安装、version/hash/basic run | M5/M6/live/soak 已通过。 |

R0 结束仅可称可消费 source baseline，不解除 M5/M6、soak、live eval、当前失败 CI 或
`NG10_RELEASE_FOUNDATION_GATE`。

### R0 source gate 与 NextGen contract gate 必须分开

`R0_SOURCE_GATE=PASS` 只证明 canonical source 已完成 integration、exact-SHA CI、明确的 source `A` / evidence
suffix `B` relation、A-built binary/SBOM/readiness binding 和 rollback 记录。它不证明任何【拟建】的 TaskTree read model、
CapabilityGrant、ToolContract、ContextManifest、WorkingLedger replay、ProviderAttemptReceipt、lifecycle journal
或 Kairos API 已实现、稳定或可供其他系统调用。

每一个进入产品控制面的合同另有独立 `<CONTRACT>_GATE=PASS` receipt，至少必须包含：

- exact canonical commit 与 rollback commit；
- schema/API revision、compatibility 与 deprecation 声明；
- manifest hash、positive/negative/fault 测试的 argv、raw exit 与真实计数；
- 只要该合同进入 ACP/TUI seam，就必须有 rebuilt-binary hash 和离线产品证明；
- 若触发 CI，则附 exact GitHub SHA、URL 与 conclusion；未跑明确写 `NOT RUN`。

缺任一字段即为 `BLOCKED_CONTRACT`。R0 的绿不能替代 contract gate；contract gate 的绿也不能替代
release、live/provider、24h soak 或人工 merge。

---

# Part II — NextGen Core phases

## 9. NG-01：TaskTreeLineage v1

**状态：** 核心已实现并在本次审计跑过 depth-4 硬拒与根取消级联；仍未完成 exact CI、release 和端到端 golden path。
**目标：** 真正表现 Main→Code→Review/Test/Evidence 的每条边。

### 必读锚点

| 路径 | 事实 |
|---|---|
| agent/crates/codegen/xai-grok-tools/src/implementations/grok_build/task/mod.rs:180-234 | 当前 depth check。 |
| 同 crate task/coordinator.rs:208-259 | nested child 保留 `parent_session_id` 的真实直接父节点，根责任另存 `lineage`。 |
| 同 crate coordinator_tests.rs:250-314,1249-1289 | 覆盖 depth-4 硬拒、根取消级联和真实直接父节点。 |
| xai-grok-shell/src/config/mod.rs:436-483 | max_depth 默认/优先级。 |
| xai-grok-shell/src/agent/subagent/handle_request.rs:375-404 | child depth 与 leaf 去 Task。 |
| xai-grok-pager/src/views/dashboard/state.rs:125-140,1254-1272 | 当前 parent/child DTO 投影。 |

### 【拟建】契约

~~~rust
pub struct TaskTreeLineageV1 {
    pub task_tree_id: TaskTreeId,
    pub root_session_id: SessionId,
    pub immediate_parent_session_id: Option<SessionId>,
    pub child_session_id: SessionId,
    pub depth: u8,
    pub lineage_path: Vec<SessionId>,
    pub root_process_scope: ProcessScopeId,
    pub schema_version: u16,
}
~~~

迁移期保留旧 parent_session_id wire 字段，其语义固定为 root；新 immediate parent 才用于 UI、scope 和 ledger branch。

**允许路径：** coordinator/types/tests、shell spawn/metadata/resume/task DTO、pager dashboard/tasks pane/tests。
**禁止：** 改默认 max_depth、改 permission、改 routing/source-lock/release。

**步骤：**

1. lineage serialization/validation 纯测试；
2. coordinator 同时保存 root process scope 和 logical parent；
3. local/GCS metadata 版本化读写；旧记录读为 root-only compatibility projection；
4. ACP DTO 兼容旧 client；
5. pager 只读树；
6. cancel/teardown 改用 lineage descendant。

**反例：** forged parent/depth、cycle、depth-3 spawn、root/immediate mismatch、resume drift、dashboard early success、old decode、sibling cancel bleed。
**命令入口：**

~~~zsh
cd agent
cargo test -p xai-grok-tools raised_max_depth_allows_nested_spawn
cargo test -p xai-grok-tools loop_tracking_covers_pending_active_and_nested_reparenting
cargo test -p xai-grok-shell --lib subagents_max_depth_
cargo test -p xai-grok-pager --lib dashboard
~~~

后两条若 filter 0 必须写 NO TESTS MATCHED。
**Exit：** lineage/compat/cancel/resume tests 通过，flag 默认关闭。
**Rollback：** 关闭 flag；旧 metadata/read wire 不删。

## 10. NG-02：CapabilityCeiling v1

**状态：** depth-3 强制只读、禁止再 spawn、禁止继承 MCP 已有离线覆盖；通用 grant/TTL/revoke token 仍是 Draft，不能把现有收缩规则称为完整 capability grant 系统。
**目标：** child effective capability 永远是 root policy ∩ parent ceiling ∩ role ∩ operation approval。

### 必读锚点

| 路径 | 事实 |
|---|---|
| shell/src/agent/subagent/handle_request.rs:375-403 | role/runtime capability 与 leaf 收口。 |
| tools task/types.rs:207-216,257-267 | unknown ToolKind/custom MCP 当前被保留。 |
| shell/src/agent/mvp_agent/subagent_coordinator.rs:492-497 | child 继承 PermissionHandle。 |
| shell/src/agent/subagent/handle_request.rs:1083-1087 | yolo/bypass 可传 child。 |
| shell/src/agent/config.rs:1785-1797 | parent CLI mode 可压 child mode。 |
| shell tests/subagent_spawn_context_tests.rs:9-74 | .env deny 负例模式。 |

### 【拟建】grant

~~~rust
pub struct CapabilityGrantV1 {
    pub grant_id: GrantId,
    pub issuer_root_session_id: SessionId,
    pub target_node_id: TaskNodeId,
    pub capability: Capability,
    pub resource_scope: ResourceScope,
    pub issued_at: Timestamp,
    pub expires_at: Timestamp,
    pub reason: String,
    pub approval_ref: ApprovalId,
    pub nonce: Nonce,
    pub state: Active | Revoked | Expired,
}
~~~

depth 1 仅 root grant 后 scoped-write；depth 2 默认只读；depth 3 无 spawn/background/arbitrary shell。unknown ToolKind/MCP 无 manifest 即 child deny。

**步骤：** taxonomy/inventory → report-only unknown audit → child deny → raw permission/yolo 改 ceiling → TTL/revoke/ancestor cancel → root approval → pager projection。
**反例：** root bypass/Auto/AcceptEdits 下传、unknown MCP、TTL/revoke、ancestor cancel、sibling scope、child commit/push、raw path/store。
**Exit：** property test 证明单调收缩；无 grant 的 depth2/3 不显示 write/shell/network。
**Stop：** 内建 MCP 无法分类即维持 deny，不能为兼容放开。

### NG-02A：ToolContract 与 result/context boundary v1

**状态：** Draft；现有 `xai-grok-tools/src/tool_taxonomy.rs` 已有 `ToolKind`、canonical metadata 与
read-only 分类，shell 已有 MCP descriptors、tool definition snapshots、compaction/recap 的 result truncation
辅助逻辑。它们尚未共同构成“每一个实际可调用 tool 都有 capability、scope、result artifact 和 context
预算”的统一合同。此切片采纳 PDF 的可取经验——专用工具优先、工具输出不能无限填满上下文、工具 schema
是能力边界——但不复制其 runtime、工具数量或权限旗标。

**目标：** `CapabilityGrant` 只授权一个已知、可分类、版本化的 tool descriptor；模型可见的 tool/result 是
有界投影，完整原始输出留在 artifact/evidence 存储。无 descriptor 的 custom MCP/Other 绝不因为“能连上”
而在 child 或 autonomous profile 中获得执行权。

~~~rust
pub struct ToolContractV1 {
    pub tool_identity: CanonicalToolIdentity,       // namespace + name + schema hash
    pub tool_kind: ToolKind,
    pub operation_class: ReadOnly | ReversibleWrite | ExternalEffect,
    pub required_capability: Capability,
    pub resource_scope: ResourceScope,
    pub input_schema_hash: Sha256,
    pub result_policy: ToolResultPolicyV1,          // preview bytes, artifact class, redaction
    pub idempotency_class: NeverReplay | IdempotentWithReceipt | ReadOnlyRetryable,
    pub provider_or_endpoint_ref: Option<EndpointFingerprint>,
    pub policy_revision: PolicyRevision,
}

pub struct ToolResultEnvelopeV1 {
    pub call_id: ToolCallId,
    pub tool_contract_hash: Sha256,
    pub operation_id: Option<OperationId>,
    pub status: Succeeded | Failed | Cancelled | Unknown,
    pub preview: RedactedBoundedText,
    pub full_artifact: Option<ArtifactRef>,
    pub full_artifact_hash: Option<Sha256>,
    pub emitted_bytes: u64,
    pub context_bytes_admitted: u32,
    pub verification_ref: Option<VerifyReceipt>,
}

pub struct DataRetentionPolicyV1 {
    pub classification: Public | WorkspacePrivate | Credential | SensitiveArtifact,
    pub persistence: Forbidden | RedactedPreviewOnly | EncryptedArtifactRef,
    pub retention_deadline: Option<Timestamp>,
    pub deletion_authority: RootSession | OperatorPolicy,
}

pub struct ContextFragmentV1 {
    pub source_ref: ArtifactRef,
    pub content_hash: Sha256,
    pub trust: RootImmutableAssignment | AcceptedEvidence | UntrustedToolOrRemoteData,
    pub render_mode: ControlPlane | QuotedDataOnly,
    pub byte_limit: u32,
}
~~~

**实施顺序：**

1. 在 `xai-grok-tools/src/tool_taxonomy.rs`、现有 registry/types 与 `xai-grok-shell/src/session/mcp_descriptors.rs`
   做 inventory，先产生纯 read-only registry snapshot/hash；不改变现有 tool dispatch。
2. 用 fixture 把 every built-in/MCP descriptor 映射到 `ToolContractV1`。unknown、ambiguous、schema-hash missing、
   endpoint scope unknown 一律拒绝 child/daemon admission；root interactive 只能显示需要显式审批，不能静默执行。
3. 在 tool dispatch 外围生成 `ToolResultEnvelopeV1`：preview 必须 redacted、bounded、标明 truncated；完整结果
   写 artifact 后再入 ledger/evidence。不要截断后假装完整，也不要把原始 HTML/MCP/terminal output 当下一轮
   system instruction。
4. 将 `tool_catalog_hash` 与允许 tool contract hashes 放入 ContextManifest；compact/resume 必须用同一 snapshot
   或显式 re-admit，禁止在恢复时悄悄增添 MCP/工具 schema。
5. 最后才启用按需工具 descriptor 展开（deferred visibility）。它只节省 context，不改变 capability；tool
   discovery 仍需由 actor 按 manifest allowlist 审批。
6. 将 credentials 与内容数据分开：`acp_session.rs` 的 credential provider、terminal 的 environment
   exclusion、`session/expert.rs` 的 `redact_and_truncate` 都是可复用实现线索。新的 manifest、claim、
   lifecycle event、tool preview、status 和 audit 只可持有 `SecretRef`/redacted hash，绝不 serialise raw key、
   bearer、authorization header 或受保护路径；full artifact 也必须有 owner、classification、retention 和
   deletion receipt。
7. 将所有 repo 文件、终端/MCP/HTTP 输出、issue/PR 文本和网页内容标为
   `UntrustedToolOrRemoteData + QuotedDataOnly`；它们可以作为受限的证据数据被模型阅读，但绝不能变成
   grant、manifest 字段、model assignment、approval、claim transition 或 tool dispatch 的控制输入。只有
   root immutable assignment 和已验证 evidence 可进入对应的 control-plane fragment；actor 决策永远只读
   typed contract/hash，不解析自然语言里的“忽略规则”“执行命令”等文本。

**反例：** `ToolKind::Other`/custom MCP、descriptor/schema swap、unbounded terminal output、tool-output prompt
injection、foreign artifact、redaction miss、truncated result 被当 PASS、resume 后 tool catalog drift、child 尝试
arbitrary shell、ExternalEffect 无 idempotency receipt、credential/authorization header 出现在 manifest/ledger/
status/preview、expired artifact 被恢复、child 请求原始 secret、HTML/终端/MCP/仓库文本诱导 grant、审批、
模型切换、ledger acceptance 或 bypass。

**Gate：** `TOOL_CONTRACT_GATE=PASS` 需 snapshot fixture、every-kind mapping、unknown-MCP deny、result-byte
bound、artifact hash/redaction、manifest catalog mismatch、typed `VerifyAfterEditOutcome` 不被 text 覆盖的正反例；
并附 `SECRET_BOUNDARY_GATE=PASS`：跨 manifest/ledger/status/ACP preview 的 secret-leak corpus、retention
expiry/deletion ownership、symlink/path redaction 与 credential reference negative cases。若接入 ACP/TUI，再加
rebuilt-binary projection。`UNTRUSTED_CONTENT_GATE=PASS` 还需覆盖 terminal/MCP/HTML/repo/issue prompt-injection
fixtures：所有攻击文本均不得改变 typed policy、grant、assignment、claim 或 dispatch。未完成前，NG-04C-2 不得
把 tools 当作可自由变化的 prompt 内容。

## 11. NG-03：TreeBudget 和受管进程 lifecycle

**状态：** 部分已实现：coordinator 有根树 live/token/tool/wall-time 限额，scheduler 有 owner lease、heartbeat、backoff 和死信。NG-03A 已在 shell actor 落地有限的 activity snapshot：foreground/queued、pending interaction、按 owner 筛选的 terminal/monitor、direct child 和 scheduler active-run lease 都会拒绝 idle unload；探测超时 fail-closed。它不是跨 adapter 原子事务，也尚未实现 generic operation lease、daily-cost/artifact 限额、event journal/outbox/reconcile 或 24h proof。
**目标：** 并行 Agent、terminal、monitor、scheduler fire 有同一 tree owner、预算、deadline、回收语义。

### 必读锚点

| 路径 | 事实 |
|---|---|
| tools task/coordinator.rs:165 | coordinator 已有 child cancel。 |
| tools scheduler/actor.rs:511-617 | 已防前一轮 descendants 未结束时重复 fire。 |
| tools workflow/mod.rs:171-176 | 有 workflow agent budget，不等于 tree budget。 |
| shell session/acp_session_impl/activity_snapshot.rs | actor-owned bounded activity read model，timeout fail-closed；不是 durable lifecycle journal。 |
| shell agent/mvp_agent/session_lifecycle.rs | dispatch lock + actor `UnloadIfIdle` 的 check-and-act seam。 |
| tools computer/local/terminal.rs | 背景 terminal owner/reap 模式。 |

### NG-03A activity aggregation

【部分实现】SessionActivitySnapshot 由 actor 内 `UnloadIfIdle` 单命令读取：foreground/queued、background terminal、monitor、scheduler active-run lease、direct subagent、pending approval。terminal/monitor 以 session owner 筛选；owner-less 旧快照保守视为本 session；adapter read 超时会拒绝 unload。

该切片只保证 actor mailbox 上的 check-and-act 顺序与“未知即保留”。它**不**声称同时读取 terminal、coordinator、scheduler 即获得分布式原子快照；在 NG-03C event journal、operation lease 和 reconciliation 到位前，late event、崩溃恢复与外部副作用仍不能结案。

反例：check/unload 间注入 prompt；monitor/scheduler/background child 活着却 unload；late completion 复活 disposed session。
Exit：所有活动存在时 unload 拒绝；actor check-and-act 无丢 prompt。

**独立 Gate：** `ACTIVITY_UNLOAD_GATE=PASS` 只证明 actor mailbox 的 check-and-act、unknown-activity
fail-closed、foreground/background/approval 的 unload 反例，以及 exact targeted raw counts。它不证明 durable
operation recovery、outbox、跨 adapter 原子快照或 24h；这些必须留给 NG-03C/NG-08。

### NG-03B 【拟建】TreeBudgetV1

~~~rust
pub struct TreeBudgetV1 {
    pub max_depth: u8,
    pub max_children_per_node: u8,
    pub max_live_nodes: u16,
    pub max_background_nodes: u16,
    pub token_reservation_limit: Option<u64>,
    pub tool_call_limit: Option<u32>,
    pub wall_time_limit: Duration,
    pub daily_cost_limit: Option<Money>,
    pub artifact_byte_limit: Option<u64>,
}
~~~

受管进程记录 tree_id、owner_node_id、lease_id、process_scope、pid、deadline、heartbeat、artifact_location、reservation_id。reserve_spawn 是原子 check+reserve；release 幂等。

可信 provider usage 才扣 token/cost；缺失明确 usage_unavailable。

**反例：** 并发 spawn 超额、duplicate cancel、orphan、late completion、reservation leak、deadline leak、retry storm、artifact 超额、usage 记零。
**Exit：** root cancel 回收整树；reserve 在 success/fail/cancel/timeout 恰好 release 一次。
**Rollback：** flag 拒绝新 child，所有 process 按 root scope 回收。

**独立 Gate：** `TREE_BUDGET_GATE=PASS` 要有 concurrent reserve property tests、cancel/late completion 的
exactly-once settlement、usage-unavailable truthfulness、budget state replay 和 raw test counts。它不授权新的
process recovery、provider reroute 或 child write；这些仍受各自 contract gate 约束。

### NG-03C：GovernedOperation、operation lease、outbox 与 reconciliation

**状态：** Draft。scheduler 的 task-scoped durable lease、heartbeat、backoff/dead-letter 与
occurrence journal 是可复用局部基础；workflow 在进程重启后转 terminal，正说明它不是通用可恢复
operation。`SessionActivitySnapshot` 也只是 unload read model，不能代替 durable event journal。

**目标：** 给每个 child、terminal、monitor、scheduler fire、workflow run 和未来 Kairos job 一条
root-owned operation identity。所有 UI/log 是 event projection；恢复、cancel、takeover 与 retry 都以
lease epoch、idempotency class、receipt 和 outbox 判定，不以 PID、stdout 或内存 registry 猜测。

#### 【拟建】最小持久合同

~~~rust
pub struct GovernedOperationV1 {
    pub operation_id: OperationId,
    pub task_tree_id: TaskTreeId,
    pub owner_node_id: TaskNodeId,
    pub owner_session_id: SessionId,
    pub operation_class: ReadOnly | ReversibleWrite | ExternalEffect,
    pub idempotency_key: Option<IdempotencyKey>,
    pub lease: OperationLeaseV1,
    pub budget_reservation_id: ReservationId,
    pub context_manifest_hash: Sha256,
    pub state: OperationState,
    pub last_event_sequence: u64,
}

pub struct OperationLeaseV1 {
    pub lease_id: LeaseId,
    pub epoch: u64,
    pub holder: SupervisorId,
    pub acquired_at: Timestamp,
    pub expires_at: Timestamp,
    pub heartbeat_at: Timestamp,
}
~~~

`GovernedLifecycleEventV1`（第 3.3.2 节）是 append-only authority；OutboxRecord 必须和 state
transition 原子写入。消费者按 event_id 幂等，不能因为重启再派发一次 effect。没有 idempotency receipt
的 ExternalEffect、任何 emitted model output、tool/effect state Unknown 都进入 `Frozen`，只可由 root/user
重新批准。

#### 实施顺序

1. 先在现有 scheduler occurrence journal 的 durable write/read/recovery pattern 上抽出 crate-local
   operation store；不得直接让 Kairos 或 shell 各自存一套 JSON。
2. 接入 read-only terminal/monitor/child lifecycle；只做 event record + projection，不开放自动 retry。
3. 接入 root cancel：ancestor cancel 必须标记 descendant operation、撤销 lease、幂等 release budget，
   再由 process adapter 收尾。
4. 接入 crash recovery：同 epoch 只有一个 holder；foreign/expired lease 先 reconcile terminal receipt；
   任何 sequence gap、unknown owner、missing manifest/budget/evidence 进入 RecoveryRequired/Frozen。
5. 最后才使 NG-08 的 Kairos 以该 API claim/heartbeat/complete/fail/freeze/take_over；Kairos 不持有
   shell/model/tool 的第二执行权。

#### 必测反例

- 两个 supervisor 同时 claim、旧 owner heartbeat、lease expired takeover、duplicate outbox consumer。
- process exit/late terminal event 在 cancel/restart 后到达；不得复活 node、重复 release 或伪造 success。
- crash 发生在 state 写入前后、outbox 写入前后、effect receipt 前后；每种状态唯一安全动作明确。
- idempotent read 与 write 可重试的界限；model output/tool/external effect/unknown 永远不自动 replay。
- orphan owner、foreign tree、budget/manifest mismatch、operator freeze 与 root close 都 fail-closed。

**Gate：** `OPERATION_RECOVERY_GATE=PASS` 要有 fake clock、two-holder、crash point、outbox duplicate、
late-event、cancel/release、Frozen negative matrix，以及 exact-binary start/stop/recover proof。它不等于
24h soak。

**停止条件：** 如果 operation 没有 durable owner/lease/manifest/budget/terminal receipt 中任一项，或
无法判定外部 effect 是否发生，就不能自动 retry、takeover 或标 success。

### NG-03D：WriteScopeLease、worktree handoff 与 merge receipt

**状态：** Draft；现有 `xai-grok-shell/src/session/worktree.rs` 与 `xai-grok-workspace/src/worktree/mod.rs`
已经能 create/resume/apply worktree，并在 apply 时基于 base commit 计算文件冲突。这只是工作目录机制，
不是“哪个树节点在何时可写哪些路径”的 authority；当前并行 child 不能仅靠不同 worktree 被误称为无冲突。

**目标：** 每一个写入 node 只能在 root 签发的、时间有限的、可审计的 write scope 内工作；提交/合并是
root-owned handoff，不是 child 自行 `git commit/push/merge`。同一 logical scope 的并发写必须在 spawn
前被拒绝，而不是等文件互相覆盖后让模型猜怎么合并。

~~~rust
pub struct WriteScopeLeaseV1 {
    pub lease_id: WriteLeaseId,
    pub task_tree_id: TaskTreeId,
    pub node_id: TaskNodeId,
    pub worktree_id: WorktreeId,
    pub base_commit: CommitId,
    pub target_ref: RefName,
    pub allowed_path_globs: Vec<CanonicalPathGlob>,
    pub write_capability_grant_id: GrantId,
    pub issued_at: Timestamp,
    pub expires_at: Timestamp,
    pub state: Active | Revoked | Expired | HandedOff,
}

pub struct MergeReceiptV1 {
    pub write_lease_id: WriteLeaseId,
    pub observed_base_commit: CommitId,
    pub changed_path_hashes: Vec<PathHash>,
    pub apply_result: Applied | Conflict | Rejected | Cancelled,
    pub verification_refs: Vec<ArtifactRef>,
    pub root_decision_ref: ApprovalId,
}
~~~

**实施顺序：**

1. 只读复用现有 `CreateWorktree*`/`ApplyWorktreeRequest` 的 base-commit 和 file-conflict 信息，先建立
   scope overlap detector；它必须 canonicalize paths、resolve symlink policy、拒绝 `..`/absolute/empty glob。
2. 在 child spawn 前以 actor 事务同时取得 `CapabilityGrantV1`、`TreeBudget` reservation 和
   `WriteScopeLeaseV1`；read-only role 不生成 write lease，depth-2/3 默认也不生成。
3. terminal/file tool dispatch 只接受 lease 内的 canonical path；worktree 的 diff 产生候选 handoff artifact，
   而非自动 apply/commit/push。
4. root 以 current target ref、base commit、path set、verification/evidence、dirty target 检查生成
   `MergeReceiptV1`。任何 stale base、overlap、conflict、unverified patch 或 root cancel 都保持 pending/
   rejected，不能让另一 child 自动 merge。
5. 关闭/cancel/expiry 时撤销 lease、停止相关 process、保留 patch/evidence；删除 worktree 必须是单独的
   recoverable lifecycle action，不能成为丢失 artifact 的清理捷径。

**反例：** two writers same glob、one writer parent glob/one child file glob、symlink escape、stale base、dirty
target、unscoped changed file、child `git commit/push`、root cancel 与 late apply、worktree restore 后 lease
owner mismatch、non-git workspace path escape。

**Gate：** `WRITE_SCOPE_GATE=PASS` 需 overlap property tests、apply conflict fixtures、canonical path/symlink
negative cases、cancel/expiry/recovery、root-only handoff receipt 和 rebuilt-binary tree projection。它不是自动
merge/auto commit 门；这些仍需独立 human/release authorization。

### NG-03E：Flow control、delivery uncertainty 与 liveness

**状态：** Draft。当前 sampler event pipeline 和 shell spawn/actor 仍有多处 unbounded channel；这对轻量
interactive path 可减少阻塞，但没有统一 queue-pressure、event-loss 或 backpressure contract。P0 已证明：
“sender 调用了 send”不等于“actor 可安全重放/完成”。

**目标：** 把吞吐优化与 authority 分开：非权威 UI token 可以合并/降采样，但一旦流被截断、channel closed、
sequence gap 或 consumer unknown，attempt/operation 的真实性必须变 `Unknown/Frozen`，而不是丢日志后继续
说成功。

~~~rust
pub struct DeliveryObservationV1 {
    pub delivery_id: DeliveryId,
    pub attempt_or_operation_id: OwnerId,
    pub sequence: u64,
    pub class: UiChunk | ToolSignal | LifecycleControl | TerminalReceipt,
    pub state: Enqueued | Coalesced | Dropped | ReceiverClosed | Unknown,
    pub queue_pressure: Normal | HighWatermark | Saturated,
    pub observed_at: Timestamp,
}
~~~

**规则：** UI text/reasoning 可在明确标记 `Coalesced` 后少显示，但第一次 raw output 仍使 provider attempt
不可 replay；tool signal、grant/cancel、lease、terminal receipt、claim/evidence event 不可静默 drop。
authority event 的 `try_send/send` failure 必须追加 delivery observation；保留/重建之前 operation 是
`RecoveryRequired/Frozen`。由 actor 定义每类 queue 上限、backpressure（reject、coalesce、block with deadline）
和 per-tree fair share；不得靠无界队列掩盖内存/延迟问题。

**验证：** fake slow consumer、高 watermark、receiver closed、cancel 与 late terminal、sequence gap、token flood、
tool flood、two tree fairness、shutdown drain deadline、event journal rebuild。每项断言 UI 诚实状态、预算/lease
不泄漏、无 duplicate completion/replay；性能测试记录 p50/p95 admission latency、queue depth、memory ceiling，
但不将一次 benchmark 当 daemon soak。

**Gate：** `FLOW_CONTROL_GATE=PASS` 需 exact load fixture、delivery observations、fault counts、bounded-resource
evidence 和 exact-binary status projection。通过后才允许把多个 child/monitor/scheduler 同时作为“24h
候选”；它本身仍不等于 24h。

## 12. NG-04：SharedWorkingLedger 与四层记忆

**状态：** `8b74b361` 已有核心 ledger、`ClaimAuthority`、`ContextManifestV1`、governed assignment 和
离线 golden fixture：child 只能 Proposed、root 才能 review/accept、Advisor/Kairos/TUI/MCP 无 acceptance 权；
foreign/torn/unproven accepted ledger 拒绝注入。root 的显式、evidence-preserving、idempotent workspace
promotion 与 root-only torn-tail repair 仍有效。它们是 crate-level foundation，不等于完整 cross-worktree
recovery/read-model、完整 claim migration、sandbox/handoff、compact-resume product proof 或 `LEDGER_REPLAY_GATE`。
**非目标：** 不把 SessionMemory/summary/vector DB 改成权威。

| 层 | 内容 | 权威/写权限 |
|---|---|---|
| SessionMemory | chat summary、workspace/global search、dream | 非权威，现有 policy。 |
| BranchScratchpad | branch 推理/临时 checkpoint | branch only，TTL，不能直升。 |
| SharedWorkingLedger | facts/progress/evidence/assumption/blocker/decision | tree authority；child Proposed，root decision。 |
| LongTermMemory | Accepted 稳定规范/经验 | root promotion only。 |

### 必读锚点

| 路径 | 事实 |
|---|---|
| xai-grok-memory/src/storage.rs:1-36,600-650 | global/workspace identity。 |
| 同 storage.rs:195-264 | Markdown append/overwrite，无 claim/CAS/conflict。 |
| xai-grok-memory/src/task_ledger.rs | append-only fact ledger、root-only tail repair、tail backup；不是完整 recovery journal。 |
| shell session/slash_commands.rs, memory_dream.rs | `/memory repair-ledger` 仅 user slash + root interactive session。 |
| shell subagent/handle_request.rs:783-817 | child memory injection limit。 |
| shell session/storage/jsonl/mod.rs:494-525,1847,1982 | strict write 与 lenient recovery。 |

### 【拟建】MemoryClaimV1

~~~rust
pub struct MemoryClaimV1 {
    pub claim_id: ClaimId,
    pub task_tree_id: TaskTreeId,
    pub branch_id: TaskNodeId,
    pub sequence: u64,
    pub revision: u64,
    pub author_node_id: TaskNodeId,
    pub kind: Fact | Progress | Evidence | Assumption | Blocker | Decision,
    pub status: Proposed | EvidenceAttached | HostVerified
              | Accepted | Rejected | Conflicted | Inconclusive | Superseded | Revoked | Frozen,
    pub content_hash: Sha256,
    pub evidence_refs: Vec<ArtifactRef>,
    pub provenance_refs: Vec<ProvenanceRef>,
    pub confidence: Confidence,
    pub policy_revision: PolicyRevision,
    pub context_manifest_hash: ManifestHash,
    pub supersedes: Option<ClaimId>,
    pub resolution_of: Option<ClaimId>,
    pub reason_code: Option<ClaimReasonCode>,
    pub created_at: Timestamp,
    pub expiry_or_review_after: Option<Timestamp>,
}
~~~

【拟建】crate/module 名为 xai-grok-working-ledger，最终边界须先 RFC。append-only journal 是 authority；SQLite/FTS/vector 是可重建 read model。

**写入：** child 仅自身 branch `Proposed`/`EvidenceAttached`；root 验 scope/grant/hash/evidence/conflict 后以
新的 resolution record 迁移状态；sibling 只读 Accepted snapshot；跨 worktree 以 task_tree_id 路由。状态不在
原记录上改写，`Superseded`/`Revoked` 也保留与旧 claim 的引用链。
**恢复：** lenient tail recovery 产出 `RecoveryEvent(skipped_count,byte_offset,raw_hash,quarantine_path)`，tree
变 `NeedsRecoveryReview`，禁止自动 promotion；重建 index 后逐 sequence/hash 比对 journal。任何 accepted claim
缺 manifest/evidence/sequence link、或 index 与 journal 不一致，read model 只可报 `Frozen`，不能给 child 注入。

**反例：** child direct Accepted、无 evidence Accepted、cancelled promotion、cross-tree read、stale hash、revision conflict、auto-merge、secret leak、torn append/index mismatch。
**Exit：** journal replay/read model 一致；所有 conflict 显式；summary 仅引用 Accepted。
**Rollback：** journal 保留，index 可删后重建。

**组合 Gate：** `LEDGER_REPLAY_GATE=PASS` 只在 `CLAIM_JOURNAL_GATE`、`CLAIM_AUTHORITY_GATE` 和
`ACCEPTED_SNAPSHOT_GATE` 均有同源 receipts 后成立，并额外证明 legacy/torn/unknown-schema/partial-migration
journal 能从 append-only authority 重建同一 accepted-set hash；不一致一律 Frozen。它不被单个 JSONL load
测试、SQLite index 存在或一次 `/memory repair-ledger` 成功替代。

#### NG-04 的不可拆分执行切片（R0 后的关键路径）

`ContextManifest` 不能先于它所引用的 Accepted snapshot 和 claim transition 真实存在。为避免做成一个
“带 hash 的自由 summary”，NG-04 必须按以下四个可回退切片走；每个切片都是独立 commit、独立 gate，
不得与 prompt 美化、长期记忆重构或模型 routing 混合：

| 切片 | 只实现什么 | 先读/允许触点 | 必须否证 | 退出收据 |
|---|---|---|---|---|
| `NG-04A ClaimJournal` | canonical `MemoryClaimV1` append、sequence/hash link、schema/reason code、read-only replay | `xai-grok-memory/src/task_ledger.rs` 及现有 tail-repair tests | foreign tree、duplicate sequence、unknown schema、middle corruption、content hash mismatch | `CLAIM_JOURNAL_GATE=PASS`：fixture journal → index → same end hash。 |
| `NG-04B ClaimAuthority` | actor-owned transition validator、root-only accept/reject/supersede/revoke、evidence binding | ledger review port、`VerifyAfterEditOutcome`、artifact/provenance paths | child direct Accepted、empty evidence、Advisor/daemon/UI acceptance、cancelled/stale grant、auto-merge | `CLAIM_AUTHORITY_GATE=PASS`：每条合法/非法转移都有 raw test counts。 |
| `NG-04C-1 AcceptedSnapshot` | deterministic selected accepted set、snapshot revision/end hash、scope redaction、legacy freeze | ledger read model、tree lineage/grant source | sibling Proposed、revoked/superseded claim、foreign artifact、secret/path leak、snapshot drift | `ACCEPTED_SNAPSHOT_GATE=PASS`：同 input same hash；冲突/缺链 Frozen。 |
| `NG-04C-2 ContextManifest` | 以下 manifest builder/renderer、spawn/compact/resume admission | `NG-01` coordinator lineage、NG-04A-C-1 snapshot、shell child/summary seams | forged parent/grant/budget/hash、tamper、legacy automatic re-admit、raw-chat fallback | `CONTEXT_MANIFEST_GATE=PASS`：actor + rebuilt-binary offline proof。 |

前三片没有完成前，任何 ContextManifest 代码只能停在 test fixture/RFC；不得把现有 JSONL summary、
workspace memory 或 child prompt 当作替身。`NG-04A` 的 journal 是权威、索引是可删可建；`NG-04B` 是唯一
claim 状态机 consumer；`NG-04C-1` 是唯一可注入事实的 selection point；`NG-04C-2` 只引用前三者的 hash。

### NG-04C：ContextManifest 与压缩/恢复重建

**状态：** `8b74b361` 已实现 `xai-grok-memory::ContextManifestV1`、canonical hash/admission 及其与
governed assignment/child tool result 的基础接线；这纠正了此前“尚无 ContextManifest”的旧事实。它尚未证明
所有 root/child prompt、compact、resume、reconnect、ACP/Pager 都只通过同一 immutable manifest 重建；因此
现有实现仍是 foundation，`CONTEXT_MANIFEST_GATE=PASS` 仍为 Draft，不能把已有 hash 当完整 anti-drift product
contract。

**目标：** 每个将被模型消费的 root/child turn 都从一个不可变、可 hash、可重建的 manifest 生成
受控输入；压缩、resume、retry、pager/ACP 都读同一 identity，而不是重用原始 chat、sibling scratch
或自由文本摘要。它是 Context 平面的 admission gate，不是新的 memory store。

**前置：** NG-01 的真实 lineage 和 NG-04 的 Accepted ledger snapshot。**非目标：** 保存模型思维链、
向 child 暴露 root 全量 chat、自动写长期记忆、借 manifest 放宽 capability 或自动重放 provider。

#### 先读的真实接缝

| 路径 | 当前事实 | 本 phase 的职责 |
|---|---|---|
| `xai-grok-tools/.../task/types.rs`、`coordinator.rs` | lineage/root 与直接父节点已有可信来源 | manifest 只从 coordinator-provided lineage 构建，不信 caller 字段。 |
| `xai-grok-memory/src/task_ledger.rs` | `accepted_facts()` 与 evidence-gated/root-only review 已存在 | 冻结 revision/hash 的 AcceptedSnapshot；不拷贝 Proposed/sibling scratch。 |
| `xai-grok-shell/src/agent/subagent/handle_request.rs` | 当前 child contract、memory injection 与 `CompactionMode::Summary` 接线 | 在 spawn 处消费 manifest；删除所有临时拼接绕过。 |
| `xai-grok-shell/src/session/summary.rs` 及实际 resume/compaction 调用链 | summary 是会话便利机制 | compact/resume 必须重新 render manifest，summary 只能是非权威辅助材料。 |

#### 【拟建】版本化合同

~~~rust
pub struct ContextManifestV1 {
    pub schema_version: u16,
    pub task_tree_id: TaskTreeId,
    pub node_id: TaskNodeId,
    pub root_session_id: SessionId,
    pub immediate_parent_id: Option<TaskNodeId>,
    pub lineage_path: Vec<TaskNodeId>,
    pub immutable_assignment_ref: ArtifactRef,
    pub immutable_assignment_hash: Sha256,
    pub user_objective_ref: ArtifactRef,
    pub task_contract_hash: Sha256,
    pub accepted_snapshot: AcceptedLedgerSnapshotRef, // tree/revision/end-sequence/end-hash/accepted-set-hash
    pub tool_catalog_hash: Sha256,
    pub permitted_tool_contract_hashes: Vec<Sha256>,
    pub capability_grant_id: GrantId,
    pub policy_revision: PolicyRevision,
    pub admission_profile: InteractiveSingleTurn | GovernedTreeDevelopment | KairosLocal,
    pub budget_reservation_id: ReservationId,
    pub deadline: Timestamp,
    pub permitted_artifact_refs: Vec<ArtifactRef>,
    pub model_selection_ref: Option<ModelSelectionReceipt>,
    pub parent_compaction_ref: Option<ManifestHash>,
    pub producer_version: String,
    pub created_at: Timestamp,
}
~~~

canonical serialization 的 SHA-256 是 `context_manifest_hash`。manifest **不包含** secret、raw
PermissionHandle、bypass/yolo、未接受 ledger、sibling chat、任意裸路径、模型隐含 prompt、或可执行
自由文本指令。它只携带 immutable assignment 的引用；显示或注入前仍受 capability/path redaction policy
处理。

**读取集与构造顺序是合同的一部分：** builder 只接受 coordinator 的 lineage、root-owned immutable
assignment、NG-04C 的 frozen `AcceptedSnapshot`、actor-issued grant/policy/budget/lease（如适用）和
allowlisted artifact refs；caller supplied `parent/depth/path`、summary、child memory、model text 都不是输入。
先 canonicalize 各引用和数组排序，再 compute hash，再写 artifact，最后由 SessionActor 发
`PromptAccepted`。任何 hash/sequence/owner mismatch、未知 schema、redaction failure 或写入错误都使
admission `Blocked`；不允许重新抓取聊天或“用最新 memory 补齐”。

#### 实施顺序

1. 在 task/coordinator 与 ledger 提供只读 `build_context_manifest_v1(...)` 输入；先做纯 DTO、canonical
   serializer、schema version、hash 与 foreign-tree validation，不修改 prompt。
2. 将 immutable assignment 与 Accepted snapshot 生成为 root-owned artifact；新 child 的
   `PromptAccepted` 必须先验证 manifest、grant、budget reservation 和 snapshot hash。缺任一项返回
   `Blocked(ContextUnavailable|ContextMismatch)`，不 best effort spawn。
3. 以 manifest renderer 替代 child prompt 的身份/能力/accepted-fact 临时拼接；renderer 只产生
   assignment、accepted facts、evidence schema、budget/deadline 与 blocking rules。
4. 在 compact、resume、reconnect 三条路径重读 immutable assignment + frozen snapshot 生成相同 hash；
   summary 可加入说明，但不影响授权或 acceptance。
5. 让 child proposal、tool receipt、verify outcome、terminal receipt 和 pager/ACP status 都回链
   `context_manifest_hash`；UI 只能展示 hash/phase/age，不展示隐私内容。
6. 旧 session 迁移为 `LegacyNoManifest`：只能继续人工 read/close；不得自动 spawn、自动 reroute、
   自动 promotion 或进入 Kairos。需要 root 明确 re-admit 才生成 V1 manifest。

#### 必测正例与反例

- root → Code → Review → Evidence leaf 得到不同 node/path/grant 的 manifest；leaf 不能 spawn。
- 同一 immutable assignment + 同一 Accepted revision 在 spawn/compact/resume 后 hash 相同；新 Accepted
  fact 不会悄悄改变已运行 child 的 snapshot。
- child 只能看自身 assignment 和 Accepted snapshot；foreign tree、sibling Proposed、raw root chat、
  secret/path outside scope 均不可见。
- forged parent/depth/grant/budget/assignment hash、tampered artifact、stale revision、sequence gap、
  corrupt canonical JSON、unknown schema 都 fail-closed。
- cancel 或 grant revoke 后 manifest 不可用于新的 tool dispatch；late completion 只能 reconcile，
  不能把旧 manifest 的 child 复活。

**命令骨架（测试落位后必须以真实计数替换）：**

~~~zsh
cd /Users/lei/code/lumen/agent
cargo test -p xai-grok-tools context_manifest_v1_ --lib
cargo test -p xai-grok-memory accepted_snapshot_ --lib
cargo test -p xai-grok-shell context_manifest_ --lib
cargo test -p xai-grok-shell subagent_compact_resume_manifest_ --lib
cargo check -p xai-grok-shell
~~~

`filter=0`、无 rebuilt-binary proof 或未保存 raw exit 都不能通过。

**Gate：** `CONTEXT_MANIFEST_GATE=PASS` 需 exact source、schema revision、manifest fixture hashes、
正/负/compact-resume test counts、actor/product proof（若 ACP 已接线）和 rollback SHA。

**停止条件：** 若为兼容旧 summary 而让 hash mismatch/unknown schema/foreign snapshot 继续执行，或为
“上下文更完整”而把 raw chat/secret/sibling scratch 塞回 child，则立即停止并回退 consumer。

### NG-04D：AgentSandbox 与 bounded branch handoff

**状态：** Draft。`8b74b361` 已有 lineage、grant/manifest/operation/write-scope 的局部 building blocks，
但它们尚未作为一个统一、可撤销的 per-agent sandbox 被签发和在所有 consumer 强制。当前不同 child 的
context、scratch、handoff、tool/process ingress 和 rebase 语义仍不能被宣称完整闭合。

**唯一 owner：** SessionActor。coordinator 可验证/投影；ToolRegistry、shell、worktree、workflow 和
Kairos 只能消费 sandbox ref，不能自己构造或扩大它。

| 子卡 | 交付 | 允许路径 | 必须拒绝 | Gate |
|---|---|---|---|---|
| `NG-04D-1` | `AgentSandboxV1` DTO、canonical hash、expiry/revoke reason | memory + tools DTO/tests | caller-provided parent/depth/permission/bypass | `SANDBOX_SCHEMA_GATE` |
| `NG-04D-2` | accepted-only read / own-branch propose capability | ledger + child admission | sibling/private scratch/cross-tree/cross-branch write | `SANDBOX_MEMORY_GATE` |
| `NG-04D-3` | `HandoffPacketV1` bounded artifact + delivery receipt | coordinator/ACP projection | free-form control message, raw chat, secret, proposal auto-accept | `HANDOFF_GATE` |
| `NG-04D-4` | grant/tool/process/worktree consumer enforcement | tool dispatch + write scope | expired/revoked sandbox dispatch, child bypass, unknown MCP | `SANDBOX_ENFORCEMENT_GATE` |

**迁移/回退：** legacy session 没有 sandbox 时只能 `interactive_single_turn`、人工 inspect/close；不能自动
spawn、resume governed tree、promotion、Kairos 或 advisor checkpoint。feature flag 关闭时拒绝新的 governed
admission，已签发 sandbox 以 root cancel/revoke 有界回收；journal/artifacts 留存，不通过删 scratch 隐藏证据。

**必测：** two sibling same accepted snapshot but distinct scratch；root accepts fact 后 child rebase 才能看见；
handoff foreign/stale/malformed/oversize/secret reject；revoke vs in-flight tool；cancel vs late terminal；depth-3
leaf cannot spawn/write/network/bypass；read-only sandbox cannot obtain write lease。每条测试保存 raw counts；
`SANDBOX_ISOLATION_GATE=PASS` 不等于 OS/container security certification。

### NG-04E：Governed Evidence Loop 与收敛合同

**状态：** Draft。现有 Expert repair、workflow、verification 与 operation state 各自能循环/重试，但没有一份
跨 agent/tree/supervisor 的 checkpoint/stop contract；因此不能把“有 retry”称为可靠 loop engineering。

**目标：** 限制每个 node 的迭代，防止重复无新证据的工作；把“下一步”从模型 prose 变成 actor-owned
checkpoint transition，并保证树级冲突、取消、预算和 delivery uncertainty 能中止而非隐式继续。

| 规则 | 强制语义 |
|---|---|
| progress | 一轮只能在新增 artifact/receipt、accepted snapshot rebase、或明确 parent decision 后继续。 |
| repair | 必须引用上轮 failure receipt；新 iteration 独立计数，不能保留上一轮 PASS。 |
| completion | 仅 `CompletionCandidate`；须 verify/host/root 三层后才 terminal success。 |
| escalation | scope、budget、model、write lease、snapshot stale、连续 no-progress 进入 `NeedsParentDecision`。 |
| uncertainty | queue/effect/lease/manifest/owner 任一 unknown 进入 `Frozen`，不可自动 retry。 |
| fairness | actor 按 tree/node 限制 active iterations、tool calls、artifact bytes、queue share，避免一个分支饿死同树其它分支。 |

**实现顺序：** (1) pure reducer + schema/fake-clock；(2) node checkpoint 绑定 operation/ledger receipts；
(3) root tree reducer 与 deterministic stop/escalate；(4) workflow/Expert repair adapter；(5) ACP/Pager truthful
projection；(6) offline corpus/fault injection。任何步骤发现需要 provider 或外部 effect 才能证明，停止并换为
fixture。`LOOP_CONVERGENCE_GATE=PASS` 需要 progress/no-progress、repair limit、budget/deadline、rebase,
conflict、cancel/late event、verification failure、closed channel/unknown delivery 的 positive+negative matrix。

## 13. NG-05：ProviderHealth 与 no-replay failover

**状态：** P4a（Expert 新任务的受限候选选择）已落地；`P0-NR-A` 与完整 receipt/P4b 均 Not started。源码还存在 ordinary root turn 的
`maybe_reroute_ordinary_turn_after_failure` → `RerouteAndResubmit` 路径；配置默认关闭不能替代
安全证明。该路径目前没有 actor-owned `ProviderAttemptReceiptV1` 或全 event-order fault matrix，
因此只能称为 feature-gated candidate，绝不能称为已验证的 no-replay capability，也不得扩展到 child、
workflow、scheduler、Kairos 或 release。后台自动重试仍是 NOT RUN。
**非目标：** cheapest/fastest router、默认跨 provider、绕 user pin、在已输出或有工具副作用后重放。

### P0-NR：先关闭所有无 receipt 的同轮重投，再谈自动 fallback

**这是当前计划与源码发现不一致后的最高优先级修正，也是 R0 的唯一 runtime 前置。** 当前
`run_turn_via_sampler` 的 completion oneshot 与 ACP event drainer 是异步路径；失败处理只拿到
`SamplingErrorInfo`，并不知道该 attempt 是否已经产生文字、thought、tool delta、backend tool，或 event
delivery 是否失败。因此“zero output”不是已被代码证明的条件。更重要的是，风险不只在 ordinary reroute：
compact、401 refresh 与 reroute 的 `*AndResubmit` 都属于同一次 turn 的新 provider attempt；此外 sampler
自身的 `RetryPolicy` 还可在同一 request 内做 retry/backoff/image-strip/HTTP1 rebuild/doom resample。后者
不经过 shell outcome enum，不能因为删除三个 variant 就被遗漏。两层都必须受同一个 no-replay gate 管理。

#### P0-NR-A：最小安全封口（**必须先于 R0**）

在没有 sealed receipt 的当前源码上，关闭两层的**所有**同轮 transport resubmit：

1. shell 层 `CompactAndResubmit`、`RefreshAuthAndResubmit`、`RerouteAndResubmit`；
2. sampler 层 normal/root/child/background request 的 retry/backoff/image-strip/HTTP1 rebuild/doom resample。

P0 的安全基线是每一正常 sampler submission 的 **effective** `max_retries=0`，而非仅设置 actor-level
`RetryPolicy.retry_only_before_output=true`。后者只读取 sampler 内部 AtomicBool，既不是 sealed receipt，也
会被 per-request `SamplingConfig.max_retries` 覆盖。失败仍应记录可辨识的 passive provider health；但当前
turn 必须保留原错误并终止，下一项**全新的 root task**才可在 user pool/priority 内选择健康候选。不得只关
ordinary reroute 而留下 compact/auth 或 sampler retry 旁路，也不得留一个可由 config 打开的不安全 boolean。

“全新的 root task”是一个 actor admission 边界，不是“同一 root turn 下一次进入 sampler loop”。当前 task
若在 tool loop 后再发起第二次 model call，或 provider health 在运行中变化，ordinary pool preselection 也
不得改变当前 session model；它只能发生在 root 的第一次 sampler submission 之前。否则即使删掉 failure
replay，仍会留下同一任务中途换模型的隐形语义。

这是一项刻意保守的产品降级：短暂失去自动修复/自动换模型，换取“绝不因未知已输出/已副作用而重放”。
UI 必须明确显示 `PartialOrUnknownAttempt — not automatically resubmitted`，而不是把失败伪装成模型
切换成功。用户 pin、pool、priority 和下一新任务的 preflight 选择保持现有语义；它们不是同轮 replay。

**限定写入路径：**

1. `agent/crates/codegen/xai-grok-shell/src/session/acp_session_impl/spawn.rs`、`.../sampler_turn.rs`：将
   normal sampler submission 的有效 `SamplingConfig.max_retries` 和 actor policy 同时收口为 `0`；不能只改
   `RetryPolicy`，因为 per-turn `reconstruct_full_config` 可重新覆盖它。先读
   `xai-grok-sampler/src/config.rs`、`actor/request_task.rs` 与 `retry.rs`，证明 5xx/413/doom 都在第一失败
   时 terminal，不留下 L2 retry 的 hidden consumer。
2. `.../acp_session_impl/sampler_turn.rs`：failure handler 在 record passive health 后统一返回 terminal
   error；删除/不可达化三种 shell recovery consumer，不只改注释。error-triggered compaction 也必须 terminal；
   正常请求前 compaction 不受此 P0 禁止。
3. `.../acp_session_impl/turn.rs` 与 `.../sampler_turn.rs`：将
   `maybe_select_ordinary_model_for_task` 移到 root admission，或显式传入 immutable
   `is_initial_root_sampling_attempt`；只有第一轮 root sampler call 可 preselect。tool loop 的第二次 call、
   partial failure、child、pin/empty pool 都不可变更 current model。
4. `.../acp_session_impl/types.rs` 与 `.../turn.rs`：删除或改为不可构造的 `*AndResubmit` outcome、outer
   `continue` 与只服务 refresh-resubmit 的 `AuthRetrySchedule`，确保编译期不存在另一个重提交流程。
5. `.../session/acp_session_tests/provider_failure_routing_tests.rs`：把“failure 后已 reroute”的正例改为
   “enabled pool 也不切换、不重投、原错误可见”；保留独立的**新任务 preflight** pool/priority 正例。
   `.../session/acp_session_tests/auth_error_no_retry_tests.rs` 中现有期望
   `RefreshAuthAndResubmit` 的 cases 也必须改为 terminal/no-second-submit；新增 compact trigger 负例，不能因
   现有 test 覆盖薄弱而漏掉第三条 consumer。
6. `.../session/acp_session_tests/inline_auto_compact_flow_tests.rs`：新增 error-triggered context overflow
   terminal 负例；不能仅测试“应当 compact”的 predicate。保留请求前 compact 的正例。
7. 搜索全部 `AndResubmit`、`submit_and_collect`、`handle_sampling_failure`、`max_retries` 与 sampler retry
   decision 调用点；任何遗漏都使 P0 失败。

**P0-NR-A 反例和 exit：** 402、401、context overflow、503/5xx、413、doom trigger；已 pin、空 pool、root
和 child；均不能形成第二次 transport submit。单元测试必须同时断言 current model、effective retry=0、
terminal error 和没有 `from/to` fallback receipt。至少一个 local HTTP counting-server + real SamplerActor
fixture 必须证明外层从 `turn.rs` 到 transport 的 attempt count 为 1；仅直接调用 `handle_sampling_failure`
不足以证明 outer loop 没有 `continue`。另有一个同 root turn 的 two-model-call/tool-loop fixture：第二次
sampler call 即使 health 变化也不改变 model；相同变更若发生在**下一次 root admission 前**才允许进入
preflight selection。
`rg` 只作为 inventory，不能代替编译/测试。`P0_NR_SAFETY_GATE=PASS` 需 exact source SHA、diff hash、
targeted raw test counts、`cargo check -p xai-grok-shell` raw exit、`git diff --check` raw exit、rollback SHA。
它只证明 active unsafe consumer 被关闭，**不**证明 ProviderAttemptReceipt、crash no-replay 或完整 provider
failover 已实现。

实施完成后最小命令（禁止 pipeline，必须保存每条原始 exit 和 passed/failed/ignored/filtered）为：

~~~zsh
cd /Users/lei/code/lumen/agent
env CARGO_INCREMENTAL=0 CARGO_BUILD_JOBS=1 cargo test -p xai-grok-shell --lib provider_failure_routing_tests -- --nocapture
env CARGO_INCREMENTAL=0 CARGO_BUILD_JOBS=1 cargo test -p xai-grok-shell --lib auth_error_no_retry_tests -- --nocapture
env CARGO_INCREMENTAL=0 CARGO_BUILD_JOBS=1 cargo test -p xai-grok-shell --lib inline_auto_compact_flow_tests -- --nocapture
env CARGO_INCREMENTAL=0 CARGO_BUILD_JOBS=1 cargo test -p xai-grok-sampler --test test_actor no_replay_policy_ -- --nocapture
env CARGO_INCREMENTAL=0 CARGO_BUILD_JOBS=1 cargo check -p xai-grok-shell
cd /Users/lei/code/lumen
git diff --check
~~~

`test_actor` 必须新增 `no_replay_policy_503_is_single_submit`、
`no_replay_policy_413_is_single_submit` 和 doom/resample 等价负例（可共享同一 prefix）；若
counting-server/normal-spawn 或 same-root-turn fixture 位于其他 test module，须把它的 exact argv 追加进
receipt。任何 `0 tests matched` 或只保存 `grep` 后 exit 都是 `P0_NR_SAFETY_GATE=FAIL`。

#### P0-NR-B：receipt 设计和再开放条件（R0 后进入 NG-05）

不得以注释、控制流推断或“理论上发生在 zero output”替代 observation。后续只有完整实现本节 13.2 的
actor-owned observation、sealed receipt 与 fault matrix 后，才能逐一重新开放某一个 resubmit reason；每个
reason 都是独立 consumer 与独立 gate。发现已发任何 token、thought、tool delta、backend tool call、dispatch、
shell/network/write effect，或事件顺序/通道未知时，一律视为 partial failure：保留原错误和 receipt，不换
模型、不重新提交。

重新开放不是把 P0 的 `max_retries=0` 改回一个用户/环境可覆写的默认值。每一种恢复理由必须有自己的
compile-visible consumer、receipt predicate、MockServer attempt-count 反例和 rollout flag；先在 disabled
consumer 下验证，再只开放该 reason。普通 root admission 的 preselection 仍只发生一次，不能因 receipt
机制完善而允许 tool loop 或已开始 turn 中途切模型。

`NO_REPLAY_GATE=PASS` 的最低收据为：exact SHA、config enablement proof、attempt/receipt schema、
first-token/thought/tool/effect/unknown/closed-channel/duplicate-failure 的 mock fault counts、UI/provenance
显示的 actual from/to/reason，以及 rollback SHA。没有它，所有 normal-turn routing 状态写
`BLOCKED_CONTRACT`。

### 13.1 已落地的 P4a：用户模型池与额度耗尽处理

`22917e37`、`a2c5c642`、`6bef1d01` 已将第一片安全能力接入 **Expert 新任务开始前**：

~~~text
/expert pool=deepseek-v4-flash,grok-4.5,deepseek-v4-pro
/expert priority=deepseek-v4-flash,grok-4.5,deepseek-v4-pro
/expert priority=auto
/expert pool=off
~~~

这不是全局默认切换器，也不是一次失败后重放当前对话。它的固定语义如下：

| 情形 | 必须发生的行为 |
|---|---|
| 用户给定 priority | 只在 pool 内按 priority 顺序选择健康、可路由候选。 |
| 用户给定 pool、priority=auto | 仅在 pool 内按任务类别选择；实现类优先 Flash，review/research 类优先 Grok，剩余候选按 pool 顺序。 |
| 标准 `/model` 是更新、更明确的用户 pin | 它优先；pool 不得绕过。 |
| 本 session 新设 `/expert pool=` | 它是较新的、明确的 Expert 选择，可取代旧的单模型 Expert 选择，但只影响后续 Expert 任务。 |
| provider 返回可辨识的余额/额度耗尽 | 标记 endpoint 为 `quota_exhausted`，在**下一项** Expert 任务选择时跳过；当前请求绝不重放。 |
| pool 内所有候选不可用 | 请求开始前 `ModelMissing`；绝不退回 pool 外默认模型或悄悄花费未选模型。 |
| 401/403/普通 400 | 不是“没额度”；不作为 pool 自动跨模型理由。 |

每次选择持久化 `AdvisorPoolRoutingEvidence { from_model, to_model, source, skipped, had_output=false }`；`/expert status` 必须显示 pool、priority、来源和最新选择。现有离线测试覆盖 priority、任务策略、quota skip、用户 pin、显式 session pool、pool 全不可路由；这不替代真实 provider/额度证明。

**用户推荐起始设置：**

~~~text
/expert pool=deepseek-v4-flash,grok-4.5,deepseek-v4-pro
/expert priority=auto
~~~

这把 Flash 放在执行默认位、Grok 放在 review/research 默认位、Pro 留作池内可选深度候选；用户可随时重排 priority。Lumen 不根据“谁更聪明”的主观断言越过你的 pool。

#### Lumen 2 最终的用户可控模型策略

此处定义产品语义，现有 Expert P4a 只实现其中的受限新任务子集。优先级从高到低固定为：

1. 一次明确的 `/model` 或 UI pin；
2. 当前 session 的 explicit pool/priority；
3. 用户持久化 profile 的 pool/priority；
4. `priority=auto` 的任务策略；
5. profile 内原始顺序；绝不使用 pool 外的静默默认。

`priority=list` 是严格用户顺序；`priority=auto` 也只能在用户 allowlist 内推荐：机械实现、快速
迭代默认偏向 `deepseek-v4-flash`，独立 code review/research 默认偏向 `grok-4.5`，高复杂度设计、
跨模块推理或用户明确要求的深度分析才可偏向 `deepseek-v4-pro`。任务分类置信度不足时按用户原始
顺序，不假装知道“最聪明”。root pin、BYOK、endpoint/privacy policy、context/tool compatibility、
failure-domain independence、budget 与 capability 永远先于任务偏好。

额度或可辨识 quota exhausted 只影响**下一项尚未开始的新任务**的候选选择；本次 attempt 绝不重放。
401/403/普通 400、usage 缺失、未知 event state 都不能伪装成 quota。pool 全部不可用时 fail-closed
并显示可操作原因；不会换到未选择模型、不会把未知 usage 记为零成本。

任何 child、workflow 或 Kairos 的模型分配，在 NG-07 前只可继承 root 明确 pin/assignment，不能自行
调用 `auto`；NG-07 后仍必须满足 no-output/no-effect、root approval、receipt 和 budget reservation。

### 13.2 P4b：普通 turn 与后台任务的唯一允许路线

普通任务和 Kairos/background task 目前**不能**因一次 provider 失败自动改模型重试。先完成下列 contract 才能实施：

~~~text
ProviderAttemptReceiptV1 {
  task_tree_id, node_id, turn_id, attempt_id,
  model_id, provider_endpoint_fingerprint,
  raw_output_state = NoOutput | OutputStarted | Unknown,
  tool_signal_state = NoToolCall | ToolCallStarted | Unknown,
  outbound_delivery_state = NotAttempted | Enqueued | DeliveryUncertain,
  external_effect_state = None | Possible | Confirmed,
  failure_kind, usage_state, sealed = bool, seal_reason,
  observer_version, started_at, finished_at
}
~~~

唯一可切换条件是 `sealed + NoOutput + NoToolCall + NotAttempted + None`，且是 allowlisted 新候选、无
user pin、预算 reservation 未消费、同一 attempt id 只一次。任一 `Unknown`、`DeliveryUncertain`、已发
文字/thought/tool signal、backend tool、shell/network/write effect、usage 不可判断，都按 partial failure：
展示原错误，保留 receipt，等待 root/user，不重放。这里的 delivery 语义是“actor 是否已尝试 enqueue”而
不是假装证明远端 UI 一定渲染；无法证明就禁止 replay。

**实施接缝（不是抽象愿望）：**

1. `agent/crates/codegen/xai-grok-sampler/src/handle.rs`、`actor/request_task.rs`、`events.rs`：提交前创建
   attempt observation，传到 `SamplerCommand`/request task；在**发送 event 前**记录 raw text/reasoning、
   tool delta、backend tool start/complete、nonempty terminal response；任一 event send 失败记
   `DeliveryUncertain`。每一 exit path（success/fatal/cancel/init/stream dropped）都 seal。内部已有
   `output_observed: AtomicBool` 只能是实现线索，不能继续只留在 sampler 内部。
2. `agent/crates/codegen/xai-grok-shell/src/session/acp_session.rs` 与 `.../acp_session_impl/sampler_turn.rs`：
   `run_turn_via_sampler` 从 sampler completion 获得 result **和 sealed receipt**；SessionActor 验证
   attempt/node identity、seal、policy/budget，再由 `handle_sampling_failure` 决定。不能靠独立 ACP event
   drainer 回填一个 bool，因为 completion 可先于已排队 output event。
3. `.../acp_session_impl/tool_calls.rs`、`turn.rs`、`tool_dispatch.rs`：模型 tool call 被接受、任何 local
   dispatch 开始、shell/network/write effect 可能发生时，绑定当前 attempt 的 effect state 升级为
   `Possible` 或 `Confirmed`。这是 provider-attempt 边界：前一轮已完成工具与新一轮 preflight 失败不能混淆。
4. `.../acp_session_tests/provider_failure_routing_tests.rs`、`replay_buffer_send_update_tests.rs`、sampler
   event tests：分别证伪 pristine preflight、首 token、thought-only、tool delta、backend tool、dispatch
   race、event channel closed、stream 无 terminal、重复 failure、compact/auth/reroute 和 retry 后 failure。

P4b 必须作为独立 commit；它不得混入模型 UI 重构、source-lock evidence 或不相关 `turn.rs` 改动。先交付
receipt DTO + mock/fault matrix，再以 disabled consumer 验证，再每次只重新开放一个 resubmit reason。feature
flag 只在 receipt gate 已通过时控制消费范围，不能成为 P0-NR-A 的不安全逃生门。

| 来源 | 必读事实 |
|---|---|
| docs/provider-failover-design.md:1-19 | 设计未实施；模型 preset 不等于 live 验证。 |
| 同文件 21-39 | CircuitBreaker/Registry/MockClock 可复用。 |
| 同文件 43-88 | breaker domain、sampler 接入、no replay、visible fallback。 |
| xai-grok-sampler/src/client.rs | 拟接入 inference transport 的目标。 |

固定合同：

~~~text
breaker key = provider + base_url
failure = connection | timeout | 5xx | recognizable quota exhausted
not automatic-route failure = 400 | 401 | 403
fallback = only before any output block
emitted block = partial failure; never replay
visibility = actual from/to/reason/state in log/UI/turn-tail
accounting = actual executor; missing usage stays unavailable
~~~

实现：sampler request 前 check、response 后 record；explicit fallback chain；每次产出 ProviderAttemptReceipt。Advisor 不得直接 retry provider。

离线矩阵：pristine preflight 402/503、first-token 后 disconnect、thought-only failure、tool delta、backend tool
start/complete、event receiver closed、stream missing terminal、compact/auth/reroute 的三类 consumer、同 turn
duplicate failure、成功 response 后**下一新 attempt** preflight failure、N×503 open/half-open/close、base URL
isolation、401/403/400、quota exhausted、catalog alias swap、missing usage、pool-all-unroutable、unknown
output/tool/effect state。
Exit：mock transport 覆盖每一状态，无真实 provider；UI/provenance 与 actual executor 一致。
Stop：无法确定是否 emitted block 或 event delivery 时按已输出处理，禁止 fallback。进程 crash/restart 的
no-replay 仍依赖 NG-03C durable operation/attempt journal；在它前面不得把 P4b 宣称为 crash-safe。

## 14. NG-06：AdvisorPolicy shadow-only

**状态：** 已实现本地 deterministic shadow advice、pin/budget/health/failure-domain 拒绝和持久化 audit；它不切换普通 turn、不能批准 assignment。`ModelSelectionAdviceV1` 的 root-approved 跨 session 分配仍为 Draft。
**目标：** 模型建议可审计、零执行影响；P4a 的实际选择不得被包装成 Advisor 有 acceptance 权。

| 路径 | 事实 |
|---|---|
| shell session/expert.rs:46-57,349-447 | Dual 单 writer 和 durable state。 |
| expert.rs:1743-1788、expert_consultant_tools.rs | consultant read-only。 |
| acp_session_impl/expert.rs:2015-2085 | dual 真实双 source 测试模式。 |
| shell agent/subagent/mod.rs:573-679 | 当前 child model resolution 顺序。 |

【拟建】ModelSelectionAdviceV1 含 task tree、target node、task class、catalog/policy/health snapshot hash、candidate/rejection list、recommended assignment、independence、budget impact、reason codes、Shadow/Recommend/Approved/Rejected/Applied。

固定过滤：user pin/harness → allowlist/BYOK/endpoint/privacy/tool → health/context/usage budget → task class → failure-domain independence → quality/latency/cost。用户 model pool 是 allowlist，不是软偏好。

Shadow 不 switch、不 spawn、不调工具、不写文件、不改 terminal、不写 Accepted。
Exit：离线 replay corpus policy violation=0、stream switch=0、pin override=0、audit missing=0。
Rollback：关闭 consumer，不删除 advice log。

### NG-06A：ClientAdvisor virtual tool（功能复现切片）

**状态：** Draft。它在产品体验上复现“用户可选 Advisor 模型、主 Agent 可在关键点咨询、系统显示咨询过程与
用量、建议回到当前任务”的能力；它**不**复用或模拟任何供应商 server-side tool type、beta header、内部
feature flag 或模型白名单。现有 Expert consultant 与 AdvisorPolicy 都是可复用前置，不自动等于此功能。

**最小公开 UX：**

~~~text
/advisor                         # 查看模式、候选池、预算、最近 receipt
/advisor off                     # 注销本地虚拟工具；不影响现有 Expert
/advisor shadow                  # 不调用 provider，只记录应触发的咨询
/advisor on-demand <model?>      # 仅允许用户池内模型；模型可省略
/advisor checkpoint <model?>     # 受 rollout gate 的固定检查点咨询
~~~

命令是 SessionActor command，不直接改 provider config；它要校验 model catalog、用户 pool/pin、endpoint/
privacy class、预算、failure domain、schema 兼容和当前 task profile。`checkpoint` 不是“每个 tool call 都请
顾问”，只在 Plan、连续失败、scope/budget escalation、CompletionCandidate 触发，并由每 tree 的次数/输入输出
token/等待时间上限约束。

**本地 virtual-tool contract：**

~~~rust
pub struct AdvisorContextCapsuleV1 {
    pub request_kind: AdvisorRequestKind,
    pub task_tree_id: TaskTreeId,
    pub node_id: TaskNodeId,
    pub context_manifest_hash: ManifestHash,
    pub accepted_snapshot_hash: Sha256,
    pub allowed_artifact_refs: Vec<ArtifactRef>,
    pub redaction_policy_hash: Sha256,
    pub input_hash: Sha256,
}

pub struct AdviceReportV1 {
    pub request_id: RequestId,
    pub findings: Vec<AdviceFinding>,
    pub counterevidence_refs: Vec<ArtifactRef>,
    pub missing_evidence: Vec<EvidenceRequirement>,
    pub suggested_verification: Vec<VerificationSuggestion>,
    pub recommendation: Continue | NeedEvidence | NeedParentDecision | Freeze,
    pub confidence: Confidence,
    pub report_hash: Sha256,
}

pub struct AdvisorUsageReceiptV1 {
    pub request_id: RequestId,
    pub mode: AdvisorMode,
    pub model_assignment_ref: ModelSelectionReceipt,
    pub capsule_hash: Sha256,
    pub report_hash: Option<Sha256>,
    pub usage: Known(ProviderUsage) | Unknown | Denied | Cancelled | TimedOut,
    pub delivery_state: DeliveryState,
}
~~~

Actor 从 manifest/Accepted snapshot/allowlisted evidence 构造 capsule；primary model 只能选择 request kind，
不能传完整自由 prompt。Consultation 的最大权限是 read-only provider call。report 作为有界、redacted tool result
返回，不能被当作 claim evidence 或 tool directive；root 采纳与否也必须形成独立 decision receipt。

**缓存、流式、取消：** tool descriptor 必须进入 `ToolCatalogSnapshot` 的 versioned feature segment；开关/模型/
schema/redaction 改变会改变 catalog + manifest hash。可以优化稳定前缀，但不为任何 provider 承诺 cache hit。
咨询 lifecycle 用独立控制 lane 支持 cancel/timeout；UI 只显示 actor receipt 状态，normal output 受 bounded
delivery policy。channel closed、report schema invalid、usage unavailable、provider denied 都保留真因，不能变成
“Advisor 已完成”或偷偷切换主任务模型。

**实施与 gates：**

1. Pure capsule builder/redaction + golden hash fixtures；先测试 raw chat/sibling Proposed/secret/path/foreign tree
   绝不进入 capsule。
2. Advice DTO/receipt、`off/shadow` command/status、ToolCatalog/manifest feature hash；证明两模式零 provider
   attempt。
3. mock-only virtual tool adapter，覆盖 invalid report、timeout、cancel、closed receiver、unknown usage、budget
   exhausted、same failure domain、pin/privacy deny。
4. gated `on-demand`；只以 test transport 验证 request/report/receipt，不能作 billable call。
5. `checkpoint` shadow corpus 和 rate-limit/fairness tests；通过后仍先保持 feature disabled until human rollout。

`CLIENT_ADVISOR_GATE=PASS` 的反例必须包括 Advisor direct claim acceptance、tool dispatch、bypass、spawn、
mid-stream switch、report-as-success、capsule secret leak、off/shadow provider attempt、unknown usage=zero 和
cancel delivery loss。它不替代 `NO_REPLAY_GATE`、provider live proof 或模型质量评估。

## 15. NG-07：recommend 与 bounded assignment

**状态：** Not started；前置 NG-01 至 NG-06 与 `NG-09A`。
仅当 new child/new turn、no output、no user pin、allowlisted/compatible、health 可用、budget reserve 成功、privacy 允许、capability 不变、root approval record 完整时可 Applied。

禁止：SetDefaultModel、替换 stream、静默 cross-provider、无限 spend、Advisor PASS→completion、advice text→tool call。
反例：user pin、private endpoint、budget exhausted、breaker open、schema mismatch、existing output、stale advice。
Exit：每个 Applied advice 有 root approval、actual model receipt、budget reservation、ledger decision。
Rollback：关闭 auto apply，历史 advice 不改写。

## 16. NG-08：KairosSupervisor local proof

**状态：** scheduler 层已实现 lease heartbeat、foreign lease proof、terminal receipt、backoff/dead-letter；统一 KairosSupervisor 状态机、operator freeze surface、exact-binary 24h/local proof 仍为 Draft。Advisor 不是前置。
**目标：** SessionActor 之下的长期运行治理，不是新 Agent 或 shell daemon。

| 层 | 当前真实状态 | 不能宣称 |
|---|---|---|
| SchedulerRunLease | task-scoped lease/heartbeat/takeover/terminal receipt 已有局部源码基础 | general process/workflow/tree recovery。 |
| KairosSupervisor | Draft；只能建立在 NG-03C operation API 上 | 已是第二执行 actor 或无人值守 daemon。 |
| 24h autonomous mode | NOT RUN | 一次 scheduler 或短测通过即可 24h。 |

| 路径 | 事实 |
|---|---|
| tools scheduler/occurrence_journal.rs:1-4 | one-shot journal durability pattern。 |
| tools scheduler/actor.rs:99-136 | persistence barrier。 |
| shell leader/lock.rs:125-147,187-294 | OS lock/PID/socket lifecycle。 |
| tools workflow/mod.rs:176 | workflow reboot 后 terminal，不能称可恢复。 |

【拟建】durable records：AutomationPlan, WakeRequest, JobLease, AttemptRecord, Heartbeat, OutboxEvent, ReconciliationRecord, DeadLetterReason, OperatorPause。

~~~text
Draft → AwaitingScheduleApproval → Scheduled → Leased → Starting
      → AwaitingActorApproval → Running → Checkpointing
      → Succeeded | Failed | RetryScheduled | DeadLetter
      → Cancelled | Frozen | TakenOver | RecoveryRequired
~~~

接口只做 claim_run/heartbeat/complete/fail/freeze/take_over。所有 tool/model/process 仍经 root actor。

#### OperatorControlPlane v1：人能接管，但 UI 不能越权

Kairos 的实际可用性不取决于“会不会自己跑”，而取决于失败时人能否看见、冻结、取消和带证据地继续。
现有 scheduler list/takeover receipt、SessionActivitySnapshot、Pager/ACP status 是只读基础；新控制面不另起
daemon，也不把 button/CLI 文本当状态转换。

| actor command | 允许的状态/作用 | 必须留下的 receipt | 禁止语义 |
|---|---|---|---|
| `InspectOperation` | 任意；返回 owner、lease epoch、manifest/evidence/budget hashes、queue pressure、next safe action | read audit | 返回 raw secret、模型思维链、未接受 sibling scratch。 |
| `FreezeOperation` | Starting/Running/RecoveryRequired；阻止新 tool/model/process dispatch，等待有界 drain | operator id、reason、observed attempt/effect states | 把未知 effect 标 success，或静默丢掉 in-flight state。 |
| `CancelOperation` | root/ancestor scope；撤销 grants/leases、幂等释放 reservation、请求 adapter stop | cancellation causal event、late-event policy | 只杀 PID、遗漏 descendants、自动删除证据。 |
| `ApproveResume` | Frozen/Blocked；仅批准明确 node/attempt/operation 与 deadline/scope | immutable approval id、new lease/manifest revision | 从旧 UI text、旧 model advice 或 expired approval 恢复。 |
| `TakeOver` | expired/foreign lease 并完成 reconcile；只更换 holder epoch | former holder、reconcile result、new epoch | 同时双 owner、绕过 missing receipt、自动 replay ExternalEffect。 |

**验收：** UI/ACP 指令只发送 typed actor command；operator race、expired approval、freeze during tool signal、
cancel after terminal, takeover with stale owner、secret-free inspect、restart after freeze 都有 fixture。`OPERATOR_CONTROL_GATE=PASS`
是 `KAIROS_LOCAL_GATE` 的前置，不等于生产值班/24h soak。

| crash state | 唯一动作 |
|---|---|
| pure read/deterministic 未执行 | source/policy/budget 有效时新 lease retry。 |
| 幂等 receipt job | 验 receipt 后 resume 或 skip。 |
| emitted model block | partial，禁止 replay，等 root/user。 |
| commit/send/install/network effect | Frozen，重新批准。 |
| promotion/completion | rehash ledger/evidence/verify 后决定。 |

验证：fake clock、lease race、two supervisor、dispatch crash、duplicate outbox、expired approval、root cancel、no replay、exact binary start/ready/crash/reconcile/stop。
Exit：`KAIROS_LOCAL_GATE=PASS`，即 local no-side-effect fixture 完成矩阵、operator freeze/unfreeze、
start/ready/crash/reconcile/stop exact-binary proof、raw exits 和 rollback receipt 齐全；仍不称 24h autonomous。
Stop：外部副作用无 idempotency receipt 时永远 Frozen。

## 17. NG-09A：三层 shadow-only offline golden path

**状态：** Not started；前置 NG-01、NG-02、NG-02A、NG-03、NG-03C/03D/03E、NG-04、NG-04C、NG-04D、NG-04E、NG-05、NG-06、NG-06A；**刻意不等待
NG-07**。先证明没有自动模型分配时的树、权限、记忆、上下文、沙箱、loop、预算与 evidence 边界，再让系统获得
任何 Applied assignment 权力。
**目标：** 用零 provider、零外部副作用的 rebuilt binary 证明 shadow-only 边界一起工作。

~~~text
root creates immutable contract and fixture workspace
  → root approves depth-1 Code
  → Code creates depth-2 Research/Review/Test
  → optional depth-3 leaf returns typed Proposal
  → branches append Proposed with fixture evidence
  → root deterministically verifies/conflict-resolves and publishes the next AcceptedSnapshot
  → child rebase sees only that snapshot; private scratch remains isolated
  → Advisor shadow or mock virtual-tool report is recorded but cannot accept/switch
  → loop checkpoint reaches completion candidate only after typed verification
  → root cancels one branch, preserves siblings, replays ledger
  → exact binary renders truthful tree/status/terminal result
~~~

必须证明：lineage/fanout；grant TTL/unknown MCP denial；sandbox scratch/handoff isolation；reserve/cancel/late completion；
proposal-only/evidence requirement；conflict no-auto-merge；Advisor non-authority；ContextManifest artifact tamper、
stale accepted snapshot、owner/session/workspace mismatch；crash read-model rebuild；UI/ACP 与 coordinator/ledger 一致。

非目标：真实 provider、Applied advice、ordinary reroute、auto commit、跨机 restore、daemon soak。
Exit：newly rebuilt exact-source binary 真跨 ACP/TUI seam；Advice 只能 `Shadow`，所有 model assignment
均由 fixture/root pin 固定。
Stop：需要 live key/network/bypass、未验证的 reroute 或 Applied advice 才能证明，说明 fixture/边界错误。

#### NG-09A-1：Harness regression corpus 与验证债上限

golden path 不能只是一段“演示刚好通过”的脚本。它必须拆成版本化 scenario manifest，每条 scenario 都有
input contract hash、fixture workspace/hash、expected state/event transitions、allowed/forbidden effects、
expected UI projection、negative mutation、exact binary hash、raw exits 和 artifact retention deadline。断言应比较
typed state/hash/reason code，而不是容易被文案变化打碎的整段模型文本。

最小 corpus 分为：

1. **Authority corpus**：forged root/parent/depth、child bypass、unknown MCP、expired grant、write-scope
   overlap、operator UI 直接 state write。
2. **Context/claim corpus**：raw chat/sibling Proposed/secret 注入、manifest/snapshot/tool catalog tamper、
   evidence missing、conflict、revoked/legacy claim、compact/resume hash drift。
3. **Execution/liveness corpus**：budget race、lease race、closed channel/high watermark、late terminal、cancel/
   freeze/takeover、crash at journal/outbox/receipt boundary。
4. **Provider/model corpus**：user pin、pool exhaustion、quota classification、first token/thought/tool/unknown
   observation、advice shadow-only、assignment deny。
5. **UX/provenance corpus**：actor truth vs pager/ACP projection、redaction/retention, no false PASS/delivery,
   failure/Blocked/Frozen reason visible。

每次 policy/schema/adapter 变更必须先运行覆盖到的 corpus，新增功能必须新增一个 negative mutation，不能只
补 happy path。维护 `verification_debt` read model：任何 `Blocked`、`Frozen`、unverified patch、未消费的
Advisor advice、failed/NOT RUN gate 都是待处理项，不允许因为新的 loop 再跑一次就消失。性能预算也必须记录
short interactive admission、tree admission、queue pressure 和 artifact growth 的基准/回归阈值；超阈值可以
阻止 feature promotion，但不能伪造成 correctness PASS。

**Gate：** `HARNESS_REGRESSION_GATE=PASS` 需 scenario coverage manifest、mutation coverage、exact binary
run、no-provider proof、known debt count 与 rollback source。它是 NG-09A exit 的组成部分，不取代 R0/CI/live/
soak gate。

### NG-09B：bounded-assignment golden-path extension

**状态：** Not started；前置 `NG-09A` 与 `NG-07`。本 phase 不重测整套 Harness，而是唯一一次有根批准的
新 child/new turn assignment 扩展；依然零 provider、零外部副作用。

必须同时证明每一条 `Applied` model advice 都有：root approval record、allowlist/privacy/compatibility
检查、sealed `ProviderAttemptReceipt`（NoOutput/NoToolCall/None）、`TreeBudget` reservation、
`ContextManifest` hash、实际执行 model receipt 和 ledger decision。一个字段缺失即不 Applied。

反例：user pin、private endpoint、breaker open、quota/pool exhausted、budget exhausted、schema mismatch、
stale advice、already emitted output、thought/tool delta、backend tool、dispatch/effect、unknown observation。
每一例都必须保持原模型/原错误或 Blocked，不能重放。

Exit：exact binary 显示 advice → root approval → receipt → actual assignment 的完整因果链；不同 failure
domain、usage unavailable 和 cancel/late event 不改变 no-replay 语义。
Stop：任何 advice 能绕过 user pin/root approval，或尝试替换正在输出的 stream，立即关闭 consumer。

## 18. NG-10：Delivery provenance、升级与 release transaction

**状态：** 现有 release 基础并非空白：`scripts/source-lock.sh` 拒绝 source/evidence 生成前的脏树，
`scripts/install-local.sh` 会验证 clean source/binary stamp 并原子安装副本，
`scripts/verify-readiness.sh` 会绑定 binary tuple，`.github/workflows/release.yml` 有 tag/source、四平台
asset、SPDX、manifest 与 Minisign 交叉校验。它们尚未在当前 candidate 上重新证明。更关键的是，当前
`scripts/release.sh` 先改 `VERSION`/`CHANGELOG.md`，再调用拒绝脏树的 `source-lock.sh`；该调用顺序会让
正式 helper 在自己制造的脏树上失败。故本 phase 不是“加更多 release gate”，而是先修复可验证的
**source-candidate → evidence-suffix → tag/release** 事务。当前两个 updater 也尚不可信：shell/PowerShell
期望的压缩包/校验格式与官方 release contract 不一致，二进制内显式 `lumen update` 仍继承上游 Grok 的
host/name/installer trust chain；它们必须隔离或替换，不能随 Lumen 2 RC 一起误导用户。

**前置：** R0_SOURCE_GATE、NG-09A、NG-09B、NG-08，及所有实际进入 release binary 的 contract gates。
**非目标：** 此 phase 不授权当前 tag、push、publish、provider live、macOS notarization 或把一次 dry-run
称为可安装 release。

### 必读锚点

| 路径 | 已有可复用事实 / 本 phase 不得破坏的约束 |
|---|---|
| `scripts/release.sh:66-150` | 已要求 clean branch/remote head、release prep、tag/push；当前 version bump 后 source-lock 的顺序必须重构并加回归测试。 |
| `scripts/source-lock.sh:21-30,107` | 生成 lock 前硬拒 staged/unstaged/untracked source；这是正确边界，不能为方便 release 放宽。 |
| `scripts/install-local.sh:13-119` | 拒绝脏 source，允许只有 lock/SBOM/readiness 的 evidence suffix，验证 binary source stamp 后原子安装。 |
| `scripts/verify-readiness.sh:50-124,187-216` | binary tuple、soak/source binding、secret scan、SBOM/readiness 需与 exact binary 绑定。 |
| `scripts/release_contract.py`、`.github/workflows/release.yml:25-170,262-743` | tag/source preflight、四资产/SPDX/manifest/Minisign、protected signing 和 remote reconciliation 是 existing contract，不是可省略的文案。 |
| `scripts/lumen-update.sh`、`scripts/lumen-update.ps1`、`xai-grok-update/src/{version,auto_update}.rs` | 现有 updater 与 Lumen release contract/identity 不匹配；RC 前只能 fail-closed 或迁到签名 Lumen tuple。 |

### NG-10A：ReleaseSourceTupleV1（先修事务，不先发布）

~~~text
source_commit A          # 真正编译的 clean product source
evidence_commit B        # A 的 allowlisted lock/SBOM/readiness/receipt 后继
release_tag              # 指向 A，绝不偷指 B
version / source_lock_sha256 / release_contract_revision
binary_sha256[target] / sbom_sha256[target]
exact_ci_sha + workflow_url / approval_receipt_hash
~~~

`A` 和 `B` 都是可审计 Git commits，但角色不同：binary、tag 和 release source 永远绑定 `A`；`B` 只保存
能证明 A 的 evidence。tag workflow 必须显式获取/验证 B 是 A 的 allowlisted direct evidence suffix，而不能
checkout tag A 后假装 B 不存在，也不能 tag B 后让 workflow 把 evidence commit 当 build source。

### 二阶段 release transaction（唯一允许顺序）

1. **A — clean source candidate：** 在独立、可 review 的 source commit 中完成 version/changelog/runtime
   变更；只用 explicit paths commit。此时 `git status --short` 必须为空，build 的 binary stamp 固定为 `A`。
2. **B — evidence suffix：** 从 clean `A` 构建/测试，生成 `SOURCE_LOCK`、SBOM、readiness、binary/hash、
   contract-gate receipts；这些文件只能形成 allowlisted evidence-only successor commit `B`，每个 receipt
   明确引用 `A` 和 binary SHA。`B` 不得夹带 runtime、version、Cargo lock 或 source policy 改动。
3. **V — independent validation：** verifier 从 `B` 验证 `B → A` 的 suffix boundary、binary stamp、source
   lock、SBOM、readiness、all required NextGen receipts；任何无法解释的路径、不同 commit、dirty build 或
   `BLOCKED/NOT RUN` 都停止。未知 publish outcome 先 remote reconcile，不重发发布动作。
4. **T — human-authorized tag/release/install：** 只有 V=PASS、exact GitHub CI 和明确 human release authority
   后，才原子 push `B:main` 与指向 `A` 的 signed tag；GitHub workflow 再验证 tag peel、A/B tuple、asset
   hashes、signature、remote immutable release。干净机安装/upgrade/rollback 是单独 product receipt，不能被
   workflow 替代。

### NG-10B 至 NG-10E：落实切片

1. **NG-10B — transaction helper + CI rehearsal：** 为 `release.sh` 提取 pure plan/preflight，令
   version/changelog source candidate 在 `source-lock` 前成为 clean committed `A`；不要偷偷 `git add -A`、
   暂存已有用户改动或为它放松 dirty check。将 source-lock/SBOM/readiness 生成到 A 的 B phase，并让 helper
   只允许列白名单的 suffix paths。给 helper 加 isolated temporary-repository tests：脏 source、version bump、
   source-lock、evidence suffix、unexpected suffix path、binary stamp mismatch、tag moved、publish unknown/
   remote exact 和 rollback 均有断言；普通 PR CI 也必须执行这些 fixture，而真实 tag/push/release 永不发生。
2. **NG-10C — build provenance + release lock：** 将 Rust toolchain、protoc、runner、Cargo.lock、A/B/tag、
   release workflow/helper/contract/SBOM generator/updater 的 hash 和每平台 asset hash 写进 versioned signed
   provenance；`SOURCE_LOCK` 的精选 runtime map 不被误称为完整 release lock，另建专用 release lock set。
   SBOM 很重要但不等同于 artifact attestation、reproducible build 或依赖/许可证风险决策。
3. **NG-10D — installer/updater v1：** 先让现有 Grok identity updater fail-closed/隔离，禁止其访问上游
   host、`~/.grok` 或 installer。新 updater 只认 pin 的 Lumen public-key fingerprint，下载 manifest + signature，
   验 Minisign、target、hash、version、A/B tuple 后原子替换；`latest` URL、release 附带未 pin 公钥、未验签
   archive 或自动跨版本覆盖一律拒绝。shell/PowerShell updater 使用同一 manifest contract；Windows 在官方
   signed asset 真正接入前保持 `NOT RUN`。
4. **NG-10E — platform proof：** 将 NextGen contract receipts（尤其 no-replay、manifest、ledger、operation、
   write scope、flow）加入 release manifest 的 required/blocked inventory；未实现的合同必须明示
   `NOT RUN/BLOCKED`，不能靠空文件通过。最后在 isolated clean user/temporary install prefix 验证
   install/upgrade/rollback；保留 binary SHA、source A、evidence B、OS/target、exact argv、raw exit。macOS
   Developer ID、notarization、staple、`spctl` 与正式 GitHub publish 仍需相应外部授权。

**反例：** version bump 后 source-lock 自己失败、release helper 暂存不相关 user 文件、source 改动混入 B、
source lock 错指 B 而 binary 来自 A、SBOM/manifest 指向不同 asset、tag 不 peel 到 expected commit、signature/asset
hash 不一致、readiness 用旧 binary、unknown publish 自动重发、upgrade 覆盖无法 rollback、clean install 执行
错误 binary、updater 接受 Grok identity/未签名 archive/未 pin key、PR 从不演练 release transaction、
toolchain/provenance 缺失却称可复现。

**Gate：** `NG10_RELEASE_FOUNDATION_GATE=PASS` 需要 temporary-repo A/B/tag transaction tests、release lock/
provenance fixture、exact binary/install/rollback receipts、ordinary-PR exact CI/source SHA 和 current contract-gate
inventory。`UPDATER_TRUST_GATE=PASS` 另需 fake-server downgrade/redirect/key-swap/tampered-manifest/archive/rollback
反例。它们都是 `v2.0.0-rc.1` 前置；不替代人工 tag/release authority、实际 signing secret、notarization、
live/provider proof 或 long soak。

---

# Part III — 协作、验收、完成定义

## 19. PR 和 owner 序列

| 卡 | 交付 | 依赖 | owner |
|---|---|---|---|
| P0-NR-A | 关闭所有无 receipt 的同轮 resubmit | 当前 source | Codex（单 writer） |
| R0-00 | manifest/scope review | 当前现场 | Codex |
| R0-01 | 分组验证 | R0-00 | Codex |
| R0-02 | clean source candidate | R0-01 | 单一集成 owner |
| R0-03 | integration + exact CI | R0-02 | Codex + human merge |
| R0-04/05 | evidence/release gates | R0-03 | Codex + release authority |
| NG-01 | TaskTree | R0 | Codex |
| NG-02 | Capability Ceiling | NG-01 | Codex |
| NG-02A | ToolContract/result boundary | NG-02 | Codex（registry 单 writer） |
| NG-03 | activity/budget/process | NG-01/02 | Codex |
| NG-03C | operation lease/event/outbox/reconcile | NG-03 | Codex |
| NG-03D | write scope/worktree handoff/merge receipt | NG-03C/02 | Codex（workspace single writer） |
| NG-03E | queue flow/delivery/liveness | NG-03C | Codex（actor/sampler single writer） |
| NG-04A/B | claim journal + authority state machine | NG-01/02 | Codex（schema 单 writer） |
| NG-04C-1/2 | AcceptedSnapshot + ContextManifest/compact-resume rebuild | NG-01/04A/B | Codex（schema/prompt 单 writer） |
| NG-04D | AgentSandbox + bounded handoff | NG-02/03C/04C | Codex（authority single writer） |
| NG-04E | Governed Evidence Loop/checkpoint reducer | NG-03E/04D/04C | Codex（actor/operation single writer） |
| NG-05 | health/no replay | R0 | Codex |
| NG-06 | Advisor shadow | NG-04/04C/05 | Codex |
| NG-06A | ClientAdvisor virtual tool | NG-04C/04D/04E/05/06 | Codex（tool/actor single writer） |
| NG-09A | shadow-only offline golden path | NG-01..06A, no NG-07 | Codex + independent reviewer |
| NG-07 | bounded assignment | NG-09A | Codex |
| NG-09B | bounded-assignment golden extension | NG-07/09A | Codex + independent reviewer |
| NG-08 | Kairos local | NG-01..04 | Codex |
| NG-10 | release source/evidence transaction + install/rollback proof | R0, NG-08, NG-09B | Codex + release authority |

## 20. Codex、DeepSeek Flash、Grok 4.5、DeepSeek Pro 的工程协作边界

| 角色 | 可以做 | 永不独立决定 |
|---|---|---|
| Codex | RFC、authority/permission/recovery、核心 Rust、integration、独立验收、built binary、CI/release truth | 未授权 merge/push/tag/billable provider。 |
| DeepSeek V4 Flash | rg inventory、文档链接、serde fixture、approved state-table 测试骨架、差异表、机械性目录核对 | authority/grant、source pin、routing、最终验收。 |
| Grok 4.5 | mock/fake-clock、property/negative、DTO/read-only tests、独立 review、fault-matrix 扩展 | SessionActor、permission、release、provider/live、merge。 |
| DeepSeek V4 Pro | 限定路径的设计反例、schema/recovery review、测试失败归因、与 Flash 结果的独立交叉审阅 | authority owner、capability/permission、source pin、自动 routing、最终 ACCEPT。 |

辅助任务必须有 Allowed paths，且不与其他写任务重叠。coordinator/schema/permission/source-lock 只能单 writer。

### 辅助模型交付卡（防止机械任务污染核心）

每次交给 Flash、Grok 或 Pro 的工作卡必须固定写出：

~~~text
exact input SHA / task ID / goal / non-goals
allowed paths / forbidden paths / no-overlap owner
existing source anchors and tests to read
proposed files and schema compatibility
positive + negative cases / exact commands
raw exit + counts + diff hash required
STOP on unexpected dirty path, API invention, provider call, or failing baseline
no merge / no tag / no source-pin change / no final acceptance
~~~

Codex 必须独立复读 diff、重跑真实命令并按 `ACCEPT` / `REJECT` 记录，而不是采纳模型总结。任何
authority、permission、coordinator、manifest schema、source-lock、ordinary reroute 或 release 路径只允许
一个 writer；并行仅适用于只读 inventory、fixture、mock、独立 review 或无重叠的 test 模块。

## 21. 八层验收和 evidence packet

| 层 | 所需证据 |
|---|---|
| Source | exact diff、source pin、format、contract/schema revision。 |
| Unit | 正例、反例、fuzz/property、真实 counts。 |
| Actor | grant/deny、持久化、cancel/recovery、owner/session/workspace/call。 |
| Product | rebuilt exact-source binary 真走 ACP/TUI。 |
| CI | exact GitHub SHA、workflow URL、conclusion。 |
| Package | clean-user install、targeted binary/SBOM/signature/provenance、upgrade/rollback；现有 local ad-hoc signing 不等于 Developer ID/notarization/attestation。 |
| Live | 单独授权 provider/host/device proof。 |
| Release | tag/assets/checksums/install/operator docs。 |

每包固定：

~~~text
source_commit / rollback_commit / diff_hash
binary_sha256 if built / source_lock_sha256
schema revisions / manifest hash
exact argv / raw exit
passed / failed / ignored / filtered / no-tests-matched
artifact/evidence hashes
NOT RUN / BLOCKED / manual gates / known risks / generated_at
~~~

## 22. 全局拒绝清单

- max_depth=3 被说成完整三级产品；
- Advisor 被当作 child 幻觉解决方案，或 Advisor PASS→success；
- child 继承 bypass/yolo/PermissionHandle，或 child 写 Accepted；
- summary、raw root chat、sibling Proposed 或 secret 被伪装成 ContextManifest/Accepted snapshot；
- unknown MCP 因无 ToolKind 自动保留；
- emitted stream 后 fallback/replay；
- ordinary reroute 的注释/默认关闭被当作 sealed no-replay receipt；
- usage 缺失却记成本为零；
- PID/scheduler/短测试被称为 24h daemon；
- source lock 覆盖 dirty tree 被称为 release source；
- evidence suffix B 被当作 build/tag source A，或 version bump 后在脏树中硬跑 source-lock；
- 上游 Grok updater、`latest` URL、未 pin key/manifest/signature 的 archive 被当作 Lumen 更新信任链；
- cargo check、grep pipeline、旧 CI、0-test filter 被称全绿；
- 辅助模型 merge/push/tag/改 pin/调用 provider/决定 release；
- PR、merge、tag、release、install、M5/M6/live/soak 互相替代。

## 23. 完成定义与最短开工

### source sync 完成

必须同时有 clean integration source A、allowlisted evidence suffix B、GitHub exact-SHA CI、source lock/SBOM/
binary/readiness 的 A/B tuple、rollback SHA 和 main 合并审查记录。tag/release/install 仍后置。

### NextGen 完成

1. clean source A、evidence suffix B、installable binary、SBOM、source lock 与 release tag(A) 有可验证 tuple；
2. tree 的运行/UI/metadata/resume/cancel 一致，默认保守关闭；
3. child capability 单调收缩，unknown MCP deny，bypass root-only/TTL/revocable；
4. 每个 tool 有 versioned capability/scope/result/idempotency contract；tool output 以 redacted artifact/有界
   preview 进入 context，catalog drift fail-closed；
5. 并行/process/budget 由 root actor 原子治理，usage unknown 如实展示；
6. SessionMemory、每 branch private scratch、Accepted WorkingLedger、LongTermMemory 分离；sibling 只读相同版本
   Accepted snapshot，proposal/handoff 不能成为控制指令；
7. 每次 spawn/compact/resume 都由不可变 ContextManifest + Accepted snapshot 重建，mismatch fail-closed；
8. child proposal 只有 evidence/verification/root acceptance 后才为事实；
9. 每 node 有 actor-issued、可撤销 `AgentSandboxV1`，其 context/memory/tool/process/filesystem/network/budget
   边界都可审计；
10. 每一 loop 由 `LoopContractV1`/checkpoint/stop condition 约束；无新 evidence、stale snapshot、repair limit、
    queue/effect uncertainty 必须 Blocked/Frozen，不可无限自评重试；
11. Expert 是第二意见；ClientAdvisor 是本地 virtual tool，受 pin/privacy/health/budget/independence/capsule
    redaction 限制，不能 acceptance、execution 或 mid-stream switch；
12. provider 仅在 sealed NoOutput/NoToolCall/NoEffect receipt 下 visible fallback，绝不重放；
13. Kairos 经 lease/crash/idempotency/takeover/no-replay 演练，external effect 缺 receipt 永远 Frozen；
14. 并行 writer 只持有 root-signed、path-scoped、可撤销的 `WriteScopeLease`；worktree handoff 有冲突、
    stale-base、dirty-target、verification 与 root-decision receipt，绝不 child auto-commit/merge/push；
15. 每一 authority event 的 queue/closed-channel/sequence-gap 有 delivery observation；UI 可明确 coalesce，
    但 tool/grant/terminal/evidence 不可静默丢失，未知状态 Frozen；
16. secret、credential、protected path 和受限 artifact 不进入 manifest/ledger/UI；retention、tombstone 和
    deletion receipt 可复核，unknown schema/partial migration fail-closed；
17. Source/Unit/Actor/Product/CI/Package/Live/Release 证据分别完整，NOT RUN/BLOCKED 不隐藏。

### 最短开工序列（当前唯一允许的实施顺序）

1. **P0-NR-A（现在）**：以开工时重新读取的 HEAD（当前审计锚点 `8b74b361`）关闭 compact/auth/
   ordinary reroute 的所有 shell 同轮 `*AndResubmit`，以及 sampler 内 retry/backoff/image-strip/HTTP1/doom
   resample；ordinary pool preselection 只允许 root 的第一次 sampler submission。定向 negative tests、
   transport counting fixture、check、diff check 成为 `P0_NR_SAFETY_GATE`。这是安全回退，不做 receipt 大设计，
   不让任何开关重新放行。
2. **R0-00/01**：以 P0 source commit 为起点重新做 path manifest、`ls-remote` remote snapshot、分组审查
   和真实 raw-exit 验证；不能拿 `0fae4c7b` lock、`9e719020` readiness 或旧 CI 当绿。
3. **R0-02 至 R0-05**：先 clean source candidate，再 exact-SHA CI；再从同一 source 构建 binary、生成
   lock/SBOM/readiness evidence suffix；最后由人决定 PR merge。它使当前 ahead 候选可消费，但不是 release，
   更不是 24h/product 完成。
4. **NG-01 → NG-02 → NG-02A**：不重写已有 lineage；补 tree read model、resume/orphan/late-event 故障
   注入，再将 depth/tool ceiling 升级为 grant/TTL/revoke/policy receipt 与 unknown-MCP deny；随后冻结可调用
   tool contract、result artifact/redaction/context budget，禁止把任意 MCP 输出塞入后续上下文。
5. **NG-03B → NG-03C → NG-03D/03E**：先 atomic budget reservation/settlement，再 operation
   lease/event/outbox/reconcile；接着补 write-scope/worktree handoff 和 queue/delivery/liveness。`8b74b361`
   的 ingress/control-lane 只是开始，普通 authority data plane 仍须 delivery observation。不要先开 24h daemon，
   不把 PID、worktree 目录或无界队列当状态。
6. **NG-04A → NG-04B → NG-04C-1 → NG-04C-2**：已有 core DTO/authority/manifest foundation 仍须按该次序
   做 migration、snapshot/read-model、完整 admission 与 compact/resume product gate；禁止把已存在的 hash
   误称已闭环。summary、长期记忆和 Advisor 都不能插队替代。
7. **NG-04D → NG-04E**：先由 actor 签发 AgentSandbox、隔离 scratch、限制 handoff，再把 node/tree/supervisor
   的 checkpoint、evidence、repair limit、stop/escalation 接入同一 operation/ledger。没有该两项，不开真实
   多层并行，也不把“反复尝试”称为 loop engineering。
8. **NG-05 → NG-06 → NG-06A**：实现 sealed receipt 后，每次只重开一种 resubmit reason；完成 provider health
   mock matrix 与 Advisor shadow corpus；随后以 `off → shadow → mock on-demand → gated checkpoint` 顺序实现
   ClientAdvisor。Advisor 始终无 acceptance/execution 权。
9. **NG-09A**：用 exact binary 跑三层 shadow-only golden path，独立验收树、sandbox、grant、claim、manifest、
   loop、预算、no-replay、mock Advisor 与 UI 真相；不调用 provider。
10. **NG-07 → NG-09B**：只在 NG-09A 后做 root-approved bounded assignment 和其 golden extension；任何
   user pin、已输出、effect/usage/receipt unknown 都 fail-closed。
11. **NG-08**：以 operation API 做 Kairos local crash/reconcile/freeze proof；随后才评估 long soak 与
    24h autonomy。
12. **NG-10**：先修复并证明 S→E release transaction，再做 clean install/upgrade/rollback receipt；每个新
    源码 candidate 都重走 source/evidence/CI gate。所有这些完成且获得人类 release authority 后，才讨论
    RC/tag/release。

这是可验证、可停止、可回退的路线。任何没有 source、命令、负例和证据的愿景，不获得执行或完成权。
