# stack

A client-side-only CLI for managing **stacked pull requests** — like Graphite,
but with no backend server. The branch DAG is synced across teammates statelessly
by embedding it as a hidden HTML comment in each PR description.

## How it works

- **DAG model** — each branch records its parent and the parent's commit at last
  sync (`ParentCommit`), which is exactly the `--onto` old-base for rebasing.
- **Rebase engine** — go-git is used for read-only inspection (refs, merge-base);
  the mutating rebase/cherry-pick steps shell out to the real `git` binary, since
  go-git has no rebase and an incomplete content-level merge.
- **State sync** — `submit` embeds the stack JSON in each PR's hidden comment;
  `adopt` reconstructs a teammate's stack from it. No external database.
- **Crash safety** — every rebase is guarded by a write-ahead journal and
  reconciled against git's ground truth on the next command (handles conflicts,
  aborts, and crashes).

## Commands

| Command | Purpose |
| --- | --- |
| `stack start <name>` | Branch off the current branch and track it |
| `stack sync` | Cascade-rebase every branch onto its updated parent |
| `stack submit` | Push the stack, open/refresh PRs, publish state into each PR |
| `stack adopt <pr#>` | Reconstruct a teammate's stack from a PR |
| `stack reparent <branch> --onto <parent>` | Move a branch to a new parent |

Branding (binary name, command verbs, comment markers) is centralized in
`internal/branding` — rebrand by editing that one file.

## Build

```sh
go build -o stack .
go test ./...
```
