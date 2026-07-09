# Lumen 吸收计划：CSSwitch + OpenClaudeScience

> **状态**：规划定稿（可执行，非许愿）  
> **日期**：2026-07-09  
> **原则**：证据驱动 · PR 粒度 · 有验收闸门 · 先修虚标再加功能  
> **参考源（只读）**：
> - `/Users/lei/CSswitch` @ `c552cd6`（≈ v0.3.6+）
> - `/Users/lei/OpenClaudeScience` @ `4a5f2ab`（InternAgentS / OCS）
> **实现仓**：`/Users/lei/lumen`

---

## 0. 这份文档解决什么

把两份参考仓的能力，**按真实代码差分**收进 Lumen，变成可排期、可验收的 PR，而不是「对标一下」的口号。

| 参考 | 对标 Lumen 层 | 吸收什么 |
|------|---------------|----------|
| **CSSwitch** | Page A · Science Bridge（`:18990/18991`） | 代理协议行为、provider 策略、诊断 catalog |
| **OpenClaudeScience** | Page B · Lab（`:18992`） | 工作台 API 契约、文件预览、远程算力审批 UX |

**明确不吸收**：CSSwitch 的 Python 代理壳、OCS 的 LangGraph/DeepAgents 运行时栈。Lumen 保持 **Go 单二进制** 优势。

---

## 1. 产品边界（避免再碎片化）

```
Oasis 数据市场
    │
    ├── Lumen Coding Agent     ≈ Claude Code
    ├── Page A Bridge          ≈ Claude Science + CSSwitch（国产 API 进 CS）
    └── Page B Lab             ≈ OpenClaudeScience（不绑 CS 的国产实验室）
```

导航叙事应是「一个科研工作台的三种能力」，不是三个互不相关产品名。

---

## 2. Part A — CSSwitch → Lumen Page A 差分

### 2.1 模块对照

| 能力域 | CSSwitch 源 | Lumen 现状 | 判定 |
|--------|-------------|------------|------|
| 原生 Anthropic 透传 | `proxy/anthropic_compat.py` | `proxy/patch.go` + `proxy.go` | **基本齐**，策略细节有缺口 |
| OpenAI Chat 翻译 | `openai_chat_compat.py` | `translate.go` ModeOpenAI | **基本齐** |
| **OpenAI Responses** | `responses_compat.py`（v0.3.6） | 模板有 `custom-openai-responses`，**代理无 Mode** | **虚标 · P0** |
| DSML shim | `dsml_shim.py` + 大量测试 | `dsml.go` / `dsml_stream.go` + 测试 | **齐**，需对拍 golden |
| provider_policy | `provider_policy.py` 独立模块 | 分散在 `models.go` / `translate.go` / `patch.go` | **需收口** |
| 模型 force-shell | `model_discovery.force_shell_response` | `handleModels` 直接吐 BuiltinModels | **缺口 · P0** |
| Kimi server tool 过滤 | `_filter_upstream_tools` 去 `web_search` | 无 | **缺口 · P1** |
| Kimi thinking:enabled | `normalize_thinking` + relay_thinking | relay 路径强制 thinking 时默认 **disabled** | **缺口 · P0** |
| capability catalog | `catalog/capabilities.v1.json` | 无 | **缺口 · P1** |
| custom OpenAI adapter | `openai-custom` 完整链路 | 模板 adapter=`openai-custom`，`resolveProfile` **Lookup 失败** | **虚标 · P0** |
| multi-profile 事务切换 | Rust runtime | `runtime/profile_switch.go` | **齐**（对照回归即可） |
| Go vs Python 代理 | Python | **Go 已领先** | 保持，只吸行为 |

### 2.2 实锤缺陷（有代码路径）

#### P0-A1：自定义 OpenAI / Responses 模板是死路

- **证据**：`internal/science/config/templates.go` 声明  
  - `Adapter: "openai-custom"`  
  - `Adapter: "openai-responses"`  
- **证据**：`internal/science/runtime/spec_resolve.go`  
  - 仅特殊处理 `relay`  
  - 其余走 `proxy.LookupProvider(adapter)`  
  - `BuiltInProviders` 只有：`deepseek|moonshot|zhipu|qwen|minimax`  
