# Lumen — verify-after-edit 自修闭环(C-5 设计)

> 作者:Claude,2026-06-17。对应 `规划书-打磨.md` 的 **P2 / C-5**。
> 一句话:**模型改完代码后,Lumen 自动 build/vet/test,把失败结构化喂回 agent loop 让它自修——直到通过或触顶交回用户。** 这是对标 Cursor / Claude Code 的命门能力。

---

## 0. 背景与目标

**问题:** 当前 Lumen 的 agent loop 改完文件后**不会自己验证**。模型可能写出编译不过、测试挂的代码却"以为完成了"——正是规划书开篇那种 `WaitInput undefined` 断裂能溜进去的根因(人/旧会话改了 `main.go` 引用,没人 build)。

**目标:** 在写入类工具批次执行后自动验证,失败时把可执行的诊断喂回模型,形成 **edit → verify → self-repair** 的闭环;成功时安静放行。全程可在 `lumen.toml` 关闭/调参。

**非目标(YAGNI):** 多语言(MVP 只 Go)、增量编译缓存、诊断去重/排序优化、跨轮持久化验证状态。这些留扩展点,不在本期做。

**北极星对齐:** 编码正确率 ↑(改完即验)、可靠性 ↑(编译/测试门内置)、体验 ↑(自修 + 状态可见)。

---

## 1. 现状锚点(读代码得来)

- `internal/agent/agent.go`:`executeOne`(约 :506–586)是单次工具执行:plan 门 → 权限门 → **`onPreEdit` 钩子(写入类工具 diff 预览,:552)** → `t.Execute` → evidence → 返回 `toolOutcome{output, errMsg, blocked, truncated}`。`toolOutcome.output` 就是喂回模型的工具结果。
- 工具分区:写入类工具**串行**执行,只读工具并行(`partitionToolCalls` / `executeParallel`)。
- 已有 **storm-breaker**(死循环熔断)与 plan-mode 门——自修循环护栏复用它,不另造。
- 工具抽象 `internal/tool/tool.go`:`Tool{Execute, ReadOnly, ...}` + 可选 `Previewer`。写入类工具:`edit_file` / `write_file` / `multi_edit`(`internal/tool/builtin/`)。
- 已有 `bash` 工具能跑命令,但本闭环**不经模型**——是 loop 自动跑,故新建 `internal/verify` 直接执行,不复用 bash 工具路径。

**`onPreEdit` 是先例:写入工具前已有钩子;本设计加一个对称的"写入批次后"钩子。**

---

## 2. 架构

```
agent loop (一轮)
  └─ 写入批次执行(edit/write/multi_edit,串行)
        每个写入工具成功 → 记录改动路径到 turn-local changeset
  └─ 批次结束 & changeset 非空 & verify.enabled
        └─ Verifier.Verify(ctx, changed) ──► Result{OK, Failed, Diagnostics, Output}
              ├─ OK   → emit VerifyResult(ok) 事件;loop 正常继续(安静 ✓)
              └─ FAIL → 合成一条 observation 注入下一步上下文;repairCycle++
                         repairCycle ≤ max → 模型自修(回到写入批次)
                         repairCycle > max → emit + 交回用户,停止自修
```

### 2.1 新包 `internal/verify`(Claude 批准新增 — 护城河核心)

**接口(为隔离/可测而切小):**

```go
package verify

// Step 是一条可执行的验证命令。
type Step struct {
    Name string   // "build" | "vet" | "test"
    Dir  string   // 工作目录(通常 = 项目根)
    Args []string // 例:["go","build","./..."]
}

// Diagnostic 是从命令输出解析出的一条结构化诊断。
type Diagnostic struct {
    File string // 相对项目根
    Line int
    Col  int
    Msg  string
    Sev  string // "error" | "warning"
}

// Result 是一次验证的结果。
type Result struct {
    OK          bool
    Failed      *Step        // 第一条失败的 step(OK 时为 nil)
    Diagnostics []Diagnostic // 来自 Failed step 输出
    Output      string       // Failed step 的原始输出(截断后)
}

// Config 来自 lumen.toml [verify]。
type Config struct {
    Enabled         bool     // 默认 true
    Command         string   // 覆盖;空 = 自动探测
    Scope           string   // "changed-pkg"(默认)| "all"
    RunTests        bool     // 默认 true
    MaxRepairCycles int      // 默认 3
}

// Detect 根据项目根、改动文件、配置,产出有序的验证 plan。纯函数。【DeepSeek 可做】
func Detect(root string, changed []string, cfg Config) []Step

// Parse 把一条 step 的输出解析为结构化诊断。纯函数。【DeepSeek 可做】
func Parse(step Step, output string) []Diagnostic

// Verifier 按 plan 顺序执行 step,任一失败即停。【Claude 做 — 跑命令编排】
type Verifier struct { /* root, cfg, runner */ }
func New(root string, cfg Config) *Verifier
func (v *Verifier) Verify(ctx context.Context, changed []string) Result
```

