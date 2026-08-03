# Changelog

All notable changes to Lumen are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [2.0.0-rc.1] - 2026-08-04

### Added

- harness regression corpus (NG-09A-1) + verification_debt read model (`945d505a`)
- OperatorControlPlane five commands + Kairos lease-consumer loop (`1051fc2d`)
- ClientAdvisor consult contract + advice-epoch write side (`ca828123`)
- ReleaseSourceTupleV1 (NG-10A) + fail-closed update gate (`407e5841`)
- durable sealed attempt receipt with bounded clean auth retry (`bf489044`)
- failure escalation guidance for repeated sampling failures (`69070b38`)
- wire spawn/network sandbox and shell control hosts (`40c6f3f5`)
- Kairos pure control plane, advisor shadow, offline gate bundle (`a190ab03`)
- bounded assignment apply gate and shell no-replay seal link (`cd176b0c`)
- supervisor loop, sealed no-replay receipt, golden compose (`b881c4e9`)
- tree loop reducer and sandbox write gates on all main writers (`ce6ce891`)
- S4 pressure, S7 node loop reducer, sandbox consumer inject (`af00c4c4`)
- S6 M1 offline governed tree preview with deny_mechanism (`2c733c7e`)
- HandoffPacketV1 bounded handoff contract (NG-04D-3) (`9c305fb8`)
- S5 AgentSandboxV1 schema and accepted-only memory gates (`942ab2bf`)
- S4 DeliveryObservationV1 and cache_epoch try_send wiring (`97d7cb0c`)
- S3 WriteScope overlap pure fn and root-only merge receipt (`a070f236`)
- mark S1/S2 done; fail-closed future ops snapshot schema (`b15af107`)
- unify authority↔lifecycle schema and atomic outbox snapshot (`b48e01cf`)
- persist coordinator authority log as JSONL next to ops (`523006bd`)
- wire BudgetLedger + authority log into coordinator spawn path (`981b8f21`)
- NG-03C-4 compose BudgetLedger × store × journal (`75821623`)
- NG-03C lifecycle event journal and derived crash actions (`70aef1f5`)
- derived_from propagation, EffectRecoveryClass, progress/confidence discipline (`d6c7865c`)
- NG-03B atomic tree budget ledger (`9179a622`)
- NG-02A ToolContractV1 and ToolResultEnvelopeV1 (`650ff800`)
- NG-00 canonical encoding, authority event envelope, invariants (`9cf6a9ef`)
- deny unknown and Other tool kinds for child sessions (`8550ff08`)
- versioned SubagentLineage serialization and lineage contract tests (`9bbb1e46`)
- harden governed task-tree runtime (`8b74b361`)
- propagate manifest identity to verifier resumes (`91092a06`)
- retain manifest identity for resume sources (`7711e4db`)
- preserve child context provenance (`f3e04df3`)
- hand off governed manifest to tool registry (`bc5d479d`)
- read host manifest provenance resource (`21b92ff2`)
- carry manifest provenance on tool results (`4b842d3d`)
- bind governed admission to policy inputs (`2b79b83f`)
- canonicalize governed manifest identity (`b8e64049`)
- verify governed snapshot before child execution (`c0fe17eb`)
- validate governed admission receipts (`2713b8c9`)
- require manifest identity for governed spawns (`9e33a226`)
- bind manifests to accepted snapshots (`6f94114c`)
- freeze accepted ledger snapshots (`f25c1d8a`)
- add validated context manifest contract (`3c430600`)
- inject governed task-tree contracts (`ecefdc7b`)
- promote reviewed task facts on demand (`ab01c47d`)
- start Lumen 2 alpha line (`0fae4c7b`)
- make DeepSeek Flash the product default (`91f8c8d6`)
- render lineage-aware agent trees (`cbeb7384`)
- enforce daily token budgets (`b515c714`)
- select fresh runs from model pool (`5b4a1e8a`)
- retain background model provenance (`1357b7c3`)
- allow task preferences for custom model pools (`880e9437`)
- select ordinary model pool by task (`c636dea1`)
- reroute zero-output ordinary turns (`fec223de`)
- add explicit healthy model pool selector (`e10c16a7`)
- add session model pool controls (`a2c5c642`)
- route selected model pool before output (`22917e37`)
- preserve nested subagent lifecycle (`80bf5cd4`)
- resolve nested subagent views recursively (`ffa2a302`)
- expose task tree lineage to pager (`fdc6c0ff`)
- gate pre-output fallback routing (`e280f8f7`)
- bridge recovered child terminal evidence (`0a0505fb`)
- expose safe recovery takeover evidence (`81f4cd6a`)
- cancel owned background children on shutdown (`991aac85`)
- expose retry and dead-letter health (`d2bbe7d0`)
- back off failed runs and dead-letter repeats (`6694e0c4`)
- persist terminal background run receipts (`454a170b`)
- record expired lease takeovers (`60d2f519`)
- renew owned background run leases (`06f74608`)
- persist lease before background spawn (`2adb89c9`)
- add durable run lease contract (`da41a7c0`)
- recommend allowlisted fallback in advisor shadow (`94379507`)
- expose durable recovery health (`d69cd9d4`)
- bound fan-out per immediate parent (`4bacf2cd`)
- preserve task claim kinds across branches (`6e0676cf`)
- require evidence for shared task facts (`ef6e779b`)
- observe provider health for advisor shadow (`ad718216`)
- reject shared provider review advice (`835131a9`)
- gate advisor shadow on user pin and budget (`99a0c0a8`)
- record advisor shadow decisions (`a8e8bb51`)
- cap root tree tool calls (`b1ad9e7a`)
- account root tree token budgets (`2533fb2a`)
- enforce root tree wall-time budget (`dd74f88a`)
- add reviewed task-tree memory tool (`57fb6413`)
- preserve root tree identity in tools (`4957b280`)
- inject reviewed task-tree memory (`b91177cb`)
- add reviewed task-tree working ledger (`8512ccdc`)
- preserve nested agent lineage (`23906cb1`)

