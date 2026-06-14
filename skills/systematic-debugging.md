---
name: systematic-debugging
description: Systematic debugging methodology — isolate, reproduce, fix, verify.
---
# Systematic Debugging
Follow this process for any bug:

1. **Reproduce**: Get a reliable reproduction. If you can't reproduce it, you can't fix it.
2. **Isolate**: Narrow the scope. Binary search through commits (git bisect) or code changes.
3. **Hypothesize**: Form a theory about the root cause before changing code.
4. **Test the hypothesis**: Add logging, assertions, or a minimal test case.
5. **Fix**: Make the minimal change that addresses the root cause.
6. **Verify**: Confirm the reproduction no longer triggers the bug.
7. **Regression test**: Add a test that catches the bug.
8. **Root cause analysis**: Document what went wrong and why.

Never fix symptoms. Always find and fix the root cause.
