# DeepSeek 任务卡 — 批次 1(阶段一:渲染/高亮机械扩展)

> 规则:每张卡自包含。只改"输出文件",别碰"禁区"。完成后跑"验收命令",必须全绿。Claude 终审后合并。
> 上下文:`internal/render` 已有 markdown→ANSI 渲染器 + 表驱动语法高亮器(`Highlight(code, lang)`)。
> 高亮器靠 `RegisterLang(&Lang{...}, names...)` 注册语言;`Lang{Keywords map[string]bool, Line, BlockOpen, BlockClose string}`。

> **并行调度(2026-06-16):**
> - ✅ **现在就发给 DeepSeek 并行跑:卡 1.1 + 卡 1.2**(纯新增文件,与 Claude 正在做的 1b/1c **零冲突**)。
> - ⛔ **卡 1.3 / 1.4 暂缓**:Claude 正在亲自实现 1b(高亮 diff,占用 `internal/render`)和 1c(`internal/lineedit` 行编辑核心),会创建同名文件,**直接并行会冲突**。等 Claude 的 1b/1c 落地后,1.3/1.4 重定义为"在已有脚手架上扩展覆盖"放进批次 2。
> - 驱动方式:把单张卡的正文贴给 DeepSeek;回来后 Claude 跑「验收命令」+ 核对禁区 + 终审合并。

---

## 卡 1.1 — 扩展语法高亮语言表
**目标:** 给 `internal/render` 增加更多语言的高亮支持。
**输出文件:** 新建 `internal/render/langs_more.go`(package render),在 `func init()` 里调用 `RegisterLang` 注册下列语言。
**要注册的语言**(给出 Keywords / 行注释 / 块注释):
- rust(`//`,`/* */`):fn let mut const struct enum impl trait pub use mod match if else for while loop return self Self true false Some None Ok Err
- java(`//`,`/* */`):class interface public private protected static final void int long double boolean new return if else for while switch case break this null true false import package extends implements
- ruby(`#`,无块):def end class module if elsif else unless while do return yield self nil true false require attr_accessor
- yaml(无关键字高亮,主要靠 string/number;`#` 行注释):注册 true false null
- toml(`#` 行注释):true false
- sql(`--` 行注释,`/* */`):SELECT FROM WHERE INSERT UPDATE DELETE CREATE TABLE JOIN ON GROUP BY ORDER LIMIT AND OR NOT NULL VALUES INTO SET(大小写都要,建议都注册小写也注册大写)
- html / xml:无关键字,跳过或仅注册空表(可不做)
**注意:** keyword 集合用辅助构造;参考 highlight.go 里 go/python 的写法。别改 highlight.go 已有语言。
**禁区:** 不要改 `highlight.go`、`markdown.go`、`stream.go` 的现有逻辑;只新增文件。
**验收命令:** `GOTOOLCHAIN=local GOFLAGS=-mod=mod go test ./internal/render/...` 必须 ok。
**附加:** 在 `internal/render/langs_more_test.go` 给每个新语言加 1 个用例:`Highlight("<含一个关键字的片段>", "<lang>")` 断言 `hasANSI` 为真且去 ANSI 后等于原文(可复用 render_test.go 里的 `plain`/`hasANSI` 思路,但测试在同包内可直接用)。

---

## 卡 1.2 — markdown 渲染 golden 夹具与边界用例
**目标:** 增强 `internal/render` 的测试覆盖,锁定渲染行为。
**输出文件:** `internal/render/markdown_golden_test.go`(package render)。
**要覆盖的用例**(每个一个 `Test...`,断言去 ANSI 后的可见文本 + `hasANSI`):
- 有序列表 `1. a\n2. b`(当前未特殊处理 → 至少保证文本保留、不崩)
- 嵌套行内:`**bold with `code` inside**`(保证不 panic,文本保留)
- 表格:`| a | b |\n|---|---|\n| 1 | 2 |`(保证 `---` 分隔行不污染、单元格文本保留)
- 多个连续代码块
- 空字符串、只有换行、只有一个 `#`
**禁区:** 只加测试文件,不改实现。如果发现实现 panic 或明显错误,**不要自己改**,在卡片末尾用注释 `// FOUND BUG: ...` 记录,交给 Claude。
**验收命令:** `go test ./internal/render/...` 必须 ok。

---

## ⛔ 卡 1.3 — [暂缓·Claude 接管 1b] diff 配色与宽度辅助
**目标:** 提供纯函数辅助,供后续高亮 diff 使用。
**输出文件:** `internal/render/diffstyle.go` + `internal/render/diffstyle_test.go`(package render)。
**要实现的纯函数:**
- `func DiffLine(kind byte, text string) string` — kind 为 `'+'`/`'-'`/`' '`;`+` 绿色、`-` 红色、` ` 默认;返回带 ANSI 的整行(行首带符号)。
- `func TruncateVisible(s string, max int) string` — 按"可见宽度"(忽略 ANSI 转义)截断到 max 列,超出加 `…`。需要正确跳过 `\x1b[...m` 序列再计数。
**验收用例:**
- `plain(DiffLine('+', "abc"))` == `"+ abc"`(或你定的格式,文档化即可),且 `hasANSI` 真。
- `TruncateVisible("\x1b[31mhello\x1b[0m world", 5)` 的可见部分长度 ≤ 6(含 `…`)。
**禁区:** 不接入任何现有文件,纯新增库函数。
**验收命令:** `go test ./internal/render/...` 必须 ok。

---

## ⛔ 卡 1.4 — [暂缓·Claude 接管 1c] `/` 命令与 `@` 文件补全的数据表
**目标:** 提供命令与补全所需的**纯数据 + 纯函数**,不碰终端 raw 模式(那部分 Claude 做)。
**输出文件:** `internal/lineedit/commands.go` + `internal/lineedit/commands_test.go`(新建 package lineedit)。
**要实现:**
- `type Command struct { Name, Help string }`
- `func Commands() []Command` — 返回 slash 命令清单:/help /exit /mode /diff /undo /compact /model /cost /resume /clear(Name 不含斜杠或含都行,文档化)。
- `func MatchCommands(prefix string) []Command` — 前缀过滤(prefix 可含或不含前导 `/`,都要兼容),按 Name 排序。
- `func CompletePath(root, prefix string) []string` — 在 root 下用 `filepath.Glob`/`os.ReadDir` 返回匹配 prefix 的相对路径(给 `@` 补全用),忽略 `.git`/`node_modules`,最多返回 50 条,排序。
**验收用例:** 建临时目录放几个文件,断言 `CompletePath` 返回排序后的匹配;`MatchCommands("mo")` 含 `/mode`、`/model`。
**禁区:** 不写任何 ANSI / 终端控制 / 按键处理;纯逻辑 + 单测。
**验收命令:** `GOTOOLCHAIN=local GOFLAGS=-mod=mod go test ./internal/lineedit/...` 必须 ok。

---

### 终审清单(Claude 做,DeepSeek 不用管)
- [ ] `go build ./...` 干净
- [ ] `go test ./internal/render/... ./internal/lineedit/...` 全绿
- [ ] `gofmt -l` 无输出
- [ ] 没碰禁区文件(`git diff` 核对)
- [ ] 行为符合卡片意图(抽查)