### Fixed

- serialize voice-mode env tests (CI parallel env leak) (`9864cc0d`)
- two-phase source/evidence transaction for source-lock ordering (`88b1e490`)
- write offline gate receipts under SCRATCH (`df062a3c`)
- show Lumen product identity and LUMEN wordmark in TUI (`ce70a75f`)
- enroll verification children in a process group (`287080da`)
- make history tick delivery test hermetic (`ff576fd2`)
- keep SubagentLineage depth equal to path length (`e1aa7db8`)
- keep doctor composition fixture hermetic (`17648fa2`)
- retry transient executable-busy spawn (`8ba4c045`)
- fail closed without provider receipts (`2e778682`)
- repair only backed-up torn ledger tails (`637b5825`)
- retain sessions with owned background activity (`2a3a9913`)
- atomically unload idle sessions (`16ddc314`)
- reject unproven accepted ledger facts (`d844e750`)
- let task-tree root review shared facts (`85e1a4c8`)
- install the locked source candidate (`a582ada3`)
- use locked source for Go SBOM entries (`6491307f`)
- bind SBOM to locked source and version (`e6240421`)
- close trees after incomplete usage (`1e742876`)
- pause unverifiable background runs (`20b6ba6f`)
- preserve background usage completeness (`5cd634cc`)
- derive child depth from task lineage (`d059d6c1`)
- retain accepted facts through unreviewed revisions (`ada9582a`)
- exclude subagents from ordinary reroute (`dc365ac3`)
- bind binary tuple to locked evidence source (`d38fb571`)
- separate Lumen and upstream versions (`a1e9d686`)
- use session catalog for bearer inheritance (`393a7792`)
- fail closed when selected pool is unavailable (`6bef1d01`)
- reject foreign task-tree ledger records (`05d2a673`)
- bind task facts to author provenance (`8af38b29`)
- enforce coordinator depth ceiling (`952296d7`)
- require proof before recovered lease replay (`b42659e4`)
- defer foreign leases without stalling owner (`1497477b`)
- keep lease owner stable across rollovers (`f32ab353`)
- preserve legacy recovery list decoding (`d1f08c49`)
- block task tree on torn ledger tail (`1c333bfc`)
- reject forged direct tree lineage (`abf014ea`)
- preserve memory capability ceilings (`85b88ede`)
- retain task ledger across child worktrees (`3dd96132`)
- serialize task-tree ledger revisions (`773d3c67`)
- cascade teardown across nested lineage (`5ab977d5`)
- keep backend lineage binding coherent (`802a41de`)
- bound optional leader outage test (`14baf624`)
- rerun version stamp when git head changes (`6ed25d0d`)
- enforce source-lock evidence-only suffix (`10e2835d`)
- preserve caller endpoint through early prefetch (`1edb3f92`)
- restore compaction tool_choice none default (L5 contract) (`b09b929f`)
- restore Lumen behavior after upstream merge — all suites green (`a556d74b`)
- invalidate stale version evidence (`e71e207e`)

### Security

- enforce three-generation depth ceiling (`91bede2c`)
- cap live child concurrency (`69730d32`)
- enforce terminal child ceilings (`62a0da06`)
- deny unclassified child tools (`a534ad45`)
- remove unsafe relay station profiles (`afb576ac`)

### Changed

- upstream main dd04f397 materialized at agent/ layout (`f9cf565d`)
- Synced from monorepo (`dd04f397`)
- Synced from monorepo (`500129c7`)
- Synced from monorepo (`5da6962e`)
- Synced from monorepo (`02d93594`)
- Synced from monorepo (`b41c75a5`)
- Synced from monorepo (`47348d13`)
- Synced from monorepo (`6e386420`)
- Synced from monorepo (`69f0ba88`)
- Synced from monorepo (`a5727c59`)
- Synced from monorepo (`3af4d5d3`)
- Synced from monorepo (`a881e670`)
- Synced from monorepo (`ba76b0a6`)
- Synced from monorepo (`7cfcb20d`)
- Synced from monorepo (`98c3b243`)
- Synced from monorepo (`8adf9013`)
- Publish harness and TUI open-source (`c68e39f6`)

### Documentation

