package main

import (
	"fmt"
	"io"
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

// reachableHashSet returns all commit hashes reachable from HEAD.
func reachableHashSet(t *testing.T, dir string) map[string]bool {
	t.Helper()
	set := make(map[string]bool)
	for _, h := range commitHashes(t, dir, "HEAD") {
		set[h] = true
	}
	return set
}

// assertHashReachable verifies that an exact commit hash still exists in
// reachable history after a rewrite.
func assertHashReachable(t *testing.T, dir, hash, context string) {
	t.Helper()
	if !reachableHashSet(t, dir)[hash] {
		t.Errorf("expected unchanged commit hash %s to remain reachable (%s)", hash, context)
	}
}

// runTool calls run() with "y" as the confirmation answer, suppressing output
// unless the test fails.
func runTool(t *testing.T, dir string, args ...string) error {
	t.Helper()
	return runToolWithOutput(t, dir, false, args...)
}

// runToolAbort calls run() with "N" as the confirmation answer.
func runToolAbort(t *testing.T, dir string, args ...string) error {
	t.Helper()
	return run(args, strings.NewReader("N\n"), dir)
}

// runToolWithOutput is the internal implementation that can optionally suppress output.
// If showOutput is false, stdout/stderr are captured and only printed on failure.
func runToolWithOutput(t *testing.T, dir string, showOutput bool, args ...string) error {
	t.Helper()
	if showOutput {
		// No suppression: let output through normally.
		return run(args, strings.NewReader("y\n"), dir)
	}

	// Capture stdout and stderr.
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()

	os.Stdout = wOut
	os.Stderr = wErr

	err := run(args, strings.NewReader("y\n"), dir)

	// Close the write ends so the read ends will EOF.
	wOut.Close()
	wErr.Close()

	// Restore stdout/stderr.
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	// Read captured output.
	outBytes, _ := io.ReadAll(rOut)
	errBytes, _ := io.ReadAll(rErr)

	// If the test failed, print the captured output for debugging.
	if err != nil {
		t.Log("\n=== Captured stdout ===")
		t.Log(string(outBytes))
		t.Log("\n=== Captured stderr ===")
		t.Log(string(errBytes))
	}

	return err
}

// runToolBinary builds the current package as a standalone binary and runs it
// against the provided repository directory, answering the confirmation prompt
// with "y".
func runToolBinary(t *testing.T, repoDir string, args ...string) error {
	t.Helper()
	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "git-remove-path-history-test-bin")

	buildCmd := exec.Command("go", "build", "-o", binPath, ".")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}

	cmd := exec.Command(binPath, args...)
	cmd.Dir = repoDir
	cmd.Stdin = strings.NewReader("y\n")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Logf("binary stdout/stderr:\n%s", string(out))
		return err
	}
	return nil
}

// ---------------------------------------------------------------------------
// Pre-flight check tests
// ---------------------------------------------------------------------------
// ---------------------------------------------------------------------------
// Commit-metadata helpers
// ---------------------------------------------------------------------------

// commitMeta holds the author/committer metadata that must be preserved
// verbatim across a rewrite.
type commitMeta struct {
	authorName     string
	authorEmail    string
	authorDate     string // ISO 8601 strict (e.g. "2020-01-15T10:30:00+00:00")
	committerName  string
	committerEmail string
	committerDate  string // ISO 8601 strict
	message        string // full commit message body, trailing newline stripped
}

