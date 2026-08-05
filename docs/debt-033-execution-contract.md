# DEBT-033 最终执行合同（2026-08-05 定稿）

> 集大成：总纲纪律 + 六项方案 + Claude 评审采纳 + Grok4.5 评审采纳 + 两轮修正。
> 性质：**执行合同，不是许愿清单**。每项交付都有：涉及文件（已核实或标注"开工审计"）、
> 具体动作、**可证伪的验收断言**、失败判定。凡"评估/建议"类一律显式标注"评估不承诺"。

---

## 0. 合同纪律（违反即违约）

1. **单 writer**：v2.1.0 发布尾（soak→readiness→证据→push→CI）完成前，不动任何源码。
2. **全状态经 SessionActor**：任何新状态/事件必须进 LifecycleJournal（append-only、prev-hash 链、serde default 向后兼容——沿用 `prev_payload_hash: Option` 模式）。
3. **先锚后改**：A1 基线未跑通，禁止进入 A2。
4. **可证伪**：每个工作包验收断言必须能跑出"通过/失败"，失败即停止该包，先修再进。
5. **外部材料只当接口草图**：Reasonix/Claude/Grok 的设计可引用结构，不照抄代码。
6. **不做清单锁定**（§8）任何一条，评审再强烈也不破。

---

## 1. 总览：三周期 × 六工作包 + 一横切

```
Cycle A（成本与一致性根基）
  A1 基线实测（⑥）→  A2 缓存稳定（①）→  A3 Profile+Effort（③）
Cycle B（可靠性跃迁）
  B1 锚点前置（②）→  B2 验证-修复闭环（④）
Cycle C（体验组合与调优）
  C1 双模型（⑤）→  C2 Uncertainty Governor（横切）→  C3 Responses 评估 + 全周期调优
```

依赖：A1 无依赖；A2 依赖 A1（对照锚点）；A3 依赖 A2（hit 观测）；B1 依赖 A2（property harness 复用）；B2 依赖 A3（effort 触发策略）；C1/C2 依赖 A+B 全部信号。

---

## 2. 已核实基建（开工事实基线，全部存在，非假设）

| 组件 | 路径 | 现状能力 |
|---|---|---|
| 前缀形状 | `lumen-discipline/src/cache_shape.rs` | `PrefixShape / PrefixChangeReason / CacheDiagnostics / capture_shape / compare_shape / estimate_tokens` |
| 会话缓存跟踪 | `lumen-discipline/src/session_cache.rs` | `SessionCacheTracker / SessionCacheSnapshot / observe / profile` |
| 前缀构造 | `lumen-discipline/src/request_prefix.rs` | `join_system_texts / tools_fingerprint_json / shape_from_parts` |
| 缓存纪元 | `xai-grok-shell/src/session/cache_epoch.rs` | 会话级缓存纪元（职责细节 → A2 第一步审计） |
| 缓存观测注册 | `xai-grok-shell/src/session/prompt_cache_registry.rs` | 观测注册（接线点） |
| 压缩配置 | `xai-grok-shell/src/util/config/resolve/compaction.rs` | 压缩配置解析（policy 扩展点） |
| 验证管线 | `lumen-verify/src/{detect,repair,runner,steps,config}.rs` | typed verification 管线已存在；**JTMS 修复闭环不存在（真缺口）** |
| 模型档案 | `xai-grok-memory/src/runtime_profile.rs` | 模型档案（Profile 扩展点） |
| 证据链 | `xai-grok-memory/src/lifecycle_journal.rs` | prev-hash 防篡改链 v2（事件扩展点） |
| 效果恢复 | `xai-grok-memory/src/effect_recovery.rs` | EffectClassInventory（Pure/Idempotent/Queryable/Opaque） |
| 义务覆盖 | `xai-grok-memory/src/obligation_coverage.rs` | ObligationCoverage + never_attempted（验证义务扩展点） |
| 优先级老化 | `xai-grok-memory/src/class_fairness.rs` | aging（Governor 信号） |
| 预算 | `xai-grok-memory/src/tool_contract.rs` | TurnContextBudgetV1（max-output 守卫接线点） |
| 目标回退 | `xai-grok-shell/src/session/.../goal/` | goal backoff / 无进展暂停（Governor 信号） |
| eval | `scripts/eval-coding.sh` / `eval-coding-live.sh` | 基线脚本（A1 扩展点） |
| soak | `scripts/smoke-deepseek-l5.sh` | L5 模式浸泡（每周期复用） |

