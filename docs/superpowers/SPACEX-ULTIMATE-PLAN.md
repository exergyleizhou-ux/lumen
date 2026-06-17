# Lumen + 绿洲 Oasis 终极规划 (SpaceX 版)

> 2026-06-19 · 以第一性原理审视 Lumen 与 Oasis 的双体闭环

---

## 0. 核心命题

```
╔══════════════════════════════════════════════════════════╗
║                                                          ║
║   Lumen 是绿洲的"算法工厂" —— 写算法、验证、打包。       ║
║   绿洲是 Lumen 的"算法货架" —— 注册、售卖、执行、出凭证。 ║
║                                                          ║
║   工厂不碰货架，货架不碰工厂。                             ║
║   边界就是一份 oasis.toml ↔ compute.Algo 契约。           ║
║                                                          ║
╚══════════════════════════════════════════════════════════╝
```

**类比 SpaceX**：
- **Lumen = 霍桑工厂**。造 Merlin 发动机。不卖火箭票。
- **绿洲 Oasis = 发射场 + 载荷适配器**。火箭来了就能装，装了就能飞。
- **算法 = 发动机**。工厂造出来，送到发射场，装箭，点火，出数据，收钱。

**关键约束**：
> 工厂的质量由发动机在发射台上的点火测试来证明。不是设计图、不是仿真、不是代码审查。
> 
> Lumen 写的算法 = 必须在绿洲后排的 Docker runner 里 **真拉镜像跑一次**。
> `--network none`、`/data:ro`、`/out/output.json` —— 跑通了，才算工厂出了货。

---

## 1. 当前产线状态（SpaceX 标准）

### 1.1 已就位（可点火）

| 工位 | 状态 | 证据 |
|------|------|------|
| 脚手架 | ✅ | `lumen oasis init my-algo` → 代码+Dockerfile+配置 |
| 本地自检 | ✅ | `lumen oasis validate` → 断言 manifest 契约 |
| 打包 | ✅ | `lumen oasis build` → Docker image by pinned digest |
| 推送 | ✅ | `lumen oasis deploy` → push to registry |
| 货架注册接口 | ✅ | marketplace `POST /compute/algorithms` |
| 作业提交 | ✅ | marketplace `POST /compute/jobs` |
| 防篡改 | ✅ | SHA-256(input) → SHA-256(output) → Ed25519 签名 → 链上验证 |
| Runner 执行器 | ✅ | `DockerRunner`：`--network none`、`/data:ro`、`/out:rw`、digest 固定 |

### 1.2 未就位（发动机没真飞过）

| 缺失 | 严重度 | 影响 |
|------|--------|------|
| `lumen oasis deploy` 之后自动调 marketplace API 注册算法 | 🔴 | 工厂和货架之间靠人力搬运 |
| marketplace runner 事件循环（自动拉 pending job、执行、写结果） | 🔴 | 货架上有货，但没人送货 |
| 算法回归测试自动累积 | 🟡 | 改算法不知道炸没炸 |
| 多语言 verify（Python / JS） | 🟡 | 非 Go 项目自修闭环失效 |
| 故障归零系统（agent 连续失败 → 自动 git checkout + 提示） | 🟡 | 坏了就停，不懂自己修 |
| 真 Docker runner 跑过一次 | 🟡 | 测试用 mock，没碰过 Docker daemon |

---

## 2. 三个阶段（SpaceX 节奏）

### 第一阶段：「猛禽试车」——即刻—2周

> 发动机在测试台连续点火 100 次，零故障。工厂和货架之间的传送带首次运转。

| # | 任务 | 验收标准 | 对应 SpaceX |
|---|------|---------|------------|
| 1 | **密钥轮换** | 新 key 不在任何文件/历史/日志中出现 | 燃料泄漏修复 |
| 2 | **首次真容器飞越** | `docker run --network none algo@sha256:xxx` 输出 `output.json`，Ed25519 签名验证通过 | 首次静态点火 |
| 3 | **传送带接通** | `lumen oasis deploy` → 自动调 marketplace API 注册算法 → 返回 algo ID | 工厂→发射场专线铁路 |
| 4 | **货架自动发货** | marketplace runner 事件循环：pull pending job → execute → attest → mark done | 自动发射程序 |
| 5 | **dogfood 100 轮** | Lumen 自己改 Lumen 100 次。统计首次 build 通过率、自修成功率、逃逸次数 | 重复点火测试 |
| 6 | **包数砍到 ≤ 40** | 每个保留的包必须被至少一条真实执行路径引用 | 删掉多余的传感器 |
| 7 | **故障归零板** | 同一文件连续 2 次 verify 失败 → 自动 `git checkout` 回滚 → 提示用户 | 发射 abort 自动回滚 |
| 8 | **性能基线** | 回车→首个 token（ms），编辑→verify 完成（s），端到端跑一个算法（min） | 点火到最大推力的时间 |

### 第二阶段：「星舰堆叠」——2—4周

> 工厂满产，货架全自动。一台发动机从图纸到入轨完全自动化。用户在 lumen 里敲 `lumen oasis deploy` 后，货架上新算法上线，买家下单，runner 拉镜像跑，结果带签名返回。

