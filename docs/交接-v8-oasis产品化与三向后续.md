# 交接 v8 — lumen oasis 产品化收束 + 三条后续线的规划

> 单次自包含交接。本会话把 **C 线(`lumen oasis` = Oasis C2D 算法作者工具链)**从"修一个 provenance bug"做到"产品化 + 隐私强制 + 真实证明 + 全目录可验证证书",并把后续三个方向各推到诚实上限。
> 仓库:`~/lumen`,`origin/main @ 1676013`,同步干净,`go build ./...` + `go vet` + `go test ./...` 全绿。本会话 17 个 PR(#93–109),全部 TDD+验证+合并。
> 工具链 PATH(每个新终端):`export PATH="$HOME/.local/bin:$HOME/sdk/go/bin:$HOME/.lmstudio/bin:$PATH" GOTOOLCHAIN=local GOFLAGS=-mod=mod`

---

## 0. 本会话做完了什么(C 线 + 1/2/3)

**C 线(#93–107)**:
- **溯源**:完整 provenance lockfile + 部署后回写 image digest(#93);`lumen oasis verify` 重核验工作区 vs lock(#94);validate 对占位 registry/空 source_ref 告警(#95);**活体逮到并修复 `ComputeSrcHash` 在 `build .` 时跳过整棵树→空哈希的真 bug**(#96)。
- **整合证明**:全链路在 live Oasis(:8090)跑通 + 独立 re-hash 验证的真证书(#97/#98)。
- **产品化**:`lumen oasis init --template <key>` + `lumen oasis templates`,**8 个完整可跑模板**(stats/histogram/quantiles/correlation/**groupby=k-匿名**/linreg/logreg/**dp-stats=ε-差分隐私**),全 TDD(语法+跑+产出合约合法 output.bin);bare init 脚手架可跑 stats 而非 TODO;作者 quickstart + README "C2D Author Toolchain" 段(#99–107)。
- **隐私强制**:`oasis check` 的 **leak lint**(哨兵法验"只输出聚合",抓泄露不误伤)(#103)。两个真隐私原语:dp-stats 的 Laplace 噪声 + groupby 的小组抑制。

**三向后续(#108–109)**:
- **#3 真实买家 job→证书**:单会话用 API 驱动完整买家闭环,**8 个算法在真 Iris 上各出一张可验证证书**(re-hash 对得上),目录从"已注册"升级为"已证明能跑"(#108)。
- **#1 Lumen 弱点**:multi_edit 全成功才落盘的原子性无测试 → 抽 `applySequentialEdits` 纯函数 + 测试(#109)。
- **#2 GTM**:作者 quickstart 是 Lumen 的贡献;市场 GTM 是 Oasis 会话的领域。

---

## 1. 三条后续线(每条自包含,开新会话用)

### 🅰 Track A — 本地 eval 实验跑数(B 线,**卡在散热**)

**目标**:跑出 Lumen 第一份**真实本地模型编码-agent pass-rate 基线 + 失败模式分布**(研究线)。设计 + 分类器 + 仪表**全部已 ship**,就差跑数。

**已就绪**(无需再写代码):
- 设计文档:`docs/research/设计-本地编码agent可行性研究.md`(审稿人2 把关过;诚实定位 = 单模型案例研究,非 "frontier";IV = ρ = 首轮prompt_tokens/服务端context窗口)。
- eval 基线:`docs/eval-baseline.md`(gemma-4-12b 5/6,16k context;8k 溢出会让模型退化成打招呼)。
- 失败分类器 `internal/eval/classify.go` + 事件接线 `internal/eval/collector.go`(已合并、有测试)。
- harness:`lumen eval --tasks evals/tasks --repeat N --json --eff-window <N> --tool-profile <p> --model-label <m>`(自描述结果含 ρ、失败模式、首轮 prompt tokens)。

**怎么跑(§4a 最小可证伪,一次性)**:
```bash
~/.lmstudio/bin/lms load google/gemma-4-12b -c 16384 --parallel 1 --estimate-only -y   # 先看内存,别越 13GB panic 线
~/.lmstudio/bin/lms load google/gemma-4-12b -c 16384 --parallel 1 -y
~/.lmstudio/bin/lms server start
# 写 cwd lumen.toml: kind=openai base_url=:1234/v1 model=google/gemma-4-12b; [tools] profile=core; [agent] context_window=16384 turn_timeout="20m" temperature=0.2
cd ~/lumen && go run ./cmd/lumen eval --eff-window 16384 --tool-profile core --repeat 5 --json
```
跑 §4a 的 2×2:gemma × {8k,16k} × {core,full}(决定性 cell = 16k×full,验 cliff 落在同一 ρ=模型属性 还是绝对 schema tokens=Lumen 属性)。

**⚠ 散热门(本会话实测的现实)**:**24GB MacBook Air 持续本地推理会过热**。上次跑到一半用户喊停。新会话**必须**:小批(一次 1 个 cell)+ 散热间歇 + 让用户在旁盯活动监视器。**别一口气跑完整 §4a。** 模型 LOAD 是 panic 风险(`lms load --estimate-only` 预检);稳态推理不吃额外内存但发热。

### 🅱 Track B — 边际打磨(诚实排序,大多低价值/有权衡)

按价值递减,**都不是必须**:
1. **第二道隐私检查**:行数缩放检测(输出大小不该随行数线性增长)—— 补 leak lint 抓不到的更隐蔽泄露。中价值,要 2 次沙箱跑。
2. **`oasis check --data <file>`**:作者用自己真实样本数据验(现只用合成数据)。UX。
3. **tool/builtin 测试覆盖**:很多核心工具(bash/glob/grep/code_search)无 _test.go(但核心逻辑如 applyReplace 经 editutil_test 覆盖)。grind,价值中等。
4. **digest 回写 oasis.toml**(现只写 lockfile)。小。
5. **刻意不做**:① live 输出 markdown 渲染 —— `event.Text` 是真 token 流式,渲染需完整块 = 流式权衡,不是干净 bug,当前 stripMD 流式是对的;② bash 沙箱默认 ModeAuto —— 会搞坏 agent 自己的 go build/test(verify-after-edit),有意保留 off。

### 🅲 Track C — Oasis 市场侧 GTM(**另一个会话的领域**)

招卖家 / 定价 / 分发 / verify-saas 分发工具包(他们仓库 PR #246)。**Lumen 这边的贡献已就位** = 作者 quickstart `docs/教程-用-lumen-oasis-写C2D算法.md`(给市场 GTM 当作者 onboarding)。新会话在 `~/ai-data-marketplace-cap`(live :8090 跑的 worktree)做,不是 `~/ai-data-marketplace`(旧分支)。

---

## 2. 关键坐标 / 踩坑(跨会话复用)

- **live Oasis 市场**:后端 :8090(进程是另一个会话),pg :55432,本地 registry `oasis-registry` @ 127.0.0.1:5000,前端 :3200。**别用 ~/ai-data-marketplace(旧分支),用 ~/ai-data-marketplace-cap。**
- **市场账号**(都 `Oasis1234!`):`demo-ops@oasis.test`(ops)、`demo-seller@oasis.test`、`demo-buyer@oasis.test`。
- **登录 API 坑**:`POST :8090/api/v1/auth/login`,body 字段是 **`account` 不是 email**;token 在 **`data.tokens.access_token`**(嵌套),15 分钟过期。
- **C2D 买家闭环 API**(本会话单会话跑通,可复用):seller `PUT /api/v1/datasets/:id/compute-offer {enabled,trust_level:"L1",allowed_algorithm_ids}` → buyer `POST /api/v1/datasets/:id/compute/purchase {quota}`(→ data.id = entitlement)→ buyer `POST /api/v1/compute/jobs {dataset_id,entitlement_id,algorithm_id,params}` → 轮询 `GET /api/v1/compute/jobs/:id`(→ output_reviewing)→ ops `POST /api/v1/admin/compute/jobs/:id/release` → buyer `GET /api/v1/compute/jobs/:id/certificate` + `/output`(re-hash 比对 output_sha256)。Iris 数据集 id `76201ae3-97ad-4438-976c-74cf873fc013`。
- **lumen oasis deploy**:需 `MARKETPLACE_URL` + `MARKETPLACE_TOKEN`(+ `MARKETPLACE_TRUST=1` 自动 approve+trust)。
- **8 个上市算法 id**:见 `GET /admin/compute/algorithms?status=approved`(默认列表按 pending 过滤,要带 status)。
- **PR 流程**:`gh pr create` 的 `--body` 别用反引号(zsh 会命令替换);GitHub 偶发 SSL flake,重试即可。

---

## 3. 一句话现状

`lumen oasis` 是一条**产品化、隐私强制、真实证明**的垂直工具链(8 模板 / 8 上市算法 / 8 张可验证证书 / 17 PR)——Aider/Cursor/CC 没有的差异化。三条后续线:A 卡散热、B 边际、C 是市场会话的领域。本会话收束于一个强、完整、可信的状态。
