---
name: test
description: Write tests that assert behavior, not rendering.
---
# Test-Driven Development
Write tests that assert behavior, not implementation details.

1. **Test the contract**: Verify the function's documented behavior.
2. **Edge cases first**: Empty input, nil, max values, boundary conditions.
3. **Error paths**: Test what happens when things go wrong.
4. **Table-driven**: Use table tests for multiple input/output pairs.
5. **Mocks sparingly**: Prefer real implementations over mocks.

Patterns to follow:
- Function-call verification: assert the right functions are called with right args.
- State transition testing: verify before→after state changes.
- Error propagation: ensure errors bubble up correctly.
