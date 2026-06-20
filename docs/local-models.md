# 本地模型能力矩阵(Local model capability matrix)

> 本轮主引擎是**本地模型**(免费、无限),DeepSeek 仅作偶尔参照。
> 本文回答一个决定性问题:**哪个本地模型真能驱动 lumen 的 agent 循环?**

## 为什么"能聊天"不等于"能驱动 agent"

lumen 的 agent 循环靠**工具调用**推进:模型必须发出 OpenAI 风格的 `tool_call`
(`edit_file`、`read_file`、`run_bash`…)才能真正改代码。一个只会输出散文的模型可以
**"嘴上说"**它要改某个文件,却永远调不动 `edit_file` —— 它能聊天/补全,但**不能驱动 agent**。

所以矩阵里最关键的一列是 **「Drives agent (tool_call)」**:✅ 表示该模型在收到工具定义 +
"用 edit_file 改这行"的指令后,真的发出了 `tool_call`;❌ 表示它只回了散文。

第二关键的是**吞吐(tokens/sec)与延迟**:本地 27B 在 Mac/Metal 上比云 API 慢,
**新瓶颈是延迟不是钱**(cost ≈ $0)。

## 如何接入本地模型

本地端点都讲 **OpenAI 兼容协议**,空/假 key 即可。三种内置预设(`lumen probe-local` 会逐个探测):

| 预设名 | 运行方式 | base_url |
|---|---|---|
| `lmstudio` | LM Studio → GUI 里 **Start Server** | `http://localhost:1234/v1` |
| `ollama` | `ollama serve` | `http://localhost:11434/v1` |
| `vllm` | `vllm serve <model>` | `http://localhost:8000/v1` |

在 `lumen.toml` 里直接用预设,或手写 provider:

```toml
[[providers]]
name     = "lmstudio"
kind     = "openai"
base_url = "http://localhost:1234/v1"
api_key  = "lm-studio"   # 本地端点忽略它;空着也行
model    = "qwen3.6-27b" # 留空则用 /v1/models 返回的第一个
```

切换:`lumen` 启动后 `/model lmstudio`,或配置 `default_model = "lmstudio"`。

## 如何生成 / 刷新矩阵

模型就绪 + 在 LM Studio 里 **Start Server** 后:

```bash
lumen probe-local                 # 探测全部内置本地预设
lumen probe-local --base-url http://localhost:1234/v1 --model qwen3.6-27b
lumen probe-local --json          # 机器可读(喂给 S1 eval / CI)
```

探测做的就是一个最小 agent 任务:**给一个 `edit_file` 工具 +「改 main.go 第 3 行」的指令**,
看模型是否发出 `tool_call`,并测吞吐/延迟。把下面表格里的占位行替换成 `probe-local` 的实测输出即可。

## 矩阵(实测,model-gated)

> 状态:**⏳ 待实测** —— `qwen/qwen3.6-27b`(~16GB)正在下载;就绪 + LM Studio Start Server 后跑
> `lumen probe-local` 填表。代码与探测器现已就位、单测通过,只差活模型那一脚。

| Endpoint | Model | Drives agent (tool_call) | tokens/sec | latency | Notes |
|---|---|---|---|---|---|
| lmstudio | qwen/qwen3.6-27b | ⏳ 待实测(预期 ✅,Qwen 系以 tool-use 著称) | ⏳ | ⏳ | **eval 基线 + dogfood 主力** |
| lmstudio | gemma-4-12b-coder | ⏳ 待实测(⚠️ 风险:Gemma function-calling 历史偏弱) | ⏳ | ⏳ | 见下方适配层评估 |
| lmstudio | gemma-3 / 更小 | ⏳ 待实测 | ⏳ | ⏳ | 快速冒烟 / 对照 |

填好后,**只有「Drives agent ✅」的模型进 S1 的 agent-eval**;❌ 的降级为对话/补全对照或标 gated。

## Gemma 若发不出 `tool_call`:轻量适配层评估(评估,不实现)

**结论:暂不实现。** 仅当实测确认 Gemma 是主力候选、且其它 ✅ 模型都不够用时再考虑。

**背景**:Gemma 系列原生 OpenAI function-calling 历史上弱/不稳。若 `probe-local` 实测它
**发不出 `tool_call`**,可选的「轻量 tool-call 适配层」是:在 system prompt 里教模型用一段
结构化文本(如 ```tool\n{"name":"edit_file","arguments":{…}}\n```)表达工具意图,再由 lumen
解析这段文本、合成成 `provider.ToolCall` 喂回 agent 循环。

**为什么暂不做:**

1. **脆弱**:文本协议靠模型自律,解析失败率高;流式里半截 JSON 更难救。原生 `tool_call`
   有 schema 强约束,适配层没有。
2. **维护面**:得在 provider 层加一条非标准解析路径 + 容错 + 测试,长期负担。
3. **ROI 低**:已有 **Qwen3.6-27b(预期 ✅)** 作 agent 主力;Gemma 即便补上也大概率不如 Qwen。
4. **诚实降级更好**:把 Gemma 记成「对话/补全可用、agent 不可用」,让它当**对照/快速冒烟**,
   比硬塞一个不可靠的适配层更可信。

**何时重估**:若多个目标模型都只会散文、且原生 tool-call 模型在本机跑不动(显存/速度),
适配层才从"不值得"变成"唯一出路" —— 届时把它单列一个 spike PR,先用 `probe-local` 量化
适配前后的成功率,再决定。

## 现状(诚实标注)

- ✅ **已做、已测**:三个本地预设(`lmstudio`/`ollama`/`vllm`)、`IsLocal()`/`LocalPresets()`、
  能力探测器 `internal/localprobe`(复用真实 openai provider 路径,httptest 覆盖 tool-call/
  prose-only/模型发现/不可达四类)、CLI `lumen probe-local`(markdown + `--json`)。`make check` 绿。
- ⏳ **待实测(model-gated,不烧钱)**:对真 `qwen3.6-27b` 跑 `probe-local` 填矩阵 +
  一个最小 `lumen run` 改 bug 任务活验证。等模型下载完 + 用户在 LM Studio Start Server。
