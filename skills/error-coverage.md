---
name: error-coverage
description: Check every sentinel error is mapped to HTTP status in handlers.
runAs: subagent
allowed-tools: read_file, grep, glob, ls, lsp_references
---
# Error Coverage Check
Cross-check error handling completeness:

1. **List all sentinel errors**: grep for `var ErrXxx = errors.New` / `fmt.Errorf`.
2. **List all handler error returns**: Check each handler/service return path.
3. **Map errors to status**: Every domain error should map to an HTTP status code.
4. **Find unmapped errors**: Errors that are returned but never explicitly mapped → 500.
5. **Find unhandled errors**: Errors that are defined but never used.

Report gaps with file:line. Flag any 500 that should be a specific error.
