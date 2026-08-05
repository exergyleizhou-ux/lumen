# C3：DeepSeek Responses API 评估报告（DEBT-033，2026-08-05）

> 性质：评估不承诺。结论为真实探针证据，决策记录见 §4。

## 1. 探针方法

对 `deepseek-v4-flash`（0731）以同一 1695-token 前缀（含 200 条系统指令 + 用户提示）分别请求：

- Chat Completions：`POST https://api.deepseek.com/chat/completions`（`reasoning_effort: high`）
- Responses API：`POST https://api.deepseek.com/v1/responses`（`output_config.effort: high`，`instructions` 承载系统文本）

间隔 6s，各 2 轮。原始响应存档于 scratch（cc-big-*.json / resp-big-*.json）。

## 2. 证据

| 路径 | 轮次 | input | 缓存命中 | 命中率 |
|---|---|---|---|---|
| Chat Completions | cc-1 | 1695 | 0 | 冷启动 |
| Chat Completions | cc-2 | 1695 | 1664 | **98.2%** |
| Responses | r1 | 1695 | 1664 | **98.2%** |
| Responses | r2 | 1695 | 1664 | **98.2%** |

补充观察：
- **跨路径缓存共享**：cc-2 持久化后，r1（Responses）立即命中——两条路径共享同一磁盘前缀单元。
- **小前缀不命中**：88-token 请求连续 3 次 cached=0（长度阈值行为；文档的"固定 token 间隔"持久化单元在该尺度下不成立）。
- Responses 路径 usage 以 `input_tokens_details.cached_tokens` 上报；Chat Completions 以 `prompt_cache_hit_tokens` / `prompt_cache_miss_tokens` 显式上报。
- Responses 输出形态：`reasoning` item + `message` item（标准 Responses 结构）；`output_tokens_details.reasoning_tokens` 正常。
- 与我们运行时兼容性：sampler 的 `TokenUsage::from` 已把 `prompt_tokens_details.cached_tokens` 映射进 `provider_cache_hit_tokens`（definitive 会计），两条路径都能喂给 A2 cache_health。

## 3. 与 Chat Completions 主路径的差异

| 维度 | Chat Completions（主路径） | Responses API |
|---|---|---|
| 缓存命中率（实测） | 98.2% | 98.2%（共享单元） |
| 缓存字段 | `prompt_cache_hit/miss_tokens` 显式拆分 | `input_tokens_details.cached_tokens` 单值 |
| 状态 | 稳定、生产路径、eval 锚点已建立 | public beta、仅 flash |
| 结构 | messages 数组 | input items（reasoning/message/tool_call） |
| 迁移成本 | — | 需新 wire 序列化路径 + tool call item 处理 + 双路径维护 |

## 4. 决策记录（C3 结论）

**维持 Chat Completions 为唯一主路径。** 理由：
1. 缓存命中率无差异（共享单元、实测相同）——"官方为 Codex 适配"的 Responses 在缓存收益上无增量。
2. Chat Completions 提供显式 hit/miss 拆分，直接支撑我们的 definitive 会计与 A2 cache_health 观测；Responses 的 `cached_tokens` 兼容字段会被我们的从映射正确吸收，但信息量更少。
3. Responses 为 beta 且仅 flash（Pro 未支持）；引入第二 wire 路径 = 双倍维护 + 序列化面扩大，违背"不做清单"里的最小面纪律。
4. 官方 Harness 基准（Terminal Bench 82.7 等）以 max effort + Chat Completions 语义跑出，我们 eval 锚点已对齐该语义。

**跟踪项**（Pro 支持 Responses / 缓存字段拆分后复审）：`docs/verification-debt.md` DEBT-033 C3 状态行更新即可，无需代码改动。

## 5. 附：探针可复现命令

```bash
# Chat Completions
curl https://api.deepseek.com/chat/completions \
  -H "Authorization: Bearer $DEEPSEEK_API_KEY" -H "Content-Type: application/json" \
  -d '{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"Reply with exactly: OK"}],"reasoning_effort":"high"}'
# Responses API
curl https://api.deepseek.com/v1/responses \
  -H "Authorization: Bearer $DEEPSEEK_API_KEY" -H "Content-Type: application/json" \
  -d '{"model":"deepseek-v4-flash","input":[{"role":"user","content":"Reply with exactly: OK"}],"output_config":{"effort":"high"}}'
```
