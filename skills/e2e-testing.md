---
name: e2e-testing
description: End-to-end testing patterns with Playwright / browser automation.
runAs: subagent
allowed-tools: read_file, grep, bash, glob
---
# E2E Testing
End-to-end testing best practices:

1. **Page Object Model**: Encapsulate selectors and actions in page objects.
2. **User-centric flows**: Test real user journeys, not implementation details.
3. **Data isolation**: Each test creates its own data or runs in isolation.
4. **Wait strategies**: Prefer waiting for visible state over fixed timeouts.
5. **Screenshots on failure**: Capture state when tests fail for debugging.
6. **CI-ready**: Headless mode, parallel execution, retry flaky tests once.
