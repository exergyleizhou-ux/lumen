# Relay Station Config (0731 中转站)

Lumen/Grok TUI 通过第三方中转站 `sub2api.eqing.tech` 接入 20+ 模型的完整配置方案。
直连 `chat_completions` 后端，无需本地 relay。

## 文件说明

| 文件 | 用途 |
|------|------|
| `0731-relay-station.md` | 完整方案文档：密钥分组、模型清单、实测结果、修复记录 |
| `grok-config.toml` | `~/.grok/config.toml` 的脱敏副本 |
| `lumen-config.toml` | `~/.lumen/config.toml` 的脱敏副本（**权威配置**，覆盖 grok 配置） |
| `relay.py` | 备用本地 relay（当前方案不需要，仅存档） |

## 密钥占位符

所有 API key 已脱敏为占位符，使用时替换为你的实际 key：

| 占位符 | 对应分组 |
|--------|---------|
| `sk-YOUR_KEY_1_GROK` | Grok 4.5 / grok-imagine |
| `sk-YOUR_KEY_2_CN_MODELS` | GLM-5.2 / MiniMax-M3 / Qwen / Kimi K3 / DeepSeek |
| `sk-YOUR_KEY_3_GPT` | GPT 5.2~5.6 全系 / codex-auto-review |
| `sk-YOUR_KEY_4_CLAUDE` | Claude Opus/Sonnet/Haiku/Fable |
| `sk-YOUR_DEEPSEEK_FLASH_KEY` | DeepSeek V4 Flash 直连 |
| `sk-YOUR_KIMI_KEY` | Kimi 官方直连（已过期，改用 Key 2 中转） |

## 部署要点（踩坑记录）

1. **改配置必须两个文件一起改**：`~/.lumen/config.toml` 是权威，覆盖 `~/.grok/config.toml`
2. **minimax-m3-proxy 需要环境变量**：`export MINIMAX_API_KEY="sk-YOUR_KEY_2_CN_MODELS"`（写入 `~/.zshenv`），否则 lumen 会用 OIDC token 劫持认证头导致 401
3. **claude-opus-5-proxy 不能带 `reasoning_effort` 参数**（上游 400），config 中已移除
4. **kimi-k3 默认模型**走中转站 Key 2（官方 key 已过期）
5. 上游延迟 3-8s 属正常；`gpt-5.2/5.4-mini/5.6-luna/5.6-terra`、`claude-fable-5/sonnet-4/sonnet-4-5` 当前上游 502，需联系中转站管理员
