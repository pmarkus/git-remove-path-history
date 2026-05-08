// git-remove-path-history rewrites a range of git commits to strip all
// changes to a given path, leaving those files at the state they had just
// before the range begins.
//
// Requires: git-filter-repo  (https://github.com/newren/git-filter-repo)
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

const usageText = `Usage: git-remove-path-history <path> [<range>]

  <path>
      Path (relative to the repository root) whose changes should be removed
      from the selected commits.  Can be:
        - A single file     e.g.  src/config.json
        - A directory       e.g.  src/generated
        - A glob pattern    e.g.  '*.lock'  (uses Python fnmatch; * and ?
          are supported but ** is NOT — use a directory path instead)

      The path should be relative to the repository root, regardless of the
      current working directory when running this tool.

  <range>  (optional)
      Specifies which commits to rewrite. Supports the following forms:
        - <ref>            Rewrite from <ref> (exclusive) to HEAD (inclusive)
        - <ref>..          Same as <ref> (trailing .. is ignored)
        - <ref1>..<ref2>   Rewrite from <ref1> (exclusive) to <ref2> (inclusive)

      Each <ref> can be a commit hash, tag, or branch name.
      Both bounds must be ancestors of HEAD (or equal to HEAD).
      If omitted, only the current HEAD commit is rewritten (HEAD-only mode).

Effect per commit in the range:
  - If the commit added a file matching <path>:     the file is not added
                                                    (as if it never existed)
  - If the commit modified a file matching <path>:  the file keeps its state
                                                    from the parent commit
  - If the commit deleted a file matching <path>:   the deletion is undone
                                                    (file restored from parent)

WARNING: Rewrites history.  A force-push is required if the branch has
         already been pushed to a remote.
`

// errUsage is returned when the user supplies incorrect arguments.
// main() uses it to suppress the "Error: …" prefix (usage text has
// already been written to stderr).
var errUsage = errors.New("usage")

func main() {
	err := run(os.Args[1:], os.Stdin, "")
	if err == nil {
		return
	}
	if !errors.Is(err, errUsage) {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	}
	os.Exit(1)
}