---

## 3. Cycle A 合同

### A1 基线实测（⑥）—— 无依赖，最先，唯一入口

**目标**：建立可复现对照锚点，让一切"强化有效"的声称可 falsify。

**交付**：
1. `docs/eval/anchor-set.md`：固定 20 题锚点集登记（题目原文 + 判定标准 + 难度分布）。**禁改**；改题需新 DEBT 登记。
2. `scripts/eval-coding.sh` 扩展：输出 `EvalRun` JSON 至 `evidence/eval/`：
   `run_id / profile / tasks[] / aggregate{pass_rate, total_input_tokens, total_output_tokens, avg_cache_hit_ratio, avg_verify_count, avg_latency_ms}`。
3. 基线一次完整运行，JSON 入证据。

**验收断言**（全可证伪）：
- 同一配置连续 2 次运行 `pass_rate` 波动 ≤ ±5%（可复现性）。
- JSON schema 稳定（readiness 可解析）。
- 基线数据进入 `verify-readiness.sh` 可视范围。

**失败判定**：跑不出 / 不可复现 → 修脚本，**禁止进入 A2**。

### A2 缓存稳定（①）—— 接线 + 不变量 + 参数，非从零建

**目标**：前缀字节稳定 + 压缩分级 + cache-reset 治理化。

**交付**：
1. **审计交付**（第一步，单独提交）：`cache_epoch.rs` + `lumen-discipline` 三文件职责边界图，确定三区视图落在哪（最小侵入：优先扩展 cache_epoch.rs / session_cache.rs，**拒绝新建 StableContext 第二权威结构**——三区只是 SessionActor 持有的视图）。
2. **渲染确定性**：`request_prefix` 现有 `tools_fingerprint_json` 已给 schema 指纹；补：manifest 头部（objective→硬约束→上下文）渲染顺序确定性 + 冻结断言。
3. **压缩 policy**：`compaction.rs` 扩展为 `CompactionPolicy{level1_threshold(初值 50k 绝对 stale token), level2, level3, remaining_budget_trigger(0.4×window), never_fold_user=true}`；绝对 token + 剩余预算双驱动；user turn/digest 永不折叠；压缩边界回退对齐 tool_call/tool_result 对，**孤儿 → fail-closed（不发请求）**；被 snip 完整内容以 hash+sequence 归档。
4. **Journal 事件**（新事件类，serde default 兼容）：`ContextResetV1{reason, old_epoch, new_epoch}`、`ToolResultSnipV1{original_sequence, content_hash, causal_parent}`、`CacheHealthSample{hit_tokens, miss_tokens, hit_ratio}`。
5. **CacheHealth 接线**：`SessionCacheTracker.observe` 接 `usage.prompt_cache_hit/miss_tokens` → session metrics + CacheHealthSample 事件。

**验收断言**（可 falsify）：
- property：≥100 次随机状态变化（新增工具/goal 更新/多轮对话）→ `header_hash(512)` 0 次变化（复用 capture_shape/compare_shape）。
- 压缩后因果链完整可回放：被 snip 内容可从 Journal 恢复（replay 测试扩展）。
- 同一 20 题任务连续 10 轮 `hit_ratio ≥ 90%`。
- 压缩前后孤儿 tool_calls 数 = 0。
- 压缩事件必含完整 snipped_hashes（INV-CS-05）。

**失败判定**：`hit_ratio < 85%` 连续 3 轮 → 告警 + 压缩策略降级（记录为已知限制，不静默）。

### A3 Profile + Adaptive Effort（③）—— 官方参数 + 治理化

**目标**：官方 agentic 参数落地 + effort 动态分档被治理。

