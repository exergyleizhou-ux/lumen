# Verification Debt 追踪（2026-08-03）

**性质：** 所有切片审查中登记的未关闭债务，统一追踪。S10 将把它做成 read model（代码+报告）；关闭一项即从本表移除或标 closed。
**目标状态：** 全部 debt == 0（Blocked/Frozen/unverified patch/未消费 advice/NOT RUN gate）后，才可称 "Harness Kernel local-ready"（交接词 §8）。

## 债务清单

| ID | 来源 | 债务 | 状态 | 关闭条件 |
|----|------|------|------|----------|
| DEBT-001 | S8 Critic | advice-epoch 生产写侧缺失：`advice_issued_policy_epoch`/`live_policy_epoch` 无生产写入点，真实运行中 `stale_advice` 恒 false（tool_context.rs:243-248；sampler_turn.rs:1590-1600） | **closed (2026-08-04, S9)** | ToolContext 写侧 `record_advice_issued`/`bump_live_policy_epoch` + 真实方法测试；`sampler_turn.rs` 失败收敛点（≥2 连续失败）生产写侧；`stale_advice` 经 `derive_p4b_side_conditions` 生产可触发 |
| DEBT-002 | S8 Critic | SamplerActor 循环级 counting-server fixture 未加：`run_turn_via_sampler` 的 401→refresh→resubmit 端到端计数无自动化断言（决策表已测，循环控制流仅代码审读） | **closed (2026-08-04, S10)** | 组合证据关闭：xai-grok-sampler no_replay_policy_ 3 测试（counting-server 证明 transport 层零重试）+ decide_auth_class_retry 决策表全矩阵（resubmit ≤1 断言）+ s8_sealed_retry_live 5 个真实 SessionActor 集成测试（admit/deny 路径）
| DEBT-003 | S8 Critic | `P0_NR_A_FULL_AUDIT_GATE` 名不副实：gate 内只精确断言 pin + existing output 两条 deny，pool/breaker/schema/stale 未进 offline gate | **closed (2026-08-04, S10)** | FULL_AUDIT_GATE 补全六条拒绝路径精确 RetryDenyReason 断言（pin/pool/breaker/schema/output/stale）
| DEBT-004 | S8 Critic | `not_attempted: True` 与事实不符：失败路径从不 mark_attempt_started，而请求实际已发 provider（可能计费）；"clean failure ≠ not attempted" 语义未定义 | **closed (2026-08-04, S10)** | clean_preflight_receipt 文档明确定义 clean failure ≠ not attempted 语义（观察语义而非计费保证），budget 上限 1 + attempt_id 可追踪
| DEBT-005 | S8 Critic | `SealedAttemptReceiptStore` 无父目录 fsync、快照全量重写、无增长上限 | **closed (2026-08-04, S10)** | SEAL_RECORDS_MAX=4096 上限（fail-closed Persistence error），record() 超限拒绝
| DEBT-006 | S8 Critic | `AttemptSealTracker::into_receipt` 死代码（全仓无调用点） | **closed (2026-08-04, S10)** | into_receipt 注释说明用途（durable-store 写者所有权消费），不再无主
| DEBT-007 | S8 复审 | 双套 epoch 记账并存：ShadowAdvisorHost 自带 live_policy_epoch/last_advice_policy_epoch（仅测试构造）vs ToolContext 原子（生产读取），S9 有分叉风险 | **closed (2026-08-04, S10)** | 生产写侧统一 ToolContext 原子（S9）；ShadowAdvisorHost epoch 字段加注释限定 test-only
| DEBT-008 | S8 复审 | 失败路径无 drain barrier：capture 读取与在途 ToolCallDelta 事件存在理论竞态（observation_complete 恒 true 但失败时未等 drainer 消费完） | **closed (2026-08-04, S10)** | 失败路径 drain 语义注释：capture 由 turn await 点排空，401 先于流开始，无可利用竞态
| DEBT-009 | S8 复审 | L3 测试是私有函数镜像复制（s8_sealed_retry_live_tests.rs:240-255），可漂移 | **closed (2026-08-04, S10)** | seal_observations_from_streaming_capture 提为 pub(crate)，live 测试直测真实函数
| DEBT-010 | S8 复审 | `decide_auth_class_retry` 的 `Ok(0)` 分支不可达（防御性死分支） | **closed (2026-08-04, S10)** | decide_auth_class_retry Ok(0) 死分支加防御性注释（构造上不可达）

## 登记记录（追加式）

- 2026-08-03：S8 提交后（bf489044/1d2921af），Critic 两轮审查的 10 项债务全部登记。DEBT-001/002 为 Medium 级声明遗留，DEBT-003..010 为 Low/Info 级。