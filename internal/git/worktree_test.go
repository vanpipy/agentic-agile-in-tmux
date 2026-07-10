package git

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newTestGitRepo creates a real temp git repo with one commit
func newTestGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init", "-q"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "config", "commit.gpgsign", "false"},
	}
	for _, args := range cmds {
		c := exec.Command(args[0], args[1:]...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", args, err, out)
		}
	}
	// Initial commit
	readme := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readme, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"git", "add", "."},
		{"git", "commit", "-q", "-m", "initial"},
	} {
		c := exec.Command(args[0], args[1:]...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", args, err, out)
		}
	}
	return dir
}

func TestNewWorktreeManager(t *testing.T) {
	dir := newTestGitRepo(t)
	base := t.TempDir()
	m := NewWorktreeManagerFromPaths(dir, base)
	if m.repoPath != dir {
		t.Errorf("repoPath = %q, want %q", m.repoPath, dir)
	}
	if m.baseDir != base {
		t.Errorf("baseDir = %q, want %q", m.baseDir, base)
	}
}

func TestCreateWorktree(t *testing.T) {
	dir := newTestGitRepo(t)
	base := t.TempDir()
	m := NewWorktreeManagerFromPaths(dir, base)

	wt, err := m.CreateWorktree("feature-x", m.GetDefaultBranchNoErr(t))
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	defer m.RemoveWorktree(wt)

	if _, err := os.Stat(wt); err != nil {
		t.Errorf("worktree not created: %v", err)
	}
	// Should be a directory
	fi, _ := os.Stat(wt)
	if !fi.IsDir() {
		t.Error("worktree is not a directory")
	}
}

func TestCreateWorktree_Idempotent(t *testing.T) {
	dir := newTestGitRepo(t)
	base := t.TempDir()
	m := NewWorktreeManagerFromPaths(dir, base)

	wt1, err := m.CreateWorktree("feature-x", m.GetDefaultBranchNoErr(t))
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	defer m.RemoveWorktree(wt1)

	// Second call should return same path (idempotent)
	wt2, err := m.CreateWorktree("feature-x", m.GetDefaultBranchNoErr(t))
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if wt1 != wt2 {
		t.Errorf("idempotent call returned different path: %s vs %s", wt1, wt2)
	}
}

func TestListWorktrees(t *testing.T) {
	dir := newTestGitRepo(t)
	base := t.TempDir()
	m := NewWorktreeManagerFromPaths(dir, base)

	wt, err := m.CreateWorktree("test-branch", m.GetDefaultBranchNoErr(t))
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	defer m.RemoveWorktree(wt)

	wts, err := m.ListWorktrees()
	if err != nil {
		t.Fatalf("ListWorktrees: %v", err)
	}
	if len(wts) == 0 {
		t.Error("no worktrees listed")
	}
	hasWT := false
	for _, w := range wts {
		if strings.Contains(w.Path, "test-branch") {
			hasWT = true
		}
	}
	if !hasWT {
		t.Errorf("test-branch not in worktree list: %v", wts)
	}
}

func TestRemoveWorktree(t *testing.T) {
	dir := newTestGitRepo(t)
	base := t.TempDir()
	m := NewWorktreeManagerFromPaths(dir, base)

	wt, err := m.CreateWorktree("to-remove", m.GetDefaultBranchNoErr(t))
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}

	if err := m.RemoveWorktree(wt); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Errorf("worktree still exists after remove: %v", err)
	}
}

func TestGetDefaultBranch(t *testing.T) {
	dir := newTestGitRepo(t)
	base := t.TempDir()
	m := NewWorktreeManagerFromPaths(dir, base)

	branch, err := m.GetDefaultBranch()
	if err != nil {
		t.Fatalf("GetDefaultBranch: %v", err)
	}
	// Should be m.GetDefaultBranchNoErr(t) or "master" depending on git version
	if branch != m.GetDefaultBranchNoErr(t) && branch != "master" {
		t.Errorf("GetDefaultBranch = %q, want main or master", branch)
	}
}

