# Agent Guide — git-remove-path-history

## Project overview

`git-remove-path-history` is a Go command-line tool that rewrites a range of git commits to strip all changes to a given path, leaving those files at the state they had just before the range begins.

## Working in this project

This project uses **spec-driven development**.

- [`.agents/spec.md`](.agents/spec.md) describes the **current implemented behaviour** and is the authoritative source of truth. Code must conform to it.
- [`.agents/plan.md`](.agents/plan.md) describes **planned future work** that has been approved but not yet implemented.

**Before making any changes:**
1. Read `.agents/spec.md` in full.
2. Check `.agents/plan.md` to understand what is in scope for upcoming work.
3. Ensure your implementation conforms to `spec.md`.
4. If the user approves a new requirement, add it to `plan.md` first, implement it, then move the description to `spec.md` and remove it from `plan.md`.

**After making any changes:**
- Update `spec.md` to reflect the new implemented behaviour before considering the task complete.
- Any behaviour, constraint, or implementation detail that was discussed and acted upon must be captured in `spec.md` — not just features, but also correctness constraints (e.g. "commits outside the range must not be rehashed").
- Update `README.md` if invocation syntax, arguments, or user-facing behaviour changed.
- Verify that `usageText` in `main.go` remains consistent with `README.md` examples.
- **Write tests for all new functionality.** Either use TDD (write tests first), or implement then immediately write tests. Do not implement features without tests. Unit tests should cover edge cases; integration tests should cover user-facing workflows and error conditions.
- Verify that all tests (old and new) pass.

## Documentation artifacts

The following files are derived from the implementation and must be kept in sync:

| File | Purpose | When to update |
|---|---|---|
| `README.md` | User-facing usage instructions and examples | When invocation syntax changes, arguments change, or behaviour visible to users changes |
| `main.go` `usageText` constant | CLI help text displayed by `./git-remove-path-history --help` | When README examples or argument descriptions change |
| `.agents/spec.md` | Authoritative technical specification of current behaviour | After every implementation change, before task is complete |

**Definition of "done":** A task is complete only when all the following are true:
1. Implementation conforms to `.agents/spec.md` (or a new spec.md entry has been added)
2. `.agents/spec.md` is updated to reflect the new behaviour
3. `README.md` is updated if user-facing syntax/behaviour changed
4. `usageText` in `main.go` is consistent with README examples
5. Tests are written for all new functionality (TDD preferred; otherwise tests must immediately follow implementation)
6. All tests pass (both old and new)

## Repository layout

| Path | Description |
|---|---|
| `main.go` | CLI entry point and core logic |
| `match.go` | Path matching logic (mirrors the Python callback; used by unit tests) |
| `go.mod` | Go module definition |
| `README.md` | Human-facing usage documentation |
| `AGENTS.md` | This file — agent entry point |
| `.agents/spec.md` | Authoritative description of current implemented behaviour |
| `.agents/plan.md` | Planned future work not yet implemented |


## Constraints

- The tool must compile to a single self-contained binary with no runtime dependencies beyond `git`.
- The tool must remain portable across standard Linux/macOS environments.
- External Go dependencies beyond the standard library must not be introduced without explicit instruction.
