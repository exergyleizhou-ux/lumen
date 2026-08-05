# 缓存审计与接线图（DEBT-033 A2 审计交付，2026-08-05）

> 目的：记录 cache_epoch.rs / lumen-discipline 职责边界、A2 周期已落地接线、
> 剩余执行钩子。本文件是 Cycle A 的审计交付物（合同 §3 A2 交付 1）。

## 1. 职责边界图（审计结论）

| 层 | 文件 | 职责 | 权威性 |
|---|---|---|---|
| 请求证据 | `xai-grok-shell/src/session/cache_epoch.rs` | cache_epoch.json 持久化、load_or_rotate（域指纹轮换）、rotate_after_history_mutation、cache_request_evidence.jsonl（WireRequestSnapshot 落盘）、**cache_health.jsonl（A2-a 新增）** | 非权威（诊断/证据） |
| 请求观察者 | 同上 `DurableCacheEvidenceObserver` | 非阻塞队列 + NG-03E 投递观察，串行后台写入 | 非权威 |
| 前缀形状 | `lumen-discipline/src/cache_shape.rs` | PrefixShape / capture_shape / compare_shape / estimate_tokens / CacheDiagnostics | 纯逻辑 |
| 会话缓存跟踪 | `lumen-discipline/src/session_cache.rs` | SessionCacheTracker：滚动命中、稳定连胜、stability_score | 纯逻辑 |
| 前缀构造 | `lumen-discipline/src/request_prefix.rs` | join_system_texts / tools_fingerprint_json / shape_from_parts | 纯逻辑 |
| 缓存纪元域 | `xai-grok-shell/src/session/cache_epoch.rs` `CacheDomain` | provider/base_url/backend/model/effort/credential/permission/工具清单指纹 → 域指纹 | 非权威 |
| 运行时接线 | `xai-grok-shell/src/session/acp_session_impl/turn.rs` | observe_call（shape+usage）**已存在**；**cache_health.jsonl 写入（A2-a 新增）** | — |
| 进程级注册 | `xai-grok-shell/src/session/prompt_cache_registry.rs` | 每 session 跟踪器（LRU 256），last_snapshot 供 ACP/info | 非权威 |
| 压缩配置 | `xai-grok-shell/src/util/config/resolve/compaction.rs` | 阈值%（85 默认）、tool_choice、墙钟预算；**CompactionPolicy 分级解析（A2-b 新增）** | 配置 |
| 压缩执行 | `xai-chat-state/src/actor/mutations.rs:307` | RetainedToolPrune（turn 龄驱动 → HARD_CLEAR_PLACEHOLDER）+ 事件 | 状态机 |
| 压缩请求 | `xai-grok-shell/src/session/acp_session_impl/sampler_turn.rs:53` | CommittedHistoryMutation → WireMutationReason（ToolResultPruned/FullCompaction） | — |
| 事件链 | `xai-grok-memory/src/lifecycle_journal.rs` | prev-hash 防篡改链；**新种类 ContextReset/ToolResultSnip/CacheHealthSample + detail 字段（A2-c 新增）** | 权威 |
| 模型档案 | `xai-grok-shell/src/agent/models/profile.rs` | **ModelProfile + EffortController（A3 新增）** | 配置/纯逻辑 |

## 2. 审计发现（2026-08-05）

1. **CacheHealth 主接线已存在**：turn.rs 已调 `observe_call` 并传
   `definitive_provider_cache_hit_tokens()`（DeepSeek `prompt_cache_hit_tokens`
   显式字段权威，`CacheUsageTruth` 三态）。缺的是**持久化**——registry 是内存态，
   eval/审计读不到 → **A2-a 补齐：cache_health.jsonl 每轮落盘**。
2. **wire_common_prefix_bytes 恒为 None**：sampler client.rs:849 写死 None。
   内存态公共前缀诊断未启用（wire 层不保留前序字节）。保持 None 是安全的
   （无泄漏面），hit/miss 走 provider usage（A2-a 已覆盖），不修。
3. **压缩执行是 turn 龄驱动**（hard_clear_age_turns → placeholder），不是
   token 阈值驱动。A2-b 的 CompactionPolicy（绝对 token 三档 + 剩余预算双驱动）
   已作为纯逻辑 + 解析落地，**执行接线点 = mutations.rs:307**（下周期：
   按 stage_for 选择 L1 头尾标记 / L2 占位符；L1 归档完整内容进
   LifecycleJournal ToolResultSnip 事件 + 孤儿 tool_calls fail-closed 守卫）。
4. **lumen 拒绝相对 `--cwd`**（os error 2）——eval 脚本已归一化绝对路径
   （2026-08-05 基线事故根因）。
5. **serde 兼容纪律**：journal detail 字段 None 时不进 canonical 前像，
   存量事件 payload_hash 不变（测试锁定）。

## 3. A2 周期已落地清单

- [x] A2-a：`CacheHealthRecord` + `append_cache_health`（cache_epoch.rs，
  镜像 request-evidence 模式：append + sync_data）；turn.rs 每轮写入
  （truth 三态、hit/miss/output、hit_ratio）；测试 2 个（round-trip、目录创建）。
- [x] A2-b：`lumen-discipline::compaction::{CompactionStage, CompactionPolicy}` +
  `stage_for`（绝对 token 三档 + 剩余预算双驱动、never_fold_user 硬不变量）；
  shell `resolve_compaction_policy`（GROK_COMPACTION_POLICY 四元组，逐项回落）；
  测试 11 个（阈值边界/预算触发/1M 窗口/序列化/解析/硬不变量）。
- [x] A2-c：`GovernedLifecycleEventKind` 新增 ContextReset/ToolResultSnip/
  CacheHealthSample；`GovernedLifecycleEventV1.detail: Option<serde_json::Value>`
  （serde default 兼容，canonical 条件包含保旧哈希）；4 处构造点补 detail: None；
  测试 3 个（新种类链 + detail 参与哈希 + legacy 解码验证）。
- [x] A2-d：本审计文档。

## 4. 剩余执行钩子（下周期，精确到点）

| 钩子 | 位置 | 内容 |
|---|---|---|
| 分级压缩执行 | `xai-chat-state/src/actor/mutations.rs:307` | 按 CompactionPolicy::stage_for 选 L1/L2；L1 保留确定性头尾标记；完整内容 → journal ToolResultSnip |
| CacheReset 事件发射 | `cache_epoch.rs::load_or_rotate`（Rotated* 分支）| 轮换时发 ContextReset{reason, old/new epoch} |
| CacheHealthSample 事件发射 | turn.rs（A2-a 写入点旁）| 每轮健康记录同时发 journal 事件（detail 复用） |
| 孤儿 tool_calls 守卫 | 压缩执行处 | 压缩后 tool_call/tool_result 必须成对，孤儿 → fail-closed 不发请求 |
| property 头部不变性 | lumen-discipline（capture_shape 之上）| 随机状态变化 → header hash 不变（B1 复用） |

## 5. 验收状态（A2 合同项）

- property：头部 512 token 不变 → **B1 周期验收**（harness 与 capture_shape 已就绪）
- 压缩后因果链可回放 → 依赖 §4 执行接线（下周期）
- hit_ratio ≥90%（20 题连续 10 轮）→ 需 A2-a 二进制重建后实测（本次 eval 基线
  binary 不含 A2-a，hit 为 null；重建后同一锚点重跑即得真值）
- 孤儿 tool_calls = 0 → 随 §4 执行接线