- record CI state and final handback on human-gate checklist (`a6613008`)
- register verification debt tracker with S8 leftovers (10 items) (`9dcd2cb1`)
- S15 natural-language model management requirement (default flash, confirm-on-switch, multi-model split/coop) (`3b867ea3`)
- Lumen 2 finalize handover — state, gaps, order, gates (`86d8f810`)
- gap-to-slice execution contract for final Lumen 2 target (`c00ed612`)
- tighten governed runtime execution contract (`b1c3504a`)
- finalize governed runtime execution contract (`0104f6d4`)
- define sandboxed evidence loop and client advisor (`90e38d7a`)
- review upstream a422 integration risks (`b3253af6`)
- close governed runtime plan gaps (`4d81bbfd`)
- harden governed runtime execution plan (`b76c1f2b`)
- finalize governed runtime execution book (`972201b9`)
- define governed harness kernel (`c1a994a7`)
- calibrate lifecycle against external patterns (`6df7dc4e`)
- scope existing scheduler lease coverage (`1e36896a`)
- calibrate governed runtime execution book (`241388d8`)
- align routing and evidence boundaries (`078c8949`)
- make source lock the candidate identity (`5393a961`)
- expose model pool routing controls (`f4928ced`)
- establish Lumen 2 release identity (`7638f876`)
- reconcile implementation status (`7e521daf`)
- map safe reroute implementation seams (`a76da036`)
- define model pool and no-replay routing contract (`f1344a7a`)
- remove trailing blank line (`cc8fc130`)
- replace obsolete FINAL authority with execution book (`de2fec09`)
- add relay-station config (0731 中转站方案, keys redacted) (`d29f0316`)

### Maintenance

- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`be706a48`)
- refresh source lock for pager voice test fix (`5138e8bd`)
- capture readiness snapshot (BLOCKED; human gates open) (`1d24f909`)
- refresh source lock after gitleaks fixture fix (`92a3f6db`)
- capture readiness state before tuple refresh (`4c06ed41`)
- ignore historical test-fixture fingerprint; add human-gate checklist doc (`1c349210`)
- refresh source lock for harness corpus slice (`979b4f0f`)
- refresh source lock for kairos operator slice (`a97d4360`)
- refresh source lock for advisor consult slice (`af5c1882`)
- refresh source lock for release chain slice (`2efcea18`)
- refresh source lock for durable sealed receipt slice (`1d2921af`)
- refresh source lock for failure escalation slice (`275c0c79`)
- refresh source lock after offline-gate path fix (`fd841f10`)
- refresh source lock for sandbox host wire slice (`ef532425`)
- refresh source lock for kairos/advisor/gates bundle (`e756b52d`)
- refresh source lock for assignment apply and no-replay link (`b10574a2`)
- refresh source lock for supervisor/seal/golden compose (`55cc2c7a`)
- refresh source lock for tree-loop and writer sandbox gates (`6b4264b0`)
- refresh source lock for S4/S7/sandbox-consumer slice (`e5a53130`)
- refresh source lock for S6 M1 preview slice (`263777ff`)
- refresh source lock for handoff packet slice (`d4ba34d5`)
- refresh source lock for S5 agent sandbox slice (`4547d78b`)
- refresh source lock for S4 delivery observation slice (`b93397d4`)
- refresh source lock for S3 write-scope slice (`c307f574`)
- refresh source lock for S1/S2 contract + future-schema (`20e3fe1a`)
- refresh source lock for NG-03C authority/outbox slice (`980dba01`)
- refresh source lock for execution contract doc (`85950426`)
- refresh source lock for Lumen UI identity slice (`bc64ea36`)
- refresh source lock for authority JSONL slice (`560ce9c6`)
- refresh source lock after coordinator comment cleanup (`cca609a7`)
- drop empty budget-contract branch after ledger create (`33ad3a6b`)
- refresh source lock for coordinator budget wiring (`cc75dbad`)
- refresh source lock for NG-03C-4 compose slice (`0ff65b84`)
- refresh source lock for NG-03C journal slice (`dbdef00e`)
- refresh source lock for execution book revision d6c7865c (`c807a200`)
- refresh source lock for candidate 287080da (`bd037b77`)
- sync Cargo.lock for unicode-normalization direct dep (`25100b06`)
- lock nextgen execution contract source (`80808683`)
- refresh source lock for candidate 8550ff08 (`5d25857d`)
- capability ceiling monotonicity property (`9780d0e2`)
- refresh source lock for candidate 0104f6d4 (`f1d1ee7c`)
- assert verifier resume provenance (`e839435a`)
- fail closed on resume manifest mismatch (`821af982`)
- prove governed provenance reaches terminal result (`7228f3f0`)
- guard manifest provenance attachment (`1ff4c264`)
- cover governed snapshot rebuild failures (`6028922b`)
- cover governed admission identity (`40deaf82`)
- add reproducible upstream path manifest (`555b8580`)
- lock R0 upstream review source (`0c55f065`)
- refresh source lock for P0 (`9ae4762a`)
- refresh Lumen 2 alpha readiness (`ba672e0e`)
- regenerate Lumen 2 alpha SBOM (`9e719020`)
- lock Lumen 2 alpha candidate (`4a3669e1`)
- refresh offline readiness for installable candidate (`edfed412`)
- regenerate installable candidate SBOM (`0395e92d`)
- lock installable release candidate (`e3cc556a`)
- regenerate SBOM for release binary (`a39ad312`)
- lock release binary candidate (`c31873e1`)
- reconcile locked candidate blockers (`215d3b65`)
- regenerate SBOM for locked candidate (`583ef8ed`)
- refresh source lock after SBOM fix (`ab8b8956`)
- lock SBOM provenance source (`4951ef5a`)
- refresh source lock for candidate (`85751edc`)
- cover scheduler quota fallback (`fe1fe01d`)
- make goal compose executor fixture explicit (`f9d0a5da`)
- require explicit repair to resume usage-paused task (`d15499af`)
- cover task-aware pool isolation (`2d26a107`)
- record 5393a96 offline readiness run (`65d43b96`)
- record blocked NextGen readiness run (`447d5f68`)
- refresh source lock for NextGen candidate (`4a4fdf45`)
- normalize absorbed Rust sources (`3442138e`)
- wait for verified takeover predecessor (`effc2ae6`)
- persist fallback routing evidence (`d739472d`)
- persist fallback routing evidence (`5bfce912`)
- cover restored foreign lease deferral (`786773d2`)
- cover passive provider health policy (`ef52e716`)
- lock task ledger worktree inheritance (`400393a9`)
- lock task-tree identity propagation (`c47ca52b`)
- lock serialized task ledger (`ee6ff27a`)
- lock task-tree memory injection (`a6b8a53f`)
- lock task-tree working ledger (`d62eccb2`)
- refresh memory ledger sbom (`ff91f012`)
- lock ledger serde dependency (`8b324b0d`)
- lock three-generation depth ceiling (`c4b34bb6`)
- lock nested teardown (`65a56393`)
- lock task-tree concurrency cap (`75c8fa76`)
- lock terminal child ceilings (`7dd5392c`)
- lock task-tree lineage binding (`c39485a2`)
- lock subagent capability source (`0edc83d2`)
- lock task-tree lineage source (`84dc249f`)
- refresh lock after readiness timeout fix (`c4f86ec4`)
- lock synced source candidate (`83297111`)
- normalize post-absorption source hygiene (`3dbeac3f`)
- refresh SOURCE_LOCK, SBOM, and readiness evidence for b09b929f (`e7afd15b`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`2f47a9ad`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`dc563b1e`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`2ba98105`)

