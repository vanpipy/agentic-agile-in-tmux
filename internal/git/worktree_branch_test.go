// worktree_branch_test.go — TDD tests for Cluster E.3 fix.
//
// Cluster E.3 (Major from 2026-06-27 audit): sanitizeBranchName stripped
// known prefixes and replaced / with -, but did NOT validate against
// git's ref-format rules. Risky if Slugify produces an invalid ref
// (e.g., leading -, .., control characters, etc.).
//
// Fix: add IsValidBranchName(s) bool in internal/git that runs
// `git check-ref-format --branch <name>` via subprocess. CreateWorktree
// rejects invalid names with a clear error.
//
// Tests:
//   - TestIsValidBranchName: table-driven, covers valid + invalid cases
//   - TestCreateWorktree_RejectsInvalidName: integration test
//   - TestWorktreeGo_UsesGitCheckRefFormat: source-level contract
package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// requireGit skips the test if `git` is not on PATH (so this test runs
// only in environments with git installed).
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH; skipping")
	}
}

// TestIsValidBranchName covers the contract: IsValidBranchName returns true
// for valid branch names (per git check-ref-format) and false for invalid.
//
// CORRECT-7 self-check:
//   C-onformance: bool output (literal true/false)
//   O-rdering: N/A (each case is independent)
//   R-ange: 8+ cases covering valid + invalid boundaries
//   R-eference: subprocess to git (handled via requireGit)
//   E-xistence: empty string case
//   C-ardinality: 0/1/N cases
//   T-ime: no time concerns
func TestIsValidBranchName(t *testing.T) {
	requireGit(t)

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		// Valid
		{"simple lowercase", "feature-branch", true},
		{"with slash", "feature/my-branch", true},
		{"with numbers", "branch-123", true},
		{"with dots", "v1.2.3", true},
		{"task prefix", "task/abc-123", true},

		// Invalid
		{"empty string", "", false},
		{"leading dash", "-branch", false},
		{"trailing dot", "branch.", false},
		{"double dot", "branch..name", false},
		{"contains space", "branch name", false},
		{"contains tilde", "branch~1", false},
		{"contains caret", "branch^1", false},
		{"contains colon", "branch:name", false},
		{"contains question", "branch?", false},
		{"contains asterisk", "branch*", false},
		{"contains bracket", "branch[1]", false},
		{"ends with .lock", "branch.lock", false},
		{"contains backslash", "branch\\name", false},
		{"contains ascii control", "branch\x01", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsValidBranchName(tt.input)
			if got != tt.want {
				t.Errorf("IsValidBranchName(%q) = %v; want %v", tt.input, got, tt.want)
			}
		})
	}
}

// TestCreateWorktree_RejectsInvalidBranchName verifies that CreateWorktree
// fails fast when the branch name is invalid (per IsValidBranchName),
// BEFORE attempting the actual git operation. This protects against
// git's cryptic error messages and prevents creating directories for
// invalid worktree names.
//
// CORRECT-7 self-check:
//   C-onformance: error return must mention "invalid branch name"
//   O-rdering: N/A
//   R-ange: 1 invalid name tested
//   R-eference: requires git
//   E-xistence: error must be non-nil
//   C-ardinality: 1 case
//   T-ime: no time concerns
func TestCreateWorktree_RejectsInvalidBranchName(t *testing.T) {
	requireGit(t)

	// Create a temp dir with a real git repo (CreateWorktree calls git).
	tmpDir := t.TempDir()
	initGitRepo(t, tmpDir)

	mgr := NewWorktreeManagerFromPaths(tmpDir, filepath.Join(tmpDir, "wt"))
	_, err := mgr.CreateWorktree("invalid..name", "main")
	if err == nil {
		t.Fatal("CreateWorktree accepted invalid branch name; want error")
	}
	if !strings.Contains(err.Error(), "invalid") && !strings.Contains(err.Error(), "branch") {
		t.Errorf("error = %q; want it to mention 'invalid' or 'branch'", err.Error())
	}
}

// TestCreateWorktree_AcceptsValidBranchName is the happy-path companion:
// a valid branch name should succeed in creating a worktree.
func TestCreateWorktree_AcceptsValidBranchName(t *testing.T) {
	requireGit(t)

	tmpDir := t.TempDir()
	initGitRepo(t, tmpDir)

	mgr := NewWorktreeManagerFromPaths(tmpDir, filepath.Join(tmpDir, "wt"))
	worktreePath, err := mgr.CreateWorktree("feature-valid", "main")
	if err != nil {
		t.Fatalf("CreateWorktree failed on valid name: %v", err)
	}
	if _, err := os.Stat(worktreePath); err != nil {
		t.Errorf("worktree directory not created: %v", err)
	}
}

// TestWorktreeGo_UsesGitCheckRefFormat pins the source-level contract:
// IsValidBranchName must use git's check-ref-format, not a custom regex.
// This catches future regressions where someone might try to hand-roll
// a regex-based validator.
func TestWorktreeGo_UsesGitCheckRefFormat(t *testing.T) {
	requireGit(t)

	src := readWorktreeGoSource(t)
	if !strings.Contains(src, "git check-ref-format") {
		t.Errorf("worktree.go does not use `git check-ref-format`; IsValidBranchName must defer to git's canonical validator.\n"+
			"Cluster E.3: custom regex validators miss edge cases (e.g., unicode normalization, control chars).")
	}
	if !strings.Contains(src, "func IsValidBranchName(") {
		t.Errorf("worktree.go missing IsValidBranchName function declaration.\n"+
			"Cluster E.3: this is the public API for branch-name validation.")
	}
}

// --- helpers ---

// initGitRepo initializes a minimal git repo at dir with an initial commit
// on the default branch.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	cmds := [][]string{
		{"git", "init", dir},
		{"git", "-C", dir, "config", "user.email", "test@test"},
		{"git", "-C", dir, "config", "user.name", "Test"},
		{"git", "-C", dir, "checkout", "-b", "main"},
		{"git", "-C", dir, "commit", "--allow-empty", "-m", "initial"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
}

func readWorktreeGoSource(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			path := filepath.Join(dir, "internal", "git", "worktree.go")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			return string(data)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}