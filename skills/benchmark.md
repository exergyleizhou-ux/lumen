---
name: benchmark
description: Performance regression detection — run benchmarks, compare results.
runAs: subagent
allowed-tools: bash, read_file, grep
---
# Benchmarking
Performance measurement workflow:

1. **Baseline first**: Run benchmarks on the base branch/commit.
2. **Compare**: Run the same benchmarks on the current branch.
3. **Flag regressions**: Report any benchmark that is >5% slower.
4. **Memory**: Check allocations (B/op) and memory (allocs/op).
5. **Profile when suspicious**: Use pprof for hot spots.

Report: benchmark name, baseline ns/op, current ns/op, delta %, B/op, allocs/op.
