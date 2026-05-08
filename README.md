# Git Remove Path History

A tool for removing all changes to a given path within a range of git commits. It rewrites the commit history so that the specified path is left in the state it had just before the range begins.

For example, if you accidentally committed sensitive files or build artifacts, this tool can erase them from a specific range of commits without affecting commits outside that range.

## Installation

**Prerequisites:** Go 1.22+ and `git-filter-repo` installed.

Build the tool:
```bash
go build -o git-remove-path-history .
```

Or install directly to your `$GOPATH/bin` (so it's available on `PATH`):
```bash
go install .
```

## Usage

Run without arguments to see help:
```bash
./git-remove-path-history
```

Basic usage:
```bash
git-remove-path-history <path> [<git-ref>]
```

**Examples:**

```bash
# Strip all changes to plans/ from HEAD only
git-remove-path-history plans

# Strip all changes to secret.txt from commit abc123 to HEAD
git-remove-path-history secret.txt abc123

# Strip all *.lock files from the last 5 commits
# (find the commit hash 5 commits ago, then specify it)
git-remove-path-history '*.lock' <hash-from-5-commits-ago>
```

## Warning

This tool **rewrites git history**. If the branch has been pushed to a remote, a force-push is required:
```bash
git push --force-with-lease
```
