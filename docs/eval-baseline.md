# Eval Baseline — Local Gemma-4-12b (first recorded run)

> The measurement line's payoff: the first **real** `lumen eval` pass-rate on a
> live local model. Unlocks the eval-gated work (package deletion regression,
> `[tools] profile` default flip). Recorded 2026-06-20.

---

## Headline

| | |
|---|---|
| **Model** | `google/gemma-4-12b` (official instruct, Q4, 7.56 GB on disk) |
| **Endpoint** | LM Studio, OpenAI-compatible `http://localhost:1234/v1` |
| **Pass-rate** | **5 / 6 (83%)** sequential — capability ceiling **6 / 6** (the 1 miss was a memory-pressure timeout, passes in isolation) |
| **Median steps** | 8 |
| **Median wall-time** | 118 s / task |
| **Total wall-time** | ~15 min (6 tasks, `--repeat 1`) |
| **Hardware** | MacBook Air, 24 GB unified memory |

**Verdict:** gemma-4-12b **drives Lumen's agentic loop** end-to-end (read → edit →
self-verify) on real Go bugfix tasks. This is the first local model on this
machine confirmed to do so — earlier attempts (Qwen3.6-27B, the Gemma creative
finetune) could not (see `docs/交接-v7-四视角评审落地.md` §2).

---

## The decisive finding: context window, not model capability

The first smoke run **failed** — the model called `code_search` once, then emitted
a generic Chinese greeting ("您好！请问…") and stopped without ever editing. Root
cause was **not** the model:

- Lumen's `core` tool profile (~42 tool schemas) + system prompt = **~11k tokens**.
- The model was loaded with an **8192-token** context (LM Studio default).
- 11k > 8192 → LM Studio slid the window, dropping the system prompt + task → the
  model saw only a tail fragment and degenerated into a fresh-chat greeting.

`probe-local` passed (✅ drives agent) because it sends a **single** tool — that fits
in 8k. The full agent loop does not.

**Fix: reload the model with a larger context.** Reloaded at **16384 / parallel 1**
(estimate 9.74 GiB, actual 7.04 GiB). 16k holds the 11k prompt with room for
multi-turn growth. After the reload the same smoke task passed: model applied the
correct `if len(nums) == 0 { return 0 }` fix, Lumen's verify ran `go test` → green.

