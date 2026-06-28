# Running lumen on a home GPU cluster (exo) for quant research

A runbook for the plan: pool several everyday machines into one inference
cluster with [exo](https://github.com/exo-explore/exo), run lumen against it for
free/private agent work, and use that agent to drive `lumen quant`.

## Read this first — what the cluster is and isn't for

- **Quant backtesting does not use the GPU.** The `lumen quant` engine (T+1,
  price limits, fees, metrics) is pure-CPU. Your GPUs do **not** speed up
  backtests.
- **exo does distributed _inference_, not training.** It shards one model across
  devices so you can *run* a model too big for a single card. It does **not** let
  you *train* models. Consumer GPUs over a home network cannot train competitive
  quant models — that is not a path to rivaling a large fund on compute.
- **The GPU's job here is to run the _agent_ (lumen), not the strategy.** A local
  model lets lumen write and refine strategies for free and privately; the
  strategy itself is then backtested on CPU. The two connect through lumen, not
  through raw compute.

So the honest target is **a private, $0-API research loop on cheap hardware whose
edge is _verifiability_** (the VQ certificate) — not out-computing anyone.

## Hardware reality (e.g. 4× RTX 3070)

- 4 × 8 GB ≈ **~32 GB pooled VRAM** (minus overhead). Enough for a ~30B model at
  4-bit (e.g. Qwen2.5-Coder-32B ≈ 18 GB) sharded across the nodes.
- The bottleneck is the **interconnect**: exo over consumer Ethernet/Wi-Fi has
  high inter-node latency, so expect a few tokens/sec, not the snappy speed of a
  single big card. Usable for batch/agent work; not for fast interactive chat.
- **Start with 1–2 nodes** to prove the model drives the agent, then scale to 4.

## Step 1 — bring up exo on the cluster

On each machine (one becomes the head node):

```bash
# per exo's README; it auto-discovers peers on the LAN
exo
```

exo serves a **ChatGPT-compatible API on `http://<head-node>:52415/v1`** and a
web UI. Pick a model whose served id supports OpenAI **tool/function calling** —
this is the make-or-break capability for an agent (see Step 3).

## Step 2 — point lumen at the cluster

`exo` is a built-in lumen local preset (`http://localhost:52415/v1`). If exo runs
on the same box:

```bash
lumen /model exo          # inside chat, or set default_model = "exo" in lumen.toml
```

If exo runs on another node, set it explicitly in `lumen.toml`:

```toml
default_model = "exo"

[tools]
profile = "core"          # leaner tool set — easier for a smaller local model

[[providers]]
name = "exo"
kind = "openai"
base_url = "http://<head-node-ip>:52415/v1"
model = "local-model"     # or the exact id from GET /v1/models
api_key_env = "EXO_API_KEY"   # local servers accept any/empty key
```

## Step 3 — verify the model can actually drive the agent

A local model is only useful to lumen if it emits real **tool calls**, not prose.
Lumen ships a probe for exactly this:

```bash
lumen probe-local                                   # tests every local preset incl. exo
lumen probe-local --base-url http://<head>:52415/v1 # test one endpoint
```

A ✅ in the "Drives agent (tool_call)" column means the cluster can run lumen. A
❌ ("prose only") means the served model can't tool-call — pick another model.
(The lumen↔OpenAI-endpoint plumbing itself is verified; what the probe gauges is
the *served model's* capability.)

## Step 4 — the quant research loop

```
exo cluster ──serves──> local model ──drives──> lumen (agent)
                                                   │
                                writes / refines strategy.py
                                                   ▼
                          lumen quant backtest .   (CPU, --network=none sandbox)
                                                   ▼
                          VQ-xxxx certificate  ──>  lumen quant verify .
```

GPUs power the agent (Steps 1–3); the quant engine runs on CPU (Step 4). The
certificate is the deliverable — a reproducible, tamper-evident backtest.

## Honest caveats

- This runbook's lumen side is verified end-to-end against an OpenAI-compatible
  endpoint (the probe certifies tool-calling). It does **not** prove a particular
  model on your hardware is *good* at writing strategies — that depends on the
  model and is yours to evaluate with real tasks.
- A verified backtest proves reproducibility, **not** future profit. The whole
  stack sells trust, not alpha.
- exo is young; heterogeneous-device inference can be finicky. Prove 1 node
  first.
