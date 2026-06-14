---
name: finishing-a-development-branch
description: When implementation is complete — integrate the work properly.
---
# Finishing a Development Branch
When implementation is complete and all tests pass:

1. **Review the diff**: Run `git diff` against the target branch. Look for leftover debug code.
2. **Update docs**: Update README, CHANGELOG, or relevant documentation.
3. **Lint**: Run the project's linter and fix all warnings.
4. **Test one more time**: Run the full test suite.
5. **Commit with a clear message**: Conventional commits format.
6. **Push**: Push to the remote branch.
7. **Create PR**: If the project uses PRs, prepare a clear description.

Options for integration:
- **Merge**: For feature branches into main.
- **Rebase**: For keeping a clean linear history.
- **Squash**: For condensing a messy branch into one commit.
