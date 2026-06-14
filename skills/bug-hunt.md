---
name: bug-hunt
description: Systematic 7-phase bug hunt across the codebase.
runAs: subagent
allowed-tools: read_file, grep, glob, ls, lsp_definition, lsp_references, lsp_diagnostics
---
# Bug Hunt
Systematic 7-phase bug detection:

1. **SQL completeness**: Missing WHERE clauses, N+1 queries, missing indexes.
2. **Ignored errors**: Discarded error returns, empty catch blocks.
3. **Always-false stubs**: TODO placeholders, return nil without implementation.
4. **TOCTOU races**: Check-then-act patterns on shared state.
5. **Type bypass**: Unsafe casts, any/types without validation.
6. **Null guards**: Missing nil checks on external inputs.
7. **Dead code**: Unreachable branches, unused functions.

Report findings with severity: critical / high / medium / low. Include file:line.
