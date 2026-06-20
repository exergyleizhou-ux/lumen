# Lumen build/quality gate. `make check` is the hard gate that CI enforces:
# it must stay green (build + vet + tests) before anything merges.

export GOTOOLCHAIN := local
export GOFLAGS := -mod=mod

.PHONY: check build vet test race lint clean facts

## check: the merge gate — compile, vet, and run all tests. Fail = red.
check: build vet test
	@echo "✓ check passed"

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

## test: run the full test suite.
test:
	go test ./...

## race: run the full suite under the race detector (this is a concurrent agent).
race:
	go test -race ./...

## clean: remove build artifacts.
clean:
	rm -rf bin/

## bin: build the lumen binary into bin/.
bin:
	go build -o bin/lumen ./cmd/lumen