// run is the testable entry point for the tool.
//
//   - args   command-line arguments (excluding the program name).
//   - stdin  used for the confirmation prompt.
//   - dir    working directory for all git commands; pass "" to use the
//     process working directory.
func run(args []string, stdin io.Reader, dir string) error {
	if len(args) < 1 || len(args) > 2 {
		fmt.Fprint(os.Stderr, usageText)
		return errUsage
	}

	filterPath := args[0]
	if filterPath == "" {
		return fmt.Errorf("path argument must not be empty")
	}

	// Parse range argument: supports <ref>, <ref>.., or <ref1>..<ref2>
	// If omitted, defaults to HEAD-only mode.
	var baseRef, upperRef string
	if len(args) == 2 {
		rangeArg := args[1]
		if strings.Contains(rangeArg, "..") {
			parts := strings.Split(rangeArg, "..")
			if len(parts) != 2 {
				return fmt.Errorf("invalid range syntax %q: use <ref>, <ref>.., or <ref1>..<ref2>", rangeArg)
			}
			baseRef = parts[0]
			upperRef = parts[1]
			if baseRef == "" {
				return fmt.Errorf("invalid range syntax %q: lower bound is empty", rangeArg)
			}
			// If upper bound is empty, it means HEAD (implicit).
			if upperRef == "" {
				upperRef = "HEAD"
			}
		} else {
			// Single ref: treat as <ref>..HEAD
			baseRef = rangeArg
			upperRef = "HEAD"
		}
	}

	// git builds an exec.Cmd for a git invocation with
	// advice.objectNameWarning suppressed and the working directory set.
	git := func(gitArgs ...string) *exec.Cmd {
		all := make([]string, 0, len(gitArgs)+2)
		all = append(all, "-c", "advice.objectNameWarning=false")
		all = append(all, gitArgs...)
		cmd := exec.Command("git", all...)
		if dir != "" {
			cmd.Dir = dir
		}
		return cmd
	}

	// gitOut runs a git command and returns its trimmed stdout.
	gitOut := func(gitArgs ...string) (string, error) {
		out, err := git(gitArgs...).Output()
		return strings.TrimSpace(string(out)), err
	}

	// -----------------------------------------------------------------------
	// Pre-flight checks
	// -----------------------------------------------------------------------

	if _, err := gitOut("rev-parse", "--git-dir"); err != nil {
		return fmt.Errorf("not inside a git repository")
	}

	if err := git("filter-repo", "--version").Run(); err != nil {
		return fmt.Errorf("git-filter-repo is not installed. See: https://github.com/newren/git-filter-repo")
	}

	if err := git("diff", "--quiet").Run(); err != nil {
		return fmt.Errorf("unstaged changes detected. Commit or stash them first")
	}

	if err := git("diff", "--cached", "--quiet").Run(); err != nil {
		return fmt.Errorf("staged changes detected. Commit or stash them first")
	}

	// -----------------------------------------------------------------------
	// Resolve branch and upper bound
	// -----------------------------------------------------------------------

	// If no upper ref was specified, use HEAD.
	if upperRef == "" {
		upperRef = "HEAD"
	}

	// Resolve the upper bound to a hash. This must be on the current branch
	// or an ancestor of it, so that we can use the current branch ref for
	// git-filter-repo's --refs flag.
	currentHead, err := gitOut("rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("cannot resolve HEAD: %w", err)
	}

	upperHash, err := gitOut("rev-parse", upperRef+"^{commit}")
	if err != nil {
		return fmt.Errorf("cannot resolve %q to a commit", upperRef)
	}

	// The upper bound must be on the current branch (an ancestor of or equal to HEAD).
	if upperHash != currentHead {
		if err := git("merge-base", "--is-ancestor", upperHash, "HEAD").Run(); err != nil {
			return fmt.Errorf("%q (%s) is not an ancestor of HEAD (%s)", upperRef, upperHash, currentHead)
		}
	}

	currentBranch, err := gitOut("symbolic-ref", "--short", "HEAD")
	if err != nil {
		return fmt.Errorf("not on a branch (detached HEAD). Check out a branch first")
	}
	filterRef := "refs/heads/" + currentBranch

	// -----------------------------------------------------------------------
	// Resolve commit range
	// -----------------------------------------------------------------------

	var baseHash, refRange string
	var rangeHashes []string // commits to rewrite, oldest first

	if baseRef != "" {
		baseHash, err = gitOut("rev-parse", baseRef+"^{commit}")
		if err != nil {
			return fmt.Errorf("cannot resolve %q to a commit", baseRef)
		}

		if err := git("merge-base", "--is-ancestor", baseHash, "HEAD").Run(); err != nil {
			return fmt.Errorf("%q (%s) is not an ancestor of HEAD (%s)", baseRef, baseHash, currentHead)
		}

		if baseHash == upperHash {
			return fmt.Errorf("%q resolves to the upper bound — nothing to rewrite", baseRef)
		}

		refRange = baseHash + ".." + upperHash

		listOut, err := gitOut("rev-list", "--reverse", baseHash+".."+upperHash)
		if err != nil {
			return fmt.Errorf("cannot list commits in range: %w", err)
		}
		rangeHashes = strings.Split(strings.TrimSpace(listOut), "\n")

	} else {
		parentHash, parentErr := gitOut("rev-parse", upperHash+"^")
		// parentHash is only valid when the command succeeded AND returned a
		// 40-character hex hash.  On a root commit git rev-parse HEAD^ exits
		// with a non-zero status; guard against any git version that might
		// print the literal string "HEAD^" to stdout instead of failing cleanly.
		if parentErr == nil && len(parentHash) == 40 {
			refRange = parentHash + ".." + upperHash
		} else {
			// Root commit: no parent exists.
			refRange = upperHash
		}
		rangeHashes = []string{upperHash}
	}

	commitCount := len(rangeHashes)

	// -----------------------------------------------------------------------
	// Confirmation prompt
	// -----------------------------------------------------------------------

	repoRoot, _ := gitOut("rev-parse", "--show-toplevel")
	fmt.Printf("Repository : %s\n", repoRoot)
	fmt.Printf("Path filter: %s\n", filterPath)
	fmt.Printf("Range      : %s  (%d commit(s))\n", refRange, commitCount)
	fmt.Println()
	fmt.Println("WARNING: Git history will be permanently rewritten.")
	fmt.Print("Proceed? [y/N] ")

	scanner := bufio.NewScanner(stdin)
	scanner.Scan()
	if answer := strings.TrimSpace(scanner.Text()); answer != "y" && answer != "Y" {
		fmt.Println("Aborted.")
		return nil
	}
	fmt.Println()

	// -----------------------------------------------------------------------
	// Run git-filter-repo
	//
	// We use a --commit-callback with an explicit rewrite set rather than
	// --path-regex/--invert-paths.  Path filtering removes paths from the
	// entire tree, which would erase files that pre-date the range.  The
	// callback approach removes only the *changes* (file_changes entries) for
	// commits in the range, so every rewritten commit inherits those files
	// from its (possibly rewritten) parent — exactly the "leave at the state
	// just before the range" semantics.
	//
	// We do NOT pass ^BASE to --refs.  When git-filter-repo receives a
	// negative ref alongside a positive one, the positive ref is silently
	// dropped from the fast-export command and the callback is never invoked.
	// Instead, the _rewrite_set guard in the callback makes the callback a
	// no-op for commits outside the range.  Those commits still pass through
	// fast-export/import unmodified, so their hashes are preserved.
	//
	//   --commit-callback  Python snippet exec'd for every processed commit
	//   --refs <branch>    the branch ref to update after rewriting
	//   --force            skip the "origin remote" safety check
	//
	// --prune-empty is intentionally left at its default ("auto") so that
	// commits whose only changes were to the filtered path are dropped rather
	// than kept as empty commits.
	// -----------------------------------------------------------------------

	// Build the rewrite set as a Python bytes-set literal.
	// git-filter-repo exposes commit.original_id as bytes, so each hash must
	// be a bytes literal.  fmt.Sprintf %q produces the same escaping rules as
	// Python byte-string literals for hex characters.
	var setParts strings.Builder
	setParts.WriteString("{")
	for i, h := range rangeHashes {
		if i > 0 {
			setParts.WriteString(", ")
		}
		fmt.Fprintf(&setParts, "b%q", h)
	}
	setParts.WriteString("}")

	// Build the Python callback.
	// fc.filename is a bytes object in git-filter-repo (Python 3), so the
	// compiled regex must also be a bytes pattern (b"...").
	callbackCode := fmt.Sprintf(
		"import re as _re\n_rewrite_set = %s\n_r = _re.compile(b%q)\nif commit.original_id in _rewrite_set:\n    commit.file_changes = [fc for fc in commit.file_changes if not _r.search(fc.filename)]\n",
		setParts.String(),
		pathToRegex(filterPath),
	)

	filterRepoArgs := []string{
		"-c", "advice.objectNameWarning=false",
		"filter-repo",
		"--commit-callback", callbackCode,
		"--refs", filterRef,
		"--force",
	}

	filterCmd := exec.Command("git", filterRepoArgs...)
	if dir != "" {
		filterCmd.Dir = dir
	}
	filterCmd.Stdout = os.Stdout
	filterCmd.Stderr = os.Stderr
	if err := filterCmd.Run(); err != nil {
		return fmt.Errorf("git-filter-repo failed: %w", err)
	}

	// -----------------------------------------------------------------------
	// Summary
	// -----------------------------------------------------------------------

	fmt.Printf("Done. %d commit(s) rewritten.\n\n", commitCount)
	fmt.Println("If this branch has been pushed to a remote, force-push with:")
	fmt.Println("  git push --force-with-lease")

	return nil
}