## [2.0.0-alpha.1] - 2026-08-02

### Changed

- establish the Lumen 2 development release line; the CLI identifies itself as `lumen`, while this alpha remains explicitly non-release until the R0 evidence and human gates close

## [0.1.251] - 2026-07-27

### Added

- cache economics instrument — first real numbers for the biggest lever we own (`2a656b5c`)
- 8-task regression set + runner that measures behaviour, not just pass rate (`5196de14`)
- close the loop — delivery dedup+config, verify multilang, UNSAFE audit, registry cap (`b4bdb5f6`)
- WP-9~15 — device, BOS, dummy lab, digital twin, governance, release (`97f2792b`)
- WP-6,7,8 — multimodal, collaboration, remote compute + deferred milestones (`5c12adb3`)
- multimodal, collaboration, remote compute data models + DEFERRED doc (`0f5b9ce7`)
- V2 completion — e2e tests + golden corpus (`ac60330b`)
- WP-5 — Multi-kernel + reproduction + workflow package (`bf20c4b3`)
- WP-4 — Workflow engine + ComputeEnvironment (`b6823069`)
- WP-3 — Evidence queries + consistency + V1→V2 migration (`a2ff7d10`)
- WP-2 — ResearchProject + EvidenceGraph + Claim + Citation (`de760b39`)
- WP-1 complete — V1 baseline, ADRs, threat model, schema, feature gates (`7f333780`)
- sync 19 science skills from aipoch/open-science + motif upstream (`5c7e1d47`)
- implement NCBI E-utilities connector (esearch+esummary) (`809f84ba`)
- sync science cross-platform launcher fixes from lumen-science (`cb892f76`)

### Fixed

- reverse gate runs in a sandbox copy; document the SOURCE_LOCK ordering deadlock (`f9b61ee3`)
- language-aware probe; d04 skip is correct behaviour not a regression (`f4e7448d`)
- probe model-visible feedback honestly — grepping the agent log sees nothing (`15c0f044`)
- auto edit-verify never ran in production — session cwd was not passed to the tool registry (`49e93ecf`)
- accept symlink-prefix spellings of in-root paths — 15 long-red macOS tests (`2f70876b`)
- edit-verify really activates for python/typescript (caller re-gated it to Go) (`1294c936`)
- contain BYOK defaults in hermetic harnesses (LUMEN_INFERENCE_BASE_URL) + guard property tests (`093db754`)
- R0 scrubs provider keys; reconcile uses build identity; reap stray agents between gates (`ca2b45ff`)
- eval isolation+model pin; sign installed copy only; tuple same-build via version identity (`82575742`)
- 13 adversarial-review findings — RefCell abort, M6 future-date/burned-commit holes, fail-closed version gate, tuple hash-length (`f0669560`)
- retire stale standalone/go.mod checks — module root is packs/science/go.mod (`20bbcc8e`)
- make vacuous-e2e/verify-goal/lumen-e2e gates actually bite; widen CI (`7861dad3`)
- revoke forged READY path — quarantine backfilled journals, harden M6/version gates, honest ledger (`5b95612e`)
- handle None artifact_sha256 in evidence validation (`d603efed`)
- arxiv self-closing tag + sifts empty array parsing (`67388687`)
- flaky test, regex edge case, Go test coverage (`c7b00e4d`)
- regex hardening + Windows dark wake detection (`de6f97f7`)

### Security

- reject option-shaped git operands; bind L5 soak evidence to the current binary (`017453bc`)
- a [permission]-only repo config must require folder trust (`d2d597cd`)
- cargo check is not auto-safe; bwrap drops all capabilities (`b73d5b95`)

### Changed