| # | 任务 | 验收标准 |
|---|------|---------|
| 9 | **算法自动上架** | 同 #3。一步到位 |
| 10 | **货架调度系统** | pending job → runner pull + execute → write output + attest → notify buyer |
| 11 | **回归测试自动积累** | 每个算法执行完，输入/输出存入 fixture 库。下次改算法，自动回归对比 |
| 12 | **多语言 verify** | Python: `ruff` + `pytest`。JS/TS: `tsc` + `jest`。lumen 自动检测项目语言 |
| 13 | **Agent 状态真持久** | 退出重启不丢上下文、不丢 verify 状态、不丢 repair cycle 计数 |
| 14 | **TUI 达到可用** | `lumen tui` 能真替代 `lumen chat` 完成一次完整 agent 对话（含 plan + verify + 多面板） |
| 15 | **算法商店 MVP** | 至少 3 个真实算法上架，每个都有一次完整 C2D 执行记录 |

### 第三阶段：「入轨」——4—8周

> 外部用户从零装上 Lumen，写一个算法，上架 marketplace，别人付钱买。全链路无人干预。

| # | 任务 | 验收标准 |
|---|------|---------|
| 16 | **5 分钟到第一个 PR** | 新用户 `clone → build → 配置 → 问答 → 改代码 → verify 通过` |
| 17 | **算法商店上线** | 3+ 真实算法在 marketplace 可购可执行 |
| 18 | **月可靠性报告** | 自动统计：崩溃/逃逸/verify 率/平均修复轮数/中断率 |
| 19 | **包数砍到 ≤ 30** | 每个模块边界清晰 |
| 20 | **发布 v1.0** | 北极星 4 指标全部 ≥ 基线 |

---

## 3. 三条 SpaceX 铁律

### 3.1 "删掉一切不必要的东西"

```
当前: 58 包
第一阶段目标: ≤ 40
第三阶段目标: ≤ 30
删包规则: 被至少一条真实执行路径引用 → 保留。否则 → 删。
```

- 猎鹰 9 没有逃逸塔。Starship 没有降落腿。
- Lumen 不需要 web IDE。不需要多租户。不需要 k8s operator。

### 3.2 "测试到炸，修好再飞"

```
每次 CI: go build / vet / test -race
每轮 dogfood: 自动记录失败原因 → 归零分析 → 加回归测试
连续逃逸 = 设计缺陷，不是运气不好。
```

- 猛禽发动机爆炸 40 次。每次捡回来，修好，再点火。
- Lumen 的 verify 失败也一样。不是 bad luck，是设计里少了一层 guard。

### 3.3 "机器造机器的机器"

```
lumen oasis 不是 CLI 玩具。
它产出能在 marketplace 上卖钱、别人真花钱买的算法。

只有当:
  1. 你用 lumen 写了一个算法
  2. 算法在 marketplace 上架
  3. 陌生买家付 $1 购数据+算法+执行
  4. 算法在 runner 里跑完
  5. 输出带 Ed25519 签名
  6. 买家验证签名——确认卖家没造假
  
这六步全通，工具链才是生产级的。
```

---

## 4. 北极星指标（可度量）

| 指标 | 当前基线 | 第一阶段目标 | 第三阶段目标 |
|------|---------|------------|------------|
| verify 首轮通过率 | 待记录 | ≥ 60% | ≥ 85% |
| 自修成功率 | 待记录 | ≥ 70% | ≥ 90% |
| `-race` 全绿天数 | ✅ 当前绿 | 持续 14 天 | 持续 30 天 |
| 包数 | 58 | ≤ 40 | ≤ 30 |
| 端点延迟 (LLM) | 未知 | < 2s 到首个 token | < 1s |
| C2D 端到端 | 0 次 | 1 次真 Docker 跑通 | 10 次 |
| 算法上架数 | 0 | 1 | 3+ |
| API key 泄漏 | 1 处 | 0 | 0 |

---

## 5. 工厂↔货架契约（不可变）

```
lumen 侧（工厂）:
  oasis.toml {
    name
    runtime       // "docker" | "wasm" | "tee"
    image         // registry.example.com/algo/name:latest
    image_digest  // sha256:...
    entrypoint    // 可选
    output_kind   // "model" | "metrics" | "report" | "bytes"
    version
    params_schema // JSON
  }
  → lumen oasis build → docker push → digest
  → POST /compute/algorithms { name, runtime, image, image_digest, ... }

marketplace 侧（货架）:
  compute.Algo { ID, SellerID, Name, Runtime, Image, ImageDigest, ... }
  compute.Job  { ID, AlgorithmID, BuyerID, DatasetID, Status, Attestation... }
  DockerRunner.Run {
    --network none
    -v /data:ro
    -v /out:rw
    algo@sha256:digest
  }
  → AttestResult → Ed25519 sign → persist

两边的 Algo 类型字段一一对应。
改动任一边的契约 = 两根火箭的适配器对不上 = 爆炸。
```

---

## 6. 立即启动的第一刀

**Phase 1, Task 2 & 3: 真容器飞越 + 传送带接通**

1. 装 Docker Desktop（如果还没装）
2. `docker pull alpine:3.20` 确认 Docker 可用
3. 用一个极简算法 image 跑 `DockerRunner.Run()`，不 mock
4. 输出 `output.json` → SHA-256 → Ed25519 签名
5. `lumen oasis deploy` 成功后自动 POST marketplace API
6. 跑完这 5 步 → 工厂和货架之间的传送带正式运转
