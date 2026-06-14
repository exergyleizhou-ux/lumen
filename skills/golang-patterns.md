---
name: golang-patterns
description: Idiomatic Go patterns — interfaces, concurrency, error handling.
---
# Go Patterns
Idiomatic Go development patterns:

1. **Interfaces**: Define interfaces where they're used (consumer side), not where implemented.
2. **Concurrency**: Use sync.WaitGroup, errgroup, or channels. Avoid sharing memory.
3. **Context**: Pass context.Context as the first parameter; use for cancellation and deadlines.
4. **Error handling**: Always check errors; wrap with context using `fmt.Errorf("...: %w", err)`.
5. **Package naming**: Single word, lowercase, no underscores. Package name is the last path element.
6. **Zero-value useful**: Make zero values of structs meaningful.
7. **Table-driven tests**: Prefer table tests over multiple TestXxx functions.
8. **Avoid init()**: Except for side-effect registrations (database drivers, tool builtins).
