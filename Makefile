# Lumen build/quality gate. `make check` is the hard gate that CI enforces:
# it must stay green (build + vet + tests) before anything merges.

export GOTOOLCHAIN := local
export GOFLAGS := -mod=mod

.PHONY: check build vet test test-unit test-integration test-live race lint clean facts eval goal-all-verify science-check science-fmt science-vet science-test-quick science-test-all science-full-verify

science-fmt:
	@test -z "$$(gofmt -l internal/science)" || (gofmt -l internal/science && exit 1)

science-vet:
	go vet ./internal/science/...

science-test-quick:
	bash scripts/test-science-quick.sh

science-test-all:
	bash scripts/test-science-all.sh

science-full-verify:
	bash scripts/science/full-verify.sh

science-lab-stress:
	bash scripts/science/lab-stress.sh

goal-all-verify:
	bash scripts/goal-all-verify.sh

science-check: science-fmt science-vet science-test-quick
	@echo "✓ science-check passed"

## check: the merge gate — compile, vet, and run all tests. Fail = red.
## (The eval harness's loader/scorer/aggregator are covered here in internal/eval;
## the live, model-driven `make eval` run is separate — it needs a model + key.)
check: build vet test
	@echo "✓ check passed"

## eval: coding-quality benchmark — run the eval tasks end-to-end through the
## configured model and print pass-rate / median steps / cost. Needs a provider
## key (e.g. DEEPSEEK_API_KEY) or a local model. `lumen eval --list` needs neither.
eval: bin
	./bin/lumen eval

## facts: print the real, script-generated repo counts. Docs cite this, not
## hand-typed numbers that drift (and have drifted) from reality.
facts:
	@echo "non-test Go LOC : $$(find . -name '*.go' ! -name '*_test.go' | xargs wc -l | tail -1 | awk '{print $$1}')"
	@echo "test files      : $$(find . -name '*_test.go' | wc -l | tr -d ' ')"
	@echo "Go packages     : $$(go list ./... | wc -l | tr -d ' ')"
	@echo "builtin tools   : $$(grep -rhoE 'RegisterBuiltin\(' internal | wc -l | tr -d ' ')"
	@echo "model presets   : $$(grep -c 'Provider:' internal/config/model_presets.go | tr -d ' ')"

## build: compile everything (catches the C-1 class of breakage).
build:
	go build ./...

## vet: static analysis.
vet:
	go vet ./...

## test-unit: deterministic default suite; no real network services.
test: test-unit

test-unit:
	go test ./...

## test-integration: controlled native MCP subprocesses backed by httptest.
test-integration:
	go test -tags=integration -p=2 -count=1 -timeout=180s ./internal/science/native ./internal/science/lab/runtime

## test-live: explicit real-service smoke probes; never part of the PR gate.
test-live:
	go test -tags=live -count=1 -timeout=120s ./internal/science/native ./internal/science/lab/runtime ./internal/provider/anthro ./internal/provider/gemini -run 'Live'

## race: run the full suite under the race detector (this is a concurrent agent).
race:
	go test -race ./...

## clean: remove build artifacts.
clean:
	rm -rf bin/

## bin: build the lumen binary into bin/.
bin:
	go build -o bin/lumen ./cmd/lumen
