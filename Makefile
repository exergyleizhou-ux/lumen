# Lumen build/quality gate. `make check` is the hard gate that CI enforces:
# it must stay green (build + vet + tests) before anything merges.

export GOTOOLCHAIN := local
export GOFLAGS := -mod=mod

.PHONY: check build vet test race lint clean

## check: the merge gate — compile, vet, and run all tests. Fail = red.
check: build vet test
	@echo "✓ check passed"

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
