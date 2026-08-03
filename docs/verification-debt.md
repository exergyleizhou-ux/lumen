# Verification Debt 追踪（2026-08-03）

**性质：** 所有切片审查中登记的未关闭债务，统一追踪。S10 将把它做成 read model（代码+报告）；关闭一项即从本表移除或标 closed。
**目标状态：** 全部 debt == 0（Blocked/Frozen/unverified patch/未消费 advice/NOT RUN gate）后，才可称 "Harness Kernel local-ready"（交接词 §8）。

## 债务清单

| ID | 来源 | 债务 | 状态 | 关闭条件 |
|----|------|------|------|----------|
| DEBT-001 | S8 Critic | advice-epoch 生产写侧缺失：`advice_issued_policy_epoch`/`live_policy_epoch` 无生产写入点，真实运行中 `stale_advice` 恒 false（tool_context.rs:243-248；sampler_turn.rs:1590-1600） | open | S9 接线 `issue_shadow_advice` 写 issued + 策略变更 bump live，并加生产路径测试证明 `stale_advice` 可触发 |
| DEBT-002 | S8 Critic | SamplerActor 循环级 counting-server fixture 未加：`run_turn_via_sampler` 的 401→refresh→resubmit 端到端计数无自动化断言（决策表已测，循环控制流仅代码审读） | open | 真实 HTTP counting-server fixture 覆盖：resubmit 用新 request_id、resubmit 跳过 handle_sampling_failure、第二次 401 落 terminal |
| DEBT-003 | S8 Critic | `P0_NR_A_FULL_AUDIT_GATE` 名不副实：gate 内只精确断言 pin + existing output 两条 deny，pool/breaker/schema/stale 未进 offline gate | open | gate 内补全六条拒绝矩阵（断言精确 reason） |
| DEBT-004 | S8 Critic | `not_attempted: True` 与事实不符：失败路径从不 mark_attempt_started，而请求实际已发 provider（可能计费）；"clean failure ≠ not attempted" 语义未定义 | open | 在 receipt/注释显式定义语义，或加字段区分 transport-no-start 与 provider-attempted-clean |
| DEBT-005 | S8 Critic | `SealedAttemptReceiptStore` 无父目录 fsync、快照全量重写、无增长上限 | open | 父目录 fsync + 记录数上限（超限 fail-closed 或归档） |
| DEBT-006 | S8 Critic | `AttemptSealTracker::into_receipt` 死代码（全仓无调用点） | open | 删除或接线 |
| DEBT-007 | S8 复审 | 双套 epoch 记账并存：ShadowAdvisorHost 自带 live_policy_epoch/last_advice_policy_epoch（仅测试构造）vs ToolContext 原子（生产读取），S9 有分叉风险 | open | S9 统一单写者（ToolContext 原子），删除/注释 ShadowAdvisorHost 字段 |
| DEBT-008 | S8 复审 | 失败路径无 drain barrier：capture 读取与在途 ToolCallDelta 事件存在理论竞态（observation_complete 恒 true 但失败时未等 drainer 消费完） | open | Failed 分支同样触发 drain oneshot 并 await 再读 capture |
| DEBT-009 | S8 复审 | L3 测试是私有函数镜像复制（s8_sealed_retry_live_tests.rs:240-255），可漂移 | open | `seal_observations_from_streaming_capture` 提为 `pub(crate)` 直测 |
| DEBT-010 | S8 复审 | `decide_auth_class_retry` 的 `Ok(0)` 分支不可达（防御性死分支） | open | 加注释说明（无害） |

## 登记记录（追加式）

- 2026-08-03：S8 提交后（bf489044/1d2921af），Critic 两轮审查的 10 项债务全部登记。DEBT-001/002 为 Medium 级声明遗留，DEBT-003..010 为 Low/Info 级。
