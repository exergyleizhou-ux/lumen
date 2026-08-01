# Lumen NextGen 最终可执行总纲

**日期：** 2026-08-01（北京时间）
**性质：** Lumen 后续实施的唯一排序、依赖、验收与交接总纲；不是功能完成、CI 通过、发布、安装或 live/provider 证明。
**范围：** Rust Lumen coding agent；macOS-first；不做 Windows 专项、未授权 provider/billable 调用、deploy 或 release。
**方法参考：** 同日 Lumen Science 执行书只提供阶段与证据结构；它不是 Lumen Core 的 API、代码或发布依赖。
**证据窗口：** 2026-07-27 至 2026-08-01 的 Lumen 提交、当前源码、当前 GitHub 和当前工作树。窗口外旧规划仅是历史，不是需求、优先级或完成依据。

本书先冻结事实，再规定每项的文件接缝、数据合同、迁移、反例、命令、退出门和回退。下文标为【拟建】的类型、crate、配置或命令，在真正提交前都不是现有 API。

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
 ├─ ExpertConsultation v1 — 已有独立第二意见
 ├─ AdvisorPolicy — 先影子，后受限分配
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

---

## 1. 当前真相冻结

### 1.1 精确基线

| 项目 | 当前实测事实 | 本书处理 |
|---|---|---|
| 本地工作树 | /Users/lei/code/lumen；分支 sync/absorb-upstream-20260731；HEAD e7afd15b1656108598c6f78966e69fea36ea7bde | 本地候选，不是发布基础。 |
| GitHub main | origin/main=2f47a9ad84e94b20291a1ad3d6b005ccbd3885f4 | 与本地候选互非祖先；禁止覆盖同步。 |
| 分叉量 | origin/main...HEAD 为 2 / 21 | 本地落后远端 2、领先 21；必须建 integration candidate。 |
| 工作树 | 本书更新前 36 个已跟踪修改、2 个未跟踪文件 | R0 manifest 必须逐路径归属。 |
| 上游吸收 | f9cf565d → 818d6488 → a556d74b → b09b929f → e7afd15b；上游 pin dd04f397 | 已在本机，尚未进入 GitHub main。 |
| 版本 | VERSION 为 0.1.251 | version、tag、release 与同步分门。 |
| GitHub CI | main 的 d29f0316：auto-checks 成功，CI 30610926547 失败 | GitHub main 不可称全绿；R0 必查失败原因。 |
| readiness | 已提交产物对应 b09b929f，状态 BLOCKED | 旧 evidence 不证明后续源码。 |
| 发布门 | L5 soak、binary tuple post、M5、M6、eval_live、reconcile 未闭合或失败 | R0 不解除这些门。 |

### 1.2 来源锁的真实含义

SOURCE_LOCK 当前记录 e7afd15b 及关键文件 hash，但并不证明脏工作树属于 e7。R0 的强制顺序：

1. 所有源码和文档合同先形成 clean source candidate commit；
2. 从该 commit 构建并记录 binary hash；
3. 再生成 source lock、SBOM、readiness；
4. 之后仅允许 lock/SBOM/readiness/evidence 组成 evidence-only suffix；
5. 任意源码变化都回到第 1 步。

### 1.3 当前资产与缺口

| 域 | 已有资产 | 不能误报为完成 |
|---|---|---|
| 子 Agent | TaskTool、depth 限制、coordinator、取消、完成事件 | 真实父子树、UI、恢复、树预算。 |
| Expert | Fast/Vision/Deep/Dual、双 proposal、单 writer、HostVerification | AdvisorPolicy、自主模型路由。 |
| memory | global/workspace、SQLite/FTS/vector、JSONL/summary | 跨 worktree 的共享事实账本。 |
| 进程 | scheduler、workflow、leader、background terminal | 统一 activity、全树 budget、24h supervisor。 |
| 验证 | VerifyAfterEditOutcome；Some(Pass) 才算 edit delivery | 全任务或 release 成功。 |
| provider | catalog、BYOK、role pin | health、failover、可解释 routing。 |

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

### 3.1 【拟建】claim 状态机

~~~text
Proposed
  → EvidenceAttached
  → HostVerified
  → Accepted | Rejected | Conflicted | Inconclusive | Superseded
~~~

