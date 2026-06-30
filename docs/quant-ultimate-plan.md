# lumen quant — 终极提升极致方案(Sonnet 5 可落地版)

> 2026-06-29 · 辩证综合自:6 路 GitHub 调研(qlib / Lean / zipline-reloaded / nautilus_trader / vectorbt / backtrader / RQAlpha / vnpy / in-toto·sigstore·SLSA·DSSE / risc0·sp1·ZKML·Numerai / RD-Agent·mlfinlab) + 5 视角对抗式批评(量化老兵 / 密码学审计 / 中国合规 / YC 产品 / Staff 工程)。
>
> 本文是给 Sonnet 5 直接执行的 backlog:每个工作项含「目标 / 借鉴 / 改哪些文件 / 验收标准 / 依赖」。

---

## 0. 一句话辩证结论

**我们一直在猛建"供给侧"(一个诚实的验证引擎),却没建"需求侧"(逼人要这张证书的人);而且我们嘴上说 "verified",手里其实只有 "可复现",还差 "非过拟合" 和 "数据可溯源" 这两条腿。**

终极版只做两件事:
1. **让 "verified" 名副其实** = 可复现(已有,需硬化)+ **非过拟合(全新)** + **数据可溯源(全新)**——三条腿缺一不可。
2. **先找到"需求侧执法者"** + **把"持续前推签名"做成旗舰**(不是一次性回测证书)。

> 三个最痛的领悟(批评共识):
> - **(量化老兵)致命 bug:涨跌停/prev_close 跑在 qfq 复权价上 → 每个除权日都算错。** 复权价让"可成交价"变成虚构。成本模型太仁慈(平滑滑点 + 静默截断)。
> - **(YC 产品)倒挂激励:有假回测的人最不会花钱来被你查。** 徽章没有"牙齿"——除非有人**要求**它。flagship 必须是"持续签名的实盘 track record",不是一次性回测。
> - **(Staff 工程)证书钉的是本地 image ID + `FROM python:3.11-slim` 可变 tag → 陌生人复现不了。** 确定性靠 4 位小数取整,不是引擎本质。

---

## 1. 借鉴地图(拿什么 / 弃什么)

| 项目 | 拿 | 弃 |
|---|---|---|
| **microsoft/qlib** | A股交易所语义当 spec:两段成本(open_cost/close_cost + min_cost 5元下限)、ADV 二次冲击 `impact*(trade_val/bar_val)²`、`limit_threshold` 元组式**方向性涨跌停**、`volume_threshold` 累计成交占比截断、停牌=NaN | 整个 RL/ML/pandas/torch 重框架(破坏纯 stdlib 确定性)——只抄语义,stdlib 重写 |
| **QuantConnect/Lean** | **含退市股的幸存者处理**(具体要清的标杆)、Fill/Slippage/Fee **可插拔接口分离** | C# 引擎 / 云数据 |
| **zipline-reloaded** | 复权数据层:splits/dividends 分表存,`history()` **按当前 sim 日 as-of 复权**(避免"全段后复权"的隐蔽未来函数);Blotter 抽象 | 它的执行/日历(2026 arXiv 2603.20319 报告其再平衡日日历 bug) |
| **nautilus_trader** | **可设种子的概率 FillModel**(部分成交/排队仍 bit-identical)、确定性 TradeId(FNV-1a)、liquidity_consumption 防重复成交 | L2/L3 撮合 + Rust 核 |
| **backtrader** | `slip_perc/slip_fixed/slip_open`(滑点作用在次开盘,正中我们 next-open)、**cheat-on-close 是未来函数地雷**(文档化我们故意不作弊) | 不可维护的 metaclass 架构 |
| **vectorbt** | **参数扫描思维**:把单个数字变成"成本/滑点/再平衡抖动敏感性分布"写进证书(可信度倍增器) | 向量化执行核(不建模部分成交 + 易引入未来函数) |
| **RQAlpha** | `today_closable` 式 **T+1 持仓级锁定**(今买不可卖)+ 变异测试 | — |
| **in-toto + DSSE + sigstore/cosign** | 证书重铸为 **in-toto v1 Statement + DSSE 信封**(`go-securesystemslib/dsse`),`predicateType` URI;可选 **Rekor 透明日志**(证明"T 时刻已存在",杀回填造假);`cosign verify-blob-attestation` 可验 | 不强制上链 |
| **SLSA** | predicate 里的字段词汇:externalParameters / resolvedDependencies(data-by-digest、引擎版本、镜像 digest) | 完整 SLSA build L3 流程 |
| **DVC(概念)/ ORAS** | **内容寻址数据**:数据集按 sha256 推到 OCI/S3,证书引用 ResourceDescriptor → 陌生人能**取到**原始数据,不只是核对哈希 | DVC 工具本身 |
| **mlfinlab / de Prado** | **CPCV(组合净化交叉验证 + purge/embargo)、PBO(回测过拟合概率)、Deflated Sharpe、多重检验校正** | — |
| **microsoft RD-Agent** | LLM 提因子的纪律:**IC 去重/新颖性门(corr>阈值否决)、8 指标全过才晋级、严格留出 LLM 看不到的 OOS、跨域跨期迁移测试** | — |
| **risc0/sp1 + Numerai** | **分层信任阶梯**(不 ZK 整个回测):L1 重跑+签名 / L2 TEE 封装运行(藏策略) / L3 窄 ZK 证明小命题;**Numerai 封存 OOS 留出 + 质押罚没**(自我执法) | 整回测 ZK(不可行) |
| **Callscan** | 可分享的 **VQ 卡 + 验证 URL + 二维码**,离线可重验 | 加密货币上链 |