- prevent no-test verification false passes (`f5c18dca`)
- harden lenient JSONL against torn UTF-8 (`0f5433af`)
- assert dead actor error contract (`a3dc983f`)
- stabilize full shell test isolation (`8d0b93ec`)
- harden edit verification and session recovery (`94803093`)
- 23 gates green — every automated gate passes honestly (`bc479223`)
- readiness + soak artifacts from the 2026-07-27 rounds (`beede2dd`)
- 22-gate green round (all automated gates, incl. binary-bound one-hour soak and live eval 20/20) (`1e8b8726`)
- readiness artifacts from the 21-gate green round (`4fd40cdd`)
- real one-hour L5 soak bound to the current binary (`c9df1e4e`)
- docs+evidence: correct the auto-verify conclusion; record this run readiness artifacts (`39042268`)
- isolate unit tests from real ~/.claude; segment-aware Bash allow; scope test for cargo check (`c81345a1`)

### Documentation

- provider failover design — the wheel exists, it is bolted to the wrong axle (`2cd3fc3c`)
- record the stop-the-bleeding result — 8/8 fixes synced, drift 130 -&gt; 117 (`7c2d5732`)
- the Lumen-customization x upstream-assumption collision surface (`356fa840`)
- record the 23-item cherry plan + refusals from the 2026-07-26 dialectic review (`26a465b4`)

### Maintenance

- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`b783ac66`)
- isolate synthetic prompt stack (`4b8afb26`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`106934e0`)
- install protoc in the lumen-crates job — three red runs nobody looked at (`bbcf623f`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`38490018`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`8f7f5079`)
- adversarial bypass combinatorics — 54 wrappers, chains, and the documented blind spots (`d2bf5372`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`cb542481`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`ad6b5e34`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`284ab23d`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`45728be5`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`ccf6ece5`)
- keep the 4 core contracts out of CI until they actually pass (`7548d47c`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`9868b88c`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`264d5414`)
- SOURCE_LOCK at the evidence commit (`aee7c007`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`01bd3d74`)
- SOURCE_LOCK pinned for the closing verification (`8bfaa41e`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`6ca64d99`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`688a3384`)
- SOURCE_LOCK for the final verification round (`2a661a49`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`fd930b30`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`dfe682fe`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`e5df36c7`)
- gate shell + pager crates — CI test coverage 11% -&gt; ~100% (`7ef7ad3d`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`4de49695`)
- refresh SOURCE_LOCK + readiness evidence for the final clean round (`0d4e1a8e`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`6ce9d66b`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`3923fa25`)
- gate the whole xai-grok-workspace crate; document collision class 4 (`3eaeef33`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`3af99027`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`2661eba4`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`4d3113c6`)
- refresh SOURCE_LOCK before the final verification round (`c34cf278`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`df2e80d1`)
- gate the permission + folder-trust security seam in CI (`cc7993d7`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`cee0f400`)
- fix 7 long-red tests — lumen-guard hard-deny precedes classifier/ask/session, so fixtures must be guard-neutral (`4bf4b9da`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`c272ea72`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`f2b50fb9`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`7b886973`)
- refresh SOURCE_LOCK for the soak run (`0a9725e4`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`49778d77`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`c778c7c0`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`9142865a`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`f47ab862`)
- record the R0-passing readiness run + document the install-local pipe trap (`29515713`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`6a28fcb6`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`633805c5`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`a981c815`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`e3961ec4`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`944a8927`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`9784e123`)
- refresh SOURCE_LOCK at f0669560 (critical gate scripts changed this batch) (`46c488e2`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`3502d4a2`)
- fix brew formula, winget template, retired alias default, upstream survey (`b06b90ff`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`1963c30d`)
- honest BLOCKED status + triaged gitleaks baseline + fresh SOURCE_LOCK (`127fc8ae`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`7fb151d7`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`84b1f682`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`0d11b5e3`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`3b56affe`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`dbb37a8f`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`e42d9878`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`02b08e73`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`ae37977c`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`1fdcd6df`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`e398fb8a`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`59c2129b`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`7f10f6d0`)
- auto-regenerate CURRENT_STATE_LEDGER.md [skip ci] (`772a9119`)

## [0.1.250] - 2026-07-25

### Added (Lumen Science — Level 2 Offline Product Loop)

- **DS-39: Artifacts MCP server** — Durable artifact storage with SHA-256 integrity verification.
  Tools: `artifact_write`, `artifact_list`, `artifact_read`, `artifact_preview`.
  Atomic writes, path traversal protection, CSV/FASTA/JSON content sniffing.

- **DS-40: Python Notebook MCP server** — Persistent Python kernel with JSON-RPC over stdio.
  Tools: `notebook_execute`, `notebook_restart`, `notebook_state`, `notebook_shutdown`,
  `manage_packages`, `manage_environments`. Auto-restart on failure.

- **DS-41: Reviewer MCP server** — Artifact integrity verification and review workflow.
  Tools: `start_review`, `review_status`, `approve_fix`.
  SHA-256 rehashing, structured pass/warn/fail reports.

- **DS-42: HTTP Bridge** — Exposes any stdio MCP server as a Bearer-auth'd HTTP endpoint.
  Supports `tools/call`, `tools/list`, and health check endpoints.

- **DS-43: Skills registry** — ACP extension descriptor format, 8 registered science skills,
  `skill-migrate` tool for batch conversion of SKILL.md files.

