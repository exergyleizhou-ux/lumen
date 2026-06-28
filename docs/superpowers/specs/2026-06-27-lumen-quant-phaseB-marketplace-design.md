# Design: `lumen quant` phase B — verified-strategy registry & marketplace

**Date:** 2026-06-27
**Status:** B1 **trust core implemented** (2026-06-28) — `lumen quant keygen / attest /
verify-attestation`: a verifier independently re-runs a strategy, confirms the
cert reproduces *and* the source hasn't drifted, then Ed25519-signs it; anyone
checks the signature offline. Remaining for B1: source *sealing* (encrypt-at-rest
so the public never sees the strategy) + the hosted registry HTTP service & public
pages. B2 unchanged (gated). Depends on phase A (merged to `main`).
**Sibling:** reuses `lumen oasis`'s notary (ED25519), seal (source-at-rest), and
marketplace-registration HTTP pattern.

---

## 1. Premise (inherited from phase A)

Phase A produces a **re-computable backtest certificate (VQ-xxxx)**: proof that an
equity curve really came from a given strategy on pinned data, with no lookahead.
It kills *fraud*, not *market risk* — it makes **no** claim about future returns.

Phase B turns that artifact into a business: a registry where the scarce thing —
a backtest you cannot fake — becomes a tradeable credential. The honest revenue
is **trust**, not alpha.

## 2. ⚠️ Regulatory reality (shapes the whole decomposition)

Selling buy/sell **signals or investment advice** to the public in mainland China
requires a securities investment-advisory license (证券投资咨询资格). Operating an
unlicensed paid signal service is a real legal exposure. Therefore phase B is
split so the licensable-risk lives in an optional, gated tier:

| Tier | What it sells | Regulatory load |
|---|---|---|
| **B1 — Verification credential** (build first) | "We independently re-ran your backtest in our sandbox; here is our signed attestation that your numbers are real and reproducible." | **Low** — it attests reproducibility, not advice. No buy/sell recommendation. |
| **B2 — Strategy/signal marketplace** (gated) | Listing/licensing strategies; subscribing to live signals | **High** — needs a 投顾 license or a licensed partner; do NOT ship without it. |

**B1 is the wedge.** It is honest, legally light, and directly monetizes phase A's
output. B2 is designed-for but fenced behind compliance.

## 3. B1 — the verification credential service

### 3.1 The trust mechanism (the product)
An author's `strategy.py` is their alpha — they will not publish source. So the
registry verifies **without seeing the source in the clear**, reusing the oasis
compute-to-data pattern:

1. Author runs `lumen quant publish` → uploads the *sealed* strategy package
   (source encrypted with the registry's key via `seal.Manager`) + the VQ cert.
2. The registry re-runs the strategy in **its own** hardened `--network=none`
   sandbox against the **pinned dataset named in the cert** (data hash must
   match), and recomputes the equity-curve hash.
3. If the recomputed cert id equals the submitted one, the registry issues a
   **notary-signed attestation** (ED25519, `notary.Sign`) over the VQ id +
   metrics + data provenance.
4. A public verification page shows the signed attestation — metrics, hashes,
   "re-verified by <registry> on <date>" — and **never the source**.

Buyers/investors trust the registry's signature, not the author's screenshot.
That is the entire value: a screenshot can be faked; a registry-signed,
independently-recomputed cert cannot.

### 3.2 New surface
- `lumen quant publish [dir]` — seal the package, POST cert + sealed source to
  the registry, print the public verification URL. (Mirrors `oasis deploy`.)
- Registry backend (`internal/quantreg` or a service): `/verify` (re-run +
  attest), `/cert/<id>` (public page), `/revoke`. Reuses oasis's register/review
  HTTP shape.
- Attestation record: `{vq_id, metrics, data_provenance, signature, verified_at,
  verifier_pubkey}` — re-checkable by anyone with the public key.

### 3.3 Revenue (B1)
Pay-per-attestation or a subscription for a hosted, shareable verification page —
sold to the people who today post backtest screenshots and need to prove they are
not lying: 私募 raising from LPs, 知识星球/课程 strategy authors, quant job
candidates. The buyer is the *seller of a strategy*, not a retail investor — which
keeps B1 clear of advisory licensing.

## 4. B2 — strategy/signal marketplace (designed, gated)

Built on B1's signed certs. **Do not implement without a license or licensed
partner.** Sketch only:
- Listing: a strategy with a fresh signed cert can be offered for license/sale;
  the cert is the quality signal.
- Live track record: periodic re-attestation on out-of-sample windows turns a
  one-off backtest cert into an ongoing, signed performance record — the thing
  that actually builds trust over time (and the honest answer to "but backtests
  lie about the future").
- Signal subscription: license-gated; out of scope until compliance is solved.

## 5. Reuse map (don't rebuild)

| Need | Existing asset |
|---|---|
| Encrypt strategy source at rest | `seal.Manager.Seal` (oasis) |
| Signed attestation over cert | `notary.Sign` / `notary.Attestation` (ED25519) |
| Marketplace register/review HTTP | `cmd/lumen/oasis.go` deploy/register pattern |
| Re-run in hardened sandbox | phase A `internal/quant` backtest (`--network=none`) |
| Re-computable cert id | phase A `quant.ComputeCertID` |

## 6. Non-goals for the first B cut
- Payments/Stripe (the known GTM bottleneck — integrate after B1 proves demand).
- B2 / any signal-selling until licensing is resolved.
- Reconstructed point-in-time index membership (a deeper survivorship axis) —
  track as a data-quality upgrade, not a B blocker.

## 7. Done criterion for B1
`lumen quant publish` seals a strategy and obtains a registry **signed
attestation** that independently reproduces the VQ cert; a public page shows the
signature and metrics without exposing source; anyone can re-verify the signature
against the registry's public key. At that point the anti-fake-backtest credential
is a sellable service.
