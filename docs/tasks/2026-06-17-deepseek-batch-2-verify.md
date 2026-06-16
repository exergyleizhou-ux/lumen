# DeepSeek 任务卡 — 批次 2(C-5:verify-after-edit 纯函数)

> 规则:每张卡自包含。只改"输出文件",别碰"禁区"。完成后跑"验收命令",必须全绿。Claude 终审后合并。
> 上下文:对应 spec `docs/superpowers/specs/2026-06-17-lumen-verify-after-edit-design.md`(§2.1 是权威签名,以它为准)。
> 这是 **verify-after-edit 自修闭环**的机械部分。Claude 负责 agent loop 钩子与 `Verifier.Verify` 编排;**你只做三个纯函数 + 测试**。
>
> **前置:** Claude 已落地骨架 `internal/editverify/editverify.go`(含 `Step` / `Diagnostic` / `Result` / `Config` 类型 + `DefaultConfig()` + `Runner` 接口 + `Verifier`)。**三张卡都在该骨架已合并后再发**,各自新建文件,**互不冲突,可并行**。
>
> **铁律:** 不碰 agent loop / 安全 / 架构;不改 `editverify.go` 骨架里的类型签名;只新增文件(函数体 + 测试)。`PATH="$HOME/.local/bin:$HOME/sdk/go/bin:$HOME/.local/go/bin:$PATH"`,`GOTOOLCHAIN=local GOFLAGS=-mod=mod`。

---

## 卡 D-V1 — `Detect` 命令探测表
**目标:** 实现 `func Detect(root string, changed []string, cfg Config) []Step`,按 spec §2.2 产出有序验证 plan。
**输出文件:** 新建 `internal/editverify/detect.go`(package editverify)+ `internal/editverify/detect_test.go`。
**行为(Go,scope=changed-pkg):** 按序——
1. `{Name:"build", Dir:root, Args:["go","build","./..."]}`
2. `{Name:"vet", Dir:root, Args:["go","vet","./..."]}`
3. 若 `cfg.RunTests`:对 `changed` 里 `.go` 文件所属包(用文件所在目录相对 root 反推为 `./<dir>` 形式,**去重**),每包一条 `{Name:"test", Dir:root, Args:["go","test","./<dir>"]}`。无 `.go` 改动→跳过 test。
- `cfg.Scope=="all"` 时第 3 步改为单条 `{Name:"test", Dir:root, Args:["go","test","./..."]}`。
- `cfg.Command != ""` 时:**整个 plan 只一条** `{Name:"custom", Dir:root, Args:["sh","-c",cfg.Command]}`,不做其他探测。
- `changed` 为空 → 仍返回 build+vet(无 test)。
**禁区:** 不碰 `editverify.go`、`parse.go`、`config.go`、agent 包、任何已有文件。
**验收命令:** `go test ./internal/editverify/ -run Detect` 必须 ok。
**测试要求(表驱动,≥6 例):** 无改动 / 单包 / 多包去重 / scope=all / Command 覆盖 / 含非 .go 文件(只对 .go 包跑 test)。每例断言返回 `[]Step` 完全相等(顺序+内容)。

---

## 卡 D-V2 — `Parse` 输出解析 + golden fixtures
**目标:** 实现 `func Parse(step Step, output string) []Diagnostic`,把命令输出解析为结构化诊断(spec §2.3)。
**输出文件:** 新建 `internal/editverify/parse.go`(package editverify)+ `internal/editverify/parse_test.go` + `internal/editverify/testdata/`(golden 输入).
**行为:**
- 通用:匹配 `file.go:LINE:COL: msg` → `Diagnostic{File, Line, Col, Msg, Sev}`;`file.go:LINE: msg`(无 col)→ Col=0。File 保持输出里的原样路径。
- `step.Name=="build"` 或 `"vet"`:Sev = build→"error",vet→"warning"。
- `step.Name=="test"`:抓 `file_test.go:LINE: msg` 行(Sev="error");`--- FAIL: TestName` 行 → `Diagnostic{Msg:"FAIL: TestName", Sev:"error"}`(File 空);panic 行 `panic: ...` → 一条 Sev="error"。
- 解析不到任何结构化行 → 返回**空切片**(不是 nil 报错;调用方仍有原始 Output)。
**禁区:** 不碰 `editverify.go`、`detect.go`、`config.go`、其他已有文件。
**验收命令:** `go test ./internal/editverify/ -run Parse` 必须 ok。
**测试要求(golden,≥5 例):** 真实片段——(1) `go build` 编译错(file:line:col);(2) `go vet` 警告;(3) `go test` 的 `--- FAIL` + `file_test.go:line:`;(4) panic 栈;(5) 干净输出(空诊断)。把输入片段放 `testdata/*.txt`,测试读入比对期望 `[]Diagnostic`。

---

## 卡 D-V3 — `[verify]` 配置加载
**目标:** 实现从 `lumen.toml` 的 `[verify]` 段加载 `Config`(spec §4)。`DefaultConfig()` **已在骨架 `editverify.go` 里**,直接调用它当基底,不要重复定义。
**输出文件:** 新建 `internal/editverify/config.go`(package editverify)+ `internal/editverify/config_test.go`。
**行为:** 实现 `func ConfigFromTOML(raw []byte) (Config, error)`:以 `DefaultConfig()` 为基底,用项目已有的 `github.com/BurntSushi/toml` 解析,只读 `[verify]` 段;缺段/缺字段→保留默认;`Scope` 非法值→回落 "changed-pkg";`MaxRepairCycles<=0`→取 3。
- 解析结构:`type verifyFile struct { Verify *struct{ Enabled *bool; Command *string; Scope *string; RunTests *bool `toml:"run_tests"`; MaxRepairCycles *int `toml:"max_repair_cycles"` } `toml:"verify"` }`,指针区分"未设置"与"显式设零值"。
**禁区:** 不碰 `editverify.go`、`detect.go`、`parse.go`、`internal/config/`(主配置接线由 Claude 做)、其他已有文件。
**验收命令:** `go test ./internal/editverify/ -run Config` 必须 ok。
**测试要求(≥5 例):** 空输入→全默认;只设 `enabled=false`;`scope="all"`;非法 scope→回落;`max_repair_cycles=0`→回落 3;`run_tests=false`。

---

> **合并顺序:** Claude 骨架 `editverify.go` 先合 → 三卡并行发 → 各自回来 Claude 跑验收命令 + 核对禁区 + 终审。三卡都不依赖彼此。