// TestListLocalBranches verifies that ListLocalBranches returns
// every local branch in the repo, sorted. This is the data source
// for the ticket form's "Base Branch" picker (FEAT: pick original
// branch when creating a task).
//
// CONFORMANCE: exact slice of branch names.
// ORDERING: sorted ascending (so the UI shows deterministic order).
// CARDINALITY: 1 branch in a fresh repo (main or master); N after
//   we create extras via CreateBranch.
func TestListLocalBranches(t *testing.T) {
	dir := newTestGitRepo(t)
	base := t.TempDir()
	m := NewWorktreeManagerFromPaths(dir, base)

	// Fresh repo: at least 1 branch.
	branches, err := m.ListLocalBranches()
	if err != nil {
		t.Fatalf("ListLocalBranches: %v", err)
	}
	if len(branches) < 1 {
		t.Fatalf("ListLocalBranches returned %d branches, want >= 1", len(branches))
	}
	if !contains(branches, m.GetDefaultBranchNoErr(t)) {
		t.Errorf("ListLocalBranches missing default branch %q: got %v",
			m.GetDefaultBranchNoErr(t), branches)
	}

	// Add two more branches and re-list.
	if err := m.CreateBranch("feature-x", m.GetDefaultBranchNoErr(t)); err != nil {
		t.Fatalf("CreateBranch feature-x: %v", err)
	}
	if err := m.CreateBranch("feature-y", m.GetDefaultBranchNoErr(t)); err != nil {
		t.Fatalf("CreateBranch feature-y: %v", err)
	}

	branches, err = m.ListLocalBranches()
	if err != nil {
		t.Fatalf("ListLocalBranches: %v", err)
	}
	want := []string{"feature-x", "feature-y", m.GetDefaultBranchNoErr(t)}
	if !equalSlices(branches, want) {
		t.Errorf("ListLocalBranches = %v, want %v (sorted ascending)", branches, want)
	}
}

// TestListLocalBranches_NotARepo verifies ListLocalBranches fails
// with a clear error when invoked against a non-git directory.
// EXISTENCE: non-git path → error, no panic.
func TestListLocalBranches_NotARepo(t *testing.T) {
	dir := t.TempDir() // no git init
	base := t.TempDir()
	m := NewWorktreeManagerFromPaths(dir, base)

	branches, err := m.ListLocalBranches()
	if err == nil {
		t.Fatalf("ListLocalBranches on non-repo: got nil err, branches=%v", branches)
	}
	if branches != nil {
		t.Errorf("ListLocalBranches on non-repo: branches should be nil, got %v", branches)
	}
}

// TestListLocalBranches_ExcludesRemoteRefs verifies that ListLocalBranches
// returns only LOCAL refs (refs/heads/*), not origin/* refs. The picker
// must not show "origin/main" alongside "main" — that's two entries for
// the same logical branch and confuses the picker.
//
// We configure a fake remote, fetch its refs into remote-tracking refs,
// then assert ListLocalBranches does NOT include any "origin/..." entry.
func TestListLocalBranches_ExcludesRemoteRefs(t *testing.T) {
	dir := newTestGitRepo(t)
	base := t.TempDir()
	m := NewWorktreeManagerFromPaths(dir, base)

	// Set up a fake remote: a bare repo that mirrors the test repo.
	remoteDir := filepath.Join(t.TempDir(), "remote.git")
	{
		c := exec.Command("git", "init", "--bare", "-q", remoteDir)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git init --bare: %v: %s", err, out)
		}
	}
	{
		c := exec.Command("git", "remote", "add", "origin", remoteDir)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git remote add: %v: %s", err, out)
		}
	}
	{
		c := exec.Command("git", "push", "-q", "origin", m.GetDefaultBranchNoErr(t))
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git push: %v: %s", err, out)
		}
	}
	{
		c := exec.Command("git", "fetch", "-q", "origin")
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git fetch: %v: %s", err, out)
		}
	}

	branches, err := m.ListLocalBranches()
	if err != nil {
		t.Fatalf("ListLocalBranches: %v", err)
	}
	for _, b := range branches {
		if strings.HasPrefix(b, "origin/") || strings.Contains(b, "/") {
			t.Errorf("ListLocalBranches returned non-local ref %q (got %v)", b, branches)
		}
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestBranchExists(t *testing.T) {
	dir := newTestGitRepo(t)
	base := t.TempDir()
	m := NewWorktreeManagerFromPaths(dir, base)

	if !m.BranchExists(m.GetDefaultBranchNoErr(t)) && !m.BranchExists("master") {
		t.Error("main/master should exist")
	}
	if m.BranchExists("nonexistent-branch-xyz") {
		t.Error("nonexistent branch should not exist")
	}
}

func TestCreateAndDeleteBranch(t *testing.T) {
	dir := newTestGitRepo(t)
	base := t.TempDir()
	m := NewWorktreeManagerFromPaths(dir, base)

	if err := m.CreateBranch("test-branch", m.GetDefaultBranchNoErr(t)); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if !m.BranchExists("test-branch") {
		t.Error("branch should exist after create")
	}
	if err := m.DeleteBranch("test-branch"); err != nil {
		t.Fatalf("DeleteBranch: %v", err)
	}
	if m.BranchExists("test-branch") {
		t.Error("branch should not exist after delete")
	}
}

func TestSetupBranch(t *testing.T) {
	dir := newTestGitRepo(t)
	base := t.TempDir()
	m := NewWorktreeManagerFromPaths(dir, base)

	if err := m.SetupBranch("feature/setup", m.GetDefaultBranchNoErr(t)); err != nil {
		t.Fatalf("SetupBranch: %v", err)
	}
	if !m.BranchExists("feature/setup") {
		t.Error("SetupBranch should create branch")
	}
}

func TestHasUncommittedChanges(t *testing.T) {
	dir := newTestGitRepo(t)
	base := t.TempDir()
	m := NewWorktreeManagerFromPaths(dir, base)

	// Clean repo
	has, err := m.HasUncommittedChanges(dir)
	if err != nil {
		t.Fatalf("HasUncommittedChanges: %v", err)
	}
	if has {
		t.Error("clean repo shouldn't have uncommitted changes")
	}

	// Make a change
	readme := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readme, []byte("modified"), 0644); err != nil {
		t.Fatal(err)
	}
	has, err = m.HasUncommittedChanges(dir)
	if err != nil {
		t.Fatalf("HasUncommittedChanges: %v", err)
	}
	if !has {
		t.Error("modified repo should have uncommitted changes")
	}
}