- **结果**：用户选「自定义 OpenAI / Responses」→ 激活时报 `unknown adapter`  
- **验收**：创建这两类 profile 后 `ResolveActiveSpec` 成功；代理能对 mock upstream 完成一轮 messages。

#### P0-A2：Responses 协议未实现

- **证据**：`Mode` 仅 `anthropic | openai`（`providers.go`）  
- **证据**：全仓无 Responses 请求体转换（对比 CSSwitch `responses_compat.py`）  
- **必须移植的行为**（来自 CSSwitch）：  
  - Anthropic Messages → OpenAI Responses  
  - 非流式上游 → 本地回放 Anthropic SSE  
  - DashScope：有 tools 时 `max_output_tokens` cap 8192  
  - DashScope：丢弃内置 `web_search` tool schema  
  - tool_choice 强制 → 保守降为 `auto`  
  - tool parameters 根 schema 归一化为 object  
- **验收**：golden 单测（从 CSSwitch `test/` 译写）+ mock upstream 200。

#### P0-A3：Relay 模型选择器未 force-shell

- **背景**：Science GUI 只认 `claude-` 前缀 id；CSSwitch 用外壳 `claude-opus-4-8` + `display_name=真实模型名`，出站强制 override 到真实模型。  
- **Lumen**：`RelaySpec` 把 BuiltinModels 原样暴露（如 `glm-5.2`），Science 顶部易显示异常或不可选。  
- **验收**：relay + modelOverride 时 `GET /v1/models` 仅返回一条 shell；出站 body.model = 真实模型。

#### P0-A4：Kimi（Anthropic relay）thinking 策略错误

- **CSSwitch**：`relay_thinking=enabled` 时强制 thinking enabled + budget；若 tool_choice 强制则 **去掉 tool_choice**（保留 tools）。  
- **Lumen**：`patchThinking` / `NormalizeAnthropicBody` 对 relay 强制 tool 时普遍 **disabled**；`moonshot` 特例只在 OpenAI 路径。  
- **验收**：kimi 模板 profile + forced tool_choice → 上游请求 thinking.enabled 且无强制 tool_choice。

### 2.3 P1（重要，可第二波）

| ID | 项 | CSSwitch 来源 | 做法 |
|----|-----|---------------|------|
| P1-A5 | Kimi 过滤 server `web_search` tool | `anthropic_compat._filter_upstream_tools` | 出站过滤，known_tools 仍保留本地 |
| P1-A6 | OpenAI base root 归一化 | `normalize_openai_base` | 去掉误填的 `/chat/completions`、`/models`；裸 host 补 `/v1` |
| P1-A7 | 自定义 OpenAI 误填 Anthropic URL 防呆 | v0.3.4 | 保存/激活时若 path 含 `/anthropic` 则拒绝并提示改模板 |
| P1-A8 | capability catalog + status 回传 rule_ids | `catalog/capabilities.v1.json` | Go 嵌 JSON；doctor/status 输出命中规则 |
| P1-A9 | DSML golden 对拍 | `test/test_dsml_shim.py` | 同输入同输出断言 |
| P1-A10 | provider_policy 收口 | `provider_policy.py` | 新建 `proxy/policy.go`，patch/translate 只调它 |

### 2.4 Part A 建议 PR 切分（可并行度标注）

| PR | 标题 | 依赖 | 预估 | 闸门 |
|----|------|------|------|------|
| **A1** | fix: resolve openai-custom / openai-responses adapters | 无 | S | `go test ./internal/science/runtime/...` + 集成 Resolve |
| **A2** | feat: OpenAI Responses translate path | A1 | M | 新 `responses_test.go` golden；mock 往返 |
| **A3** | feat: relay force-shell models for Science selector | 无 | S | `handleModels` 单测 |
| **A4** | fix: Kimi/relay thinking + tool_choice policy | 无 | S | 表驱动 `policy_test.go` |
| **A5** | fix: Kimi web_search server-tool filter | A4 | S | 单测 |
| **A6** | feat: capability catalog + rule_ids in status | A4 后更顺 | M | catalog schema 校验 + doctor 输出 |
| **A7** | chore: DSML golden parity with CSSwitch | 无 | S | 现有 dsml 测试扩展 |

**推荐执行顺序**：A1 → A3 → A4 → A5 → A2 → A7 → A6  
（先消灭虚标与真机踩坑，再上 Responses 与 catalog。）