---

## 2. 核心:让 "verified" 名副其实的三条腿

```
        ┌──────────────── 一张可信证书 ────────────────┐
 Leg A 可复现        Leg B 非过拟合          Leg C 数据可溯源
 (已有→硬化)        (全新→核心卖点)         (全新→反造假根基)
 digest 钉镜像       CPCV+PBO+DSR           原始价+复权因子分离
 定点货币核          跨域跨期迁移            内容寻址+签名快照数据
 DSSE/in-toto/sigstore 封存OOS留出          多源对账
```
**只有三条腿都在,"我们卖证据"才不是空话。** 现在只有 Leg A 的一半。

---

## 3. Sonnet 5 可执行 Backlog(按依赖排序,4 个 Sprint)

> 工作量记号:S=半天 · M=1–2天 · L=3–5天 · XL=1–2周。文件路径相对 `~/lumen`。

### Sprint 1 — 修致命正确性 + 硬化复现(先把现有的做对)

**Q1.1 原始价/复权因子分离(致命修复)** · L
- 目标:涨跌停、prev_close、成交价用**原始价**;收益率用**复权价**。当前 qfq 让可成交价虚构、除权日算错。
- 借鉴:zipline 分表 as-of 复权;qlib `original=$close/$factor`。
- 改:`fetch.py`(拉原始 OHLC + adjustment factor,不再直接 qfq)、`dataset.py`(Bar 增 `raw_*` + `factor`,prev_close 用原始)、`rules.py`(limit 用原始价)、`engine.py`(成交用原始、计净值用复权)、`data.py`(Bar 结构)。
- 验收:构造一个除权日 fixture,断言涨跌停价用原始 prev_close;再 fetch 不改变 data_hash(数据点对点稳定);新增 regression test。

**Q1.2 镜像按 digest 钉死 + 定点货币核** · M
- 目标:陌生人能复现。当前钉本地 image ID + `FROM python:3.11-slim` 可变 tag;确定性靠 `{v:.4f}` 取整。
- 借鉴:Staff 批评;SLSA resolvedDependencies。
- 改:`backtest.go`(`FROM python:3.11-slim@sha256:...`,把 base digest 记进 cert+lock)、`engine.py`/`metrics.py`(钱算术改整数分 or Decimal 定点,哈希不再依赖显示取整)。
- 验收:CI gate——同一策略在两个不同 base digest / 架构(amd64+arm64)跑出**逐位相同** equity hash;改 base tag → cert 变。

