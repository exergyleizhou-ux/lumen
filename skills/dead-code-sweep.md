---
name: dead-code-sweep
description: Find and remove unused code — functions, types, interfaces, imports.
runAs: subagent
allowed-tools: read_file, grep, glob, lsp_definition, lsp_references
---
# Dead Code Sweep
Find and report dead code:

1. **Unused exports**: grep for exported symbols, check lsp_references count.
2. **Always-false stubs**: Functions that always return nil/empty/default.
3. **Superseded methods**: Methods that duplicate newer implementations.
4. **Unused imports**: Check for imports without references.
5. **Orphan interfaces**: Interfaces with no consumers.

Report each finding with file:line and a recommended action (delete/simplify/verify).
Do NOT delete code — only report. The parent agent decides what to remove.
