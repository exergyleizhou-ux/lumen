# Lumen Science M1E — OpenAlex admission record

**Checkout:** `/Users/lei/Documents/Codex/2026-07-24/ji-3/work/lumen-science-fusion`

**Branch:** `codex/science-fusion-full`, based on
`main@8bd51b51ff874ecf035f52398898c2fbd40e9390`

**Decision:** implementation is under offline fixture validation; live OpenAlex
remains **pending-deny**.

## Native Rust product slice

No OpenAlex runtime or source code is imported. Rust Lumen remains the only
permission, execution, artifact, evidence, provenance, replay, and audit
authority.

- The descriptor permits only HTTPS `api.openalex.org`.
- One `/works` search exchange is bounded to 50 records and a conservative
  local cap of one request per second.
- The request uses `select=id,doi,display_name,publication_year`.
- Abstracts, full text, authorships, locations, references, topics, and content
  URLs are neither selected nor parsed. The fixture includes sentinels for
  excluded fields to prove the parser ignores them.
- Invalid work IDs, missing titles, invalid years, missing hit counts, and
  malformed result arrays fail closed before artifact registration.
- The offline product route uses explicit fixture-only policy validation. It
  opens no socket and neither requires nor accepts a credential in its request.
- The ignored live probe requires runtime-only `OPENALEX_API_KEY`. The key is
  attached only to the ephemeral HTTP request and never enters
  `ValidatedRequest`, artifacts, evidence, provenance, or audit records.
- The SessionActor/ACP product response carries a mandatory CC0/API-terms and
  article-rights notice.

## Official-source review

Reviewed 2026-07-24:

- https://developers.openalex.org/
- https://developers.openalex.org/guides/authentication
- https://developers.openalex.org/guides/searching
- https://developers.openalex.org/guides/page-through-results
- https://developers.openalex.org/guides/selecting-fields
- https://developers.openalex.org/api-reference/authentication
- https://developers.openalex.org/api-reference/works/list-works
- https://openalex.org/OpenAlex_termsofservice.pdf

The current API uses `https://api.openalex.org`, requires a free API key for
normal use, meters search calls, returns a daily free budget, and reports rate
and cost information in headers and response metadata. The documented hard
service limit is 100 requests per second; this connector applies a stricter
local one-request-per-second cap. `per_page` permits up to 100, while this
connector permits at most 50 and performs only one page.

OpenAlex search considers title, abstract, and full text when matching. That
server-side search behavior does not grant content reuse rights. The `select`
parameter limits the response to four bibliographic fields, so this connector
does not retrieve the matched abstracts or full text. OpenAlex describes its
dataset as CC0, while API access remains subject to its service terms and
underlying article content retains article-level rights.

## Offline evidence

No OpenAlex, provider, or other live endpoint was called.

Evidence will be recorded after targeted unit tests, strict Science clippy, an
exact-product-source `lumen` build, and the rebuilt-binary ACP connector test
complete. Until then this document is an admission design and source review,
not accepted product proof.

## Live admission remains closed

Live proof requires explicit billable-network authorization, an
operator-approved runtime-only API key, retained redacted rate-limit/cost/status
and response evidence, and an actual presentation-point test for the mandatory
notice. Any article-content reuse also requires checking the article-level
license. Until then, this is deterministic offline work only, not live L5.
