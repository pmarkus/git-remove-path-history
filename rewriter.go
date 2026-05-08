package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Rewriter reconstructs commits in a range, stripping changes to a given path.
// It uses git plumbing commands (read-tree, update-index, write-tree, commit-tree)
// to rewrite only the specified range while leaving commits outside the range
// completely untouched.
type Rewriter struct {
	repoDir     string   // working directory for git commands
	filterPath  string   // path pattern to filter out
	baseRef     string   // base commit hash (exclusive lower bound)
	rangeHashes []string // commits to rewrite (oldest first)

	// Environment setup
	tempIndexFile string // temporary GIT_INDEX_FILE for tree operations
}

// NewRewriter creates a rewriter for the given repository and range.
func NewRewriter(repoDir, filterPath, baseRef string, rangeHashes []string) *Rewriter {
	return &Rewriter{
		repoDir:     repoDir,
		filterPath:  filterPath,
		baseRef:     baseRef,
		rangeHashes: rangeHashes,
	}
}

// Rewrite executes the rewrite operation, returning the hash of the new HEAD.
// All commits outside the range are left untouched; only commits in rangeHashes
// are rewritten.
func (r *Rewriter) Rewrite() (string, error) {
	// Create a temporary file for GIT_INDEX_FILE to avoid touching the real index.
	f, err := os.CreateTemp("", "git-remove-path-*.idx")
	if err != nil {
		return "", fmt.Errorf("create temp index file: %w", err)
	}
	f.Close()
	r.tempIndexFile = f.Name()
	defer os.Remove(r.tempIndexFile)

	rewrittenParent := r.baseRef
	for i, commitHash := range r.rangeHashes {
		newCommitHash, err := r.rewriteCommit(commitHash, rewrittenParent)
		if err != nil {
			return "", fmt.Errorf("rewrite commit %d (%s): %w", i+1, commitHash, err)
		}
		if newCommitHash == "" {
			// Commit was pruned (empty after filtering)
			// rewrittenParent stays the same
		} else {
			rewrittenParent = newCommitHash
		}
	}

	return rewrittenParent, nil
}

// rewriteCommit rewrites a single commit, returning its new hash.
// If the commit becomes empty (all changes are filtered), returns "" and doesn't create a new commit.
func (r *Rewriter) rewriteCommit(commitHash, rewrittenParentHash string) (string, error) {
	// Get the parent hash of the original commit
	parentHash, err := r.gitOut("rev-parse", commitHash+"^")
	if err != nil {
		// Root commit (no parent)
		parentHash = ""
	}

	// Get the rewritten parent's tree (or empty tree for root)
	var parentTreeHash string
	if rewrittenParentHash == "" {
		// No parent (at base or root)
		parentTreeHash = "4b825dc642cb6eb9a060e54bf8d69288fbee4904" // Git's empty tree hash
	} else {
		pt, err := r.gitOut("rev-parse", rewrittenParentHash+"^{tree}")
		if err != nil {
			return "", fmt.Errorf("get rewritten parent tree: %w", err)
		}
		parentTreeHash = pt
	}

	// Start with the parent's tree (for the rewritten parent)
	// Remove the index file first to ensure a fresh start
	os.Remove(r.tempIndexFile)
	if err := r.gitWithEnv("read-tree", parentTreeHash); err != nil {
		return "", fmt.Errorf("read parent tree: %w", err)
	}

	// Get the list of changed files in this commit
	var diffOutput string
	if parentHash == "" {
		// Root commit: diff against empty tree
		diffOutput, err = r.gitOut("diff-tree", "--root", "-r", "--name-status", commitHash)
	} else {
		diffOutput, err = r.gitOut("diff-tree", "-r", "--name-status", parentHash, commitHash)
	}
	if err != nil {
		return "", fmt.Errorf("get diff-tree: %w", err)
	}

	// Parse changes and apply non-filtered changes to the index
	hasKeptChanges := false
	for _, line := range strings.Split(diffOutput, "\n") {
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, "\t", 2)
		if len(parts) < 2 {
			continue
		}
		status := parts[0]
		filePath := parts[1]

		// Check if this file matches the filter
		if matchesPath(r.filterPath, filePath) {
			continue // Skip filtered files
		}

		hasKeptChanges = true

		// Apply this change to the index
		if err := r.applyFileChange(commitHash, status, filePath); err != nil {
			return "", fmt.Errorf("apply change %s %s: %w", status, filePath, err)
		}
	}

	// If no changes remain (all were filtered), prune the commit
	if !hasKeptChanges {
		return "", nil
	}

	// Write the new tree from the modified index
	newTreeHash, err := r.gitOut("write-tree")
	if err != nil {
		return "", fmt.Errorf("write tree: %w", err)
	}

	// Get the original commit's metadata
	author, err := r.gitOut("log", "-1", "--format=%aN", commitHash)
	if err != nil {
		return "", fmt.Errorf("get author name: %w", err)
	}
	authorEmail, err := r.gitOut("log", "-1", "--format=%aE", commitHash)
	if err != nil {
		return "", fmt.Errorf("get author email: %w", err)
	}
	authorDate, err := r.gitOut("log", "-1", "--format=%aI", commitHash)
	if err != nil {
		return "", fmt.Errorf("get author date: %w", err)
	}
	committerName, err := r.gitOut("log", "-1", "--format=%cN", commitHash)
	if err != nil {
		return "", fmt.Errorf("get committer name: %w", err)
	}
	committerEmail, err := r.gitOut("log", "-1", "--format=%cE", commitHash)
	if err != nil {
		return "", fmt.Errorf("get committer email: %w", err)
	}
	committerDate, err := r.gitOut("log", "-1", "--format=%cI", commitHash)
	if err != nil {
		return "", fmt.Errorf("get committer date: %w", err)
	}
	message, err := r.gitOut("log", "-1", "--format=%B", commitHash)
	if err != nil {
		return "", fmt.Errorf("get commit message: %w", err)
	}

	// Create a new commit with the new tree and original metadata
	env := []string{
		"GIT_AUTHOR_NAME=" + author,
		"GIT_AUTHOR_EMAIL=" + authorEmail,
		"GIT_AUTHOR_DATE=" + authorDate,
		"GIT_COMMITTER_NAME=" + committerName,
		"GIT_COMMITTER_EMAIL=" + committerEmail,
		"GIT_COMMITTER_DATE=" + committerDate,
	}

	var parentArgs []string
	if rewrittenParentHash != "" {
		parentArgs = []string{"-p", rewrittenParentHash}
	}

	args := append([]string{"commit-tree", newTreeHash}, parentArgs...)
	args = append(args, "-m", message)

	cmd := r.buildCmd(args...)
	cmd.Env = append(os.Environ(), env...)

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("commit-tree: %w", err)
	}

	newCommitHash := strings.TrimSpace(string(out))
	return newCommitHash, nil
}

