# git-remove-path-history — Planned Work

## Purpose of this file

This file tracks future work that has been discussed and approved but not yet implemented. See [`spec.md`](spec.md) for current implemented behaviour and [`AGENTS.md`](../AGENTS.md) for project overview and conventions.

When a planned item is implemented, move its description to `spec.md` and remove it from here.

---

## 1. Verified completion summary

**Motivation:** The tool currently reports "Done. N commit(s) rewritten." based solely on the count of commits passed to `git-filter-repo`, without verifying that the rewrites actually produced the expected result. If a bug causes no commits to be rewritten, the summary is misleading.

**Planned behaviour:** After rewriting completes, the tool must verify that the path changes are no longer present in the rewritten commits before printing the success message. The reported count must reflect only commits that were genuinely rewritten.

---

## 2. Replace git-filter-repo with a custom in-process rewriter

**Motivation:** `git-filter-repo` does not guarantee hash preservation for commits it processes, even with a no-op callback. Our implementation relies on this false property to leave out-of-range commits with their original hashes. On real repositories the tool rewrites almost every commit on the branch, including commits before the rewrite range. See [`.agents/investigation-filter-repo.md`](investigation-filter-repo.md) for the full diagnosis.

**Planned approach:** Remove the dependency on `git-filter-repo` and replace it with a pure-Go implementation using standard git plumbing commands:

1. Walk the commits in the rewrite range (oldest to newest) via `git rev-list BASE..UPPER --reverse`.
2. For each commit `C`:
	a. Get changed files via `git diff-tree -r --name-status C^ C` (or `--root` for the root commit).
	b. Partition changes into *filtered* (match path pattern) and *kept* (do not match).
	c. Start from the rewritten parent's tree using `git read-tree` against a temporary `GIT_INDEX_FILE`.
	d. Apply *kept* changes to the temporary index: additions/modifications via `git update-index --add --cacheinfo`, deletions via `git update-index --remove`.
	e. Write a new tree: `git write-tree` (with `GIT_INDEX_FILE`).
	f. Create a new commit: `git commit-tree <tree> -p <rewritten-parent> -m <message>` with `GIT_AUTHOR_*` / `GIT_COMMITTER_*` env vars from the original commit's metadata.
3. After all range commits are rewritten, update the branch ref: `git update-ref refs/heads/<branch> <final-hash>`.
4. Sync the working tree and index: `git reset --hard <final-hash>`.

Commits outside the range are **never processed**. Their objects are untouched and their hashes are guaranteed to remain the same.

The `git-filter-repo` pre-flight check is removed. The binary has no runtime dependency beyond `git` itself.

**Edge cases to handle:**
- Root commit (no parent): use `git diff-tree --root C` and start from an empty tree.
- Merge commits (multiple parents): diff against the first parent; the new commit-tree uses the rewritten first parent plus unchanged additional parents.
- Empty commits after filtering: drop the commit (advance the rewritten-parent pointer without calling `git commit-tree`), matching the current `--prune-empty auto` behaviour.
- Unchanged commits (no filtered changes and parent unchanged): keep the original commit as-is, preserving its hash.

**Tests required (TDD — write before implementing):**
- Metadata preservation tests are captured in plan item 3 below and must be written first.
- Hash preservation tests: existing `TestCommitsBeforeRangeUntouched` and friends must continue to pass; `TestFilterRepoPreservesOutOfRangeHashes` must pass (not just pass vacuously on toy repos).
- All existing integration and unit tests must pass.

---

## 3. Require and test commit-metadata preservation

**Motivation:** The rewrite must preserve each in-range commit's author name, author email, author date (timestamp), committer name, committer email, committer date, and full commit message verbatim. `git-filter-repo` already does this, but the requirement is not captured in any test. Tests must be written before implementing item 2 so any regression is caught immediately.

**Planned test:** `TestCommitMetadataPreservedAfterRewrite` — creates in-range commits with distinct author identities and fixed timestamps, runs the tool, then asserts each rewritten commit carries the original author name, author email, author date, committer name, committer email, committer date, and message.

**Test helpers needed:**
- `commitMeta` struct: author name/email/date, committer name/email/date, message.
- `mustGitEnv(t, dir, env, args...)`: like `mustGit` but merges additional environment variables.
- `commitWithMeta(t, dir, message, meta)`: commits whatever is currently staged using the given metadata via `GIT_AUTHOR_*` / `GIT_COMMITTER_*` env vars.
- `getCommitMeta(t, dir, hash)`: reads author/committer/message from a commit using `git log -1 --format=...`.

