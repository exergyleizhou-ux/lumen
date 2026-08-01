# 0731 中转站配置 — Lumen Proxy 模型完整方案

## 最终方案
`chat_completions` 后端直连 `sub2api.eqing.tech/v1`，无需 relay、无需 launchd。

---

## API Keys（4 把中转站 + 1 把 DeepSeek Flash 新 key）

### Key 1 — Grok 4.5
```
sk-YOUR_KEY_1_GROK
```
模型: grok-4.5, grok-imagine-image, grok-imagine-image-lite

### Key 2 — 国产模型
```
sk-YOUR_KEY_2_CN_MODELS
```
模型: GLM-5.2, MiniMax-M3, qwen3.8-max-preview, deepseek-v4-pro, kimi-k3

### Key 3 — GPT 系列
```
sk-YOUR_KEY_3_GPT
```
模型: gpt-5.2~5.6 全系列, codex-auto-review

### Key 4 — Claude 系列
```
sk-YOUR_KEY_4_CLAUDE
```
模型: claude-opus-4-8~5, claude-sonnet-4~5, claude-haiku-4-5, claude-fable-5

### DeepSeek V4 Flash 正式版 (0731)
```
sk-YOUR_DEEPSEEK_FLASH_KEY
```

---

## 当前配置状态

### 直接模型（未改动）
- `deepseek-v4-pro` — `api.deepseek.com/v1`, `DEEPSEEK_API_KEY`
- `deepseek-v4-flash` — `api.deepseek.com/v1`, 新 key (0731正式版), `temperature=1.0, top_p=0.95`
- `deepseek-chat` — `api.deepseek.com/v1`, `DEEPSEEK_API_KEY`
- `grok-4.5` — `api.x.ai/v1`, SuperGrok (额度已耗尽)
- `kimi-k3` — `api.kimi.com/coding/v1`, `KIMI_CODE_API_KEY`

### 23 个 Proxy 模型（今 天新增）
均使用:
```toml
base_url = "https://sub2api.eqing.tech/v1"
api_backend = "chat_completions"
supports_reasoning_effort = true
reasoning_effort = "high"
```
各模型 `api_key` 见上表。不在正文赘述，grep config 即可。

### 其他改动
- `[features] remote_fetch = false` — 关闭启动时 xAI 模型列表拉取（SuperGrok 额度耗尽后避免 login 弹 窗）
- `default = "grok45-proxy"` in ~/.grok/config.toml
- `default = "kimi-k3"` in ~/.lumen/config.toml

---

## 已验证可用的模型（0731 全量实测，27 模型并发扫描）

### ✅ 可用（20 个）
| 模型 | 延迟 | 备注 |
|------|------|------|
| deepseek-v4-pro | 1s | 直连，推理模型 |
| deepseek-v4-flash (0731) | 1s | 直连，新 key 正常 |
| deepseek-chat | 1s | 直连 |
| grok45-proxy | 7s | **已恢复且大幅提速（原 151s）** |
| glm52-proxy | 4s | |
| minimax-m3-proxy | 5s | |
| qwen3-max-proxy | 4s | |
| gpt53spark-proxy | 3s | |
| gpt54-proxy | 6s | |
| gpt55-proxy | 8s | |
| gpt56-proxy | 6s | |
| gpt56sol-proxy | 7s | |
| codex-auto-review-proxy | 5s | |
| claude-opus-5-proxy | 3s | |
| claude-sonnet-5-proxy | 3s | |
| claude-sonnet-4-6-proxy | 3s | |
| claude-opus-4-8-proxy | 6s | |
| claude-opus-4-7-proxy | 4s | |
| claude-haiku-4-5-proxy | 3s | |
| kimi-k3 | 3s | **修复后走中转站 Key 2** |

### ❌ 不可用（7 个，均为中转站账户侧问题，本地无法修复）
| 模型 | 错误 |
|------|------|
| gpt52-proxy | 502 Upstream access forbidden |
| gpt54mini-proxy | 502 Upstream access forbidden |
| gpt56luna-proxy | 502 Upstream access forbidden |
| gpt56terra-proxy | 502 Upstream access forbidden（原 503，未恢复） |
| claude-fable-5-proxy | 502 All available accounts exhausted |
| claude-sonnet-4-proxy | 502 All available accounts exhausted |
| claude-sonnet-4-5-proxy | 502 All available accounts exhausted |