// applyFileChange applies a single file change (add, modify, or delete) to the index.
func (r *Rewriter) applyFileChange(commitHash, status, filePath string) error {
	switch status {
	case "D":
		// Delete: remove from index
		return r.gitWithEnv("update-index", "--remove", "--", filePath)

	case "A", "M":
		// Add or Modify: get the blob from the commit and add to index
		lsTreeOutput, err := r.gitOut("ls-tree", commitHash, "--", filePath)
		if err != nil {
			return fmt.Errorf("ls-tree: %w", err)
		}

		// Parse: <mode> SP <type> SP <object> TAB <path>
		fields := strings.Fields(lsTreeOutput)
		if len(fields) < 3 {
			return fmt.Errorf("unexpected ls-tree output: %s", lsTreeOutput)
		}

		mode := fields[0]
		hash := fields[2]

		// Update index with this blob
		return r.gitWithEnv("update-index", "--add", "--cacheinfo", mode+","+hash+","+filePath)

	default:
		// Other statuses (R, C, T, etc.) - treat as modify for simplicity
		// (In practice these are rare for our use case)
		lsTreeOutput, err := r.gitOut("ls-tree", commitHash, "--", filePath)
		if err != nil {
			return fmt.Errorf("ls-tree: %w", err)
		}
		fields := strings.Fields(lsTreeOutput)
		if len(fields) < 3 {
			return fmt.Errorf("unexpected ls-tree output: %s", lsTreeOutput)
		}
		mode := fields[0]
		hash := fields[2]
		return r.gitWithEnv("update-index", "--add", "--cacheinfo", mode+","+hash+","+filePath)
	}
}

// buildCmd creates a git command with the repo directory set.
func (r *Rewriter) buildCmd(args ...string) *exec.Cmd {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.repoDir
	return cmd
}

// gitWithEnv runs a git command with the temp index file and returns an error if it fails.
func (r *Rewriter) gitWithEnv(args ...string) error {
	cmd := r.buildCmd(args...)
	cmd.Env = append(os.Environ(), "GIT_INDEX_FILE="+r.tempIndexFile)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return nil
}

// gitOut runs a git command and returns trimmed stdout.
func (r *Rewriter) gitOut(args ...string) (string, error) {
	cmd := r.buildCmd(args...)
	cmd.Env = append(os.Environ(), "GIT_INDEX_FILE="+r.tempIndexFile)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}