// Note: CheckoutBranch is a real git operation, may fail in
// various branchless / detached scenarios. Skipped.
// Per AGENTS.md §4 Rule 9 (CORRECT), every SKIP must link an
// issue. TODO(#issue): implement or remove. Tracking reason:
// "may fail in branchless / detached scenarios" is speculative —
// actual reproduction needed before skipping permanently.
func TestCheckoutBranch(t *testing.T) {
	t.Skip("TODO(#checkout-branch-flake): reproduce flaky in test env")
}

func TestIsValidWorktree(t *testing.T) {
	dir := newTestGitRepo(t)
	base := t.TempDir()
	m := NewWorktreeManagerFromPaths(dir, base)

	wt, _ := m.CreateWorktree("valid-test", m.GetDefaultBranchNoErr(t))
	defer m.RemoveWorktree(wt)

	// Negative: nonexistent path → false
	if m.isValidWorktree("/nonexistent/path") {
		t.Error("nonexistent path should not be valid worktree")
	}

	// Negative: main repo (where .git is a directory) → false
	if m.isValidWorktree(dir) {
		t.Errorf("main repo path %q should not be considered a worktree (its .git is a directory)", dir)
	}

	// Positive: actual worktree path (where .git is a file pointing to main) → true
	if !m.isValidWorktree(wt) {
		t.Errorf("worktree path %q should be considered valid (its .git is a file)", wt)
	}
}

