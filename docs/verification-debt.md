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
| DEBT-011 | 全维度100% A2 | ToolContract 生产 dispatch | **closed (2026-08-04)** | shell prepare_tool_call 强制 authorize_tool_dispatch；结果 clamp_tool_result_text；offline TOOL_CONTRACT_DISPATCH_GATE=PASS |
| DEBT-012 | 全维度100% A4 | mailbox 有界 + FLOW_CONTROL | **closed (2026-08-04)** | flood/two-tree/shutdown fixtures + offline FLOW_CONTROL_GATE=PASS |
| DEBT-013 | 全维度100% A5–A8 | adapter 全强制 / Expert repair / advisor ToolRegistry+UI | **closed (2026-08-04)** | nextgen_exit_gates + offline A5–A8 gates；shell resume→authorize_context_rebuild；expert repair→authorize_expert_repair_pass；ConsultAdvisorHost lumen_advisor_consult 工具入口；SCRATCH offline-contract-gates-full.json 17/17 |
| DEBT-014 | 全维度100% A9–A12 | recommend/Kairos/NG-09B/release 全链 | **closed (2026-08-04)** | authorize_applied_assignment_chain + operator_control_five_command_matrix + kairos_fake_clock_lease_cycle + authorize_rollback_receipt；offline A9_A11/A10/A12 gates PASS；正式 installer/UI 可选路径不阻塞 pure Exit Gate |
| DEBT-015 | 全维度100% C1 | 正式 v2.0.0 tag 未打 | **closed (2026-08-04)** | `scripts/release.sh 2.0.0` 已完成：source A `d42ea6e8` + evidence B `77a81393`；signed tag `v2.0.0` 指向 A 并已 atomic push origin；本地 binary `lumen 2.0.0 (d42ea6e8)` tuple 绿 |
| DEBT-016 | 全维度100% D1–D2 | probe 收据 / grok-4.5 交叉审查 | **closed (2026-08-04)** | D1: docs/evidence/reducer-purity-probe-2026-08-03.json 入库；D2: DeepSeek V4 Flash + Grok 4.5 交叉验收记录见 SCRATCH/cross-accept-*.md |

## 登记记录（追加式）

- 2026-08-03：S8 提交后（bf489044/1d2921af），Critic 两轮审查的 10 项债务全部登记。DEBT-001/002 为 Medium 级声明遗留，DEBT-003..010 为 Low/Info 级。
- 2026-08-04（全维度100%）：关闭 P1–P5；A1 CapabilityGrantV1；A2 契约层 API；A3 BudgetLedger 既有真入口。DEBT-011..016 登记剩余 Exit Gate。
- 2026-08-04 续：A2 生产 shell 接线 + A4 FLOW_CONTROL_GATE + fixtures；DEBT-011/012 closed。
- 2026-08-04 终：A5–A12 Exit Gates（nextgen_exit_gates + offline 17 gates + shell A5/A7/A8 真入口）；DEBT-013/014/016 closed；DEBT-015 正式 tag 仍 open（仅 dry-run）。
- 2026-08-04 正式版：`v2.0.0` signed tag → `d42ea6e8`，push origin；DEBT-015 closed；verification_debt 全 closed。
