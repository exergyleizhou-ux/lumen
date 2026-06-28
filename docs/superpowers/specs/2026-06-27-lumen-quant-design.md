# Design: `lumen quant` — verifiable A-shares backtest toolchain

**Date:** 2026-06-27
**Status:** Design — pending user review (no implementation until approved)
**Author:** brainstorming session
**Sibling vertical:** `lumen oasis` (this design deliberately mirrors its
`init → build → check → deploy → verify` provenance pipeline)

---

## 1. Problem & honest framing

The user wants a Lumen "quant vertical," analogous to how `lumen oasis` is
Lumen's privacy-computing/data-marketplace vertical. Stated criteria: **赚钱
(make money)** and **靠谱 (be reliable/trustworthy)**, market = **A-shares**.

The honest constraint that shapes everything:

> **A verified backtest does not prove a strategy is profitable.** Verifiable
> provenance proves *"this equity curve really came from this code on this data,
> with no future-data leakage."* It kills **fraud** (fabricated numbers,
> lookahead bias, survivorship bias). It does **not** predict future returns.

Therefore the product does **not** sell alpha or return promises. It sells
**trust**: in the A-shares retail-quant market, fabricated "年化300%" backtest
screenshots are endemic and *unverifiable*. The scarce, sellable thing is a
backtest that **cannot be faked**. This is the same `verifiable-provenance`
moat as the rest of the user's portfolio, pointed at finance, and the explicit
contrast with High-Flyer-style black-box backtests.

## 2. Decomposition (this spec covers A only)

| Phase | Sub-project | Revenue role |
|---|---|---|
| **A — this spec** | `lumen quant` toolchain → verifiable backtest certificate `VQ-xxxx` | The certificate *is* the first sellable artifact: an anti-fake-backtest credential for strategy authors / 私募 who must prove legitimacy. |
| B — later | Verified-strategy marketplace / signal subscription (Oasis-for-strategies) | Where revenue scales (marketplace cut / subscriptions). Built on A. |
| C — optional, last | Live execution of *already-verified* strategies | Capital/broker-API gated; rides on top of A. Out of scope here. |

A is both the technical foundation for B/C **and** independently sellable.
**This spec is sub-project A only.** B and C get their own spec → plan cycles.

## 3. Goals / Non-goals

**Goals**
- A CLI vertical `lumen quant <init|data|backtest|verify>` mirroring `lumen oasis`.
- Deterministic, reproducible backtests: same code + same pinned data + same
  seed → bit-identical metrics on any machine.
- A signed **backtest certificate `VQ-xxxx`** anyone can independently re-compute.
- Enforced **point-in-time data** (no lookahead) and **survivorship-corrected
  universe** — without these, "verified" is meaningless.
- Reuse Lumen's existing provenance infra (sandbox runner, lockfile,
  source-hash, digest-pinning, `verify`) rather than building anew.

**Non-goals (YAGNI for A)**
- No alpha generation, no strategy library, no return promises.
- No live trading / broker integration (phase C).
- No marketplace backend / payments (phase B).
- No intraday/tick microstructure for v1 — **daily-bar** A-shares only.
- No multi-factor optimization framework — the engine runs *whatever strategy
  the author wrote*; it does not invent strategies.

## 4. Architecture

Mirrors `cmd/lumen/oasis.go` (a `runQuant(args)` dispatcher) + an
`internal/quant/` package, reusing `internal/sandbox` and the oasis lockfile
machinery.

```
lumen quant init <name>     scaffold a strategy package
lumen quant data            fetch + hash-pin the A-shares dataset
lumen quant backtest        run strategy in sandbox → metrics + equity curve
lumen quant verify          re-run, re-hash → emit/validate certificate VQ-xxxx
```

### 4.1 Strategy package layout (produced by `init`)

```
my-strategy/
  quant.toml            # manifest: name, universe spec, date range, freq=daily,
                        #   initial capital, fees/slippage model, data source
  strategy.py           # author code: implements the fixed interface (§4.3)
  Dockerfile            # pinned base image for the sandbox run
  data.lock             # written by `quant data`: dataset hash + provenance
  quant.lock            # written by `backtest`: code hash + data hash + image
                        #   digest + engine version + seed
  results/
    metrics.json        # CAGR, Sharpe, Sortino, maxDD, turnover, win-rate, N-trades
    equity.csv          # daily equity curve (+ its hash recorded in the cert)
    cert-VQ-xxxx.json   # the verifiable backtest certificate
```

### 4.2 The backtest sandbox (the moat-making core)

Runs in a container with the **same hardening as `oasis check`**:
- `--network=none` — the strategy cannot fetch future data or phone home.
- Read-only mount of the **hash-pinned** dataset (from `quant data`).
- Pinned random seed (recorded in `quant.lock`); deterministic engine.
- The **engine — not the strategy — owns the data feed.** The strategy never
  reads files directly; it receives a point-in-time `Context` (§4.3). This is
  what structurally prevents lookahead: on day *t* the engine only exposes data
  with timestamp ≤ *t*.
- **Survivorship correction:** the universe on day *t* is the set of symbols
  *listed and tradable on day t* (including since-delisted names), reconstructed
  from the pinned dataset's listing/delisting calendar — not today's survivors.