### 2.5 Part A 明确不做

- 不把 CSSwitch Python 代理嵌进 Lumen  
- 不重写 Tauri 桌面壳（Lumen Science.app 已够用）  
- 不追求「provider 列表像素级一致」优先于「能连通」

---

## 3. Part B — OpenClaudeScience → Lumen Lab 差分

### 3.1 API 表面对照

| 能力 | OCS | Lumen Lab | 判定 |
|------|-----|-----------|------|
| health | runtime backend status | `GET /api/lab/health` | **齐**（字段可再丰富） |
| projects / sessions | workspaces + threads | projects + sessions | **基本齐** |
| chat SSE | LangGraph stream | `POST /api/lab/chat` SSE | **后端有** |
| **approval UI** | `ToolApprovalInterrupt.tsx` | SSE 发 `approval_request` 后 **直接 return true**；前端 **不渲染卡片** | **假审批 · P0** |
| files list | `/api/workspace/files` | `/api/lab/files` | **齐但弱**（无递归树、无 previewKind） |
| file content | + office/pdf preview | 纯文本截断 512KB | **缺口 · P1** |
| file search | `/api/workspace/search` | 无 | **缺口 · P1** |
| attachments | `/api/workspace/attachments` | upload 有 | **半齐** |
| SSH hosts | GET+POST 注册 + token | GET 解析 `~/.ssh/config` | **半齐**（无注册/notes） |
| remote jobs | submit + poll + **harvest globs/base64** | submit + CombinedOutput 字符串 | **缺口 · P0/P1** |
| skills list | 分层 catalog + enable | 列表 CS pack + lumen skills | **半齐** |
| skills import | `/api/skills/import` | 无 | **P2** |
| notebooks | 无同等一等公民 | `/api/lab/notebooks` | **Lumen 领先** |
| brief / C2D / bridge | 无 Oasis 闭环 | 有 | **Lumen 领先** |
| provenance | 面板概念 | `provenance.jsonl` API | **Lumen 领先** |

### 3.2 UI 规模现实

| | OCS | Lumen Lab |
|--|-----|-----------|
| 前端形态 | Next.js 大量 TSX（ChatInterface ~4k 行级） | 单文件 `static/app.js` ~400 行 |
| 三栏 | 完整 | 骨架有 |
| 工具调用展示 | ToolCallBox | 几乎只拼 text |
| 文件预览 | pdf/docx/xlsx/pptx/molecule/science | `<pre>` 文本 |
| 移动端 | 相对完整 | 三栏固定宽，小屏不可用（已知审计） |

**策略**：不整仓搬 Next；**先修 API 契约与最小可用 UI**，预览可渐进。

### 3.3 实锤缺陷

#### P0-B1：审批是空壳

- **证据**：`lab/ctrl.go` `webApprover` emit 后 `return true, nil`（自动放行）  
- **证据**：`static/app.js` `streamChat` 只处理 `text`/`thinking`，忽略 `approval_request`  
- **验收**：agent 模式写文件/跑 bash 时 UI 出 Approve/Deny；Deny 后不执行；Approve 后继续。

#### P0-B2：Chat SSE 事件面过窄（前端）

- 后端可发 tool / approval / error / turn_done  
- 前端几乎只累加 text → 用户看不见工具轨迹  
- **验收**：至少渲染 tool 名、approval 卡、error。

#### P1-B3：Workspace 预览契约

OCS `getPreviewKind`：docx/xlsx/pptx/markdown/science/molecule/pdf/image/text。  
Lab 目标 **第一刀**（务实）：

1. API 返回 `previewKind` + `mimeType` + `tooLarge`  
2. 文本/markdown/json/csv 内联  
3. 图片 raw URL  
4. pdf 浏览器原生  
5. office/molecule：**第二刀**（可后续嵌 Ketcher/3Dmol 已有能力对齐 molecule）

#### P1-B4：远程 Job 应像 OCS 一样可回收产物

OCS：`outputGlobs` + 远端 scratch + harvest base64（有 size cap）+ 本地 token 鉴权。  
Lab 现状：`ssh host cmd` + 整段 stdout 塞 JSON —— 大产物/二进制不可用。

**最小可用契约**（对齐 OCS 语义，不必抄 TS）：