### 2.2 默认 Go plan(`Detect` 行为,scope=changed-pkg)

按序、任一失败即停:
1. `{build, root, ["go","build","./..."]}` — 全量 build(故意全量:抓跨包断裂,即 C-1 那类)。
2. `{vet, root, ["go","vet","./..."]}`。
3. 若 `RunTests`:对**改动文件所属的去重包集合**,每包一条 `{test, root, ["go","test", "<pkgPath>"]}`(由改动文件路径反推包导入路径;非 Go 改动跳过 test)。

`scope="all"` 时第 3 步退化为 `["go","test","./..."]`。`Command` 非空时:整个 plan 由该命令字符串单条覆盖(`{custom, root, sh -c <command>}`),不再自动探测。

环境:执行时注入 `GOTOOLCHAIN=local GOFLAGS=-mod=mod`(与项目工具链一致),并继承当前 PATH。

### 2.3 解析(`Parse`)

- `go build` / `go vet`:行形如 `path/to/file.go:LINE:COL: message` → Diagnostic{File,Line,Col,Msg,Sev:"error"}(vet 视作 warning,但失败即喂回)。
- `go test`:抓 `--- FAIL: TestX` 与 `file_test.go:LINE: message` 行;panic 栈首行也抓。
- 解析不到结构化行时:Diagnostics 为空,但 `Output`(截断)仍喂回——模型仍能读原文。

### 2.4 喂回格式(失败时注入 loop 的 observation)

合成一段紧凑文本,作为额外上下文进入下一步(实现见 §3):

```
⚠ verify failed at step `test` (go test ./internal/foo):
  internal/foo/bar.go:42:6: undefined: helper
  internal/foo/bar_test.go:13: expected 3, got 0
Fix these, then continue. (repair cycle 1/3)
```

> 设计取舍:**喂回走"合成 observation",不是改最后一个写入工具的 output**——因为验证是批次级、跨多个工具的,挂在单个工具结果上语义不清,也会破坏缓存形状。

### 2.5 事件(给 TUI 状态条)

新增 `event.VerifyStarted` / `event.VerifyResult`(后者带 OK / 失败 step 名 / 诊断数)。TUI(C2 的状态/P4 体验)订阅显示 `⟳ verifying…` / `✓ verified` / `✗ build failed`。

---

## 3. agent loop 改动(Claude 专属 — 碰 loop/控制流/安全)

1. **改动收集:** 在 `executeOne` 写入工具成功分支,把改动路径写入 turn-local 的 `changeset`(从工具 args 或 Previewer.Preview 的 `change.Path` 取,避免重复解析)。changeset 随每轮重置。
2. **批次后触发:** 写入批次跑完(回到批次循环的批次边界)且 `changeset` 非空且 `cfg.Enabled` → 调 `Verifier.Verify`。
3. **自修注入:** 失败 → 构造 §2.4 文本,作为合成 observation 进入模型下一步;`repairCycle++`。成功 → 清 changeset、`repairCycle=0`、发 ok 事件。
4. **护栏:** `repairCycle > MaxRepairCycles` → 停止自修,发事件 + 在回复里告知用户"验证仍失败,已停止自修",把最后一次 Result 附上。与 storm-breaker 协同(验证-自修不计入或单独计数,避免误熔断;具体在实现计划里定)。
5. **plan 模式:** plan 模式不写文件,故不触发 verify(自然短路)。