- **A-shares trading rules** modeled in the matching engine: T+1 settlement,
  ±10%/±20% price limits (涨跌停 → no fill when limit-locked), round-lot (100-share)
  sizing, configurable commission + stamp duty + slippage.

### 4.3 Strategy interface (`strategy.py`)

A minimal, point-in-time-only contract (illustrative):

```python
def initialize(ctx):            # set universe spec, params; runs once
    ...

def on_bar(ctx) -> dict:        # called per trading day; returns target weights
    # ctx.history(symbol, field, lookback) -> only data with ts <= today
    # ctx.universe                          -> survivorship-correct, today's tradables
    # ctx.portfolio                         -> current holdings/cash
    return {"600519.SH": 0.2, ...}          # engine handles T+1, limits, lots, fees
```

The engine rejects any attempt to read raw files or reach the network; all data
flows through `ctx`. That is the enforceable boundary that makes "no lookahead"
a property of the harness, not a promise of the author.

### 4.4 Data source

- **Default: akshare** — free, no token, daily A-shares OHLCV + adjustment +
  listing calendar. `quant data` fetches the requested universe/date-range,
  normalizes, and writes a content-addressed dataset + `data.lock` (sha256 +
  source + fetch metadata). Because it's hash-pinned, the data source's own
  quality is *recorded and reproducible* even if imperfect.
- **Upgrade path: Tushare Pro** (token) — same normalized schema, swappable via
  `quant.toml` `data.source`. Out of scope to implement both for v1; the schema
  is source-agnostic so adding Tushare later is additive.

### 4.5 The certificate `VQ-xxxx`

JSON, mirroring Oasis's `VO-` certs. Fields:
- `cert_id` (`VQ-` + hash prefix), `engine_version`, `created_at`
- `code_hash` (strategy source tree hash), `image_digest` (sandbox image)
- `data_hash` (pinned dataset) + `data_provenance` (source, range, universe)
- `seed`, A-shares `rules` block (fees, T+1, limits) used
- `metrics` (CAGR/Sharpe/Sortino/maxDD/turnover/…), `equity_curve_hash`
- A one-line **re-compute recipe** so any third party can reproduce it.

`lumen quant verify` re-runs the pinned image on the pinned data with the pinned
seed and asserts **bit-identical** metrics + equity-curve hash, exactly as
`lumen oasis verify` re-hashes provenance today.

## 5. Data flow

```
init ──> author writes strategy.py
data ──> fetch A-shares daily bars ──> normalize ──> hash-pin ──> data.lock
backtest ─> sandbox(--network=none, pinned data+seed) ─> engine drives strategy
          ─> metrics.json + equity.csv ─> quant.lock + cert-VQ-xxxx.json
verify ──> re-run pinned image ──> assert bit-identical ──> cert valid ✓/✗
```

## 6. Testing strategy (TDD)

- **Engine determinism:** same inputs → identical metrics across two runs (Go test).
- **No-lookahead enforcement:** a deliberately cheating strategy that tries to
  read future bars / raw files / network must be *blocked or produce no
  advantage* — assert it cannot beat a no-lookahead baseline. This is the
  highest-value test: it proves the moat claim.
- **Survivorship:** a universe including a since-delisted symbol must include it
  on dates it was listed and exclude it after delisting.
- **A-shares rules:** limit-locked day → no fill; T+1 → can't sell same-day buy;
  round-lot sizing; fee/stamp-duty arithmetic.
- **Cert reproducibility:** `verify` on an unchanged package passes; a 1-byte
  code edit flips it to ✗ (mirrors the oasis source-hash test that already
  caught a real bug).
- **CLI:** `init` scaffolds a runnable package; `backtest` on the scaffold
  produces a cert; golden-file the cert shape (minus volatile fields).

## 7. Reuse map (don't rebuild)

| Need | Existing Lumen asset |
|---|---|
| Sandbox container run, `--network=none` | `internal/sandbox` (used by `oasis check`) |
| Source-tree hashing | oasis `ComputeSrcHash` (note the abs-path / hidden-dir bug history) |
| Lockfile + digest-pinback | oasis lockfile writer (`#93`, `#114`) |
| `verify` re-hash pattern | `lumen oasis verify` (`#94`) |
| CLI dispatch shape | `cmd/lumen/oasis.go` `runOasis` |

## 8. Open questions for the user

1. **Data source for v1** — akshare (free, zero-token, recommended) vs Tushare
   Pro (needs token, higher quality)? Default = akshare.
2. **Universe scope for v1** — whole A-share market, or a fixed index
   constituent set (e.g. 沪深300) to keep data volume small? Default = 沪深300
   constituents (survivorship-correct), expandable later.
3. **Certificate signing** — plain hash-chain (re-computable by anyone, like
   Oasis today) vs additionally key-signed by an issuer? Default = re-computable
   hash-chain for v1; issuer-signing is a phase-B marketplace concern.

Defaults above are chosen so implementation can proceed without blocking; the
user can override any before the plan is written.

## 9. What "done" means for sub-project A

`lumen quant init demo && lumen quant data && lumen quant backtest` produces a
`VQ-xxxx` certificate on real A-shares daily data, and `lumen quant verify`
re-computes it bit-identically — with the no-lookahead and survivorship tests
green. At that point the anti-fake-backtest credential is real and demonstrable,
and phase B (marketplace) has its foundation.