// mustGitEnv runs a git command with Dir=dir and fatals on error.
// env is a list of "KEY=VALUE" strings added on top of the current process
// environment (later entries override earlier ones for the same key).
func mustGitEnv(t *testing.T, dir string, env []string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// commitWithMeta commits whatever is currently staged in dir, using the
// author/committer identity and message from meta.
func commitWithMeta(t *testing.T, dir, message string, meta commitMeta) {
	t.Helper()
	mustGitEnv(t, dir, []string{
		"GIT_AUTHOR_NAME=" + meta.authorName,
		"GIT_AUTHOR_EMAIL=" + meta.authorEmail,
		"GIT_AUTHOR_DATE=" + meta.authorDate,
		"GIT_COMMITTER_NAME=" + meta.committerName,
		"GIT_COMMITTER_EMAIL=" + meta.committerEmail,
		"GIT_COMMITTER_DATE=" + meta.committerDate,
	}, "commit", "-m", message)
}

// getCommitMeta reads author/committer metadata and the full commit message
// from the given commit hash using git log.
func getCommitMeta(t *testing.T, dir, hash string) commitMeta {
	t.Helper()
	// Use %x00 (NUL) as a field delimiter so that commit messages containing
	// newlines do not confuse parsing.  SplitN preserves the rest of the body
	// as a single element.
	const format = "%aN%x00%aE%x00%aI%x00%cN%x00%cE%x00%cI%x00%B"
	out := gitOut(t, dir, "log", "-1", "--format="+format, hash)
	parts := strings.SplitN(out, "\x00", 7)
	if len(parts) != 7 {
		t.Fatalf("getCommitMeta(%s): expected 7 NUL-delimited fields, got %d; raw output: %q",
			hash, len(parts), out)
	}
	return commitMeta{
		authorName:     parts[0],
		authorEmail:    parts[1],
		authorDate:     parts[2],
		committerName:  parts[3],
		committerEmail: parts[4],
		committerDate:  parts[5],
		message:        strings.TrimRight(parts[6], "\n"),
	}
}

// ---------------------------------------------------------------------------
// Metadata preservation test
// ---------------------------------------------------------------------------

// TestCommitMetadataPreservedAfterRewrite verifies that author name, author
// email, author date, committer name, committer email, committer date, and
// commit message are all preserved verbatim on commits that are rewritten by
// the tool. This is a core requirement that must hold regardless of the
// rewriting mechanism used.
func TestCommitMetadataPreservedAfterRewrite(t *testing.T) {
	dir := makeRepo(t)
	addCommit(t, dir, "README.md", "base", "base")
	base := gitOut(t, dir, "rev-parse", "HEAD")

	janeMeta := commitMeta{
		authorName:     "Jane Doe",
		authorEmail:    "jane@example.com",
		authorDate:     "2020-01-15T10:30:00+00:00",
		committerName:  "Jane Doe",
		committerEmail: "jane@example.com",
		committerDate:  "2020-01-15T10:30:00+00:00",
	}
	bobMeta := commitMeta{
		authorName:     "Bob Smith",
		authorEmail:    "bob@example.org",
		authorDate:     "2020-03-22T06:00:00+00:00",
		committerName:  "Bob Smith",
		committerEmail: "bob@example.org",
		committerDate:  "2020-03-22T06:00:00+00:00",
	}

	// Commit A: touches the filtered path (plans/) AND a kept path (keep/).
	// After stripping plans/ it must survive as a non-empty commit.
	writeFile(t, dir, "plans/x.md", "plan content")
	writeFile(t, dir, "keep/a.txt", "keep content")
	mustGit(t, dir, "add", "plans/x.md", "keep/a.txt")
	commitWithMeta(t, dir, "commit A", janeMeta)

	// Commit B: touches only a kept path.  Its parent will change after A is
	// rewritten, so it also receives a new hash, but metadata must survive.
	writeFile(t, dir, "keep/b.txt", "b content")
	mustGit(t, dir, "add", "keep/b.txt")
	commitWithMeta(t, dir, "commit B", bobMeta)

	if err := runTool(t, dir, "plans", base); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	// Both commits survive the rewrite (neither becomes empty).
	rewrittenHashes := commitHashes(t, dir, base+"..HEAD")
	if len(rewrittenHashes) != 2 {
		t.Fatalf("expected 2 rewritten commits, got %d", len(rewrittenHashes))
	}

	// Commit A' is the first commit after base; commit B' is the second.
	gotA := getCommitMeta(t, dir, rewrittenHashes[0])
	gotB := getCommitMeta(t, dir, rewrittenHashes[1])

	for _, tc := range []struct {
		field string
		got   string
		want  string
	}{
		{"A author name", gotA.authorName, janeMeta.authorName},
		{"A author email", gotA.authorEmail, janeMeta.authorEmail},
		{"A author date", gotA.authorDate, janeMeta.authorDate},
		{"A committer name", gotA.committerName, janeMeta.committerName},
		{"A committer email", gotA.committerEmail, janeMeta.committerEmail},
		{"A committer date", gotA.committerDate, janeMeta.committerDate},
		{"A message", gotA.message, "commit A"},
		{"B author name", gotB.authorName, bobMeta.authorName},
		{"B author email", gotB.authorEmail, bobMeta.authorEmail},
		{"B author date", gotB.authorDate, bobMeta.authorDate},
		{"B committer name", gotB.committerName, bobMeta.committerName},
		{"B committer email", gotB.committerEmail, bobMeta.committerEmail},
		{"B committer date", gotB.committerDate, bobMeta.committerDate},
		{"B message", gotB.message, "commit B"},
	} {
		if tc.got != tc.want {
			t.Errorf("commit %s: got %q, want %q", tc.field, tc.got, tc.want)
		}
	}
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

	assertHashReachable(t, dir, base, "base commit before rewrite range")
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

	assertHashReachable(t, dir, base, "base commit before rewrite range")
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

	assertHashReachable(t, dir, base, "base commit before rewrite range")
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

	assertHashReachable(t, dir, base, "base commit before rewrite range")
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

	assertHashReachable(t, dir, base, "base commit before rewrite range")
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

	assertHashReachable(t, dir, base, "base commit before rewrite range")
}

// TestNonMatchingPathUnchanged verifies that specifying a path that has no
// changes in the range completes without error and leaves history unchanged.
func TestNonMatchingPathUnchanged(t *testing.T) {
	dir := makeRepo(t)
	addCommit(t, dir, "README.md", "base", "base")
	base := gitOut(t, dir, "rev-parse", "HEAD")
	addCommit(t, dir, "src/main.go", "package main", "add src")
	headBefore := gitOut(t, dir, "rev-parse", "HEAD")

	// Filter a path that does not exist in the range
	if err := runTool(t, dir, "plans", base); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	hashAfter := gitOut(t, dir, "rev-parse", "HEAD")
	if hashAfter != headBefore {
		t.Errorf("non-matching range commit changed hash unexpectedly: before=%s after=%s", headBefore, hashAfter)
	}

	assertHashReachable(t, dir, base, "base commit before rewrite range")

	// The src change should still be present.
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
// kept as an empty commit).
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

// TestBinaryCLI_CommitsBeforeRangeUntouched verifies hash-preservation for
// commits at/before base when running the compiled CLI binary (black-box).
func TestBinaryCLI_CommitsBeforeRangeUntouched(t *testing.T) {
	dir := makeRepo(t)

	addCommit(t, dir, "keep/a.txt", "a1", "01: keep/a")
	addCommit(t, dir, "keep/b.txt", "b1", "02: keep/b")

	base := gitOut(t, dir, "rev-parse", "HEAD")
	prefixBefore := commitHashes(t, dir, base)

	addCommit(t, dir, "plans/x.md", "x1", "03: plans/x")
	addCommit(t, dir, "keep/c.txt", "c1", "04: keep/c")

	if err := runToolBinary(t, dir, "plans", base+".."); err != nil {
		t.Fatalf("binary run failed: %v", err)
	}

	for _, h := range prefixBefore {
		assertHashReachable(t, dir, h, "prefix hash should stay reachable in binary run")
	}
}

// TestCommitsBeforeRangeUntouched_WithMergeBeforeBase verifies that a merge
// commit chosen as base keeps its hash and remains reachable after rewrite.
func TestCommitsBeforeRangeUntouched_WithMergeBeforeBase(t *testing.T) {
	dir := makeRepo(t)

	addCommit(t, dir, "keep/root.txt", "root", "01: root")

	mustGit(t, dir, "checkout", "-b", "feature")
	addCommit(t, dir, "keep/feat.txt", "feat", "02f: feature commit")

	mustGit(t, dir, "checkout", "main")
	addCommit(t, dir, "keep/main.txt", "main", "02m: main commit")
	mustGit(t, dir, "merge", "--no-ff", "-m", "03: merge feature", "feature")

	base := gitOut(t, dir, "rev-parse", "HEAD")
	prefixBefore := commitHashes(t, dir, base)

	addCommit(t, dir, "plans/a.md", "pa", "04: plans change")
	addCommit(t, dir, "keep/after.txt", "ka", "05: keep after")

	if err := runTool(t, dir, "plans", base+".."); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	for _, h := range prefixBefore {
		assertHashReachable(t, dir, h, "prefix hash should stay reachable with merge base")
	}
}

// TestFilterRepoPreservesOutOfRangeHashes is a regression test for a known
// failure mode in the current implementation.
//
// The tool must guarantee that commits before the rewrite range retain their
// original hashes. The current git-filter-repo based implementation does not
// provide this guarantee; see .agents/investigation-filter-repo.md for details.
// This test documents the requirement. When the implementation is replaced
// (per .agents/plan.md item 2), this test will transition from a known-broken
// test to a mandatory correctness check.
func TestFilterRepoPreservesOutOfRangeHashes(t *testing.T) {
	dir := makeRepo(t)

	// Build a simple history: 3 commits before base, then 2 in the range.
	addCommit(t, dir, "keep/a.txt", "a1", "pre-1: before range")
	addCommit(t, dir, "keep/b.txt", "b1", "pre-2: before range")
	addCommit(t, dir, "keep/c.txt", "c1", "pre-3: before range")

	base := gitOut(t, dir, "rev-parse", "HEAD")
	prefixBefore := commitHashes(t, dir, base)

	// These two commits are IN the range.
	addCommit(t, dir, "plans/x.md", "x1", "in-range-1: plans change")
	addCommit(t, dir, "keep/d.txt", "d1", "in-range-2: non-plans change")

	if err := runTool(t, dir, "plans", base+".."); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	// Every commit at or before base must keep its original hash.
	for i, h := range prefixBefore {
		assertHashReachable(t, dir, h, fmt.Sprintf("pre-range commit %d must be unchanged", i+1))
	}
}

// ---------------------------------------------------------------------------
// Range syntax tests
// ---------------------------------------------------------------------------

// TestRangeSyntax_SingleRef verifies that <ref> syntax works correctly.
func TestRangeSyntax_SingleRef(t *testing.T) {
	dir := makeRepo(t)
	addCommit(t, dir, "README.md", "base", "base")
	base := gitOut(t, dir, "rev-parse", "HEAD")
	addCommit(t, dir, "plans/foo.md", "plan", "add plans")
	addCommit(t, dir, "src/main.go", "code", "add src")

	// Rewrite from base to HEAD using single-ref syntax
	if err := runTool(t, dir, "plans", base); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	changed := diffFiles(t, dir, base, "HEAD")
	if containsAny(changed, "plans") {
		t.Errorf("plans/ still appears in diff: %v", changed)
	}

	assertHashReachable(t, dir, base, "base commit before rewrite range")
}

// TestRangeSyntax_TrailingDots verifies that <ref>.. syntax works correctly.
func TestRangeSyntax_TrailingDots(t *testing.T) {
	dir := makeRepo(t)
	addCommit(t, dir, "README.md", "base", "base")
	base := gitOut(t, dir, "rev-parse", "HEAD")
	addCommit(t, dir, "plans/foo.md", "plan", "add plans")
	addCommit(t, dir, "src/main.go", "code", "add src")

	// Rewrite from base to HEAD using trailing-dots syntax
	if err := runTool(t, dir, "plans", base+".."); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	changed := diffFiles(t, dir, base, "HEAD")
	if containsAny(changed, "plans") {
		t.Errorf("plans/ still appears in diff: %v", changed)
	}

	assertHashReachable(t, dir, base, "base commit before rewrite range")
}

// TestRangeSyntax_ExplicitBothBounds verifies that <ref1>..<ref2> syntax works
// when the upper bound is HEAD (the common case).
func TestRangeSyntax_ExplicitBothBoundsToHEAD(t *testing.T) {
	dir := makeRepo(t)
	addCommit(t, dir, "README.md", "base", "base")
	base := gitOut(t, dir, "rev-parse", "HEAD")

	addCommit(t, dir, "plans/foo.md", "plan", "add plans")
	addCommit(t, dir, "src/main.go", "code", "add src")
	head := gitOut(t, dir, "rev-parse", "HEAD")

	// Rewrite using explicit bounds where upper bound is HEAD
	if err := runTool(t, dir, "plans", base+".."+head); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	changed := diffFiles(t, dir, base, "HEAD")
	if containsAny(changed, "plans") {
		t.Errorf("plans/ still appears in diff: %v", changed)
	}
	if !containsAny(changed, "src") {
		t.Errorf("src/ missing from diff: %v", changed)
	}

	assertHashReachable(t, dir, base, "base commit before rewrite range")
}

// TestRangeSyntax_BoundsWithBranchNames verifies that branch/tag names work as refs.
func TestRangeSyntax_BoundsWithBranchNames(t *testing.T) {
	dir := makeRepo(t)
	addCommit(t, dir, "README.md", "base", "base")
	mustGit(t, dir, "tag", "v1.0")
	v1 := gitOut(t, dir, "rev-parse", "HEAD")

	addCommit(t, dir, "plans/foo.md", "plan", "add plans")
	addCommit(t, dir, "src/main.go", "code", "add src")

	// Rewrite from v1.0 to HEAD using tag names
	if err := runTool(t, dir, "plans", "v1.0..HEAD"); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	changed := diffFiles(t, dir, v1, "HEAD")
	if containsAny(changed, "plans") {
		t.Errorf("plans/ still appears in diff: %v", changed)
	}
	if !containsAny(changed, "src") {
		t.Errorf("src/ missing from diff: %v", changed)
	}

	assertHashReachable(t, dir, v1, "lower-bound commit before rewrite range")
}

// TestRangeSyntax_ErrorLowerBoundNotResolved verifies error handling.
func TestRangeSyntax_ErrorLowerBoundNotResolved(t *testing.T) {
	dir := makeRepo(t)
	addCommit(t, dir, "README.md", "hello", "initial")

	err := runTool(t, dir, "plans", "nonexistent..HEAD")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "cannot resolve") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestRangeSyntax_ErrorUpperBoundNotResolved verifies error handling.
func TestRangeSyntax_ErrorUpperBoundNotResolved(t *testing.T) {
	dir := makeRepo(t)
	addCommit(t, dir, "README.md", "hello", "initial")

	err := runTool(t, dir, "plans", "HEAD..nonexistent")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "cannot resolve") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestRangeSyntax_ErrorLowerBoundNotAncestor verifies that lower bound must
// be an ancestor of upper bound.
func TestRangeSyntax_ErrorLowerBoundNotAncestor(t *testing.T) {
	dir := makeRepo(t)
	addCommit(t, dir, "README.md", "v1", "initial")

	// Create an orphan branch (not an ancestor of main)
	mustGit(t, dir, "checkout", "--orphan", "orphan")
	mustGit(t, dir, "rm", "-rf", ".")
	addCommit(t, dir, "other.md", "other", "orphan commit")
	orphanHash := gitOut(t, dir, "rev-parse", "HEAD")

	// Back to main and add more commits
	mustGit(t, dir, "checkout", "main")
	addCommit(t, dir, "README.md", "v2", "second")

	// Try to use orphan (non-ancestor) as lower bound
	err := runTool(t, dir, "plans", orphanHash+"..HEAD")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not an ancestor") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestRangeSyntax_ErrorUpperBoundNotAncestorOfHEAD verifies that upper bound
// must be an ancestor of or equal to HEAD.
func TestRangeSyntax_ErrorUpperBoundNotAncestorOfHEAD(t *testing.T) {
	dir := makeRepo(t)
	addCommit(t, dir, "README.md", "v1", "initial")
	hash1 := gitOut(t, dir, "rev-parse", "HEAD")

	// Create an orphan branch (not an ancestor of main)
	mustGit(t, dir, "checkout", "--orphan", "orphan")
	mustGit(t, dir, "rm", "-rf", ".")
	addCommit(t, dir, "other.md", "other", "orphan commit")
	orphanHash := gitOut(t, dir, "rev-parse", "HEAD")

	// Back to main (HEAD is now hash1)
	mustGit(t, dir, "checkout", "main")

	// Try to use orphan (non-ancestor of HEAD) as upper bound
	err := runTool(t, dir, "plans", hash1+".."+orphanHash)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not an ancestor") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestRangeSyntax_ErrorBoundsEqual verifies error when lower and upper are the same.
func TestRangeSyntax_ErrorBoundsEqual(t *testing.T) {
	dir := makeRepo(t)
	addCommit(t, dir, "README.md", "hello", "initial")
	hash := gitOut(t, dir, "rev-parse", "HEAD")

	err := runTool(t, dir, "plans", hash+".."+hash)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "nothing to rewrite") {
		t.Errorf("unexpected error: %v", err)
	}
}
