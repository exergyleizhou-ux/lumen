# 用 lumen oasis 写一个 C2D 算法(5 分钟上手)

`lumen oasis` 是 Oasis「可用不可见」数据市场的**官方算法作者工具链**。你用它写一个隐私计算算法、本地把关、推到市场;买家在 `--network=none` 沙箱里对**他们看不到的数据**跑你的算法,只拿到聚合结果 + 一张可验证的结果证书。

> 适合谁:有一段数据分析/建模逻辑、想把它变成"别人能在私有数据上调用、但拿不到原始数据"的服务的人。无需懂市场后端 —— 工具链替你对齐契约。

## 0. 前置

- 一个容器运行时(Docker)
- 一个镜像 registry(本地 demo:`docker run -d -p 5000:5000 registry:2`)
- `lumen`(本仓库 `go build -o lumen ./cmd/lumen`)

## 1. 从一个**能跑的模板**起步(不是空骨架)

```bash
lumen oasis templates                      # 看可用模板
lumen oasis init myalgo --template stats    # 脚手架一个完整可跑的例子
cd myalgo
```

内置 3 个模板,都是纯 Python stdlib、**只输出聚合**(原始行永不离开数据边界):

| 模板 | 做什么 |
|---|---|
| `stats`(默认) | 每列描述统计:n / mean / min / max / std |
| `correlation` | 数值列两两 Pearson 相关矩阵 |
| `linreg` | 多元线性回归(梯度下降)→ 系数 + R²(在你看不到的数据上训练一个模型) |

`init` 出来的 `train.py` **当场就能跑** —— 你可以直接用,或改成你的逻辑。

## 2. C2D 容器契约(你只需记这三条)

1. **读** `/data`(只读挂载的数据集 CSV/TSV)+ 可选 `/params.json`(超参)
2. **写** `/out/output.bin` = `zip(model.json, metrics.json)`
3. **只输出聚合** —— 日志和输出里都不要出现任何一条原始记录

模板已经把读取、写出、日志、数值列识别都写好了;你只动 `main()` 里的计算部分。本地测试可用 `VO_DATA_DIR` / `VO_OUT_DIR` / `VO_PARAMS` 覆盖路径。

## 3. 把关 → 构建 → 校验 → 上市

```bash
# 编辑 oasis.toml:把 image 改成你的 registry(如 127.0.0.1:5000/algo/myalgo),填 source_ref
lumen oasis validate .     # 校验 manifest(占位 registry / 空 source_ref 会告警)
lumen oasis build .        # docker build + 写 provenance lockfile(钉死源码哈希)
lumen oasis check .        # ★ 在【与市场完全相同】的 --network=none 沙箱里跑一遍,验契约
lumen oasis verify .       # 重新核验工作区与 lockfile 一致(源码没漂移)

# 上市(对着你的 Oasis 后端):
export MARKETPLACE_URL=http://localhost:8090
export MARKETPLACE_TOKEN=<你的 ops token>     # 见下
lumen oasis deploy .       # 推镜像 + 把 digest 钉回 lockfile + 注册到市场
# 加 MARKETPLACE_TRUST=1 可顺手 approve+trust(否则留 pending 等 ops 审)
```

`lumen oasis publish .` = `build → check → deploy` 一条龙。

> **拿 ops token**:`POST /api/v1/auth/login`,body 是 `{"account":"<email>","password":"..."}`(字段是 `account` 不是 `email`),token 在响应的 `data.tokens.access_token`。

## 4. 为什么可信:可验证溯源

`oasis build`/`deploy` 会写一份 `oasis-lock.json`,把**源码 SHA-256 → 镜像 digest** 钉死;`deploy` 把市场返回的 digest 回写进去。买家拿到结果后可以**重算 SHA-256** 与证书比对 —— 整条链(源码 → 镜像 → 市场注册 → 沙箱执行 → 输出指纹 → 证书)都可重新验证。

## 5. 真实例子(已端到端验证)

`stats` 模板的算法 `colstats` 在真 Oasis 上跑通,在 Iris 数据集上产出过真证书 **`VO-3D77D6E1E44C`**:478B 纯聚合输出,re-hash `9b0eec98…` 与证书一字不差,算法镜像 digest `sha256:7fbfd41a454c…` 在作者 lockfile 和市场记录里完全一致。完整证据见 [`docs/记录-C2D作者闭环-真实环境整合证明.md`](记录-C2D作者闭环-真实环境整合证明.md)。

目前 demo 市场上由 `lumen oasis` 模板上市的算法:`colstats`(stats)、`correlation`、`linreg` —— 三个都 approved + trusted。

## 6. 命令速查

| 命令 | 作用 |
|---|---|
| `lumen oasis templates` | 列出算法模板 |
| `lumen oasis init <name> [dir] --template <key>` | 脚手架一个完整可跑的算法 |
| `lumen oasis validate .` | 校验 manifest（含告警） |
| `lumen oasis build .` | 构建镜像 + 写 provenance lockfile |
| `lumen oasis check .` | 在真 `--network=none` 沙箱里验 C2D 契约 |
| `lumen oasis verify .` | 核验工作区与 lockfile 一致 |
| `lumen oasis deploy .` | 推镜像 + 钉 digest + 注册到市场 |
| `lumen oasis publish .` | build → check → deploy 一条龙 |