```
POST /api/lab/compute/jobs
{
  "project_id", "host", "command",
  "work_dir?", "output_globs"?: string[],
  "timeout_sec"?
}
GET  /api/lab/compute/jobs/:id → status, exit_code, outputs[{path,size,local_path}]
```

#### P1-B5：Skills 三层目录

OCS：

```
~/.internagents/myskills
~/.internagents/imported-skills
skills/
.internagents/imported-skills
```

Lab 建议：

```
~/.lumen/skills
~/.lumen/imported-skills
$SCI_DIR/skills
$PROJECT/.lumen/skills
```

API：`GET/PUT /api/lab/skills` 支持 enabled 列表（第二波再做 import）。

#### P2-B6：移动端 / Oasis embed

- 三栏改可折叠；embed 前缀 `/lumen-lab` 已有，需保证 API 反代到 Lab 而非 marketplace backend（线上已知 404 问题属 Oasis 仓，另立 PR）。

### 3.4 Part B 建议 PR 切分

| PR | 标题 | 依赖 | 预估 | 闸门 |
|----|------|------|------|------|
| **B1** | fix: real approval gate + SSE card UI | 无 | M | 手工：deny 不执行；单测 approver 阻塞 |
| **B2** | feat: render tool/error/turn events in lab UI | B1 可并行 | S | UI 快照或事件解析单测 |
| **B3** | feat: files API previewKind + raw route | 无 | S | API 契约测试 |
| **B4** | feat: compute job harvest outputs | 无 | M | mock ssh 或 integration script |
| **B5** | feat: skills layered catalog + enable | 无 | S | 列表顺序断言 |
| **B6** | feat: workspace search | B3 | S | 搜索命中测试 |
| **B7** | polish: mobile collapsing panels | B2 | S | 390px 手工/截图 |

**推荐顺序**：B1 → B2 → B3 → B6 → B4 → B5 → B7

### 3.5 Part B 明确不做（本规划周期）

- 不引入 LangGraph / DeepAgents 替换 Lumen agent loop  
- 不整页重写为 Next.js  
- 不一次做完 Office 全预览（可第二周期）  
- 不把 OCS 的 update/rollback 桌面热更搬过来  

### 3.6 Lumen 已领先、禁止回退

- provenance.jsonl  
- Research Brief 四源  
- 5-ship native MCP + CS bio fleet  
- Jupyter API  
- C2D list/publish 与 Oasis 闭环  
- Go 单二进制 lab server  

---

## 4. 跨层（Oasis 工作台）— 仅记账，本文件不展开实现

线上 `demo.oasisdata2026.xyz` 已知（2026-07-06 审计）：

- Lab API 被 marketplace 吃掉 → 404  
- Science iframe 指 127.0.0.1 失败  
- 导航三入口碎片化  

→ 在 `ai-data-marketplace-seed` 单独立项，**不阻塞** A/B PR。

---

## 5. 总优先级与里程碑

### Milestone M1 — 消灭虚标（生产力第一刀）

- [ ] A1 自定义 OpenAI 真能连  
- [ ] A3 Science 模型名 force-shell  
- [ ] A4 Kimi thinking 策略  
- [ ] B1 真审批  
- [ ] B2 工具事件可见  

**完成定义**：用户用国产 key 走 Bridge 不踩已知坑；Lab 对话能看见工具并确认敏感操作。

### Milestone M2 — 协议与产物

- [ ] A2 Responses 路径  
- [ ] A5 Kimi tool filter  
- [ ] B3/B6 文件预览+搜索  
- [ ] B4 job harvest  

### Milestone M3 — 可观测与技能

- [ ] A6 capability catalog  
- [ ] A7 DSML golden  
- [ ] B5 skills 分层  
- [ ] B7 移动端  

---

## 6. 验证闸门（每次 PR 合并前）

```bash
# 通用
cd /Users/lei/lumen
go test ./internal/science/...

# Bridge 全量（已有）
bash scripts/science/full-verify.sh   # 或 make science-full-verify

# Lab
bash scripts/science/lab-smoke.sh
bash scripts/science/lab-parity-verify.sh
```

**铁律**（继承 CSSwitch/Lumen）：

- 不碰真实 `~/.claude-science` 与端口 `8765`  
- 代理只绑 loopback  
- 密钥不进日志与命令行  