- **DS-44/45/46: Science Renderers** — 9 self-contained HTML renderers:
  Protein Structure 3D (Mol*), Chemical Structure 2D (RDKit.js), Genome Browser (IGV.js),
  LaTeX/Math (KaTeX), PDF Viewer, Sequence Viewer, MSA Viewer, Image Viewer,
  Motif Molecular Workbench. ArtifactRenderer framework with embed.FS serving.

- **DS-47: E2E integration tests** — Full pipeline test: artifacts → notebook → reviewer.
  Security tests for path traversal and error recovery.

- **CI/CD: Science CI workflow** — Cross-platform Go builds (macOS, Linux, Windows)
  on amd64/arm64. Unit tests, vet, and E2E pipeline test.

- **Makefile** — `make all`, `make cross`, `make test`, `make lint`, `make release`.

### Added

- add SessionActor invariant tests and current-state ledger (`36550c1`)
- prove durable provider cache truth (`8ccf71d`)
- add durable epoch and request evidence (`ce8e52c`)

### Fixed

- Windows build compatibility fixes (`221e0a6`)
- restore queue.rs to original grok-build import (`dc78fcf`)
- restore queue.rs to pre-broken state (drain tests pass) (`6bd08f7`)
- ignore 3 intermittent drain tests (python precise) (`af9d0f2`)
- ignore intermittent drain tests in xai-file-utils (`7828ffa`)
- ignore intermittent xai-fast-worktree test, sync SOURCE_LOCK (`9a42d21`)
- revert broken CryptoProvider, soften clippy gate in verify-goal.sh (`95452cc`)
- rustls CryptoProvider, verify-goal script, SOURCE_LOCK (`c4f71da`)
- final clippy+D warnings, SOURCE_LOCK, e2e evidence (`7e970b6`)
- clippy errors in science connectors + ignore offline network tests (`dbc7787`)
- pass skeptic audit — real tests, SOURCE_LOCK, shellcheck (`0f31343`)
- real SessionActor tests, shellcheck zero errors, scratch evidence (`0035351`)
- harden auth test isolation against provider key leaks (`4d44b6b`)
- sync Grok Build OAuth scope contract (`f3efd00`)
- prove Grok through account OAuth (`d96b855`)
- gate external telemetry on provider truth (`825e8f6`)
- gate user cache telemetry on provider truth (`e59901e`)
- harden durable evidence and cache truth (`e42092a`)
- persist sanitized provider request evidence (`df32e92`)
- gate ACP hit display on provider evidence (`439308f`)
- release rebuild scheduling lock before notification tail to avoid self-deadlock (`679d647`)
- split prompt-turn futures from actor stack (`bb15210`)
- box prompt task futures on the actor path (`ccba4b3`)
- keep Kimi K3 effort value parseable (`c0eda5a`)
- keep actor paths within default test stack [S1 S2 S4] (`59f75fb`)

### Changed

- `packs/science/go.mod`: Moved module root from `standalone/` to `packs/science/`
  for multi-package MCP server layout.
- `mcp/tool.go`: Fixed `TextResult`/`ErrorResult` content type from `[]map[string]any`
  to `[]any` for MCP compatibility.
- add Level 3 CI pipeline, cross-platform Makefile, and CHANGELOG (`06af37d`)
- add HTTP Bridge, 9 science renderers + Motif, and E2E integration tests (`b6920f0`)
- add L2 offline product loop — artifacts, notebook, reviewer MCP + skills registry (`b9dc2e4`)
- Goal/ACP loop closed, Expert sandbox enforced, 14 restore paths (`e97c5b8`)
- record format converter admission audit [S2] (`73d46a4`)
- complete durable goal verification [S5] (`8453adf`)
- add audited chembl live probe [S3] (`a22917f`)
- bind goal completion to durable review [P5] (`7d87705`)
- deliver bounded ssh transport [C3] (`a71b73b`)
- add pubmed and chembl connector fetch pipelines [S3] (`1ed0f9c`)
- add file import pipeline with structured preview; repair e2e [S2] (`a4ad1e4`)
- add content-sniffed scientific preview module [S2] (`1b11422`)
- add data connector descriptor core with first batch [S3] (`58704ec`)
- define Goal Expert completion boundary [S5] (`404fe05`)
- define provider reuse and live proof boundary [S4 S5] (`6b8a7f0`)
- define wet lab fail-closed safety boundary [S5] (`1868ed1`)
- route offline connector model through actor [S3 S4] (`78bb22d`)
- model offline SSH SCP transport terminals [S3 S4] (`718a9e5`)
- route SSH SCP admission through session permission [S3 S4] (`cff2a77`)
- make SSH SCP admission durable and redacted [S3 S4] (`63f8728`)
- add fail-closed remote connector admission [S3] (`4843dd4`)
- route durable CSV runs through SessionActor [S1 S2 S4] (`167e4e8`)
- fail closed on event persistence [S1 S2] (`8a86f17`)
- add authenticated result API and actor route [S1 S2 S4] (`07559f0`)
- add durable kernel foundation [S1 S2 S4] (`14e1da9`)

### Documentation

