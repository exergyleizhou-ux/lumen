---
name: error-handling
description: Robust error handling patterns — typed errors, boundaries, retries.
---
# Error Handling
Error handling patterns:

1. **Typed errors**: Use sentinel errors (var ErrNotFound = ...) or custom error types.
2. **Error wrapping**: Use `fmt.Errorf("context: %w", err)` to preserve the chain.
3. **Error boundaries**: Catch errors at module/service boundaries, convert to domain errors.
4. **Retry logic**: Exponential backoff with jitter for transient failures.
5. **Circuit breaker**: Stop calling a failing service after N failures.
6. **User-facing errors**: Log the technical detail, show a safe message to the user.
7. **Never silently swallow**: Always log or propagate errors.