**真机**（A3/A4/A2 合并后）：隔离沙箱 Science 点一轮模型切换 + 带工具对话；记入 `docs/science/findings/`。

---

## 7. 参考文件索引（执行时打开）

### CSSwitch

- `proxy/provider_policy.py`  
- `proxy/responses_compat.py`  
- `proxy/anthropic_compat.py`  
- `proxy/openai_chat_compat.py`  
- `proxy/model_discovery.py`  
- `proxy/dsml_shim.py`  
- `catalog/capabilities.v1.json`  
- `test/test_dsml_shim.py` / `test_provider_policy.py`  

### Lumen

- `internal/science/runtime/spec_resolve.go` ← A1  
- `internal/science/proxy/*` ← A2–A7  
- `internal/science/config/templates.go`  
- `internal/science/lab/ctrl.go` / `api.go` / `static/app.js` ← B*  
- `internal/science/lab/compute/jobs.go` ← B4  
- `docs/science/LAB.md` / `COMPARISON.md`  

### OCS

- `ui/src/app/api/workspace/**`  
- `ui/src/app/api/compute/_lib/ssh-remote-jobs.ts`  
- `ui/src/app/components/ToolApprovalInterrupt.tsx`  
- `internagents/remote_compute_tools.py`  

---

## 8. 变更记录

| 日期 | 说明 |
|------|------|
| 2026-07-09 | 首版落盘：基于 CSSwitch c552cd6 + OCS 4a5f2ab 与 Lumen 源码实锤差分 |

---

## 9. 下一步（执行时）

1. 用户确认 M1 范围无异议  
2. 开分支 `science/absorb-m1`  
3. 按 A1 → A3 → A4 → B1 → B2 顺序 PR  
4. 每 PR 附本文件章节号与闸门输出  

**未确认前不写实现代码。**

---

## 10. 补遗审查（2026-07-09）— 首版缺口与解法

> 对首版再扫一轮代码后的补丁。**标 ★ 的是若不补会导致 PR 做错地方或验收假通过。**

### 10.1 ★ B1 写得太浅：缺「等待 + 回复 API」

**缺口**：首版只写了「前端渲染 Approve/Deny」。但 Lab 当前 `webApprover` 是：

```go
emit("approval_request", ...)
return true, nil  // 立刻放行，根本不等人
```

即便 UI 画了按钮，**后端也不会等**。

**解法（抄本仓已有实现，勿发明）**：

| 组件 | 源 | 迁到 Lab |
|------|-----|----------|
| 等待通道 | `internal/server/server_approval.go` | `lab` 内同样 `sync.Map` + `chan bool` |
| 回复路由 | `POST /v1/approve` `{id, allow}` | **`POST /api/lab/approve`** 同语义 |
| 模式语义 | Plan→拒、Bypass→过、Default→等人 | Lab `webApprover` 对齐，勿一直 true |
| 前端 | coding UI 若有 | SSE 收到 `approval_request` → 按钮 → POST approve |

**额外 bug**：Lab 的 approval `id` 用 `os.Getpid()`，同进程并发会撞 id。解法：用 `atomic.Uint64` 序号（与 server 一致）。

**B1 验收补强**：

1. 单测：approver 阻塞直到 inject allow/deny  
2. API：`POST /api/lab/approve` 未知 id → 404  
3. 手工：agent 模式写文件 → 卡审批 → Deny 不落盘  

**PR 修正**：B1 从「UI 卡」改为 **后端等待门闩 + API + UI** 三件套，预估 M→L 若一次做完；可拆：

- B1a：后端门闩 + `/api/lab/approve`（无 UI 也可用 curl 测）  
- B1b：前端卡  

---

### 10.2 ★ A4 改错文件会白改：热路径是 `patch.go`

**缺口**：首版把 thinking 问题同时挂在 `NormalizeAnthropicBody` 与 `patchThinking` 上。  
**实锤热路径**：`proxy.go` `handleAnthropic` **只调用** `PatchAnthropicBodyRaw`（`patch.go`）。  
`NormalizeAnthropicBody` 基本只在单测出现 → 只改 translate.go **线上零效果**。

**解法**：

1. A4 **主改** `patch.go` 的 `patchThinking`（及工具过滤若同 PR）  
2. 抽 `policy.go`，`patch` 与 `NormalizeAnthropicBody` **共用**，避免双实现漂移  
3. 单测同时覆盖：`PatchAnthropicBodyRaw` 输出字节级断言 + 可选保留 Normalize 一致性测试  