> Lesson for any small local model + Lumen: **context must exceed the core-profile
> prompt (~11k).** 8k is too small; 16k works for these tasks.
>
> **Guard added:** the agent now runs a pre-flight overflow check on the first
> turn — if the stable prefix (system prompt + tool schemas) + input already
> crowds the configured `[agent] context_window`, it emits a WARN pointing at
> `[tools] profile="core"` / a larger window, instead of letting the window
> silently slide and degrade the model into a greeting. (Note: it keys off the
> *configured* window — set `context_window` to your server's value, since the
> OpenAI `/v1/models` endpoint doesn't report context length.)

---

## Per-task results (`--repeat 1`)

| Task | Result | Steps | Time | Note |
|---|---|---|---|---|
| 01-average-empty | ✓ | 6 | 58.5 s | divide-by-zero guard |
| 02-stack-lifo | ✓ | 10 | 73.2 s | pop order fix |
| 03-reverse-runes | ✓ | 6 | 65.2 s | rune reversal |
| 04-binary-search | ✓ | 8 | 118.4 s | off-by-one / index |
| **05-counter-race** | **✗** | **1** | **301.0 s** | **5-min turn TIMEOUT under memory pressure** — see analysis |
| 06-stringer-impl | ✓ | 9 | 300.2 s | interface impl (Area+Perimeter) |

5 / 6 passed. Total notional cost $0.0697 — **notional only**: no `[providers.pricing]`
was set so the readout uses a default rate. Real local cost ≈ $0 (electricity).

---

## Latency profile (the real local bottleneck)

- **First turn per task: ~60–100 s.** This is prompt *prefill* — ingesting the ~11k
  core-profile prompt at this machine's prefill speed. It dominates short tasks.
- **Subsequent turns: ~2 s TTFT**, ~13–25 tok/s generation (KV cache reuse).
- The two slowest tasks (05, 06 at ~300 s) ran last, when free memory was tightest
  (prefill slows under memory pressure — see below).

So the headline cost of a weak-but-free local model here is **wall-time, not money or
correctness**: 83% correct, but ~2 min/task median.

---

## Memory behaviour (the watch item)

The prior hard finding was that ≥13 GB model loads kernel-panic this 24 GB machine.
This run was instrumented with a 20 s memory sampler throughout.

- **Swap stayed at 0 for the entire 15-min run.** Never engaged.
- Free memory dipped as low as **0.06 GB**, but **5–6 GB stayed reclaimable
  (inactive)** the whole time — the system lived off cache, never thrashed.
- The `parallel 1 × 16384` reload (7.04 GiB) is *lighter* than the default
  `parallel 4 × 8192` it replaced, and well under the ~13 GB panic line.
- **No lockup, no panic.** The danger is the model *load* (one-time, gated by
  `lms load --estimate-only`), not steady-state inference.

---

## 05-counter-race: the one failure was a timeout, not a capability gap

Signature: **1 step, 301.0 s**. That ~300 s is not a coincidence:

- `internal/provider/openai/openai.go:37` — HTTP client `Timeout: 5 * time.Minute`.
- `internal/agent/agent.go:314` — per-turn `context.WithTimeout(ctx, 5*time.Minute)`.

counter-race ran **last**, when free memory was tightest (free hit 0.06 GB). Its
single first-turn prefill of the ~11k prompt was so slow under memory pressure that
it exceeded the 300 s turn timeout → the turn was cancelled after 1 step → fail.
Contrast 06-stringer-impl: same ~300 s total, but **9 steps** that passed — its
prefill was cached across turns, so no single turn hit the wall.

So the failure is **memory-pressure-induced prefill latency**, not "the model can't
reason about a data race."

**Confirmed by isolated re-run** (counter-race alone, memory headroom restored):
**✓ pass, 9 steps, 158.1 s.** The model produced a textbook fix — added a
`sync.Mutex` and locked *both* `Inc` and `Value` — and `go test -race` went green
(`ok evaltask/counter 1.701s`). Same task, same model, same config; the only
variable was memory pressure. **Capability ceiling on this set is therefore 6/6** —
the sequential 5/6 reflects one environmental timeout, not a reasoning gap.

Implication: a slow local model on a memory-constrained machine may need a **longer
turn timeout** than the 5-min default, OR the prefill cost lowered (smaller prompt /
faster machine).

> **Fixed:** the per-turn timeout is now configurable via `[agent] turn_timeout`
> (e.g. `turn_timeout = "20m"`), threaded through both the turn context and the
> OpenAI client deadline. A slow local model's first-turn prefill no longer dies at
> the old hardcoded 5 minutes.

## What this unlocks

1. **Package-deletion regression** (S4, eval-gated): this 5/6 is the **before**
   number. Delete `blueprint/topology/policy/jsonpath/cronparser/seal/notary/
   graphwalker/schema` + wrappers, re-run, confirm pass-rate does not drop.
2. **`[tools] profile` default flip to `core`**: the run *validates* core drives a
   real model. Flipping the default is now data-backed (was the deferred S4 item).

---

## Reproduce

```bash
# 1. Load the model with enough context (16k > the ~11k core prompt). Check first:
~/.lmstudio/bin/lms load google/gemma-4-12b -c 16384 --parallel 1 --estimate-only -y
~/.lmstudio/bin/lms load google/gemma-4-12b -c 16384 --parallel 1 -y
~/.lmstudio/bin/lms server start

# 2. Point lumen.toml at the local endpoint with the core profile:
#    [[providers]] kind="openai" base_url="http://localhost:1234/v1"
#                  model="google/gemma-4-12b" api_key="lm-studio"
#    [tools] profile="core"

# 3. Run from the repo root:
cd ~/lumen && go run ./cmd/lumen eval --repeat 1
```

Config used: `max_steps=25`, `temperature=0.2` (low temp → reliable tool-calling on
a weak model), `verify.enabled=true`, `verify.max_repair_cycles=2`.

---

## Open follow-ups

- ~~**05-counter-race** isolated re-run~~ — **done: ✓ passes in isolation (9 steps,
  158 s, correct mutex). The sequential miss was a 5-min turn timeout under memory
  pressure.** Possible code follow-up: make the turn timeout configurable for slow
  local backends.
- `--repeat 2/3` for a variance band (local models are non-deterministic).
- `probe-local` live matrix → `docs/local-models.md`.
- Fallback model `Qwen2.5-Coder-7B-Instruct` if more headroom / speed wanted.