Advisor report、搜索摘要、child self-report、网页片段默认最多 Proposed。Accepted claim 也不等于 terminal success。

### 3.2 child heartbeat

~~~text
task_tree_id / node_id / parent_id / state revision
current objective / last evidence ref / next bounded step
remaining budget / grant expiry / blocker or uncertainty
~~~

输入缺失、与 Accepted facts 冲突、下一步扩大 scope、重复无新证据、tool result 无法支持结论时，child 必须 Blocked 或 NeedParentDecision。

---

## 4. 依赖图与并行纪律

~~~mermaid
flowchart LR
  R0["R0 source + GitHub sync"] --> T1["NG-01 TaskTree"]
  T1 --> C2["NG-02 Capability Ceiling"]
  C2 --> B3["NG-03 TreeBudget + lifecycle"]
  C2 --> L4["NG-04 WorkingLedger"]
  R0 --> F5["NG-05 Provider health"]
  F5 --> A6["NG-06 Advisor shadow"]
  L4 --> A6
  B3 --> A7["NG-07 bounded assignment"]
  A6 --> A7
  B3 --> K8["NG-08 Kairos local"]
  L4 --> K8
  A7 --> G9["NG-09 offline golden path"]
  K8 --> G10["NG-10 release hardening"]
~~~

允许并行：R0 只读审计、NG-01 DTO/UI inventory、NG-04 schema RFC、NG-05 mock transport matrix、Kairos fake-clock harness。
绝不提前：没有 Tree/Ceiling/Budget 不开三层；没有 health/no-replay 不做自动 routing；没有 ledger scope 不共写记忆；没有 lease/crash proof 不称 24h；没有 R0 exact SHA/CI 不把本地当正式基础。

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

1. 从 clean candidate 构建并记录 binary_sha256；
2. source lock 锁 candidate；
3. 生成 SBOM/readiness/evidence；
4. 只允许 lock/SBOM/readiness/evidence 文件形成 suffix；
5. verifier 检查 source→evidence 的每一个路径。

新增负例：source-lock 前 tracked/staged dirty 必须失败；任意源代码混入 evidence suffix 必须失败；binary SHA/source stamp 不同必须失败。

### R0-05 PR/merge/tag/release/install

| 门 | 所需证据 | 不代表 |
|---|---|---|
| PR | exact integration SHA、required CI、review | 已合并/发布。 |
| Merge | GitHub main 指向审过 source/evidence | 已有 tag。 |
| Tag | tag 指向验证 source | assets 完备。 |
| Release | assets/checksum/SBOM/signature/manifest | 干净机可安装。 |
| Install | 隔离环境安装、version/hash/basic run | M5/M6/live/soak 已通过。 |

R0 结束仅可称可消费 source baseline，不解除 M5/M6、soak、live eval 或当前失败 CI。

---

# Part II — NextGen Core phases

## 9. NG-01：TaskTreeLineage v1

**状态：** Draft。
**目标：** 真正表现 Main→Code→Review/Test/Evidence 的每条边。

### 必读锚点

| 路径 | 事实 |
|---|---|
| agent/crates/codegen/xai-grok-tools/src/implementations/grok_build/task/mod.rs:180-234 | 当前 depth check。 |
| 同 crate task/coordinator.rs:172-210 | nested child 改写 parent_session_id=root_parent。 |
| 同 crate coordinator_tests.rs:1287-1299 | 旧测试固定 root reparent。 |
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

**状态：** Draft；前置 NG-01。
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

## 11. NG-03：TreeBudget 和受管进程 lifecycle

**状态：** Draft；前置 NG-01/02。
**目标：** 并行 Agent、terminal、monitor、scheduler fire 有同一 tree owner、预算、deadline、回收语义。

### 必读锚点

| 路径 | 事实 |
|---|---|
| tools task/coordinator.rs:165 | coordinator 已有 child cancel。 |
| tools scheduler/actor.rs:511-617 | 已防前一轮 descendants 未结束时重复 fire。 |
| tools workflow/mod.rs:171-176 | 有 workflow agent budget，不等于 tree budget。 |
| shell agent/mvp_agent/session_lifecycle.rs:401-434 | idle unload 对活动聚合仍有 TODO/race。 |
| shell agent/activity.rs:51-221 | 存在 activity 部件，尚非完整 actor aggregate。 |
| tools computer/local/terminal.rs | 背景 terminal owner/reap 模式。 |

