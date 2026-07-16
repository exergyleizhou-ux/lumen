# 多模型适配（Multi-provider）

Lumen **不是**只能用 DeepSeek。  
**默认**选 DeepSeek，是为了长会话 **prompt cache 命中率高**（Reasonix 纪律），你自己现在主力也可以是 DeepSeek——但引擎按 **BYOK + 多后端** 设计。

## 支持的协议

| `api_backend` | 协议 | 典型厂商 |
|---------------|------|----------|
| `chat_completions`（默认） | OpenAI `/v1/chat/completions` | OpenAI、DeepSeek、xAI、GLM、Qwen、Ollama、vLLM… |
| `messages` | Anthropic Messages | Claude |
| `responses` | OpenAI Responses | 部分 OpenAI 新路径 |

自定义任意厂商：在 `~/.grok/config.toml` 写 `[model.xxx]`，填 `base_url` + `model` + `env_key`。

## 内置目录（`default_models.json`）

| ID | 厂商 | 环境变量（示例） |
|----|------|------------------|
| `deepseek-chat` / `deepseek-reasoner` | DeepSeek | `DEEPSEEK_API_KEY` |
| `openai-gpt4o` / `openai-gpt4o-mini` | OpenAI | `OPENAI_API_KEY` |
| `claude-sonnet` / `claude-opus` | Anthropic | `ANTHROPIC_API_KEY` |
| `xai-grok` | xAI | `XAI_API_KEY` |
| `glm-4` | 智谱 | `ZHIPUAI_API_KEY` |
| `qwen-plus` / `qwen-max` | 通义 | `DASHSCOPE_API_KEY` |
| `mimo` | 小米 MiMo | `MIMO_API_KEY` |
| `ollama` | 本地 Ollama | 可选 `OLLAMA_API_KEY` |
| `local-openai` | vLLM / llama.cpp 等 | `LOCAL_API_KEY` |

完整示例：`config/lumen.example.toml`。

## 怎么切换

```bash
# 会话默认仍是 deepseek-chat；临时换模型：
lumen -m openai-gpt4o "fix the flaky test"
lumen -m claude-sonnet
lumen -m ollama
lumen -m qwen-plus

# TUI 内
/model claude-sonnet
# 或 Ctrl+M 打开 model picker
```

持久默认（例如你主力改成 Claude）：

```toml
[models]
default = "claude-sonnet"
```

## Ollama 本地开源模型

```bash
ollama pull qwen2.5-coder
ollama serve
export OLLAMA_API_KEY=ollama   # 若端点要求非空 key
lumen -m ollama
```

在 config 里把 `model = "qwen2.5-coder"` 改成你 `ollama list` 里的名字。

## 加一个新厂商（三分钟）

```toml
[model.my-provider]
model = "their-model-id"
name = "My Provider"
base_url = "https://api.example.com/v1"
api_backend = "chat_completions"   # Claude 用 "messages"
env_key = "MY_PROVIDER_API_KEY"
```

```bash
export MY_PROVIDER_API_KEY=...
lumen -m my-provider
```

## 和「DeepSeek 第一公民」的关系

- **默认 ID** = `deepseek-chat`：冷启动、新手路径、cache 演示走这条。  
- **能力** = 全目录 + 任意 OpenAI/Anthropic 兼容端点。  
- Reasonix **cache 可见 / 前缀稳定** 对 DeepSeek 优化最深；换别的模型同样能干活，cache 统计取决于该厂商是否返回 `cached_tokens`。

## 安全提醒

- Key 放环境变量，不要写进 git。  
- 各厂商 tool-calling 质量不同；Critical-0 安全轨在 agent 侧，与模型无关。  
- 换模型后建议再跑：`./scripts/smoke-deepseek-agent.sh` 的同类检查（或对该 key 做一次 `-p` + 工具任务）。