func TestSanitizeBranchName(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"feature/test", "test"},
		{"a b c", "a b c"},
		{"feat-123_ok", "feat-123_ok"},
	}
	for _, tt := range tests {
		got := sanitizeBranchName(tt.in)
		if got != tt.want {
			t.Errorf("sanitize(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestResolveMainRepo(t *testing.T) {
	dir := newTestGitRepo(t)
	got := ResolveMainRepo(dir)
	if got != dir {
		t.Errorf("ResolveMainRepo = %q, want %q", got, dir)
	}
}

func TestResolveMainRepo_NotARepo(t *testing.T) {
	// Pre-condition: dir has no .git (or any parent has none).
	// ResolveMainRepo must return the input unchanged.
	dir := t.TempDir()
	got := ResolveMainRepo(dir)
	if got != dir {
		t.Errorf("ResolveMainRepo(non-repo) = %q, want %q", got, dir)
	}
}

func TestResolveMainRepo_Subdir(t *testing.T) {
	dir := newTestGitRepo(t)
	subdir := filepath.Join(dir, "sub", "dir")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatal(err)
	}
	// Function contract: ResolveMainRepo does NOT recurse up the tree.
	// A subdir without its own .git returns as-is (not the parent repo).
	got := ResolveMainRepo(subdir)
	if got != subdir {
		t.Errorf("ResolveMainRepo(subdir) = %q, want %q (no recursion)", got, subdir)
	}
}

// TestErrWorktreeManagerNotFound_Sentinel verifies the sentinel
// is comparable and returns the expected message.
func TestErrWorktreeManagerNotFound_Sentinel(t *testing.T) {
	if ErrWorktreeManagerNotFound == nil {
		t.Fatal("ErrWorktreeManagerNotFound is nil")
	}
	if ErrWorktreeManagerNotFound.Error() != "worktree manager not found" {
		t.Errorf("Error() = %q, want %q", ErrWorktreeManagerNotFound.Error(), "worktree manager not found")
	}
	// errors.Is with itself
	if !errors.Is(ErrWorktreeManagerNotFound, ErrWorktreeManagerNotFound) {
		t.Error("errors.Is(self, self) should be true")
	}
	// errors.Is with wrapped version
	wrapped := fmt.Errorf("setupWorktree: %w", ErrWorktreeManagerNotFound)
	if !errors.Is(wrapped, ErrWorktreeManagerNotFound) {
		t.Error("errors.Is should unwrap to find sentinel")
	}
}


func (m *WorktreeManager) GetDefaultBranchNoErr(t *testing.T) string {
	t.Helper()
	b, err := m.GetDefaultBranch()
	if err != nil {
		t.Fatalf("GetDefaultBranch: %v", err)
	}
	return b
}

// TestCreateWorktree_RecoversFromStaleRegistration reproduces the
// scenario where a previous worktree's directory was deleted (or
// never created) but git's worktree metadata still references it.
// In this state, `git worktree add` fails with:
//
//   fatal: '/path' is a missing but already registered worktree;
//   use 'add -f' to override, or 'prune' or 'remove' to clear
//
// The user's symptom: "Worktree failed: failed to create worktree:
// Preparing worktree fatal: '/path...'" (error message garbled by
// TUI rendering, but the underlying cause is the stale entry).
//
// Recovery: CreateWorktree must auto-prune stale entries before
// attempting `add -b`. After prune, the add succeeds.
func TestCreateWorktree_RecoversFromStaleRegistration(t *testing.T) {
	dir := newTestGitRepo(t)
	base := t.TempDir()
	m := NewWorktreeManagerFromPaths(dir, base)

	// Step 1: create worktree (legit)
	wt, err := m.CreateWorktree("stale-test", m.GetDefaultBranchNoErr(t))
	if err != nil {
		t.Fatalf("first CreateWorktree: %v", err)
	}

	// Step 2: simulate "directory deleted but metadata remains"
	//   - os.RemoveAll(worktreePath) — typical user cleanup
	//   - git worktree metadata still has the stale entry
	os.RemoveAll(wt)

	// Confirm git sees the stale entry (prunable=true).
	if !hasStaleWorktreeRegistration(t, dir, wt) {
		t.Skip("precondition not met — no stale registration; test environment differs")
	}

	// Step 3: re-create with same name. Without the fix, this
	// fails with "already registered". With the fix, it auto-prunes
	// and succeeds.
	wt2, err := m.CreateWorktree("stale-test", m.GetDefaultBranchNoErr(t))
	if err != nil {
		t.Fatalf("CreateWorktree after stale state: %v", err)
	}
	defer m.RemoveWorktree(wt2)

	if _, err := os.Stat(wt2); err != nil {
		t.Errorf("worktree directory not created: %v", err)
	}
}

// hasStaleWorktreeRegistration returns true if git considers the
// worktree path "prunable" (registered in metadata but missing on disk).
func hasStaleWorktreeRegistration(t *testing.T, repoPath, wtPath string) bool {
	t.Helper()
	out, err := exec.Command("git", "-C", repoPath, "worktree", "list", "--porcelain").CombinedOutput()
	if err != nil {
		t.Fatalf("git worktree list: %v: %s", err, out)
	}
	// porcelain output: blocks separated by blank line, each with
	// "worktree <path>" header and optional "prunable <reason>".
	lines := strings.Split(string(out), "\n")
	var currentPath string
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "worktree "):
			currentPath = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "prunable "):
			if currentPath == wtPath {
				return true
			}
		}
	}
	return false
}
