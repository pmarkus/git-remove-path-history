# git-remove-path-history — Planned Work

## Purpose of this file

This file tracks future work that has been discussed and approved but not yet implemented. See [`spec.md`](spec.md) for current implemented behaviour and [`AGENTS.md`](../AGENTS.md) for project overview and conventions.

When a planned item is implemented, move its description to `spec.md` and remove it from here.

---

## 1. Redesign argument interface to support range syntax

**Motivation:** The current second argument is always treated as a bare lower-bound ref, with the upper bound hardcoded to HEAD. This is inconsistent with how users familiar with git naturally express ranges (e.g. `git log`, `git rev-list`).

**Planned behaviour:** Replace the existing `[<git-ref>]` argument with a `[<range>]` argument that supports the following forms:

| Form | Lower bound (exclusive) | Upper bound (inclusive) |
|---|---|---|
| *(omitted)* | `HEAD^` (parent of HEAD) | `HEAD` |
| `<ref>` | `<ref>` | `HEAD` |
| `<ref>..` | `<ref>` | `HEAD` |
| `<ref1>..<ref2>` | `<ref1>` | `<ref2>` |

Rules:
- `<ref>` and `<ref>..` are treated identically.
- The lower bound is always **exclusive** (the referenced commit itself is not rewritten).
- The upper bound is always **inclusive**.
- All refs are resolved to commit hashes before use.
- The lower bound must be a strict ancestor of the upper bound.
- The upper bound must be an ancestor of HEAD (or equal to HEAD) so that the branch ref can still be used for `--refs`.

## 2. Verified completion summary

**Motivation:** The tool currently reports "Done. N commit(s) rewritten." based solely on the count of commits passed to `git-filter-repo`, without verifying that the rewrites actually produced the expected result. If a bug causes no commits to be rewritten, the summary is misleading.

**Planned behaviour:** After `git-filter-repo` completes, the tool must verify that the path changes are no longer present in the rewritten commits before printing the success message. The reported count must reflect only commits that were genuinely rewritten.

