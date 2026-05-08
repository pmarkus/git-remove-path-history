package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// makeRepo creates a new git repository in a temporary directory, configures
// a local user identity, and returns the repo path.  The directory is
// automatically removed when the test finishes.
func makeRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitCmds := [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test User"},
	}
	for _, args := range gitCmds {
		mustGit(t, dir, args...)
	}
	return dir
}

// addCommit writes content to file (creating any necessary parent
// directories), stages it, and creates a commit with the given message.
func addCommit(t *testing.T, dir, file, content, message string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(file))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	mustGit(t, dir, "add", file)
	mustGit(t, dir, "commit", "-m", message)
}

// deleteCommit stages a deletion of file and creates a commit.
func deleteCommit(t *testing.T, dir, file, message string) {
	t.Helper()
	mustGit(t, dir, "rm", file)
	mustGit(t, dir, "commit", "-m", message)
}

// mustGit runs a git command with Dir=dir and fatals the test on error.
func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// gitOut runs a git command with Dir=dir and returns trimmed stdout.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out))
}

// commitHashes returns the list of commit hashes for the given rev-list
// arguments, oldest first.
func commitHashes(t *testing.T, dir string, revListArgs ...string) []string {
	t.Helper()
	args := append([]string{"rev-list", "--reverse"}, revListArgs...)
	out := gitOut(t, dir, args...)
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// diffFiles returns the set of file paths changed between two refs
// (equivalent to git diff <from>..<to> --name-only).
func diffFiles(t *testing.T, dir, from, to string) []string {
	t.Helper()
	out := gitOut(t, dir, "diff", from+".."+to, "--name-only")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// containsAny reports whether any element of paths starts with prefix.
func containsAny(paths []string, prefix string) bool {
	for _, p := range paths {
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			return true
		}
	}
	return false
}

// runTool calls run() with "y" as the confirmation answer.
func runTool(t *testing.T, dir string, args ...string) error {
	t.Helper()
	return run(args, strings.NewReader("y\n"), dir)
}

// runToolAbort calls run() with "N" as the confirmation answer.
func runToolAbort(t *testing.T, dir string, args ...string) error {
	t.Helper()
	return run(args, strings.NewReader("N\n"), dir)
}

// ---------------------------------------------------------------------------
// Pre-flight check tests
// ---------------------------------------------------------------------------

func TestPreFlight_NotARepo(t *testing.T) {
	dir := t.TempDir() // plain directory, no git init
	err := runTool(t, dir, "plans")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not inside a git repository") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPreFlight_EmptyPath(t *testing.T) {
	dir := makeRepo(t)
	addCommit(t, dir, "README.md", "hello", "initial")
	err := runTool(t, dir, "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "must not be empty") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPreFlight_UnstagedChanges(t *testing.T) {
	dir := makeRepo(t)
	addCommit(t, dir, "README.md", "hello", "initial")
	// Modify without staging
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("dirty"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := runTool(t, dir, "plans")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unstaged changes") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPreFlight_StagedChanges(t *testing.T) {
	dir := makeRepo(t)
	addCommit(t, dir, "README.md", "hello", "initial")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("staged"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "add", "README.md")
	err := runTool(t, dir, "plans")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "staged changes") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPreFlight_DetachedHEAD(t *testing.T) {
	dir := makeRepo(t)
	addCommit(t, dir, "README.md", "v1", "initial")
	addCommit(t, dir, "README.md", "v2", "second")
	// Detach HEAD at the first commit
	hash := gitOut(t, dir, "rev-parse", "HEAD~1")
	mustGit(t, dir, "checkout", "--detach", hash)
	err := runTool(t, dir, "plans")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "detached HEAD") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPreFlight_BaseRefDoesNotResolve(t *testing.T) {
	dir := makeRepo(t)
	addCommit(t, dir, "README.md", "hello", "initial")
	err := runTool(t, dir, "plans", "nonexistent-ref")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "cannot resolve") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPreFlight_BaseRefNotAncestorOfHEAD(t *testing.T) {
	dir := makeRepo(t)
	addCommit(t, dir, "README.md", "v1", "initial")
	// Create an orphan branch so its tip is not an ancestor of main
	mustGit(t, dir, "checkout", "--orphan", "orphan")
	mustGit(t, dir, "rm", "-rf", ".")
	addCommit(t, dir, "other.md", "other", "orphan commit")
	orphanHash := gitOut(t, dir, "rev-parse", "HEAD")
	mustGit(t, dir, "checkout", "main")
	addCommit(t, dir, "README.md", "v2", "second")
	err := runTool(t, dir, "plans", orphanHash)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not an ancestor") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPreFlight_BaseRefResolvesToHEAD(t *testing.T) {
	dir := makeRepo(t)
	addCommit(t, dir, "README.md", "hello", "initial")
	headHash := gitOut(t, dir, "rev-parse", "HEAD")
	err := runTool(t, dir, "plans", headHash)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "nothing to rewrite") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Confirmation prompt
// ---------------------------------------------------------------------------

func TestConfirmationAbort(t *testing.T) {
	dir := makeRepo(t)
	addCommit(t, dir, "README.md", "base", "base commit")
	base := gitOut(t, dir, "rev-parse", "HEAD")
	addCommit(t, dir, "plans/foo.md", "secret", "add plans")

	hashBefore := gitOut(t, dir, "rev-parse", "HEAD")
	err := runToolAbort(t, dir, "plans", base)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hashAfter := gitOut(t, dir, "rev-parse", "HEAD")
	if hashBefore != hashAfter {
		t.Error("history was modified despite user aborting")
	}
}

// ---------------------------------------------------------------------------
// Happy-path integration tests
// ---------------------------------------------------------------------------

// TestStripDirectoryInRange verifies that changes to a directory are removed
// from all commits in the specified range.
func TestStripDirectoryInRange(t *testing.T) {
	dir := makeRepo(t)
	addCommit(t, dir, "src/main.go", "package main", "add src")
	base := gitOut(t, dir, "rev-parse", "HEAD")
	addCommit(t, dir, "plans/foo.md", "# plan", "add plans")
	addCommit(t, dir, "src/util.go", "package main", "add util")
	addCommit(t, dir, "plans/bar.md", "# bar", "add more plans")

	if err := runTool(t, dir, "plans", base); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	changed := diffFiles(t, dir, base, "HEAD")
	if containsAny(changed, "plans") {
		t.Errorf("plans/ still appears in diff after rewrite: %v", changed)
	}
	// src changes must still be present
	if !containsAny(changed, "src") {
		t.Errorf("src/ changes were unexpectedly removed: %v", changed)
	}
}

// TestStripSingleFileInRange verifies that only the targeted file is removed.
func TestStripSingleFileInRange(t *testing.T) {
	dir := makeRepo(t)
	addCommit(t, dir, "README.md", "base", "base")
	base := gitOut(t, dir, "rev-parse", "HEAD")
	addCommit(t, dir, "secret.txt", "secret", "add secret")
	addCommit(t, dir, "public.txt", "public", "add public")

	if err := runTool(t, dir, "secret.txt", base); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	changed := diffFiles(t, dir, base, "HEAD")
	for _, f := range changed {
		if f == "secret.txt" {
			t.Errorf("secret.txt still appears in diff after rewrite")
		}
	}
	if !containsAny(changed, "public.txt") {
		t.Errorf("public.txt was unexpectedly removed from diff: %v", changed)
	}
}

// TestStripGlobMatchedFiles verifies that glob patterns strip all matching files.
func TestStripGlobMatchedFiles(t *testing.T) {
	dir := makeRepo(t)
	addCommit(t, dir, "go.sum", "initial sum", "base")
	base := gitOut(t, dir, "rev-parse", "HEAD")
	addCommit(t, dir, "yarn.lock", "lock v1", "add yarn.lock")
	addCommit(t, dir, "src/main.go", "package main", "add source")
	addCommit(t, dir, "package-lock.json", "lock", "add package-lock")

	if err := runTool(t, dir, "*.lock", base); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	changed := diffFiles(t, dir, base, "HEAD")
	for _, f := range changed {
		if strings.HasSuffix(f, ".lock") {
			t.Errorf(".lock file still appears in diff: %s", f)
		}
	}
	if !containsAny(changed, "src/main.go") {
		t.Errorf("src/main.go was unexpectedly removed: %v", changed)
	}
}

// TestCommitsBeforeRangeUntouched verifies that commits before the base ref
// keep their original hashes after the rewrite.
func TestCommitsBeforeRangeUntouched(t *testing.T) {
	dir := makeRepo(t)
	addCommit(t, dir, "a.txt", "a", "commit A")
	addCommit(t, dir, "b.txt", "b", "commit B")
	// Record hashes of all commits before the base
	hashesBefore := commitHashes(t, dir, "HEAD")

	base := gitOut(t, dir, "rev-parse", "HEAD")
	addCommit(t, dir, "plans/foo.md", "plan", "add plans")

	if err := runTool(t, dir, "plans", base); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	// Commits at and before base must have the same hashes
	allHashes := commitHashes(t, dir, "HEAD")
	// allHashes is oldest-first; the first len(hashesBefore) entries are the old commits
	for i, h := range hashesBefore {
		if allHashes[i] != h {
			t.Errorf("commit %d hash changed: before=%s after=%s", i+1, h, allHashes[i])
		}
	}
}

// TestCommitsInRangeGetNewHashes verifies that commits inside the rewrite
// range receive new hashes (their content was altered).
func TestCommitsInRangeGetNewHashes(t *testing.T) {
	dir := makeRepo(t)
	addCommit(t, dir, "README.md", "base", "base")
	base := gitOut(t, dir, "rev-parse", "HEAD")
	addCommit(t, dir, "plans/foo.md", "plan", "add plans")
	hashBefore := gitOut(t, dir, "rev-parse", "HEAD")

	if err := runTool(t, dir, "plans", base); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	hashAfter := gitOut(t, dir, "rev-parse", "HEAD")
	if hashBefore == hashAfter {
		t.Error("commit hash did not change after rewrite — rewrite may not have occurred")
	}
}

// TestHeadOnlyMode verifies that when no base ref is supplied, only HEAD is
// rewritten and its parent keeps its original hash.
func TestHeadOnlyMode(t *testing.T) {
	dir := makeRepo(t)
	addCommit(t, dir, "README.md", "base", "base")
	parentHashBefore := gitOut(t, dir, "rev-parse", "HEAD")

	// HEAD contains both a kept file and a filtered file so it is not empty
	// after stripping and will not be pruned by --prune-empty auto.
	writeFile(t, dir, "src/main.go", "package main")
	writeFile(t, dir, "plans/foo.md", "plan")
	stageAllAndCommit(t, dir, "add src and plans")
	headHashBefore := gitOut(t, dir, "rev-parse", "HEAD")

	if err := runTool(t, dir, "plans"); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	headHashAfter := gitOut(t, dir, "rev-parse", "HEAD")
	parentHashAfter := gitOut(t, dir, "rev-parse", "HEAD~1")

	if headHashBefore == headHashAfter {
		t.Error("HEAD hash did not change — rewrite may not have occurred")
	}
	if parentHashBefore != parentHashAfter {
		t.Errorf("parent commit hash changed unexpectedly: before=%s after=%s",
			parentHashBefore, parentHashAfter)
	}
}

// TestRootCommit verifies the tool handles a HEAD with no parent (root commit).
func TestRootCommit(t *testing.T) {
	dir := makeRepo(t)
	// Root commit contains both a file to keep and one to strip so that
	// the commit is not empty after filtering (an all-empty root commit
	// would be pruned by --prune-empty auto, leaving HEAD unborn).
	writeFile(t, dir, "README.md", "keep")
	writeFile(t, dir, "plans/foo.md", "plan")
	stageAllAndCommit(t, dir, "root commit with plans and README")

	if err := runTool(t, dir, "plans"); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	// After rewrite the plans file must not appear in the tree
	out := gitOut(t, dir, "show", "--stat", "HEAD")
	if strings.Contains(out, "plans/") {
		t.Errorf("plans/ still appears in root commit after rewrite:\n%s", out)
	}
}

// TestStripDeletedFile verifies that if a commit deletes a filtered file,
// the deletion is undone (file is restored from parent).
func TestStripDeletedFile(t *testing.T) {
	dir := makeRepo(t)
	addCommit(t, dir, "plans/foo.md", "original", "add plan")
	base := gitOut(t, dir, "rev-parse", "HEAD")
	deleteCommit(t, dir, "plans/foo.md", "delete plan")

	if err := runTool(t, dir, "plans", base); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	// The diff between base and HEAD should not show plans/foo.md as deleted
	changed := diffFiles(t, dir, base, "HEAD")
	for _, f := range changed {
		if f == "plans/foo.md" {
			t.Errorf("plans/foo.md still appears in diff (deletion not undone): %v", changed)
		}
	}
}

// TestStripModifiedFile verifies that if a commit modifies a filtered file,
// the modification is undone (file reverts to its parent state).
func TestStripModifiedFile(t *testing.T) {
	dir := makeRepo(t)
	addCommit(t, dir, "plans/foo.md", "original", "add plan")
	base := gitOut(t, dir, "rev-parse", "HEAD")
	addCommit(t, dir, "plans/foo.md", "modified content", "modify plan")

	if err := runTool(t, dir, "plans", base); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	changed := diffFiles(t, dir, base, "HEAD")
	for _, f := range changed {
		if f == "plans/foo.md" {
			t.Errorf("plans/foo.md still appears in diff (modification not undone): %v", changed)
		}
	}
}

// TestMultipleCommitsInRange verifies all commits in a multi-commit range are
// rewritten, not just the first or last.
func TestMultipleCommitsInRange(t *testing.T) {
	dir := makeRepo(t)
	addCommit(t, dir, "README.md", "base", "base")
	base := gitOut(t, dir, "rev-parse", "HEAD")
	addCommit(t, dir, "plans/a.md", "a", "add a")
	addCommit(t, dir, "src/main.go", "package main", "add src")
	addCommit(t, dir, "plans/b.md", "b", "add b")
	addCommit(t, dir, "plans/c.md", "c", "add c")

	if err := runTool(t, dir, "plans", base); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	changed := diffFiles(t, dir, base, "HEAD")
	if containsAny(changed, "plans") {
		t.Errorf("plans/ still in diff after rewriting multiple commits: %v", changed)
	}
	if !containsAny(changed, "src") {
		t.Errorf("src/ unexpectedly missing from diff: %v", changed)
	}
}

// TestNonMatchingPathUnchanged verifies that specifying a path that has no
// changes in the range completes without error and leaves history unchanged.
func TestNonMatchingPathUnchanged(t *testing.T) {
	dir := makeRepo(t)
	addCommit(t, dir, "README.md", "base", "base")
	base := gitOut(t, dir, "rev-parse", "HEAD")
	addCommit(t, dir, "src/main.go", "package main", "add src")

	// Filter a path that does not exist in the range
	if err := runTool(t, dir, "plans", base); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	hashAfter := gitOut(t, dir, "rev-parse", "HEAD")
	// The commit has no plans changes to strip, but git-filter-repo still
	// rewrites it (same tree, new hash) — this is acceptable. We just verify
	// that the src change is still present.
	changed := diffFiles(t, dir, base, hashAfter)
	if !containsAny(changed, "src/main.go") {
		t.Errorf("src/main.go missing after filtering unrelated path: %v", changed)
	}
}

// ---------------------------------------------------------------------------
// Helpers for multi-file commits
// ---------------------------------------------------------------------------

// writeFile creates or overwrites a file inside dir without staging it.
func writeFile(t *testing.T, dir, file, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(file))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// stageAllAndCommit stages all working-tree changes and creates a commit.
func stageAllAndCommit(t *testing.T, dir, message string) {
	t.Helper()
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-m", message)
}

// ---------------------------------------------------------------------------
// Empty-commit pruning
// ---------------------------------------------------------------------------

// TestEmptyCommitAfterStripIsDropped verifies that a commit whose only
// changes are to the filtered path is dropped entirely from history (not
// kept as an empty commit).  This requires git-filter-repo's default
// --prune-empty auto behaviour; passing --prune-empty never would break it.
func TestEmptyCommitAfterStripIsDropped(t *testing.T) {
	dir := makeRepo(t)
	addCommit(t, dir, "keep/a.txt", "ka", "base")
	base := gitOut(t, dir, "rev-parse", "HEAD")

	addCommit(t, dir, "work/a.txt", "wa", "work only — should be dropped")
	workOnlyHash := gitOut(t, dir, "rev-parse", "HEAD")

	addCommit(t, dir, "keep/b.txt", "kb", "keep b")

	if err := runTool(t, dir, "work", base); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	// Only 1 commit should remain in the range (keep b); the work-only
	// commit must have been dropped rather than kept as an empty commit.
	count := gitOut(t, dir, "rev-list", "--count", base+"..HEAD")
	if count != "1" {
		t.Errorf("expected 1 commit in range after dropping empty commit, got %s", count)
	}

	// The work-only commit's hash must not appear in the reachable history.
	allHashes := make(map[string]bool)
	for _, h := range commitHashes(t, dir, "HEAD") {
		allHashes[h] = true
	}
	if allHashes[workOnlyHash] {
		t.Errorf("work-only commit (%s) should be dropped but still appears in history", workOnlyHash)
	}
}

// ---------------------------------------------------------------------------
// Commits on branches not reachable from HEAD
// ---------------------------------------------------------------------------

// TestCommitsOnOtherBranchUntouched verifies that commits on a branch that
// diverges before the base ref — and is therefore not reachable from HEAD —
// are never processed and keep their original hashes.
func TestCommitsOnOtherBranchUntouched(t *testing.T) {
	dir := makeRepo(t)
	addCommit(t, dir, "keep/a.txt", "ka", "base")
	base := gitOut(t, dir, "rev-parse", "HEAD")

	// Create a diverging branch with commits (including a work/ change).
	mustGit(t, dir, "checkout", "-b", "other")
	addCommit(t, dir, "other/file.txt", "of", "other: unrelated commit")
	addCommit(t, dir, "work/x.txt", "wx", "other: work commit")
	otherTipBefore := gitOut(t, dir, "rev-parse", "HEAD")

	// Back to main: add commits including a work/ change.
	mustGit(t, dir, "checkout", "main")
	addCommit(t, dir, "work/b.txt", "wb", "main: work commit")
	addCommit(t, dir, "keep/b.txt", "kb", "main: keep commit")

	if err := runTool(t, dir, "work", base); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	// The other branch ref must still point to the same commit.
	otherTipAfter := gitOut(t, dir, "rev-parse", "refs/heads/other")
	if otherTipBefore != otherTipAfter {
		t.Errorf("other branch tip changed: want %s, got %s", otherTipBefore, otherTipAfter)
	}
}

// ---------------------------------------------------------------------------
// Complex branching history (full scenario)
// ---------------------------------------------------------------------------

// TestComplexBranchingHistory exercises the full scenario:
//
//	01 -> 02 -> 03 -> 05 -> 06 -> 07 -> 08 -> 09 -> 10
//	              \-> 04 ->/        \-> 11 -> 12 -> 13 <HEAD>
//
// Running the tool with path "work" and base ref 02 should:
//   - Drop commits 04, 11, 13 (they only touch work/).
//   - Drop commit 06 (merge commit becomes empty after 04 is dropped).
//   - Rewrite commit 07 to 07' (work/b dropped, keep/c kept).
//   - Rewrite commit 12 to 12' (keep/b change kept, parent becomes 07').
//   - Leave commits 01, 02, 03, 05 with their original hashes.
//   - Leave commits 08, 09, 10 (on the unreachable "other" branch) untouched.
func TestComplexBranchingHistory(t *testing.T) {
	dir := makeRepo(t)

	// 01: add work/a
	addCommit(t, dir, "work/a.txt", "a-v1", "01: add work/a")
	hash01 := gitOut(t, dir, "rev-parse", "HEAD")

	// 02: add keep/a  ← base ref
	addCommit(t, dir, "keep/a.txt", "ka-v1", "02: add keep/a")
	hash02 := gitOut(t, dir, "rev-parse", "HEAD")

	// 03: modify keep/a
	addCommit(t, dir, "keep/a.txt", "ka-v2", "03: modify keep/a")
	hash03 := gitOut(t, dir, "rev-parse", "HEAD")

	// feature branch from 03: commit 04 modifies work/a
	mustGit(t, dir, "checkout", "-b", "feature")
	addCommit(t, dir, "work/a.txt", "a-v2-feature", "04: modify work/a on feature")
	hash04 := gitOut(t, dir, "rev-parse", "HEAD")

	// back to main: commit 05 adds keep/b
	mustGit(t, dir, "checkout", "main")
	addCommit(t, dir, "keep/b.txt", "kb-v1", "05: add keep/b")
	hash05 := gitOut(t, dir, "rev-parse", "HEAD")

	// commit 06: merge feature (no-ff to guarantee a merge commit object)
	mustGit(t, dir, "merge", "--no-ff", "-m", "06: merge feature", "feature")

	// commit 07: add work/b AND keep/c in one commit
	writeFile(t, dir, "work/b.txt", "wb-v1")
	writeFile(t, dir, "keep/c.txt", "kc-v1")
	stageAllAndCommit(t, dir, "07: add work/b and keep/c")

	// other branch from 07: commits 08, 09, 10 (not reachable from HEAD=13)
	mustGit(t, dir, "checkout", "-b", "other")
	addCommit(t, dir, "work/c.txt", "wc-v1", "08: add work/c")
	addCommit(t, dir, "work/c.txt", "wc-v2", "09: modify work/c")
	addCommit(t, dir, "work/d.txt", "wd-v1", "10: add work/d")
	otherTipBefore := gitOut(t, dir, "rev-parse", "HEAD")

	// back to main: commits 11, 12, 13
	mustGit(t, dir, "checkout", "main")

	// commit 11: modify work/a AND add work/c (two files)
	writeFile(t, dir, "work/a.txt", "a-v3")
	writeFile(t, dir, "work/c.txt", "wc-main-v1")
	stageAllAndCommit(t, dir, "11: modify work/a and add work/c")
	hash11 := gitOut(t, dir, "rev-parse", "HEAD")

	// commit 12: modify keep/b
	addCommit(t, dir, "keep/b.txt", "kb-v2", "12: modify keep/b")

	// commit 13: modify work/a  ← HEAD
	addCommit(t, dir, "work/a.txt", "a-v4", "13: modify work/a")
	hash13 := gitOut(t, dir, "rev-parse", "HEAD")

	// Run the tool: strip "work" since commit 02.
	if err := runTool(t, dir, "work", hash02); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	// --- Assertions ---

	// 0. Exact commit count in the range: 03, 05, 07', 12' survive;
	//    04, 06 (empty merge), 11, 13 are dropped.
	count := gitOut(t, dir, "rev-list", "--count", hash02+"..HEAD")
	if count != "4" {
		t.Errorf("expected 4 commits in range after rewrite, got %s", count)
	}

	// 1. No work/ files in the diff from base to the new HEAD.
	changed := diffFiles(t, dir, hash02, "HEAD")
	if containsAny(changed, "work") {
		t.Errorf("work/ still in diff after rewrite: %v", changed)
	}

	// 2. keep/c.txt must still be present at HEAD (added in 07', not stripped).
	keepCContent := gitOut(t, dir, "show", "HEAD:keep/c.txt")
	if keepCContent != "kc-v1" {
		t.Errorf("keep/c.txt content wrong after rewrite: got %q, want %q", keepCContent, "kc-v1")
	}

	// 3. Commits 01, 02, 03, 05 must appear in HEAD's reachable history
	//    with their original hashes (they are either before the base or have
	//    no work/ changes and unaffected parents).
	allHashes := make(map[string]bool)
	for _, h := range commitHashes(t, dir, "HEAD") {
		allHashes[h] = true
	}
	for _, tc := range []struct{ name, hash string }{
		{"01", hash01},
		{"02", hash02},
		{"03", hash03},
		{"05", hash05},
	} {
		if !allHashes[tc.hash] {
			t.Errorf("commit %s (%s) should be in history but is not", tc.name, tc.hash)
		}
	}

	// 4. Commits 04, 11, 13 must NOT appear in HEAD's reachable history —
	//    they only touched work/ files and should be dropped.
	for _, tc := range []struct{ name, hash string }{
		{"04", hash04},
		{"11", hash11},
		{"13", hash13},
	} {
		if allHashes[tc.hash] {
			t.Errorf("commit %s (%s) should be dropped but still appears in history", tc.name, tc.hash)
		}
	}

	// 5. The "other" branch ref must still point to its original tip (commits
	//    08–10 are not reachable from HEAD and must never be processed).
	otherTipAfter := gitOut(t, dir, "rev-parse", "refs/heads/other")
	if otherTipBefore != otherTipAfter {
		t.Errorf("other branch tip changed: want %s, got %s", otherTipBefore, otherTipAfter)
	}
}