**Q1.3 成本/滑点/成交 可插拔三模型** · M(依赖 Q1.1)
- 目标:把"成本/滑点/成交"拆成独立注入、独立可测的模型,证书记录用了哪个+参数。
- 借鉴:Lean/zipline Blotter 分离;backtrader `slip_open`。
- 改:新 `models.py`(FeeModel/SlippageModel/FillModel 接口 + 默认实现);`engine.py` 注入;`cert.go` 记 model id+params。
- 验收:每模型独立单测;cert 含 cost_model 指纹;替换模型 → cert 变。

**Q1.4 真实流动性成本(不再静默截断)** · M(依赖 Q1.3)
- 目标:大单付更多;未成交=**滑单/错过**而非免费。
- 借鉴:qlib 二次冲击 `impact*(trade_val/bar_val)²`、两段成本 + min_cost 下限、bid/ask spread。
- 改:`models.py`(spread + sqrt/linear 冲击)、`engine.py`(carry 未成交量)。
- 验收:大单 vs 小单成本单调;未成交量被记录到下一根。

### Sprint 2 — 非过拟合(全新 Leg B,核心卖点)

**Q2.1 CPCV + Purge/Embargo** · L
- 目标:把单次 IS/OOS 切分升级成组合净化交叉验证(防泄漏)。
- 借鉴:mlfinlab / de Prado。
- 改:`oos.py`(加 `combinatorial_purged_cv(bars, strat, n_splits, embargo)`)+ `test_oos.py`。
- 验收:已知"样本内偷看"的策略被 CPCV 拒;purge/embargo 边界有测试。

**Q2.2 PBO + Deflated Sharpe + 多重检验校正** · M(依赖 Q2.1)
- 目标:每张证书附"过拟合概率 PBO、紧缩 Sharpe、试验次数、t-stat"。
- 借鉴:de Prado PBO/CSCV、Deflated Sharpe。
- 改:`metrics.py`(加 `deflated_sharpe`、`pbo`)、`cert.go`(significance block)。
- 验收:随机噪声策略 PBO→高、DSR→~0;cert 含 significance 字段。

**Q2.3 跨域/跨期迁移测试当生存条件** · M
- 目标:一个因子在 A 股票池/期挖出,必须在**不相交**池/期保持符号+显著 IC 才算"持续"。
- 借鉴:RD-Agent / FinRL Contests。
- 改:`oos.py`(`transfer_test`)、screen 的 `persisted` 升级为"跨域也过"。
- 验收:仅在单池有效的因子被判 not-persisted。

### Sprint 3 — 数据可溯源(全新 Leg C) + 标准化证书

**Q3.1 点对点(PIT)不可变数据脊柱** · XL
- 目标:含退市股的 survivorship-free 标的母表(list/delist_date + Active/Delisted/Suspended)+ 指数成分 as-of 重建 + 停牌日历;一次 as-of 一个不可变快照。
- 借鉴:Lean 含退市;qlib PIT;zipline 复权表;Tushare stock_basic/suspend_d/index_weight。
- 改:新 `instruments.py`(标的母表)、`calendar.py`(交易/停牌日历)、`membership.py`(指数 as-of 成分)、`fetch.py` 多源。
- 验收:含一只退市股的回测在其退市日正确剔除并计入退市损失;基准换成 **PIT 成分、市值加权 CSI300**(直接修掉筛选里的风格混淆 bug)。

**Q3.2 多源 + 对账 + 重试(今天 eastmoney 宕机暴露)** · M
- 目标:抗单源失败。akshare(eastmoney)+ Tushare/baostock + 重叠对账。
- 借鉴:Staff 批评;今天我们已加 sina 回退 + DATA FAIL 显式标记。
- 改:`fetch.py`(多源 + 退避重试 + 重叠 bar 交叉校验,分歧标红)。
- 验收:单源宕机自动切换;两源分歧被显式报告而非静默。

