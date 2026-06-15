# Contributing to Lumen

Lumen is an open-source agent operating system — a full-stack platform for building, running, and orchestrating LLM agents at scale.

## Quick Start

```bash
# Clone
git clone https://github.com/exergyleizhou-ux/lumen.git
cd lumen

# Build
GOTOOLCHAIN=local go build -o bin/lumen ./cmd/lumen

# Test
GOTOOLCHAIN=local go test ./...

# Run REPL
./bin/lumen repl
```

## Architecture

Lumen has **8 domain layers** spanning 180+ packages:

| Layer | Packages | Purpose |
|-------|----------|---------|
| 🏗️ Orchestration | orchestrator, maestro, playbook, blueprint | Multi-agent workflow engines |
| 🤖 Intelligence | knowledge, graphql, jsonpath, compiler | AST analysis, semantic search |
| 🌐 Networking | apigateway, broker, websocket, mux | API gateway, message bus |
| 🔒 Security | seal, notary, vault, audit, policy | Encrypted key mgmt, tamper-proof audit |
| 📊 Observability | observer, monitor, tracepoint, diag | OpenTelemetry, Prometheus, diagnostics |
| 💾 Storage | sessiondb, shard, bloom, artifact | SQLite, consistent hashing |
| 🧵 Data | datapipeline, stream, reducer, swizzle | ETL, MapReduce, streaming |
| ⚙️ Infra | signal, blueprint, circuitbreaker, lockfile | DI, circuit breaking, signals |

## Development

```bash
# Run all tests
GOTOOLCHAIN=local go test -count=1 -timeout=60s ./...

# Run specific package tests
GOTOOLCHAIN=local go test ./internal/maestro/...

# Build + vet + test (ship gate)
GOTOOLCHAIN=local go build ./... && \
  GOTOOLCHAIN=local go vet ./... && \
  GOTOOLCHAIN=local go test -race ./...
```

## Package Naming

- `internal/<domain>/` — Core domain packages
- `internal/tool/builtin/` — Built-in agent tools
- `e2e/` — End-to-end integration tests
- `cmd/lumen/` — Main entry point

## Adding a New Package

1. Create `internal/<name>/<name>.go` with proper imports
2. Create `internal/<name>/<name>_test.go` with ≥3 real tests
3. `GOTOOLCHAIN=local go build ./internal/<name>/...`
4. `GOTOOLCHAIN=local go test ./internal/<name>/...`
5. All imports in `import (...)` at file top only

## Code Quality Standards

- All packages must compile and pass `go vet`
- Every non-trivial function must have a test
- Imports at file top in `import (...)` block only
- No dead code or unused imports
- Interfaces > concrete types for boundaries

## Security

- Cryptographic operations use standard library or `golang.org/x/crypto`
- Secrets never logged or serialized
- Audit trail has cryptographic integrity (hash chain)
- All user input validated through `verify` package
- Authentication through `seal` + `notary` packages

## License

MIT