**交付**：
1. `runtime_profile.rs` 扩展 `ModelProfile`：`id="deepseek-v4-flash-0731"`、`context_window=1048576`、`max_output`（按 API 语义 + TurnContextBudget 守卫定，**拒绝 384K**——那是本地 ThinkMax 上下文推荐非输出上限）、`sampling{temp=1.0, top_p=0.95}`、`thinking{Enabled, default=High}`、`effort_policy=Adaptive`、`verify_policy`、`cache_policy=StrictPrefix`。
2. `EffortController` 决策表：escalate（goal 复杂度超阈值 / 连续验证失败 ≥2 / repair loop 激活）；demote（remaining_output_budget 低于阈值 / hit_ratio<85% / turn 预算紧张）；每次变更 → `EffortChanged{from, to, reason, signals_snapshot, causal_parent}` 事件（INV-EP-01）。
3. 请求体透传 `thinking.type` + `reasoning_effort`；`reasoning_content` 流式解析补齐。
4. **reasoning_content 历史回传格式实测**（对照官方 API 文档）：带/不带回传各跑一轮 20 题，确认缓存与行为影响 → 实测结论入 A3 交付物（**这是 A2 缓存稳定与 B2 闭环之间的隐性依赖，实测为准**）。
5. max effort → 强制 `verify_policy=VerifyFirst`（INV-EP-02）；budget 低于阈值禁止升 max（INV-EP-03）。

**验收断言**：
- 抓真实请求体：temp/top_p/reasoning_effort/thinking 全对（INV-EP-04）。
- Adaptive vs Fixed(High) 对照 20 题：输出 token 消耗下降且 pass_rate 不降。
- `/goal` 长任务（≥20 轮）不触发输出上限（守卫生效）。

**失败判定**：Adaptive 导致 pass_rate 下降 ≥3 点 → 回退 Fixed(High)，触发器参数重校准再试。

---

## 4. Cycle B 合同

### B1 锚点前置（②）—— 依赖 A2 property harness

**交付**：
1. renderer 强制顺序 `objective → 硬约束 → 任务上下文 → 其余`，测试锁定。
2. 每轮 user turn 头部"当前任务摘要"（1–2 句），动态细节尾部。
3. property：头部 500 token 字节不变（复用 A2 harness）。

**验收**：20 题对照前置前/后：pass_rate 不降 + 成本下降（头部命中省 attention 预算）；property 0 次变异。

### B2 验证-修复闭环（④）—— 真缺口，最大工程包

**目标**：从"模型说做了"到"证据证明做对了"。

**交付**：
1. `lumen-verify` 管线（已核实 detect/repair/runner/steps）接 profile 触发：effort=max 强制 VerifyFirst（INV-VO-01）；`VerificationContract{tool_name, effect_class, required, max_repair_attempts=3}`。
2. 验证失败 → `ObligationManager.create_repair_obligation` → JTMS 修复子目标（接 TurnContextBudget，INV-VO-05）。
3. 超限 → `RepairExhausted` → **fail-closed**（INV-VO-03）；成功 → 签收证据成为后续 causal_parent（INV-VO-04）。
4. `VerifyEvent` 五件套（Started/Succeeded/Failed/RepairObligationCreated/RepairExhausted）进 Journal。
5. ObligationCoverage 扩展"验证义务覆盖率"（复用 never_attempted 语义）。

**验收断言**：
- 注入错误编辑 → 修复循环严格 ≤3 次，第 4 次 fail-closed（有事件）。
- 20 题 with/without：通过率上升且平均修复次数有界（≤2）。
- 验证过程 token 计入预算（INV-VO-05 断言）。

**失败判定**：修复循环失控（>上限仍前进）→ 直接判违约，回滚该包。

---

## 5. Cycle C 合同

### C1 双模型预设（⑤）
**交付**：`DualSessionConfig{planner, executor, plan_format=StructuredJson}`；`StructuredPlan{goal_id, steps[], success_criteria[], complexity}` 机器可解析；`PlanSubmitted / PlanAccepted` 事件；planner 只读工具集（readOnlyHint 过滤）+ 轻量预算；`CrossSessionLeakDetected` → fail-closed（INV-PE-03）。
**验收**：集成测试双 session 消息流不串 + prefix hash 独立（INV-PE-01/02）；注入串扰 → fail-closed。

