# DeepSeek 任务卡 — P2-1b:状态条渲染 verify 指示

> 对应 `规划-生产级终极方案书-v1.md` P2-1 的"后半渲染"。自包含。**DeepSeek 无多模态——本卡用纯文字精确描述视觉,不依赖任何截图。**
> 工具链:`PATH="$HOME/.local/bin:$HOME/sdk/go/bin:$HOME/.local/go/bin:$PATH"`,`GOTOOLCHAIN=local GOFLAGS=-mod=mod`,在 `~/lumen`。
> 铁律:不碰 agent loop/安全/架构;只动点名文件;只改渲染。Claude 终审后合。

## 背景(Claude 已落地的契约,直接用)
`internal/tui/tui.go` 已有:
- `StatusBar` 结构新增字段:`verifyState string`(取值 `""` | `"running"` | `"ok"` | `"fail"`)与 `verifyDetail string`(失败摘要,如 `"verify failed: build (2 diagnostics)"`)。
- 这两个字段由 `VerifyMsg` 经 `Update` 自动写入(bridge 已接 `event.VerifyStarted/VerifyResult`)。**你不用碰这些,只需在状态条里把它们渲染出来。**
- 渲染函数:`func (m *Model) renderStatus(w int) string`。当前它在右侧 `right := lipgloss.JoinHorizontal(...)` 里拼 `tokensPart · cachePart · costPart · stepPart`。已有可复用样式:`green`(绿)、`cyan`(青)、`dim`;红/黄可仿 `costPart` 写法 `lipgloss.NewStyle().Foreground(lipgloss.Color("1"))`。

## 卡 P2-1b — 在状态条加 verify 段
**输出文件:** 改 `internal/tui/tui.go` 的 **仅 `renderStatus`**;加 `internal/tui/verify_render_test.go`。
**要求(精确视觉规格):** 根据 `m.status.verifyState` 生成一个 `verifyPart` 字符串,插入右侧组,**位置在 `costPart` 之后、`stepPart` 之前**;`verifyState==""` 时**完全不出现**(不要留多余空格/分隔符)。各状态:
- `"running"` → 文本 `⟳ verifying…`,颜色**黄**(`lipgloss.Color("3")`)。
- `"ok"` → 文本 `✓ verified`,颜色**绿**(复用 `green`)。
- `"fail"` → 文本 `✗ ` + `verifyDetail`(若 `verifyDetail==""` 用 `verify failed`),颜色**红**(`lipgloss.Color("1")`)。失败摘要可能较长,**截断到 ≤ 40 列**(用现有 `TruncateVisible` 若可见,或简单 rune 截断加 `…`),避免撑爆状态条。
**约束:**
- 只在 `verifyPart != ""` 时把它加进 `right` 的 `JoinHorizontal`,并在它与相邻段之间补一个空格分隔(与现有 `tokens/cache/cost/step` 之间的 `" "` 分隔风格一致)。
- **不要**改 `tokens/cache/cost/step` 的内容或顺序,不要改左侧 `left`,不要动 gap/宽度计算逻辑之外的东西(gap 用 `lipgloss.Width(right)`,你正常拼好 `right` 即可,无需手算)。
- 早返回分支(`s.Model==""` 时返回 `"loading…"`)保持不变。
**禁区:** 不改 `VerifyMsg`、`StatusBar`、`Update`、bridge(`cmd/lumen/terminal.go`);不碰其它渲染函数。
**验收命令:** `go test ./internal/tui/` 绿;`go vet ./internal/tui/` 干净;`make check` 仍绿。
**测试要求(`verify_render_test.go`,≥3 例):** 构造 `m := NewModel()`,先 `m.Update(StatusMsg{Model:"deepseek-chat", Provider:"deepseek", Mode:"default"})` 让状态条进入正常渲染分支,再分别 `m.Update(VerifyMsg{State:"running"})` / `{State:"ok"}` / `{State:"fail", Detail:"verify failed: build (2 diagnostics)"}`,调用 `out := m.renderStatus(120)`,断言:
- running → `out` 含 `verifying`;
- ok → 含 `verified`;
- fail → 含 `✗` 且含 `build`;
- 空状态(不发 VerifyMsg)→ `out` **不含** `verifying/verified/✗`。
（断言用 `strings.Contains`;ANSI 颜色码不影响子串匹配。）

> 回来 Claude 跑验收 + 核禁区 + 终审。与其它卡无冲突。
