// create_project_path_test.go — TDD tests for Cluster E.1 fix.
//
// Cluster E.1 (Major from 2026-06-27 audit): createProjectFromPath used
// os.Stat(gitDir) which follows symlinks. A user could `ln -s /etc/passwd
// /tmp/evil/.git` to trick awp into accepting arbitrary paths. While this
// is a single-user CLI (low real-world risk), the validation is incomplete.
//
// Fix: use filepath.EvalSymlinks to resolve the canonical path BEFORE the
// .git check. Reject broken symlinks. Verify the resolved .git is a real
// directory or worktree-file, not a symlink.
//
// Tests cover 4 cases:
//   - Real git repo: accepted, repo path stored verbatim
//   - Symlink to real repo: accepted, RESOLVED path stored (not symlink path)
//   - Broken symlink: rejected
//   - .git as worktree file: accepted
package ui

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/pi/awp/internal/config"
	"github.com/pi/awp/internal/project"
)

// makeTestModelForCreateProject returns a Model wired for testing
// createProjectFromPath. Unlike makeTestModel, this uses a fresh registry
// pointing at t.TempDir() so Add() writes are sandboxed.
func makeTestModelForCreateProject(t *testing.T) *Model {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.UI.Theme = "default"
	t.Setenv("AWP_CONFIG_DIR", t.TempDir())
	reg, err := project.LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	store := project.NewGlobalTicketStore(reg)
	m := NewModel(cfg, store, reg, "", nil)
	m.width = 80
	m.height = 24
	return m
}

// setupFakeGitRepo creates a temp directory with a .git subdirectory
// (mimicking a real git repo). Returns the path.
func setupFakeGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.Mkdir(gitDir, 0755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	// Add a minimal HEAD file so it looks more like a real .git.
	headPath := filepath.Join(gitDir, "HEAD")
	if err := os.WriteFile(headPath, []byte("ref: refs/heads/main\n"), 0644); err != nil {
		t.Fatalf("write HEAD: %v", err)
	}
	return dir
}

// setupFakeWorktree creates a temp directory with .git as a FILE (not dir)
// pointing to the main repo's .git/worktrees/<name>. Mimics a git worktree.
// Returns the worktree path.
func setupFakeWorktree(t *testing.T, mainRepoDir string) string {
	t.Helper()
	wtDir := filepath.Join(t.TempDir(), "wt")
	if err := os.Mkdir(wtDir, 0755); err != nil {
		t.Fatalf("mkdir wt: %v", err)
	}
	gitFile := filepath.Join(wtDir, ".git")
	// A real worktree .git file looks like:
	//   gitdir: /path/to/main/.git/worktrees/wt
	gitDir := filepath.Join(mainRepoDir, ".git", "worktrees", "wt")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatalf("mkdir worktree gitdir: %v", err)
	}
	content := "gitdir: " + gitDir + "\n"
	if err := os.WriteFile(gitFile, []byte(content), 0644); err != nil {
		t.Fatalf("write .git file: %v", err)
	}
	return wtDir
}

// TestCreateProjectFromPath_AcceptsRealGitRepo pins the basic happy path:
// a real git repo is accepted and added to the registry.
func TestCreateProjectFromPath_AcceptsRealGitRepo(t *testing.T) {
	repoDir := setupFakeGitRepo(t)
	m := makeTestModelForCreateProject(t)
	m.addProjectPath.SetValue(repoDir)
	m.mode = ModeCreateProject

	m.createProjectFromPath()

	if got := len(m.projectRegistry.Projects); got != 1 {
		t.Fatalf("after createProjectFromPath: %d projects; want 1", got)
	}
	var added *project.Project
	for _, p := range m.projectRegistry.Projects {
		added = p
		break
	}
	// Path stored should match what was given (resolved through EvalSymlinks
	// should be idempotent for a non-symlinked path).
	if !pathsEqual(added.RepoPath, repoDir) {
		t.Errorf("RepoPath = %q; want %q", added.RepoPath, repoDir)
	}
	if added.Name != filepath.Base(repoDir) {
		t.Errorf("Name = %q; want %q", added.Name, filepath.Base(repoDir))
	}
}

