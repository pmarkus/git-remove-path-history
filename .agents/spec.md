# git-remove-path-history — Specification

## Purpose of this file

This file describes the **current implemented behaviour** of `git-remove-path-history`. It is the source of truth for what the tool does today.

An AI agent must read this file before making changes to understand what exists, and must keep this file in sync with the implementation as work progresses. See [`AGENTS.md`](../AGENTS.md) for project overview and working conventions, and [`plan.md`](plan.md) for planned future work.

---

## Purpose

Rewrites a range of git commits to strip all changes to a given path, leaving those files at the state they had just before the range begins. Uses git plumbing commands (`read-tree`, `update-index`, `write-tree`, `commit-tree`) to rewrite only the commits in the range while leaving commits outside the range completely untouched with their hashes preserved.

## Implementation language

The tool is implemented in Go. Entry point: `main.go`. Rewriter logic: `rewriter.go`. Path matching logic: `match.go`.

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
2. The working tree has no unstaged changes.
3. The index has no staged changes.
4. HEAD is on a named branch (detached HEAD is not supported).

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

- The core logic lives in `run(args []string, stdin io.Reader, dir string) error` in `main.go` and the `Rewriter` type in `rewriter.go`. Accepting `stdin` and `dir` as parameters makes the function directly testable without subprocess overhead.
- The rewriter walks each commit in the range (oldest to newest), using the following algorithm for each commit:
  1. Load the rewritten parent's tree into a temporary git index using `git read-tree`.
  2. Get the list of file changes in the original commit via `git diff-tree -r --name-status`.
  3. For each non-filtered file change, update the index using `git update-index` (additions/modifications use `--cacheinfo`, deletions use `--remove`).
  4. Write a new tree from the modified index using `git write-tree`.
  5. Create a new commit using `git commit-tree` with the original commit's author name, email, date, committer name, email, date, and message (preserving all metadata).
  6. If the rewritten commit has no kept changes (all changes were filtered), the commit is pruned (no new commit object is created).
- All commits outside the range are **never processed**. Their objects remain untouched in the git database and their hashes are guaranteed to remain the same.
- A temporary index file (`GIT_INDEX_FILE`) is used for each rewrite to avoid disturbing the working tree's real index.
- The branch ref is updated to point to the new HEAD after rewriting using `git update-ref`.
- The working tree and index are then reset using `git reset --hard` to match the new HEAD.
- `match.go` contains the path-matching logic (`matchesPath`) and the regex-building logic (`pathToRegex`) used by the rewriter's filtering callback. Tests use these functions directly to verify matching behavior.

## Test suite

Unit tests (`match_test.go`) verify `matchesPath` for exact paths, directory prefixes, globs, leading-slash normalisation, and empty paths.

Integration tests (`integration_test.go`) call `run()` directly against temporary git repositories. They cover:
- Pre-flight checks (not a repo, empty path, unstaged/staged changes, detached HEAD, bad/non-ancestor/HEAD base ref)
- Confirmation abort
- Stripping a directory, a single file, and glob-matched files
- Commits before the range keeping their original hashes (validated by exact hash reachability checks, not only diff shape)
- Commits in the range getting new hashes
- HEAD-only mode (no base ref)
- Root commit (no parent)
- Stripping a deleted file and a modified file
- Multiple commits in range
- Non-matching path (no-op, with unchanged hash assertion)
- Empty commits after strip being dropped (`--prune-empty auto`)
- Commits on an unrelated branch being untouched
- Complex branching history with merge commits, empty merge dropping, and multi-file commits
- Hash-preservation with a merge commit at the lower bound
- Black-box CLI run (compiled binary) preserving hashes at/before lower bound
- Commit metadata preservation: author name, author email, author date, committer name, committer email, committer date, and commit message are all preserved verbatim on rewritten commits (`TestCommitMetadataPreservedAfterRewrite`)

## Output

On success:
```
Done. <N> commit(s) rewritten.

If this branch has been pushed to a remote, force-push with:
  git push --force-with-lease
```
