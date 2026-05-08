# git-remove-path-history — Planned Work

## Purpose of this file

This file tracks future work that has been discussed and approved but not yet implemented. See [`spec.md`](spec.md) for current implemented behaviour and [`AGENTS.md`](../AGENTS.md) for project overview and conventions.

When a planned item is implemented, move its description to `spec.md` and remove it from here.

---

## 1. Verified completion summary

**Motivation:** The tool currently reports "Done. N commit(s) rewritten." based solely on the count of commits passed to `git-filter-repo`, without verifying that the rewrites actually produced the expected result. If a bug causes no commits to be rewritten, the summary is misleading.

**Planned behaviour:** After `git-filter-repo` completes, the tool must verify that the path changes are no longer present in the rewritten commits before printing the success message. The reported count must reflect only commits that were genuinely rewritten.