**Q3.3 证书重铸为 in-toto/DSSE + 内容寻址数据** · L
- 目标:用标准信封,cosign/in-toto 生态可验;数据可被陌生人取回。
- 借鉴:in-toto v1 Statement、DSSE(`go-securesystemslib/dsse`)、SLSA 词汇、ORAS/OCI 内容寻址、可选 Rekor。
- 改:`cert.go`/`attest.go`(DSSE 信封 + `predicateType=https://lumen.dev/quant-backtest/v1` + PAE 规范化,替掉手搓 `%.6f`)、`data.go`(数据集推 OCI by-digest,cert 引用 ResourceDescriptor)。
- 验收:`cosign verify-blob-attestation --type ... --key ed25519.pub` 通过;陌生人据 cert 能拉到原始数据并重算。

### Sprint 4 — 需求侧 + 旗舰(把它变成生意)

**B1 持续前推再签名(旗舰,不是 B2 脚注)** · L
- 目标:一次性回测证书 → **滚动 OOS、周期性签名的实盘/前推 track record**(YC 批评:这才是 painkiller)。
- 改:新 `forward.py`(冻结策略 → 每交易日拉新 bar → 纸面盘 → 周期签名追加到 hash-chain)、`cmd/lumen/quant.go`(`quant forward`)。
- 验收:跑几天产出"连续签名的前推净值链",任何人可逐段重验。

**B2 公开免费签名榜 + 过拟合取证报告(冷启动 + 需求侧)** · L
- 目标:用"打假"做分发(把年化300%卖家请上榜),把 OOS-诚实发现产品化成给配置方(LP/FOF)的付费"过拟合尽调报告"。
- 改:静态榜页 + `quant audit <track-record>` 生成尽调报告。
- 验收:能对一个公开 track record 生成"可复现 + PBO + 风格归因"的可分享报告。

**B3 可分享 VQ 卡 + 验证 URL + 二维码** · S
- 借鉴:Callscan。改:`cmd/lumen/quant.go`(`quant card`)生成离线可验的 HTML/PNG。

**B4(可选)质押罚没自我执法** · M
- 借鉴:Numerai。卖家对**前推窗口**质押(稳定币/法币押金),独立重跑不过则罚没——让造假有成本。

### Sprint 5(研究闭环,可与上并行)— 纪律化 LLM 提策略

**R1 LLM 提 → 诚实 OOS 判 → 仅 OOS-稳健+新颖者存活** · L
- 借鉴:RD-Agent(IC 去重 + 8 指标 + 严格留出)。
- 改:`propose.py`(本地模型经 lumen agent 提因子)、接 Q2 的 CPCV/PBO 门、严格留出 LLM 看不到的 test。
- 验收:LLM 提的过拟合因子被 OOS/CPCV 拒;只有跨域稳健的进库。

---

## 4. 反共识 / 现在别碰(辩证:哪些"高级"是陷阱)

- **整回测 ZK**:不可行/不划算。只做 L3 窄命题或先 L2 TEE。ZK 是路标不是下一步。
- **vectorbt 式向量化核**:别替换事件循环——会引入未来函数、不建模部分成交。只当外层扫描。
- **重写成 Rust/另一语言**:先写 **spec + golden vectors**,语言移植留后;纯 stdlib 的 bit-identical 是现有优势,别为性能丢了它。
- **自建支付/社区**:接 知识星球/小鹅通,别造。
- **B2 信号市场**:牌照前永不碰。

---

## 5. 北极星 & 第一块多米诺

- **北极星**:成为"零售/新锐量化的 GIPS"——一个配置渠道当作 table-stakes 的**可信验证标准**。终局不是卖证书,是**拥有标准**。
- **第一块多米诺(Sonnet 5 从这开始)**:**Sprint 1 的 Q1.1(原始价/复权分离)**——这是致命正确性 bug,不修,后面所有"可信"都站不住。修完接 Q1.2(digest 钉镜像)。
- **三条腿最小可信版**:Q1.1 + Q2.2(PBO/DSR) + Q3.3(DSSE 证书)——做完这三个,"verified"才第一次三条腿齐全,可以拿去给第一个"需求侧执法者"看。

---

## 附:验收总原则
每个工作项必须:① 有失败优先的测试(TDD);② 不破坏"本地==硬化docker bit-identical";③ 改了 cert 语义就更新 `predicateType`/版本;④ 诚实标注数据/方法的局限(别把局限藏进绿色对勾)。