---

## 4. 配置(`lumen.toml`)

```toml
[verify]
enabled = true            # 总开关
command = ""              # 覆盖;空=自动探测(Go: build+vet+test)
scope = "changed-pkg"     # changed-pkg | all
run_tests = true
max_repair_cycles = 3
```

默认值即上表;`[verify]` 整段缺失时全部取默认(enabled=true)。

---

## 5. 测试策略(TDD,先红后绿)

- **`Detect`(DeepSeek):** 表驱动——给定 root+changed+cfg → 期望 `[]Step`。覆盖:无改动、单包、多包去重、scope=all、Command 覆盖、非 Go 文件跳 test。
- **`Parse`(DeepSeek):** golden fixtures——真实 `go build`/`vet`/`test` 输出片段 → 期望 `[]Diagnostic`。覆盖:编译错、vet 警告、test FAIL、panic、无结构化行。
- **`Config`(DeepSeek):** 加载/默认值/缺段。
- **`Verifier.Verify`(Claude):** 用 fake runner(注入命令执行器)单测编排:首步失败即停、step 顺序、Result 组装。再加 1 个**对真实 go 工具链**的集成测试(建临时模块,改坏→Verify 报 build 失败;改对→OK)。
- **loop 钩子(Claude):** 用 fake provider + fake verifier 驱动 agent,断言:失败→注入+repairCycle++;触顶→停;成功→静默继续。

---

## 6. 分工(严格按规划书 §3)

**Claude(碰 agent loop / 控制流 / 安全 / 架构 / 接口):**
- §3 全部(loop 钩子、changeset、自修注入、护栏、storm-breaker 协同)。
- `Verifier.Verify` 编排 + fake-runner 单测 + 真实工具链集成测试。
- `verify` 包接口定义(本 spec 已给)、事件类型新增、配置接线进 agent。

**DeepSeek(纯函数 / 规格明确 / 可并行,见 §7 三张卡):**
- `Detect`、`Parse`、`Config` 三个纯/准纯单元 + 测试。
- 禁区铁律:不碰 agent loop / 安全 / 架构;只动点名文件;只加测试或按本 spec 签名补码;Claude 终审后合。

---

## 7. DeepSeek 机械卡(≥3,见 `docs/tasks/2026-06-17-deepseek-batch-2-verify.md`)

- **D-V1** `verify.Detect` 命令探测表(Go 三步;其他语言留 stub)+ 表驱动测试。
- **D-V2** `verify.Parse`:`go build`/`vet`/`test` 输出 → `[]Diagnostic` + golden fixtures。
- **D-V3** `verify.Config` 结构 + `lumen.toml [verify]` 加载/默认值 + 测试。

> 三张卡共用本 spec §2.1 的签名,**互不冲突**(各自新文件),可并行发。Claude 先落 `internal/verify/verify.go` 骨架,**只含类型(`Step`/`Diagnostic`/`Result`/`Config`)+ `Verifier` + fake-runner 接口**;**不声明** `Detect`/`Parse`/`DefaultConfig`/`ConfigFromTOML`——这些函数体由 DeepSeek 在各自新文件(`detect.go`/`parse.go`/`config.go`)里实现(Go 无前向声明,骨架若也声明会重复定义冲突)。

---

## 8. 落地顺序(交给 writing-plans 细化)

1. Claude:建 `internal/verify/verify.go` 骨架(接口/类型/`Verifier` 空实现 + fake-runner 接口)。
2. 并行:DeepSeek 三卡(D-V1/2/3)填纯函数;Claude 写 `Verifier.Verify` 编排 + 集成测试。
3. Claude:agent loop 钩子 + changeset + 自修注入 + 护栏 + 事件,TDD。
4. Claude:配置接线 + 默认值;端到端在临时模块验证一次完整 edit→fail→repair→pass。
5. 文档:`lumen.toml` 示例 + README 一节。

**验收(整体):** 在一个故意写坏的临时 Go 模块里,模型一次编辑触发 verify→失败诊断喂回→模型自修→再 verify 通过;`go test -race ./internal/verify/ ./internal/agent/` 全绿;`[verify] enabled=false` 时完全短路。