// TestCreateProjectFromPath_ResolvesSymlink pins Cluster E.1's main contract:
// a symlink to a real repo is accepted, but the RESOLVED (canonical) path
// is stored, not the symlink path. This means a user who has `~/dotfiles`
// symlinked to `~/repos/dotfiles` gets the actual repo path stored.
//
// This is defense against path-traversal tricks AND a UX improvement:
// operations on the stored path won't accidentally follow the user's
// symlink (which might have changed by the time we re-stat).
func TestCreateProjectFromPath_ResolvesSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require admin on Windows; skipping")
	}
	realRepo := setupFakeGitRepo(t)

	// Create a symlink in a different temp dir.
	linkDir := t.TempDir()
	symlink := filepath.Join(linkDir, "repo-link")
	if err := os.Symlink(realRepo, symlink); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	m := makeTestModelForCreateProject(t)
	m.addProjectPath.SetValue(symlink)
	m.mode = ModeCreateProject

	m.createProjectFromPath()

	if got := len(m.projectRegistry.Projects); got != 1 {
		t.Fatalf("after createProjectFromPath: %d projects; want 1", got)
	}
	var added *project.Project
	for _, p := range m.projectRegistry.Projects {
		added = p
		break
	}
	// The stored RepoPath must be the RESOLVED (real) path, not the symlink.
	// We compare raw strings (NOT via EvalSymlinks) so that a path stored as
	// the symlink fails the assertion.
	addedResolved, _ := filepath.EvalSymlinks(added.RepoPath)
	if added.RepoPath == symlink || addedResolved == symlink {
		t.Errorf("RepoPath = %q; want resolved path %q (not symlink %q).\n"+
			"Cluster E.1: EvalSymlinks must canonicalize before storage.",
			added.RepoPath, realRepo, symlink)
	}
}

// TestCreateProjectFromPath_RejectsBrokenSymlink pins the contract that
// a symlink pointing to a non-existent target is rejected with a clear
// notification. Without EvalSymlinks, os.Stat would follow the symlink
// and return ENOENT, but the error message would be confusing.
func TestCreateProjectFromPath_RejectsBrokenSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require admin on Windows; skipping")
	}

	linkDir := t.TempDir()
	brokenLink := filepath.Join(linkDir, "broken")
	// Point at a path that doesn't exist.
	if err := os.Symlink("/nonexistent/path/that/cannot/exist", brokenLink); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	m := makeTestModelForCreateProject(t)
	m.addProjectPath.SetValue(brokenLink)
	m.mode = ModeCreateProject

	m.createProjectFromPath()

	if got := len(m.projectRegistry.Projects); got != 0 {
		t.Errorf("after createProjectFromPath: %d projects; want 0 (broken symlink must be rejected).\n"+
			"Cluster E.1: filepath.EvalSymlinks should fail on broken symlink.",
			got)
	}
	if !strings.Contains(m.notification, "symlink") && !strings.Contains(m.notification, "broken") && !strings.Contains(m.notification, "not exist") && !strings.Contains(m.notification, "no such") {
		t.Errorf("notification = %q; want it to mention the broken-symlink problem", m.notification)
	}
}

// TestCreateProjectFromPath_AcceptsWorktreeGitFile pins the contract that
// a worktree (where .git is a FILE pointing to the main repo's .git/worktrees)
// is accepted. Without this, awp would reject worktrees because the .git
// path is a file, not a directory.
func TestCreateProjectFromPath_AcceptsWorktreeGitFile(t *testing.T) {
	mainRepo := setupFakeGitRepo(t)
	wtDir := setupFakeWorktree(t, mainRepo)

	m := makeTestModelForCreateProject(t)
	m.addProjectPath.SetValue(wtDir)
	m.mode = ModeCreateProject

	m.createProjectFromPath()

	if got := len(m.projectRegistry.Projects); got != 1 {
		t.Fatalf("after createProjectFromPath: %d projects; want 1 (worktrees must be accepted).\n"+
			"notification: %q", got, m.notification)
	}
}

// pathsEqual compares two paths after EvalSymlinks. Useful because t.TempDir()
// returns symlinked paths on macOS (under /var/folders/...).
func pathsEqual(a, b string) bool {
	aResolved, errA := filepath.EvalSymlinks(a)
	if errA != nil {
		aResolved = a
	}
	bResolved, errB := filepath.EvalSymlinks(b)
	if errB != nil {
		bResolved = b
	}
	return aResolved == bResolved
}