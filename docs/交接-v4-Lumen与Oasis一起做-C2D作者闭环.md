# 交接 v4 — Lumen 与 Oasis 一起做(C2D 作者闭环为轴）

> 写于 2026-06-18。**这是新会话的单一自包含起点。** 全权由 Claude 决定方向；用户的指令是「两个项目 Lumen 和 Oasis 一起做」。

## 0. 决定（方向）

把两个项目焊成**一个产品**的那根线 = **C2D 作者闭环**(Lumen 的北极星 C = Oasis 的官方作者工具链)。

**整合北极星:** 作者用 **Lumen** 写一个 C2D 算法 → `lumen oasis check` 本地把关 → 推到 **Oasis** → 在 `--network none` 沙箱里隐私执行 → 买家拿到带签名的可验证结果。而 Lumen 同时是日常编码主力(做上面这件事本身就是 dogfood)。

**不预先纠结「先推 Lumen 还是先推 Oasis 给用户」** —— 先把作者闭环跑顺(Phase 1),拿到证据再定(Phase 2)。

## 1. 现在的位置（诚实）

- **Lumen 地基**:可靠性/正确性/并发三轮审计 + dogfood + 自审,~44 修复全部进 `main`。这是最难、最值钱、最隐形的部分,**已完成**。
- **C2D 作者闭环**:本地 `init→check→build→deploy` 通;**今天对齐了真 Oasis 契约**(`/out/output.bin`,见 §4)。
- **Oasis**:今天**活过来 + L1 沙箱端到端出凭证**(kmeans,cert `VO-2AE2E98F6136`)。L3 联邦可用、L2 TEE/PSI 已搭脚手架(卡在外部基建)。

## 2. Phase 1 起手卡（新会话,按优先级,0 外部依赖）

**卡 1（最高价值,整合证明）:Lumen 写一个全新 C2D 算法 → 真 Oasis 出凭证。**
- 选一个**不是 kmeans** 的算法(如 logreg / DP 统计聚合)。
- **用 Lumen 自己写它**(= dogfood,顺带刷 target A 的「不用救场」)。
- `lumen oasis init <name>` → 实现(读 `/data` + `/out/input.json`,写 `/out/output.bin` = zip(model.json,metrics.json)) → `lumen oasis check .`(应 PASS) → build + push 到 `127.0.0.1:5000` → 按 §3 在真 Oasis 上 ops 注册+approve trusted + seller 开 offer → buyer dev 授权 + 提交 job → 真沙箱执行 → ops 放行 → 取 cert。
- 这一张同时打到三个目标:Lumen 日常验收(A)+ 作者闭环(C)+ Oasis 真跑(市场)。

**卡 2:`lumen oasis publish`(或扩 `deploy`)一条龙。** 现在 init→check→build→push→register→offer 是手工拼脚本(见 `/private/tmp/oasis-run/scripts/demo-kmeans-up.sh`)。把它做成 oasis.toml 驱动的引导式一条龙,让作者一把过。

**卡 3:Lumen A dogfood 连击。** 卡 1 的算法实现就是 dogfood;继续用 Lumen 改 Lumen 做更多真任务卡,刷到「连续 N 张不用救场」(target A 唯一没勾的 DoD)。

## 3. 活的 Oasis — 接续方法（关键）

**活体环境在 worktree `/private/tmp/oasis-run`(在 `main` 分支),不是 `~/ai-data-marketplace`(那个在更老的 `chore/security-hardening` 分支,没有 algorithms/)。**

```bash
# 0) 工具链 PATH（每个新终端）
export PATH="$HOME/.local/bin:$HOME/sdk/go/bin:$HOME/sdk/node/bin:$PATH"

# 1) 依赖（复用带种子数据的旧卷 —— 必须用这个 project 名）
docker compose -p ai-data-marketplace-next16 up -d postgres redis

# 2) 后端（宿主机！这样 runner 能 docker run 算法镜像）
cd /private/tmp/oasis-run/backend
APP_ENV=development COMPUTE_RUNNER=docker AUTO_MIGRATE=1 STORAGE_DIR=./data/storage \
  GOTOOLCHAIN=auto nohup go run ./cmd/api > /tmp/oasis-backend.log 2>&1 &
curl -fsS http://localhost:8080/healthz   # → {"status":"ok"}

# 3) 持久镜像仓库（一直活着）: oasis-registry @ 127.0.0.1:5000
```

**已种好且持久（在 pgdata 卷里）:**
- 账号(pw `Oasis1234!`):`demo-ops@oasis.test`(role=ops)、`demo-seller@oasis.test`(KYC verified, 数据集 owner)、`demo-buyer@oasis.test`(KYC verified)。
- 一个 published 数据集指向磁盘 CSV `backend/data/storage/objects/demo/c2d-train.csv`(200 行,3 特征)。查 id:`docker exec ai-data-marketplace-next16-postgres-1 psql -U app -d ai_data_marketplace -tAc "select id from datasets limit 1"`。
- kmeans 算法已 trusted + offer(L1)。已出过 cert `VO-2AE2E98F6136`。

**端到端 API 流程（卡 1 复用）:** `POST /api/v1/auth/login` → ops `POST /api/v1/admin/compute/algorithms` + `/review {approved,trusted}` → seller `PUT /api/v1/datasets/:id/compute-offer {enabled,trust_level:"L1",allowed_algorithm_ids:[...]}` → buyer `POST /api/v1/datasets/:id/compute/purchase {quota}`(dev 直授权)→ buyer `POST /api/v1/compute/jobs {dataset_id,entitlement_id,algorithm_id,params}` → 轮询 `GET /compute/jobs/:id`(running→output_reviewing)→ ops `POST /api/v1/admin/compute/jobs/:id/release` → buyer `GET /compute/jobs/:id/certificate` + `/output`。

## 4. C2D 契约（ground-truth,别再搞错）

真 runner(`/private/tmp/oasis-run/backend/internal/modules/compute/runner_docker.go`)隔离旗标:`--network none --read-only --security-opt no-new-privileges --cap-drop ALL --pids-limit --memory --cpus --tmpfs`,`/data` ro,`/out` 可写。

- 算法**读** `/data`(数据集)+ `/out/input.json`(params)。
- 算法**写** `/out/output.bin` —— runner **只读这一个文件**,哈希 + Ed25519 签名。
- `output_kind=model` 时 `output.bin` = **zip(model.json, metrics.json)**。
- ⚠️ 之前记忆里说"写 stdout/output.json"是**错的**(读错了分支的 runner);lumen PR #2 已把 `oasis check` + scaffold 对齐到 `output.bin`。

## 5. 仓库状态 / 工具链

- **lumen**:`main @ fb4e973`(本会话全部已合:3 审计 + dogfood + P4 + oasis 契约对齐 PR #1/#2)。构建:`PATH="$HOME/.local/bin:$PATH" GOTOOLCHAIN=local GOFLAGS=-mod=mod`;门:`make check` + `go test -race ./...`。一卡一 PR,信 go test 不信报告。
- **marketplace**:用 `/private/tmp/oasis-run`(main @ 37f7b37)。**不要**用 `~/ai-data-marketplace`(老分支)做 C2D。

## 6. 仍是用户专属（我做不了）
- 轮换那把仍在 lumen git 历史里的 DeepSeek key(DeepSeek 控制台)。
- L2 真 TEE 硬件 / L3 Secretflow 多节点 / 合规法律 —— 卡在外部基建,长线。
