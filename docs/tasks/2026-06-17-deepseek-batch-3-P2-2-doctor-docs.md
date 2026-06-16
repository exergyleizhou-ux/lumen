# DeepSeek 任务卡 — P2-2:doctor 自检覆盖 + 安装/首启文档

> 对应 `规划-生产级终极方案书-v1.md` 的 P2-2。自包含。只改"输出文件",别碰"禁区"。完成跑"验收命令"全绿,Claude 终审后合。
> 工具链:`PATH="$HOME/.local/bin:$HOME/sdk/go/bin:$HOME/.local/go/bin:$PATH"`,`GOTOOLCHAIN=local GOFLAGS=-mod=mod`,在 `~/lumen`。
> 铁律:不碰 agent loop / 安全逻辑 / 架构;只动点名文件;只加测试或按现有模式补码。

## 背景(现状,已读代码)
`internal/doctor/doctor.go` 已有健康检查框架:
- `type Result struct { Name, Status, Detail string }`(Status ∈ "ok"|"warn"|"fail")
- `type Report struct { Results []Result; AllOk bool }`
- `func Run(cfg *config.File) *Report` 里依次调用 `r.checkConfig()`、`r.checkProvider(pc)`、`r.checkWorkspace()`、`r.checkGit()`。
- 每个检查用 `r.add(Result{Name:..., Status:..., Detail:...})` 追加。
- 已有 `lookPathImpl` 用于查可执行文件(参考 `checkGit` 的写法)。

## 卡 P2-2a — 给 doctor 补 3 个检查项
**输出文件:** 改 `internal/doctor/doctor.go`(只新增检查方法 + 在 `Run` 里调用)+ `internal/doctor/doctor_test.go`(加用例)。
**新增 3 个检查方法**(仿照现有 `checkGit`,各自 `r.add(...)`,并在 `Run()` 里调用):
1. `checkGoToolchain()` — 查 `go`:不在 PATH → `fail`;能跑 `go version` → `ok`,Detail 放版本串。
2. `checkGopls()` — 查 `gopls`:不在 PATH → `warn`(Detail:"LSP features disabled; install golang.org/x/tools/gopls");在 → `ok` + 路径。
3. `checkVerify()` — verify-after-edit 可见性:用 `config.FindConfig()` 找 lumen.toml;读出后用 `editverify.ConfigFromTOML` 看 `[verify]` 是否 enabled,Detail 写 "enabled (scope=…)" 或 "disabled"。**顺带安全提示**:若 lumen.toml 文本里出现内联 `api_key = "sk..."`(明文密钥),加一条 `Result{Name:"security:api_key", Status:"warn", Detail:"api_key is inline in lumen.toml — move to env/.env and rotate"}`。
**禁区:** 不改 `Result`/`Report` 结构;不动 `checkProvider` 的网络探测逻辑;不碰 `internal/editverify`(只调用它的 `ConfigFromTOML`/`DefaultConfig`)。
**验收命令:** `go test ./internal/doctor/` 绿;手动 `go run ./cmd/lumen doctor`(若有该子命令)输出含新项。
**测试要求(≥4 例,表驱动或子测试):** go 存在→ok;gopls 缺失→warn;[verify] disabled→对应 Detail;内联 api_key→出 security warn。可用临时目录 + 写假 lumen.toml 驱动 `checkVerify`(把可注入的部分抽成接受 raw []byte 的小辅助,避免依赖真实 PATH/cwd)。

## 卡 P2-2b — 安装/首启文档校正
**输出文件:** 改 `README.md` 的 "Quick Start" 一节(以及必要的安装段);**不改**其他章节的功能描述。
**要求:**
- 给出**真实可跑通**的最短路径(目标 ≤5 分钟):`git clone … && cd lumen && go build -o bin/lumen ./cmd/lumen`(或 `make bin`),然后配置 `lumen.toml`(给最小示例:default_model + 一个 provider + `[verify]` 默认),再 `./bin/lumen`。
- 明确**前置**:Go 1.23+;可选 gopls(给安装命令);`./bin/lumen doctor` 自检。
- 校正任何与现状不符的旧描述(包数、命令名等;别引入 §反模式 的行数/包数吹嘘)。
**禁区:** 不改 README 的 Features/Architecture/Roadmap 等其它章节(除非与 Quick Start 直接矛盾)。
**验收:** 按 README 步骤在干净环境(或临时目录 clone)能真的 build 出二进制并启动;`make check` 仍绿。

> 合并顺序:P2-2a、P2-2b 互不冲突可并行。各自回来 Claude 跑验收 + 核对禁区 + 终审。