### 其他结论
- **grok-4.5 SuperGrok（api.x.ai 直连）**：仍 403，团队账户无额度 → 用 grok45-proxy 替代，无需处理
- **上游延迟已大幅改善**：全部可用模型 3-8s 内响应（原 60-180s）
- 注意：deepseek-v4-pro/flash、glm52 等推理模型，`max_tokens` 过小时输出为空（thinking 占满），属正常现象，非故障

---

## 曾尝试但失败/不需要的方案
- ❌ `responses` 后端 → serialization error
- ❌ `messages` 后端 → empty response (thinking block 解析问题)
- ❌ 本地 relay → 技术可行但上游太慢不实用
- ✅ **最终方案**: `chat_completions` + `api_key` 直连

---

## 遗留文件
| 文件 | 用途 | 是否需要 |
|------|------|---------|
| `~/.lumen/scripts/proxy-relay.py` | 本地 relay | ❌ 不再需要 |
| `~/.lumen/scripts/proxy-relay-launcher.sh` | relay 启动脚本 | ❌ 不再需要 |
| `~/Library/LaunchAgents/com.lumen.proxy-relay.plist` | launchd 配置 | ❌ 不再需要 (已 unload) |
| `~/.grok/config.toml` | 主配置 | ✅ 核心文件 |
| `~/.lumen/config.toml` | Lumen 配置 | ✅ 核心文件 |

---

## 0731 修复记录（本会话完成，两轮）

### 🔧 第一轮：kimi-k3 默认模型修复
- **问题**：`KIMI_CODE_API_KEY` 已过期（401），默认模型不可用
- **修复**：kimi-k3 改走中转站 Key 2（model="kimi-k3"），api_key 硬编码，别名与 default 不变
- **改动**：`~/.grok/config.toml`、`~/.lumen/config.toml`、`~/.grok/creds/kimi-k3`、桌面副本

### 🔧 第二轮：上游 502 之外的两个"我们侧"隐藏故障

**1. claude-opus-5-proxy：400 Bad Request（reasoning_effort 参数被拒）**
- 定位：参数隔离测试发现 `reasoning_effort:"high"` 与 `temperature` 对 claude-opus-5 均被中转站拒绝（其他 Claude 模型不受影响）
- 修复：从该模型 config 移除 `supports_reasoning_effort` / `reasoning_effort`

**2. minimax-m3-proxy：401（OIDC JWT 劫持认证头）**
- 定位：本地假上游抓包，lumen 发出的 `authorization` 是 xAI OIDC JWT 而非 config 的 api_key
- 根因：lumen 内置 `minimax-m3` 定义（Anthropic 后端 + MINIMAX_API_KEY env）与 model 名 `MiniMax-M3` 匹配，认证层忽略硬编码 api_key 回退 OIDC
- 修复：`~/.zshenv` 增加 `export MINIMAX_API_KEY="sk-YOUR_KEY_2_CN_MODELS"`
- 注意：minimax 的 config base_url 修改须**两个配置文件都改**——lumen 以 `~/.lumen/config.toml` 为权威（覆盖 ~/.grok）

### 最终状态：我们侧 21/21 模型端到端验证通过
kimi-k3、deepseek-v4-pro/flash/chat、grok45-proxy、glm52-proxy、minimax-m3-proxy、qwen3-max-proxy、gpt53spark/54/55/56/56sol-proxy、codex-auto-review-proxy、claude-opus-5/sonnet-5/sonnet-4-6/opus-4-8/opus-4-7/haiku-4-5-proxy + 默认模型、lumen-kimi 包装脚本、全新 zsh 会话均返回 OK

### 待办结论
1. ✅ `deepseek-v4-flash` 新 key 正常（1s）
2. ❌ `gpt56terra-proxy` 未恢复（仍 502 forbidden，中转站账户侧问题）
3. ❌ `grok-4.5` SuperGrok 未恢复（403 无额度）→ grok45-proxy 已替代，无需处理
4. ✅ 上游延迟大幅改善（3-8s，原 60-180s）

### ⚠️ 遗留提醒
- 7 个不可用模型（gpt52/gpt54mini/gpt56luna/gpt56terra forbidden、claude-fable-5/sonnet-4/sonnet-4-5 耗尽）需联系中转站管理员，本地无法修复
- `~/.zshrc` / `~/.zshenv` 中旧 KIMI_CODE_API_KEY 已不影响 lumen；`~/.local/bin/lumen-kimi` 包装脚本同理
- relay.py / launchd 相关文件确认为遗留物，未删除