### C2 Uncertainty Governor（横切）—— 轻量决策表，非新子系统
**交付**：信号聚合（goal_backoff 无进展计数 / class_fairness aging / A2 hit_ratio / A3 effort 变更 / B2 repair 深度，**全部现成**）→ 决策表 → 现有治理动作（Continue / EscalateEffort / PauseAndRequestHuman←对齐 M5/M6 / DemoteGoalPriority←aging 机制 / ForceCompaction / FailClosed）；`UncertaintyDetected / GovernanceActionTaken` 事件（INV-UG-01）。
**验收**：随机信号组合 → 动作在允许集；FailClosed 后不可自动恢复（INV-UG-02）。

### C3 Responses API 评估 + 全周期调优
**评估不承诺**：官方 Responses beta 状态、Pro 支持、缓存语义差异、reasoning_content 差异 → 评估报告 + 是否切主路径的决策记录。
**调优**：用 A/B/C 全程数据校准压缩阈值 / effort 触发器 / hit 告警线。

---

## 6. 不变量与事件登记

- 五组不变量（INV-CS-01..07 / INV-EP-01..04 / INV-VO-01..05 / INV-PE-01..04 / INV-UG-01..03）按所属周期进验收；新不变量登记进 `docs/nextgen/INVARIANTS.toml`，过 `check-invariant-manifest.sh`（36 → 递增，每周期一次）。
- 新事件类一律：append-only、serde default 向后兼容、prev-hash 链续接。
- 32 gates / 36 INV 清单在每周期 readiness 更新。

## 7. 每周期发布要求（与 v2.1.0 同标准）

`改代码 → 对照 eval（20 题 JSON 入 evidence）→ journal 断言 → 1h soak（smoke-deepseek-l5.sh）→ EVAL_LIVE=1 readiness → 证据提交 → push → CI 绿`。
M5/M6 人工门不阻塞工程周期（先发布后真人使用再关闭）。
周期规模纪律：单周期等效工作量超 2 周 → 拆子周期（记录在案，不许暗中膨胀）。

## 8. 明确不做（边界，任何评审不破）

Responses 一等公民（仅 C3 评估）、本地推理（ds4/dsgo）、模型侧研究（DeepSpec/Engram）、CL4R1T4S 类能力、多模型编排、vision 补齐、StableContext 第二权威结构。

## 9. 开工条件（Gate）

1. v2.1.0 发布尾全绿（soak 通过 → readiness → 证据 → push → CI）。
2. 本合同 + DEBT-033 已登记（本文件即登记物）。
3. 开工第一动作：A1 跑通 20 题基线 + A2 审计交付（cache_epoch.rs / lumen-discipline 职责图）。

---

## 附录 A：周期执行状态（2026-08-05 更新）

| 周期 | 状态 | 证据 |
|---|---|---|
| Cycle A（⑥①③） | ✅ **完成** | 提交 138b8273 + 9dd1efbe；基线 19/20 → A2 重跑 **20/20（100%）**，**avg_cache_hit_ratio 0.9845**（cache_health.jsonl 124 行 provider 真值，≥90% 验收 PASS）；memory 593、discipline 47、models 85、cache_epoch 15、compaction 13、profile 7 全绿；clippy 绿；审计 docs/nextgen/cache-audit.md |
| Cycle B（②④）核心 | ✅ **决策核心完成** | 提交 f4b7ebc6；RepairLoop 治理器 7 测（INV-VO-03/05、Inconclusive 防伪、interrupt 终态）+ journal verify 五种类；memory 594 绿；**集成接线（下周期）**：profile 触发 verify-first + JTMS 修复子目标 + 事件发射（lumen-verify 无 shell 调用点，钩子见 cache-audit §4） |
| Cycle B 剩余 | ⏳ 下周期 | B1 锚点前置（prompt 构造层定位 + property 测试）；B2 接线（mutations.rs 分档压缩同周期） |
| Cycle C（⑤+C2+C3） | ⏳ 下周期 | 双模型预设、Uncertainty Governor 决策表、Responses 评估、全周期调优 |
