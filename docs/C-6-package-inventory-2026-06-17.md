# C-6 包瘦身盘点 (2026-06-17)

> 作者:Claude。对应 `规划书-打磨.md` 的 **C-6**。北极星:少即是多,维护面决定迭代速度。

## 结论

- **192 → 58 个 internal 包**,删除 **134 个零引用死包**。
- 判据:`go list -deps ./cmd/lumen` 得到真正编译进二进制的包(55 个);其余 137 个不在二进制里。
- 其中 **134 个零引用**(无任何 .go 文件——含测试——import 它们)→ 100% 安全删除,删后 `go build ./...` + `go vet ./...` + `go test ./...` 全绿。
- **保留的非二进制包(2 个,被活包的测试引用,故不删):**
  - `internal/mock` — agent harness 测试用。
  - `internal/apigateway` — `e2e/e2e_test.go` 用。

## 方法(可复现)

```
go list -deps ./cmd/lumen | grep ^lumen/internal/ | sort > inbin   # 编译进二进制的
go list ./internal/... | sort > all                                # 全部
comm -23 all inbin                                                 # 不在二进制 = 死weight 候选
# 再对每个候选 grep 其 import 路径(含测试),零命中 = 安全删除
```

## 删除的 134 个零引用死包

> 多为 bloat 期产物 / demo(`bloom graphdb vectorstore apigateway-类 clustermap circuitbreaker maestro` 等),与终端写代码无关,也不被 agent 主路径引用。git 可随时 restore。

- internal/acp
- internal/adapt
- internal/approval
- internal/archive
- internal/artifact
- internal/asset
- internal/ast
- internal/authengine
- internal/batch
- internal/bloom
- internal/broker
- internal/browser
- internal/buildinfo
- internal/cache_system
- internal/cliutils
- internal/cloud
- internal/clustermap
- internal/codegen
- internal/codegraph
- internal/command
- internal/compiler
- internal/complexity
- internal/compliance
- internal/compressor
- internal/config_validate
- internal/configmigrate
- internal/connector
- internal/contextmgr
- internal/cron
- internal/crypto
- internal/datapipeline
- internal/deadcode
- internal/deadletter
- internal/deploy
- internal/di
- internal/diff_engine
- internal/dispatcher
- internal/docsgen
- internal/drift
- internal/embeddings
- internal/evacuate
- internal/eventbus
- internal/export
- internal/exporter
- internal/extender
- internal/featureflag
- internal/filewatcher
- internal/fingerprint
- internal/fluid
- internal/formatter
- internal/fuzzer
- internal/gateway
- internal/graphdb
- internal/graphql
- internal/hardening
- internal/harness
- internal/healthz
- internal/hook
- internal/hotplug
- internal/i18n
- internal/importer
- internal/inline
- internal/keymanager
- internal/keys
- internal/knowledge
- internal/linter
- internal/loadgen
- internal/lockfile
- internal/logger
- internal/maestro
- internal/manifest
- internal/marketplace
- internal/memory
- internal/metrics
- internal/migrate
- internal/mold
- internal/monitor
- internal/mux
- internal/notify
- internal/observer
- internal/orphan
- internal/packager
- internal/patch
- internal/pipeline
- internal/playbook
- internal/plugin
- internal/poller
- internal/profiler
- internal/proptest
- internal/queue
- internal/ratelimit
- internal/registry
- internal/release
- internal/repl
- internal/report
- internal/resolver
- internal/retry
- internal/routing
- internal/runtime
- internal/sandbox
- internal/scheduler
- internal/schemavalid
- internal/scrape
- internal/searchengine
- internal/security
- internal/selector
- internal/serve
- internal/sessiondb
- internal/sessionmgr
- internal/shard
- internal/signal
- internal/snapshot
- internal/sourcemap
- internal/statechart
- internal/statemachine
- internal/swizzle
- internal/sync
- internal/tagline
- internal/taskengine
- internal/template
- internal/terminal
- internal/tokenizer
- internal/toolpipeline
- internal/tracepoint
- internal/tracer
- internal/transpile
- internal/vault
- internal/vectorstore
- internal/verify
- internal/versioner
- internal/wand
- internal/watchpoint
- internal/websocket
- internal/workspace

## 保留:编译进二进制的 55 个活包(agent 主路径)

- internal/agent
- internal/audit
- internal/blueprint
- internal/checkpoint
- internal/codemap
- internal/computeruse
- internal/config
- internal/configlive
- internal/control
- internal/cronparser
- internal/diag
- internal/diff
- internal/diffengine
- internal/doctor
- internal/editverify
- internal/env
- internal/event
- internal/evidence
- internal/exchange
- internal/fileutil
- internal/frontmatter
- internal/github_ops
- internal/graphwalker
- internal/guard
- internal/heapdump
- internal/hermes
- internal/jobs
- internal/jsonpath
- internal/lineedit
- internal/lsp
- internal/mcplife
- internal/modelpool
- internal/notary
- internal/orchestrator
- internal/permission
- internal/policy
- internal/provider
- internal/provider/anthro
- internal/provider/gemini
- internal/provider/openai
- internal/reducer
- internal/render
- internal/schema
- internal/seal
- internal/skill
- internal/stream
- internal/telemetry
- internal/timeline
- internal/tool
- internal/tool/builtin
- internal/toolkit
- internal/topology
- internal/tui
- internal/websearch