### NG-03A activity aggregation

【拟建】SessionActivitySnapshot 由 actor 内 UnloadIfIdle 单命令原子读取：foreground、background terminal、monitor、scheduler fire、background subagent、lease、pending approval。

反例：check/unload 间注入 prompt；monitor/scheduler/background child 活着却 unload；late completion 复活 disposed session。
Exit：所有活动存在时 unload 拒绝；actor check-and-act 无丢 prompt。

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

## 12. NG-04：SharedWorkingLedger 与四层记忆

**状态：** Draft；前置 NG-01/02。
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
              | Accepted | Rejected | Conflicted | Inconclusive | Superseded,
    pub content_hash: Sha256,
    pub evidence_refs: Vec<ArtifactRef>,
    pub provenance_refs: Vec<ProvenanceRef>,
    pub confidence: Confidence,
    pub policy_revision: PolicyRevision,
    pub supersedes: Option<ClaimId>,
    pub created_at: Timestamp,
    pub expiry_or_review_after: Option<Timestamp>,
}
~~~

【拟建】crate/module 名为 xai-grok-working-ledger，最终边界须先 RFC。append-only journal 是 authority；SQLite/FTS/vector 是可重建 read model。

**写入：** child 仅自身 branch Proposed；root 验 scope/grant/hash/evidence/conflict 后迁移状态；sibling 只读 Accepted snapshot；跨 worktree 以 task_tree_id 路由。
**恢复：** lenient tail recovery 产出 RecoveryEvent(skipped_count,byte_offset,raw_hash,quarantine_path)，tree 变 NeedsRecoveryReview，禁止自动 promotion；重建 index 后逐 sequence/hash 比对 journal。

**反例：** child direct Accepted、无 evidence Accepted、cancelled promotion、cross-tree read、stale hash、revision conflict、auto-merge、secret leak、torn append/index mismatch。
**Exit：** journal replay/read model 一致；所有 conflict 显式；summary 仅引用 Accepted。
**Rollback：** journal 保留，index 可删后重建。

## 13. NG-05：ProviderHealth 与 no-replay failover

**状态：** Draft；近期设计明确未实施；前置 R0。
**非目标：** cheapest/fastest router、默认跨 provider、绕 user pin、改 modelpool。

| 来源 | 必读事实 |
|---|---|
| docs/provider-failover-design.md:1-19 | 设计未实施；模型 preset 不等于 live 验证。 |
| 同文件 21-39 | CircuitBreaker/Registry/MockClock 可复用。 |
| 同文件 43-88 | breaker domain、sampler 接入、no replay、visible fallback。 |
| xai-grok-sampler/src/client.rs | 拟接入 inference transport 的目标。 |

固定合同：

~~~text
breaker key = provider + base_url
failure = connection | timeout | 5xx
not breaker failure = 400 | 401 | 403
fallback = only before any output block
emitted block = partial failure; never replay
visibility = actual from/to/reason/state in log/UI/turn-tail
accounting = actual executor; missing usage stays unavailable
~~~

实现：sampler request 前 check、response 后 record；explicit fallback chain；每次产出 ProviderAttemptReceipt。Advisor 不得直接 retry provider。

离线矩阵：N×503 open/half-open/close、base URL isolation、401/403/400、first-block fallback、post-block disconnect、quota exhausted、catalog alias swap、missing usage。
Exit：mock transport 覆盖每一状态，无真实 provider；UI/provenance 与 actual executor 一致。
Stop：无法确定是否 emitted block 时按已输出处理，禁止 fallback。

## 14. NG-06：AdvisorPolicy shadow-only

**状态：** Draft；前置 NG-04/05。
**目标：** 模型建议可审计、零执行影响。

| 路径 | 事实 |
|---|---|
| shell session/expert.rs:46-57,349-447 | Dual 单 writer 和 durable state。 |
| expert.rs:1743-1788、expert_consultant_tools.rs | consultant read-only。 |
| acp_session_impl/expert.rs:2015-2085 | dual 真实双 source 测试模式。 |
| shell agent/subagent/mod.rs:573-679 | 当前 child model resolution 顺序。 |