---

### 10.3 ★ Profile / Spec 缺 thinking 策略字段

**缺口**：CSSwitch 模板有 `thinking_policy`（enabled / adaptive / …）。  
Lumen `Profile` 只有 `TemplateID, BaseURL, APIKey, Model…`，`ProviderSpec` 也无 thinking 字段。  
A4 若只在代码里写死 `if name=="kimi"` 会脆（用户自定义 Anthropic 指向 Kimi 时失效）。

**解法**：

| 层 | 改动 |
|----|------|
| `templates.go` | 增加 `ThinkingPolicy string`（如 kimi→`enabled`，minimax→`adaptive`，deepseek→`""`） |
| `ProviderSpec` | 增加 `ThinkingPolicy`、`ForceModelOverride bool`、`ForceModel string` |
| `RelaySpec` / `resolveProfile` | 从模板填入 spec |
| `patchThinking` | 读 `spec.ThinkingPolicy`，不读死 provider 名字符串（可用 model 含 kimi 作次级启发式） |

---

### 10.4 ★ A1 与 A2 边界要钉死

**缺口**：A1「resolve adapter」 alone **不能**让 Responses 可用。  
resolve 成功后若仍无 `ModeResponses` + 翻译，只会连错路径或 panic。

**解法（依赖图）**：

```
A1: 能构造 ProviderSpec（openai-custom → ModeOpenAI + 正确 URL；
      openai-responses → ModeResponses + /responses URL）
A2: handleMessages 增加 ModeResponses 分支 + 转换 + 非流回放 SSE
```

A1 验收只保证 **Resolve + 启动不报 unknown adapter**；  
A2 验收才保证 **messages 往返 200**。文档与 PR 描述勿混。

**openai-custom 最小实现**（A1）：

```go
case "openai-custom":
  base := NormalizeOpenAIBase(baseURL) // 同 A6 可先内联
  spec := ProviderSpec{
    Name: "openai-custom", Mode: ModeOpenAI,
    URL: base+"/chat/completions",
    DefaultModel: p.Model, Force shell models if needed,
  }
```

**openai-responses**（A1 只造 spec，A2 实现 mode）：

```go
Mode: ModeResponses, URL: base+"/responses"
```

---

### 10.5 A3 force-shell：与 RelaySpec 字段绑定

**缺口**：首版写了行为，没写数据流。

**解法**：

1. `RelaySpec(..., modelOverride)` 已把 ModelMap 指到 override；补：  
   - `ForceModelOverride=true`  
   - `Models = []{{ID:"claude-opus-4-8", DisplayName: modelOverride或真实名}}`  
2. `handleModels`：若 ForceModelOverride → 只返回一条 shell  
3. `ResolveModel`：已有 map 则 OK；强制路径优先 `ForceModel`  
4. DeepSeek 原生已用 claude- 外壳，**勿破坏** deepseek 多模型列表  

---

### 10.6 MiniMax 双身份不一致

**现状**：

- 模板 `minimax`：`Adapter: relay`，URL `api.minimaxi.com/anthropic`（正确新域）  
- `BuiltInProviders["minimax"]`：`api.minimax.chat`（旧），且 **profile 路径不走它**（走 relay）

**风险**：遗留 slot / 文档 / doctor 仍引用旧 BuiltIn。

**解法**：

- 短期：注释 BuiltIn minimax 为 legacy；或 URL 改成与模板一致  
- 中期：删除重复，统一 relay 模板  
- 测试：激活 minimax 模板 profile 时 URL host 必须是 `minimaxi.com`  

可挂在 **A3 或 A1 的 chore 小提交**，不必单开大 PR。

---

### 10.7 代理复用指纹可能偏弱

**CSSwitch 教训**：切换 profile 时若 fingerprint 不含 `api_format/template`，会复用旧代理语义。

**Lumen 现状**（`runtime.go`）：大致用 profile id / key / adapter 判断是否重启——需在实现 A1 时 **读一遍并补测**：

- 同 adapter 不同 `APIFormat`（openai_chat vs responses）必须重启  
- 同 base 不同 model override 必须重启（force-shell 相关）  

