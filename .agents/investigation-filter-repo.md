# Investigation: git-filter-repo hash-preservation failure

## Status

**Finding confirmed. Architecture is broken. Replacement implementation planned.**

---

## What we assumed

The current implementation delegates rewriting to `git-filter-repo` with a Python commit callback. The callback uses a `_rewrite_set` (a Python `frozenset`-style bytes set) to identify in-range commits. For commits **outside** the rewrite set the callback is a no-op (`pass`):

```python
_rewrite_set = {b'hash1', b'hash2', ...}
_r = re.compile(b'<path-regex>')
if commit.original_id in _rewrite_set:
    commit.file_changes = [fc for fc in commit.file_changes if not _r.search(fc.filename)]
```

The assumption was: *commits the callback leaves untouched pass through git-filter-repo's fast-export/fast-import pipeline with their hashes preserved*, because a commit's hash is determined by tree + parent + metadata, and none of those change if the callback is a no-op.

## What we observed

When the tool was run against a real repository with a complex history (hundreds of commits, multiple root commits, merge commits), **almost all commits in the history received new hashes** — including commits that were entirely before the rewrite range.

The symptom: a commit that was reachable from HEAD before the tool ran (`git merge-base --is-ancestor <hash> HEAD` returned true) was no longer reachable afterwards.

## Controlled reproduction

To isolate git-filter-repo's behaviour we ran a **no-op callback** directly:

```bash
git filter-repo --commit-callback "pass" --refs refs/heads/<branch> --force
```

This callback does nothing — it does not touch any commit. Results:

| Property | Before | After |
|---|---|---|
| Total commits processed | — | 829 (parsed by filter-repo) |
| Root commits reachable | 3 | 3 ✓ |
| Pre-range commit reachable | yes | **no ✗** |
| Approximate hash change rate | — | ~98% of commits |

The root commits themselves were preserved (their objects were unmodified). But intermediate commits — including commits that were entirely before the intended rewrite range — received new hashes. This triggered a cascade: because a commit's hash includes its parent's hash, every descendant of any rehashed commit also received a new hash.

## Root cause

`git filter-repo`'s fast-export/fast-import round-trip is **not hash-preserving** for the commits it processes, even when the callback is a no-op.

git-filter-repo's own documentation and design acknowledge this: it is a history-**rewriting** tool, not a history-**reading** tool. It does not claim to preserve hashes for unmodified commits. Our implementation incorrectly relied on an undocumented and false property.

The exact trigger for which intermediate commits get rehashed is not fully understood. Root commits (no parents) appear to survive the round-trip. Commits with complex metadata or certain merge topologies do not. Because parent hash is part of the commit object, any rehashed commit causes a cascade through all descendants.

## Why tests pass on toy repositories

The integration tests use small linear repositories (typically 5–15 commits, no complex merge history, all authored by the same synthetic identity). These happen to survive git-filter-repo's round-trip with hashes intact, so the tests that assert hash preservation (`assertHashReachable`) pass.

The real-world failure mode only appears in repositories with a topology or commit metadata that causes the round-trip to produce different commit objects. Our test suite gives false confidence.

## Implications

The architecture is fundamentally broken:

- Passing `--refs refs/heads/<branch>` causes git-filter-repo to process **all commits reachable from that branch**, not just the ones in the rewrite range.
- Even with a no-op callback, processed commits can receive new hashes.
- A `_rewrite_set` guard in the callback cannot fix this: the hashing happens in git's fast-import layer, not in the Python callback.

There is no known git-filter-repo flag that guarantees hash preservation for unmodified commits. The tool was built for the use-case of rewriting **all** history, not a range.

## Planned fix

Replace the git-filter-repo delegation with a custom in-process rewriter that:

1. Walks only the commits in the rewrite range (oldest to newest).
2. For each commit, reconstructs a new tree by starting from the rewritten parent's tree and applying only the non-filtered file changes from the original commit.
3. Creates a new commit object (`git commit-tree`) with the reconstructed tree and the original commit's author name, author email, author date, committer name, committer email, committer date, and message.
4. Leaves all commits **outside** the range completely untouched (their objects are never rewritten; only the branch ref is updated to point to the new tip).

This approach does not require git-filter-repo and guarantees that out-of-range commits are never processed or rehashed.

See `plan.md` for the full implementation plan.