【拟建】ModelSelectionAdviceV1 含 task tree、target node、task class、catalog/policy/health snapshot hash、candidate/rejection list、recommended assignment、independence、budget impact、reason codes、Shadow/Recommend/Approved/Rejected/Applied。

固定过滤：user pin/harness → allowlist/BYOK/endpoint/privacy/tool → health/context/usage budget → task class → failure-domain independence → quality/latency/cost。

Shadow 不 switch、不 spawn、不调工具、不写文件、不改 terminal、不写 Accepted。
Exit：离线 replay corpus policy violation=0、stream switch=0、pin override=0、audit missing=0。
Rollback：关闭 consumer，不删除 advice log。

## 15. NG-07：recommend 与 bounded assignment

**状态：** Not started；前置 NG-01 至 NG-06。
仅当 new child/new turn、no output、no user pin、allowlisted/compatible、health 可用、budget reserve 成功、privacy 允许、capability 不变、root approval record 完整时可 Applied。

禁止：SetDefaultModel、替换 stream、静默 cross-provider、无限 spend、Advisor PASS→completion、advice text→tool call。
反例：user pin、private endpoint、budget exhausted、breaker open、schema mismatch、existing output、stale advice。
Exit：每个 Applied advice 有 root approval、actual model receipt、budget reservation、ledger decision。
Rollback：关闭 auto apply，历史 advice 不改写。

## 16. NG-08：KairosSupervisor local proof

**状态：** Draft；前置 NG-01/02/03/04。Advisor 不是前置。
**目标：** SessionActor 之下的长期运行治理，不是新 Agent 或 shell daemon。

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

| crash state | 唯一动作 |
|---|---|
| pure read/deterministic 未执行 | source/policy/budget 有效时新 lease retry。 |
| 幂等 receipt job | 验 receipt 后 resume 或 skip。 |
| emitted model block | partial，禁止 replay，等 root/user。 |
| commit/send/install/network effect | Frozen，重新批准。 |
| promotion/completion | rehash ledger/evidence/verify 后决定。 |

验证：fake clock、lease race、two supervisor、dispatch crash、duplicate outbox、expired approval、root cancel、no replay、exact binary start/ready/crash/reconcile/stop。
Exit：local no-side-effect fixture 完成矩阵；仍不称 24h autonomous。
Stop：外部副作用无 idempotency receipt 时永远 Frozen。

## 17. NG-09：三层 offline golden path

**状态：** Not started；前置所有适用 NG gate。
**目标：** 用零 provider、零外部副作用的 rebuilt binary 证明边界一起工作。

~~~text
root creates immutable contract and fixture workspace
  → root approves depth-1 Code
  → Code creates depth-2 Research/Review/Test
  → optional depth-3 leaf returns typed Proposal
  → branches append Proposed with fixture evidence
  → root deterministically verifies/conflict-resolves
  → Advisor advice is recorded but cannot accept/switch
  → root cancels one branch, preserves siblings, replays ledger
  → exact binary renders truthful tree/status/terminal result
~~~

必须证明：lineage/fanout；grant TTL/unknown MCP denial；reserve/cancel/late completion；
proposal-only/evidence requirement；conflict no-auto-merge；Advisor non-authority；artifact tamper、
stale snapshot、owner/session/workspace mismatch；crash read-model rebuild；UI/ACP 与 coordinator/ledger 一致。

非目标：真实 provider、auto commit、跨机 restore、daemon soak。
Exit：newly rebuilt exact-source binary 真跨 ACP/TUI seam。
Stop：需要 live key/network/bypass 才能证明，说明 fixture 错误。

---

# Part III — 协作、验收、完成定义

## 18. PR 和 owner 序列

| 卡 | 交付 | 依赖 | owner |
|---|---|---|---|
| R0-00 | manifest/scope review | 当前现场 | Codex |
| R0-01 | 分组验证 | R0-00 | Codex |
| R0-02 | clean source candidate | R0-01 | 单一集成 owner |
| R0-03 | integration + exact CI | R0-02 | Codex + human merge |
| R0-04/05 | evidence/release gates | R0-03 | Codex + release authority |
| NG-01 | TaskTree | R0 | Codex |
| NG-02 | Capability Ceiling | NG-01 | Codex |
| NG-03 | activity/budget/process | NG-01/02 | Codex |
| NG-04 | WorkingLedger | NG-01/02 | Codex |
| NG-05 | health/no replay | R0 | Codex |
| NG-06/07 | Advisor shadow/apply | NG-04/05 and all prior | Codex |
| NG-08 | Kairos local | NG-01..04 | Codex |
| NG-09 | offline golden path | applicable gates | Codex + independent reviewer |