**解法**：扩展 fingerprint 输入：`adapter|api_format|base_url|model|keyFP|shim`；加单测「格式变了必 Restarted」。

挂 **A1 尾部** 或独立 **A1.1**。

---

### 10.8 双路径策略漂移（OpenAI vs Anthropic）

`handleOpenAI` 走 `AnthropicToOpenAI` + 可能另一套 thinking；`handleAnthropic` 走 patch。  
A4 收口 policy 后，**OpenAI 路径**也要调用同一 `NormalizeThinking`（map 级），避免 Kimi OpenAI 槽与 Anthropic 槽行为分裂。

---

### 10.9 Lab 默认 mode=plan 与「能看见工具」的冲突

`app.js` 默认 `mode || "plan"`。Plan 模式在 coding server 里 **直接拒绝** 工具，不会走审批。

**缺口**：B1/B2 若只测 plan，会以为「没审批/没工具」是 bug。

**解法**：

- UI：默认改为 `agent`/`default`，plan 作显式选项  
- 文档验收：B1 必须在 **agent 模式** 测  
- plan 模式验收：应显示「只规划不执行」而不是假工具轨迹  

---

### 10.10 流式错误 / keepalive（CSSwitch P1 未入库）

CSSwitch 0.3.5 收紧 SSE keepalive 与上游错误透传。Lumen 有 `stream_error.go` 但未与 CSSwitch 合同对拍。

**解法**：M2 增加 **P1-A11**：对照 CSSwitch 流错误用例，补 `proxy` 单测（上游 429/半截 SSE 不挂死）。非 M1。

---

### 10.11 Oasis 反代：虽「不阻塞」但要有入口工单

**缺口**：首版只记账。Lab 本地修好，线上仍 404 → 用户感知「白修」。

**解法**：在 `ai-data-marketplace-seed` 建 issue/短计划（本文件不实现）：

| ID | 项 |
|----|-----|
| O1 | Caddy/nginx：`/lumen-lab/api/*` → `127.0.0.1:18992` |
| O2 | Science iframe：优先同源反代 `/lumen-science`，禁止写死失败的 127.0.0.1 无回落 |
| O3 | 导航合并为一个 Workspace |

与 M1 并行，由 marketplace 仓负责。

---

### 10.12 文档与对外矩阵同步

合并 M1 后更新：

- `docs/science/COMPARISON.md`  
- `docs/science/LAB.md`（补 `/api/lab/approve`、preview 字段）  
- `docs/science/known-issues.md`（虚标毕业）  

---

### 10.13 许可与抄袭边界

两仓均为 MIT。吸收时：

- 行为/测试向量可移植  
- 大段粘贴加文件头来源注释（尤其 catalog JSON、golden fixtures）  
- 不整文件 copy Python 进 Go 仓  

---

### 10.14 修正后的 M1 清单（以本补遗为准）

| 序 | 项 | 补遗要点 |
|----|-----|----------|
| 1 | **A1** | resolve + fingerprint；Responses 只造 spec |
| 2 | **A3** | force-shell + RelaySpec 字段 |
| 3 | **A4** | **只认 patch 热路径** + template ThinkingPolicy |
| 4 | **B1a** | 抄 `server_approval.go` 门闩 + `POST /api/lab/approve` |
| 5 | **B1b** | UI 卡 + 默认 mode=agent |
| 6 | **B2** | tool/error/turn 渲染 |

A2 / B3+ 仍属 M2。

---

### 10.15 补遗后仍不必做的

- 不重写 Lab 为 Next.js  
- 不把 CSSwitch Python 嵌进来  
- M1 不做 Office 全预览、不做 catalog 全文、不做 job harvest（B4）  

---

## 11. 变更记录（续）

| 日期 | 说明 |
|------|------|
| 2026-07-09 | 首版落盘 |
| 2026-07-09 | §10 补遗：审批等待 API、patch 热路径、ThinkingPolicy、A1/A2 边界、MiniMax 双身份、指纹、plan 模式、Oasis 工单 |
| 2026-07-09 | **M1 代码已实现（lumen + oasis seed）**：A1/A3/A4、B1 门闩+UI、B2 事件渲染；Caddy 把 `/api/lab/*` 从 marketplace backend 拆出；workspace Lab 页诚实探活。上线 demo 需服务器部署步骤见仓库说明。 |
