#!/usr/bin/env bash
#
# git-remove-path-history.sh
#
# Rewrites a range of git commits to strip all changes to a given path,
# leaving those files at the state they had just before the range starts.
#
# Requires: git-filter-repo  (https://github.com/newren/git-filter-repo)

set -euo pipefail

SCRIPT_NAME="$(basename "$0")"

# ---------------------------------------------------------------------------
# Usage
# ---------------------------------------------------------------------------
usage() {
    cat >&2 << EOF
Usage: $SCRIPT_NAME <path> [<git-ref>]

  <path>
      Path (relative to the repository root) whose changes should be removed
      from the selected commits.  Can be:
        - A single file     e.g.  src/config.json
        - A directory       e.g.  src/generated
        - A glob pattern    e.g.  '*.lock'  (uses Python fnmatch; * and ?
          are supported but ** is NOT — use a directory path instead)

      The path should be relative to the repository root, regardless of the
      current working directory when running this script.

  <git-ref>  (optional)
      The git reference (commit hash, tag, branch name, …) that marks the
      start of the rewrite range.  The range is open at the bottom:
          <git-ref>  (exclusive — NOT rewritten)
          ...
          HEAD       (inclusive — rewritten)
      The reference must be an ancestor of HEAD (or the commit you are
      currently on in detached-HEAD mode).
      If omitted, only the current HEAD commit is rewritten.

Effect per commit in the range:
  - If the commit added a file matching <path>:     the file is not added
                                                    (as if it never existed)
  - If the commit modified a file matching <path>:  the file keeps its state
                                                    from the parent commit
  - If the commit deleted a file matching <path>:   the deletion is undone
                                                    (file restored from parent)

WARNING: Rewrites history.  A force-push is required if the branch has
         already been pushed to a remote.

EOF
    exit 1
}

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
die() { echo "Error: $*" >&2; exit 1; }

# ---------------------------------------------------------------------------
# Parse arguments
# ---------------------------------------------------------------------------
[[ $# -ge 1 && $# -le 2 ]] || usage

FILTER_PATH="$1"
BASE_REF="${2:-}"

[[ -n "$FILTER_PATH" ]] || die "Path argument must not be empty."

# ---------------------------------------------------------------------------
# Pre-flight checks
# ---------------------------------------------------------------------------

# Must be inside a git repo
git rev-parse --git-dir > /dev/null 2>&1 \
    || die "Not inside a git repository."

# git-filter-repo must be installed
git filter-repo --version > /dev/null 2>&1 \
    || die "git-filter-repo is not installed. See: https://github.com/newren/git-filter-repo"

# Working tree and index must be clean
git diff --quiet 2>/dev/null \
    || die "Unstaged changes detected. Commit or stash them first."
git diff --cached --quiet 2>/dev/null \
    || die "Staged changes detected. Commit or stash them first."

# ---------------------------------------------------------------------------
# Resolve commit range
# ---------------------------------------------------------------------------

# git rev-parse HEAD works both on a branch and in detached-HEAD mode.
CURRENT_HEAD="$(git rev-parse HEAD)"

if [[ -n "$BASE_REF" ]]; then
    # Resolve to a concrete commit hash (dereferences tags, branches, etc.)
    BASE_HASH="$(git rev-parse "${BASE_REF}^{commit}" 2>/dev/null)" \
        || die "Cannot resolve '$BASE_REF' to a commit."

    # Must be a strict ancestor of HEAD
    git merge-base --is-ancestor "$BASE_HASH" "$CURRENT_HEAD" \
        || die "'$BASE_REF' ($BASE_HASH) is not an ancestor of HEAD ($CURRENT_HEAD)."

    [[ "$BASE_HASH" != "$CURRENT_HEAD" ]] \
        || die "'$BASE_REF' resolves to the current HEAD — nothing to rewrite."

    REF_RANGE="${BASE_HASH}..${CURRENT_HEAD}"
    COMMIT_COUNT="$(git rev-list --count "${BASE_HASH}..${CURRENT_HEAD}")"
else
    # No base supplied: restrict to HEAD only
    PARENT_HASH="$(git rev-parse "${CURRENT_HEAD}^" 2>/dev/null || true)"
    if [[ -n "$PARENT_HASH" ]]; then
        REF_RANGE="${PARENT_HASH}..${CURRENT_HEAD}"
    else
        # Root commit: there is no parent
        REF_RANGE="$CURRENT_HEAD"
    fi
    COMMIT_COUNT=1
fi

# ---------------------------------------------------------------------------
# Confirmation prompt
# ---------------------------------------------------------------------------

echo "Repository : $(git rev-parse --show-toplevel)"
echo "Path filter: $FILTER_PATH"
echo "Range      : $REF_RANGE  ($COMMIT_COUNT commit(s))"
echo ""
echo "WARNING: Git history will be permanently rewritten."
read -r -p "Proceed? [y/N] " _CONFIRM
[[ "$_CONFIRM" =~ ^[Yy]$ ]] || { echo "Aborted."; exit 0; }
echo ""

# ---------------------------------------------------------------------------
# git-filter-repo commit callback
#
# For each commit in the range, remove every FileChange whose source or
# destination path matches FILTER_PATH.  Removing a FileChange means the
# file retains the state it had in the (already-rewritten) parent commit —
# the change is simply treated as if it never happened.
# ---------------------------------------------------------------------------

CALLBACK="$(cat << 'PYEOF'
import os as _os, fnmatch as _fnm

# Normalise the filter path (strip any accidental leading slash)
_fp = _os.environ['GIT_FILTER_PATH'].lstrip('/')

def _matches(raw):
    """Return True if raw filename (bytes or str) matches the filter path."""
    if not raw:
        return False
    fn = raw.decode('utf-8', errors='replace') if isinstance(raw, bytes) else raw
    fn = fn.lstrip('/')
    # Exact file match or directory-prefix match
    if fn == _fp or fn.startswith(_fp.rstrip('/') + '/'):
        return True
    # Simple glob (*, ?) via fnmatch
    return _fnm.fnmatch(fn, _fp)

# Strip matching changes.  For renames (type R/C), check both source
# (fc.filename) and destination (fc.new_filename) so that renames into or
# out of the target path are also removed.
commit.file_changes = [
    fc for fc in commit.file_changes
    if not _matches(fc.filename)
    and not _matches(getattr(fc, 'new_filename', None))
]
PYEOF
)"

# ---------------------------------------------------------------------------
# Run git-filter-repo
#
#   --commit-callback  Python snippet to strip matching file changes
#   --refs             Limit rewriting to the computed range
#   --partial          Allow partial rewrites (not all commits are touched)
#   --force            Skip the "origin remote" safety check
# ---------------------------------------------------------------------------

GIT_FILTER_PATH="$FILTER_PATH" git filter-repo \
    --commit-callback "$CALLBACK" \
    --refs "$REF_RANGE" \
    --partial \
    --force

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------

echo "Done. $COMMIT_COUNT commit(s) rewritten."
echo ""
echo "If this branch has been pushed to a remote, force-push with:"
echo "  git push --force-with-lease"