## 19. Codex、DeepSeek Flash、Grok 4.5

| 角色 | 可以做 | 永不独立决定 |
|---|---|---|
| Codex | RFC、authority/permission/recovery、核心 Rust、integration、独立验收、built binary、CI/release truth | 未授权 merge/push/tag/billable provider。 |
| DeepSeek Flash 0731 | rg inventory、文档链接、serde fixture、approved state table 测试目录、差异表 | authority/grant、source pin、routing、最终验收。 |
| Grok 4.5 | mock/fake-clock、property/negative、DTO/read-only tests、独立 review | SessionActor、permission、release、provider/live、merge。 |

辅助任务必须有 Allowed paths，且不与其他写任务重叠。coordinator/schema/permission/source-lock 只能单 writer。

## 20. 八层验收和 evidence packet

| 层 | 所需证据 |
|---|---|
| Source | exact diff、source pin、format、contract/schema revision。 |
| Unit | 正例、反例、fuzz/property、真实 counts。 |
| Actor | grant/deny、持久化、cancel/recovery、owner/session/workspace/call。 |
| Product | rebuilt exact-source binary 真走 ACP/TUI。 |
| CI | exact GitHub SHA、workflow URL、conclusion。 |
| Package | macOS install/signing/SBOM/attestation/rollback。 |
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

## 21. 全局拒绝清单

- max_depth=3 被说成完整三级产品；
- Advisor 被当作 child 幻觉解决方案，或 Advisor PASS→success；
- child 继承 bypass/yolo/PermissionHandle，或 child 写 Accepted；
- unknown MCP 因无 ToolKind 自动保留；
- emitted stream 后 fallback/replay；
- usage 缺失却记成本为零；
- PID/scheduler/短测试被称为 24h daemon；
- source lock 覆盖 dirty tree 被称为 release source；
- cargo check、grep pipeline、旧 CI、0-test filter 被称全绿；
- 辅助模型 merge/push/tag/改 pin/调用 provider/决定 release；
- PR、merge、tag、release、install、M5/M6/live/soak 互相替代。

## 22. 完成定义与最短开工

### source sync 完成

必须同时有 clean integration SHA、GitHub exact-SHA CI、source lock/SBOM/binary/readiness 指向同一 source、rollback SHA 和 main 合并审查记录。tag/release/install 仍后置。

### NextGen 完成

1. main、installable binary、SBOM、source lock、release tag 同源；
2. tree 的运行/UI/metadata/resume/cancel 一致，默认保守关闭；
3. child capability 单调收缩，unknown MCP deny，bypass root-only/TTL/revocable；
4. 并行/process/budget 由 root actor 原子治理，usage unknown 如实展示；
5. SessionMemory、scratchpad、WorkingLedger、LongTermMemory 分离；
6. child proposal 只有 evidence/verification/root acceptance 后才为事实；
7. Expert 是第二意见；Advisor 受 pin/privacy/health/budget/independence 限制；
8. provider 无输出才 visible fallback，绝不重放；
9. Kairos 经 lease/crash/idempotency/takeover/no-replay 演练；
10. Source/Unit/Actor/Product/CI/Package/Live/Release 证据分别完整，NOT RUN/BLOCKED 不隐藏。

### 最短开工序列

1. R0-00：path-level manifest，不动 runtime；
2. R0-01：复验 source/diff/test，失败即修或 BLOCKED；
3. R0-02 至 R0-04：clean candidate、GitHub integration、exact CI/source/binary/evidence；
4. NG-01 真 lineage；不开放三层；
5. NG-02 封 bypass/MCP/permission inheritance；
6. NG-03 activity/budget/process；
7. NG-04 ledger；
8. NG-05 no-replay health；
9. NG-06 shadow，NG-07 bounded assignment；
10. NG-08 local Kairos，NG-09 exact-binary offline golden path；
11. 全部通过后才进入 release candidate、long soak 和人工门。

这是可验证、可停止、可回退的路线。任何没有 source、命令、负例和证据的愿景，不获得执行或完成权。