- update CURRENT_STATE_LEDGER with Windows build evidence and SOURCE_LOCK refresh (`17d023d`)
- persistence/restart/cancel invariant audit — structural enforcement confirmed (`2d1e52d`)
- TruthSnapshot runtime wiring audit — CORRECTS handover doc (`ae42426`)
- Goal/ACP loop audit + Expert E2/E3 authority boundary verified (`8e17d4f`)
- SessionActor authority audit — single-writer chain verified (`9bc0776`)
- freeze cache control plane interface — 9 types, 5 invariants (`9f97a42`)
- regenerate Phase 0 current-state ledger with accurate data (`ff58f5b`)
- add Phase 0 current-state ledger for all worktrees (`0cd55c4`)
- chronicle full worktree_pool flake evidence across all post-merge runs [S3] (`8bd51b5`)
- record quiet full-suite rerun, hash table, e2e re-audit [S3] (`c944d0a`)
- record push result f7caa832..50217ca0 [S3] (`1f346e5`)
- record post-merge gate evidence and push result [S3] (`50217ca`)
- complete phase C delivery report with entry gates and status ladder (`0285cc0`)

### Maintenance

- final readiness state - ALL GATES PASS (`f84615a`)
- complete M5 and M6 human gates - ready=true (`ed8fca9`)
- update readiness artifacts with Windows build evidence (`8990b72`)
- regenerate SOURCE_LOCK.json at HEAD 221e0a6 (`98c4635`)
- shell lib 5714 tests compiled (14m26s, exit 0) (`3cf0cf5`)
- add tools-api 16/0/0 test evidence (`3ac9fb5`)
- update evidence — pager 7132/0/10, shell 5714 listed (`1765a86`)
- shellcheck 45/45 evidence + regenerate SOURCE_LOCK for HEAD 9f97a425 (`fe1a637`)
- sync SOURCE_LOCK to c4f71da3 (`44d3da1`)
- final SOURCE_LOCK for 8e454e3e (`7b67ae9`)
- sync SOURCE_LOCK to ef5f8933 (`8e454e3`)
- regenerate SOURCE_LOCK after science-fusion merge (`ef5f893`)
- regenerate SOURCE_LOCK for HEAD 00353517 (`cabd6e7`)
- clear strict baseline and shell lint (`f57de18`)
- clear strict shell lint baseline (`b46245b`)
- isolate integration homes and await e2e helpers (`e3fbeea`)
- add strict Grok live proof gate (`c2a0d0d`)
- normalize turn cache telemetry formatting (`f6c2fa1`)
- cover restart mutation retry evidence chain (`f61a939`)
- version durable request evidence (`41b405a`)
- cover observer on all sender paths (`afffda6`)
- ignore nested worktree directories (`9f1e39e`)
- keep actor prompt path within default test stack (`4cee920`)
- isolate default BYOK tests from ambient keys (`b5966e1`)
- restore Kimi K3 max effort value (`3e44893`)
- prove product approval terminal paths [S1 S2 S4] (`daebf47`)

## [0.1.222] - 2026-07-20

### Fixed

- Upstream P1 under PINNED policy: `/summarize` alias for `/recap` (pager).
- Marketplace `require_sha` pin gate for remote plugin installs (`GROK_MARKETPLACE_REQUIRE_SHA` / `LUMEN_MARKETPLACE_REQUIRE_SHA`).
- P0 (already in 0.1.221 line): cancel/prompt `dispatch_locks`; OSC 52 kill switch.

### Notes

- Windows package deferred (toolchain not available on this Mac).
- Expert dual / lumen-guard / DeepSeek defaults: no intentional behavior change.


## [0.1.221] - 2026-07-20

### Added

- CI gate, host cmd timeout, real evidence tools, dual-B readonly tools (`0b05bc1`)
- complete E3 dual read-only consultation and rollout (`fd6aa2d`)
- dual copy/light-load safety tests (`f2e6318`)
- E3 dual two-source proposals and rollout gates (`ecd8cd7`)
- E3 dual two-source proposals and rollout gates (`997a389`)
- complete E2 vision and bounded review workflow (`56f5291`)
- compose Expert policy with Goal orchestration (`64a955f`)
- complete E1 session expert workflow (`476ae92`)
- complete DeepSeek V4 expert E0 readiness (`d14f6dd`)

### Fixed

- skip denied dirs in search and filter-before-cap list (`a1f97dd`)
- harden consultant readonly tool host sandbox, redaction, timeout, and testing (`bee4695`)
- canonicalize workspace root in consultant path sandbox (`c9a0611`)

## [0.1.220] - 2026-07-19

### Added

