# 记录 — C2D 作者闭环在真实环境的端到端整合证明

> 2026-06-21。两个会话协作完成:Lumen 会话(作者工具链)+ Oasis 市场会话(ops 基建)。
> 这是 C 线(`lumen oasis` = Oasis 官方 C2D 算法作者工具链)**真跑通**的证据存证 —— 不是设计、不是单测,是在真 docker + 真 registry + 真 live 市场上端到端执行的一次完整 run。

## 0. 一句话

用 `lumen oasis` 把一个**全新的隐私算法** `colstats` 从 `init` 一路走到在 live Oasis 市场(:8090)上 **registered + approved + trusted**,**整条溯源链(源码哈希 → 镜像 digest)端到端一致、可重新验证**。过程中活体逮到并修复了一个不真跑就发现不了的 provenance 真 bug。

## 1. 被证明的算法:colstats

一个真实、合规的 C2D 隐私算法 —— **只有聚合离开数据边界**:逐列描述统计(n / mean / min / max / std),零原始行进入输出或日志。买家学到私有数据集的"形状",看不到任何一条记录。纯 Python stdlib,符合 C2D 容器契约(读 `/data` 只读、写 `/out/output.bin` = zip(model.json, metrics.json))。

## 2. 端到端链路(每步真实执行)

| 步骤 | 命令 | 结果 |
|---|---|---|
| 脚手架 | `lumen oasis init colstats` | ✅ 生成 manifest + Python 骨架 + Dockerfile |
| 实现 | (填入 colstats 逻辑) | ✅ python 语法通过 |
| 校验 | `lumen oasis validate` | ✅ **#95 两个告警都触发**(占位 registry + 空 source_ref);修正后干净 |
| 构建 | `lumen oasis build .` | ✅ docker build → 真镜像;写完整 provenance lockfile(**#93**) |
| 契约自检 | `lumen oasis check .` | ✅ **真 `--network none --read-only` docker 沙箱**跑通,产出合法 `/out/output.bin` |
| 溯源核验 | `lumen oasis verify .` | ✅ **#94**:`source matches the provenance lock` |
| 部署 | `lumen oasis deploy .` | ✅ 推真 registry(127.0.0.1:5000)+ digest 回写 lockfile(**#93**)+ 注册到 live :8090 + approve + trust(L1) |

## 3. 溯源链一致性(核心证据)

同一个镜像 digest 钉死在作者本地和市场记录上 —— 溯源链完整、可重算:

| 环节 | 值 |
|---|---|
| 算法源码哈希(lockfile `source_sha256`) | `6bd1a6b516715e6436a3740dab7d29519b5bdee93784a919f154b8b21f3d765e` |
| 镜像 digest(lockfile `image_digest`) | `sha256:7fbfd41a454c93791fe57d56490e7a5b00422cc8bed305a987f12420af6c3b1f` |
| 镜像 digest(live 市场 :8090 DB 注册记录) | `sha256:7fbfd41a454c93791fe57d56490e7a5b00422cc8bed305a987f12420af6c3b1f` |
| 市场算法 id | `ec82765f-369d-4713-95ed-9fafa819b367` (status=approved, trusted=L1) |

**lockfile digest == 市场注册 digest**,一字不差。从作者本地 → build → push → 市场注册,溯源链端到端钉死在同一 digest。

## 4. 活体逮到的真 bug(#96)—— "真跑"的最大回报

`oasis build .`(最常见用法)算出的源码哈希一开始是**空输入的 SHA-256**(`e3b0c44298fc…`)。根因:`filepath.Walk` 的第一个回调是走查根目录本身,当 `dir == "."` 时其 `Name()` 是 `.`,被隐藏目录守卫 `strings.HasPrefix(name, ".")` 命中 → `SkipDir` 跳过了整棵树 → 哈希了个寂寞。**provenance 源码哈希(买家 cert 的锚)一直是假的。**

单元测试全用绝对路径 `t.TempDir()`(根名不是 `.`),所以**漏了**。只有真跑这个闭环才暴露。修复(#96):走查根目录永不应用隐藏目录跳过(`path == dir`)。修后是真哈希 `6bd1a6b51671…`,`verify` 确认。

> 这恰好证明了"深做 C + 真跑"的决定是对的 —— 不真跑,这个 bug 永远发现不了。

## 5. 全链路闭环 —— 真 result cert(已独立验证)

市场使用流也跑通了(Oasis 会话备 seller/buyer + 数据集,买家提 job):**卖家**在 Iris 上开 colstats 的 compute-offer → **买家**提 job `0d13364c-3029-494d-851e-966a4e1c065f` → **`--network=none` 沙箱真跑 colstats** → ops 放行 → 出证书 **`VO-3D77D6E1E44C`**。

**Lumen 会话独立验证(不靠转述,亲手 re-hash)**:取证书 + 478B output,本地 SHA-256 重算 = `9b0eec98…`,与证书 `output_sha256` 一字不差。证书把**输出指纹**绑到**已审核算法的钉死镜像 digest**:

| 链环 | 值 |
|---|---|
| 算法源码哈希(lockfile) | `6bd1a6b51671…` |
| 镜像 digest(lockfile == 市场注册 == **证书 algorithm.image_digest**) | `sha256:7fbfd41a454c…` |
| 输出指纹(证书 `output_sha256` == **我独立 re-hash 478B output**) | `9b0eec98…` |
| 证书号 / job / 数据集 | `VO-3D77D6E1E44C` / `0d13364c…` / Iris `76201ae3…` |

**整条链端到端钉死、可重算、已验证:作者源码 → 镜像 digest → 市场注册 → 沙箱执行 → 输出指纹 → 证书。** 证书诚实声明"平台自行出具,尚未接入第三方公证/区块链"。

**Lumen → Oasis 垂直整合全链路闭环 —— Lumen 写算法 → 部署进市场 → 买家沙箱真跑 → 一张可验证的结果证书,且每一环都被独立 re-hash 验证过。**

## 6. 复现

前置:docker 在跑、本地 registry `oasis-registry` @ 127.0.0.1:5000、live Oasis 后端可达、一个 ops 账号。

```bash
lumen oasis init colstats && cd colstats
# 实现 train.py(读 /data,写 /out/output.bin = zip(model.json,metrics.json))
# 在 oasis.toml 把 image 改成你的 registry、填 source_ref
lumen oasis validate .          # 看 #95 告警
lumen oasis build .             # 真 provenance hash(#96 修复后)
lumen oasis check .             # 真 --network none 沙箱契约自检
lumen oasis verify .            # #94 溯源锁核验
# 登录拿 ops token(注意 login 字段是 account 不是 email):
#   curl -X POST :PORT/api/v1/auth/login -d '{"account":"<ops>","password":"<pw>"}'
#   token 在 data.tokens.access_token
MARKETPLACE_URL=http://localhost:<port> MARKETPLACE_TOKEN=<token> MARKETPLACE_TRUST=1 \
  lumen oasis deploy .          # 推 registry + 注册 + approve + trust
# 核验:GET /api/v1/admin/compute/algorithms?status=approved → 你的算法 + digest 一致
```

## 7. 相关 PR

#93 完整 provenance lockfile + digest 回写 · #94 `lumen oasis verify` · #95 validate 告警 · #96 源码哈希空值 bug(活体逮到)。均在 `origin/main`。
