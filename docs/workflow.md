# Daily workflow

1. `stack start <name>` to begin a branch on top of the current one.
2. Commit your work as usual.
3. `stack sync` after the parent changes to cascade-rebase children.
4. `stack submit` to push and open/refresh the stacked PRs.