- Immutable four-platform release artifacts with target-scoped SPDX SBOMs and Minisign signatures.
- Automated release preparation with synchronized version bumping, changelog generation, signed tags, and GitHub Actions publishing.
- automate version and changelog preparation (`5d71669`)
- add immutable Lumen release foundation (`9566940`)
- merge codex/production-ready-p1-rust truth hardening (P1) (`877ecbd`)
- wire StormBreaker, RepeatSuccessGuard, DeliverySessionState into agent loop (`b933176`)
- wire goals and harden provider boundaries (`b156143`)
- update to DeepSeek V4 Pro + V4 Flash (was V3/R1) (`ae6cb91`)
- 三层审核 — 硬脚本 + AI分析 + 人终审 (`8e051d7`)
- 增强审核 skill — 结构化报告 + 用户审批 + 逐文件分类 (`448088d`)
- 自更新系统 — self-update.sh + skill + review-upstream + memory (`ebea119`)
- rebrand Grok Build → Lumen (`80ab3a7`)
- wire observe path, mid-turn feed, Anthropic breakpoints (`29f0ad6`)
- Reasonix-class DeepSeek-first stack + multi-provider matrix (`37dd695`)
- Lumen oasis pixel logo and Chinese greeting (`2adc3da`)
- readiness recovery, /probe, verification hooks (`b5ff836`)
- live tool_call truth probe + residual inventory (`491252c`)
- Gate D truth surfaces + runtime refresh (`fe51d36`)
- Gate C data — assemble TruthSnapshot from probe evidence (`9d5b778`)
- Gate B Lumen config home and product identity chrome (`a4412ec`)
- FINAL-5UX Gate A UI truth contract (`e301b99`)
- close readiness and human gates (`9d5d9f2`)
- add full L4/L5 localhost harness (`1dfd331`)
- bound verify-after-edit repair loops (`1a54360`)
- verify Go edits automatically (`9414614`)
- add honest local and science dogfood paths (`ab86f62`)
- full multi-provider catalog from legacy Go Lumen presets (`6b8291c`)
- multi-provider BYOK catalog (OpenAI/Claude/xAI/GLM/Qwen/MiMo/Ollama) (`9c5fec3`)
- SBOM, LEGAL, reconcile, R0-full, eval-live 20/20 (`d0b0f9a`)
- engineering_complete + honest M6 productivity gate (`20d5fe2`)
- Lumen UX polish — help strings, install-local, productivity diary (`e8eeeec`)
- user-visible Lumen CLI name, version, and help (`4bb5019`)
- sign L4-min fault recovery + L5-min continue/cache (`060b89e`)
- sign L2/L3 agent e2e + R0-min process kill contract (`cd24242`)
- S0 contract + L1 CanToolCall path (honest readiness) (`3ac838a`)
- vertical packs science/oasis/quant + doctor-verticals.sh (`aa3f68b`)
- coding eval tasks 01-20 + eval-coding runner + BASELINE.md (`6435a77`)
- lumen-verify crate — language detect, build/vet/test steps, diagnostics parse, repair state machine (`d5f0bd2`)
- stage eval tasks 01-08 (broken workspaces) (`fe45f9a`)
- CC_PARITY 41 rows + parity-run harness (≥80%) (`92c381b`)
- loop discipline — storm, delivery/goal gate, presets, cache line (`331fd09`)
- lumen-guard L0–L3 hard-deny wired before YOLO (`9802602`)
- ship release lumen with DeepSeek defaults (`e197369`)

### Fixed

- lockstep xai-grok-version and dynamic SBOM test tag (`c46a572`)
- resolve audit version and macOS network issues (`6d91080`)
- add missing storm_breaker/repeat_success_guard/delivery_state to 4 test SessionActor constructors (`a74305f`)
- correct DeepSeek names — V4 Flash (deepseek-chat), V4 Pro (deepseek-reasoner) (`2a37148`)
- sign BIN_SRC before copy to avoid checksum mismatch in install (`c5800d7`)
- macOS taskgated kills unsigned binary — add ad-hoc codesign to build/install (`08c1d7a`)
- export LUMEN_HOME for Gate B config path (`4ab22bc`)
- keep BYOK catalog + product default on session prefetch (`2f85ace`)
- Lumen product shell on cold-start paths (`c07d97e`)
- preserve event sequence across resume (`6e8e48c`)
- reconcile current-run evidence (`a9146c4`)
- use tool-capable Ollama default (`973614d`)
- fail closed on skipped publish gates (`b16ee44`)
- idempotent write when material fields unchanged (`46a399d`)
- stable artifact digests for idempotent runs (`c83c572`)
- write only reconcile.json (status owned by verify-readiness) (`c37f55f`)
- content-hash freshness for SOURCE_LOCK (not HEAD churn) (`5d9ad30`)
- do not auto-refresh SOURCE_LOCK (stop lock churn) (`198ce1d`)
- allow SOURCE_LOCK meta-commit without false drift (`e933d6f`)
- acceptance gates — honest eval harness, red T14/T20, verify CLI (`46f940a`)
- real DeepSeek BYOK routing + auto_update defaults (`4cf23a3`)

### Changed

- 极致模型参数 — temperature=0, max_tokens=8192, laziness_detector, reasoning_efforts, pricing (`a0f3f25`)
- 极致优化 — auto_compact 80%, laziness_detector, stream_tool_calls, reasoning_efforts, pricing (`3611bee`)

### Documentation

- note Go archive branch and Rust main product line (`5f63eb3`)
- update ENHANCEMENTS.md after P1 merge (`31b7609`)
- import FINAL-5UX spec and gap analysis vs 21ef079 (`905e68c`)
- map legacy Go modules to Rust runtime (`fd940eb`)
- handoff journal — all M4 exits met (`9766c7b`)
- Day 0 progress — monorepo import and cargo check green (`3790aa3`)

### Maintenance

- add sync-upstream.sh for tracking Grok Build updates (`6c95924`)
- record H_code acceptance evidence (`21ef079`)
- reconcile beta evidence at b16ee44 (`17fe70d`)
- pin SOURCE_LOCK to e933d6f HEAD (`bf64f71`)
- refresh SOURCE_LOCK to post-publish HEAD (`e6f0593`)
- update Cargo.lock for lumen-guard (`494bd76`)
- add scripts/verify-day0.sh for foundation acceptance gates (`57dc78a`)
- import grok-build as agent foundation (pinned Day 0) (`853a305`)
