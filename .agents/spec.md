# git-remove-path-history — Specification

## Purpose of this file

This file describes the **current implemented behaviour** of `git-remove-path-history`. It is the source of truth for what the tool does today.

An AI agent must read this file before making changes to understand what exists, and must keep this file in sync with the implementation as work progresses. See [`AGENTS.md`](../AGENTS.md) for project overview and working conventions, and [`plan.md`](plan.md) for planned future work.

---

## Purpose

Rewrites a range of git commits to strip all changes to a given path, leaving those files at the state they had just before the range begins. Delegates the actual rewriting to `git-filter-repo`.

## Implementation language

The tool is implemented in Go. Entry point: `main.go`. Path matching logic: `match.go`.

## Invocation

```
git-remove-path-history <path> [<git-ref>]
```

## Arguments

### `<path>` (required)
Path relative to the repository root whose changes should be stripped. Can be:
- A single file — e.g. `src/config.json`
- A directory — e.g. `src/generated`
- A glob pattern — e.g. `*.lock` (uses Python `fnmatch`; `*` and `?` are supported, `**` is not)

### `<range>` (optional)
Specifies which commits to rewrite. Supports the following forms:

| Form | Lower bound (exclusive) | Upper bound (inclusive) |
|---|---|---|
| *(omitted)* | `HEAD^` | `HEAD` |
| `<ref>` | `<ref>` | `HEAD` |
| `<ref>..` | `<ref>` | `HEAD` |
| `<ref1>..<ref2>` | `<ref1>` | `<ref2>` |

Rules:
- `<ref>` and `<ref>..` are treated identically.
- Each ref can be a commit hash, tag, or branch name.
- The lower bound is always **exclusive** (the referenced commit itself is not rewritten).
- The upper bound is always **inclusive**.
- Both bounds must be ancestors of HEAD (or equal to HEAD) so that the current branch ref can be used for `--refs` to git-filter-repo.
- If omitted, only the current HEAD commit is rewritten (HEAD-only mode).

## Pre-flight checks

Before doing any work the tool verifies:
1. The current directory is inside a git repository.
2. `git-filter-repo` is installed.
3. The working tree has no unstaged changes.
4. The index has no staged changes.
5. HEAD is on a named branch (detached HEAD is not supported).

## Effect per commit

For each commit in the range:

| What the commit did to `<path>` | Result after rewrite |
|---|---|
| Added a file | File is not added (as if it never existed) |
| Modified a file | File keeps the state it had in the parent commit |
| Deleted a file | Deletion is undone (file restored from parent) |

Commits outside the range are left completely untouched, including their hashes.

## Confirmation prompt

Before rewriting, the tool prints a summary (repository root, path filter, commit range, commit count) and asks the user to confirm. Answering anything other than `y`/`Y` aborts without modifying history.

## Implementation notes

- The core logic lives in `run(args []string, stdin io.Reader, dir string) error` in `main.go`. Accepting `stdin` and `dir` as parameters makes the function directly testable without subprocess overhead.
- `git-filter-repo` is invoked with `--refs refs/heads/<branch>` (the current branch) so that the branch pointer is updated after rewriting.
- **No `^BASE` ref is passed to `--refs`.** When git-filter-repo receives a negative ref (`^hash`) alongside a positive ref, the positive ref is silently dropped from the internal `git fast-export` command, causing the callback to never be invoked. Instead, range-limiting is done inside the callback via a `_rewrite_set`: `git rev-list BASE..HEAD` is called in Go before invoking git-filter-repo, and the resulting commit hashes are embedded in the callback as a Python `frozenset`-style set literal. The callback is a no-op for commits whose `original_id` is not in the set. Commits outside the range pass through fast-export/import unmodified, preserving their hashes (same tree + same parent + same metadata = same hash).
- The Python callback is built in `run()` using `fmt.Sprintf` and passed to `git-filter-repo` via `--commit-callback`. It uses `commit.original_id` (bytes) for the range check and `fc.filename` (also bytes) for path matching. The regex is compiled as a Python bytes pattern (`b"..."`) using the string produced by `pathToRegex()`.
- `match.go` contains a Go implementation of the same path-matching logic (`matchesPath`) and the regex-building logic (`pathToRegex`). It mirrors the Python callback and is used by unit tests. The two implementations must be kept in sync.
- `--prune-empty` is left at its default (`auto`): commits that become empty after filtering are dropped. This means test scenarios should not create commits whose *only* changes are to the filtered path unless the test is specifically verifying that the empty commit is dropped. In particular, the root commit and HEAD-only-mode tests must include at least one non-filtered file change so the rewritten commit is not pruned.
- All git subprocess calls use `HEAD` rather than the resolved HEAD hash to avoid triggering the "refname is ambiguous" advisory in repositories that happen to have a ref named after a commit hash. The resolved hash (`currentHead`) is retained for equality comparisons and display only.
- `advice.objectNameWarning=false` is passed to every git invocation as a safety net via the `git` closure inside `run()`.

## Test suite

Unit tests (`match_test.go`) verify `matchesPath` for exact paths, directory prefixes, globs, leading-slash normalisation, and empty paths.

Integration tests (`integration_test.go`) call `run()` directly against temporary git repositories. They cover:
- Pre-flight checks (not a repo, empty path, unstaged/staged changes, detached HEAD, bad/non-ancestor/HEAD base ref)
- Confirmation abort
- Stripping a directory, a single file, and glob-matched files
- Commits before the range keeping their original hashes
- Commits in the range getting new hashes
- HEAD-only mode (no base ref)
- Root commit (no parent)
- Stripping a deleted file and a modified file
- Multiple commits in range
- Non-matching path (no-op)
- Empty commits after strip being dropped (`--prune-empty auto`)
- Commits on an unrelated branch being untouched
- Complex branching history with merge commits, empty merge dropping, and multi-file commits

## Output

On success:
```
Done. <N> commit(s) rewritten.

If this branch has been pushed to a remote, force-push with:
  git push --force-with-lease
```
