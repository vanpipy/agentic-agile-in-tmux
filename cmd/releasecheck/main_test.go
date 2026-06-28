package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestRunChecks_TooFewArgs pins the invocation contract: argv without
// <version> and <expected-branch> must exit with code 2 (distinct from
// check failures at code 1).
func TestRunChecks_TooFewArgs(t *testing.T) {
	cases := [][]string{
		{"releasecheck"},
		{"releasecheck", "0.1.0"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			msg, code, err := runChecks(args)
			if code != 2 {
				t.Errorf("runChecks(%v) code = %d, want 2", args, code)
			}
			if err == nil {
				t.Errorf("runChecks(%v) err = nil, want usage error", args)
			}
			if msg != "" {
				t.Errorf("runChecks(%v) msg = %q, want empty on invocation error", args, msg)
			}
		})
	}
}

// TestRunChecks_InvalidSemver pins that a non-semver string exits with
// code 1 and surfaces the underlying semver error (so the user knows
// what to fix, not just "something failed").
func TestRunChecks_InvalidSemver(t *testing.T) {
	dir := setupCleanRepo(t)
	chdir(t, dir)
	// We can't easily inject a "not on main" branch scenario here without
	// a real branch switch. We test the semver path which runs first.
	msg, code, err := runChecks([]string{"releasecheck", "latest", "main"})
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Errorf("err should mention 'reserved', got: %v", err)
	}
	if msg != "" {
		t.Errorf("msg should be empty on failure, got: %q", msg)
	}
}

// TestRunChecks_HappyPath drives all four checks to a green state and
// verifies the OK message names the version, tag, and branch.
func TestRunChecks_HappyPath(t *testing.T) {
	dir := setupCleanRepo(t)
	chdir(t, dir)

	msg, code, err := runChecks([]string{"releasecheck", "0.1.0", "main"})
	if err != nil {
		t.Fatalf("runChecks happy path: %v", err)
	}
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	// The OK message must echo back the version, the tag (with v prefix),
	// and the branch so the Makefile log is auditable.
	for _, want := range []string{"0.1.0", "v0.1.0", "main"} {
		if !strings.Contains(msg, want) {
			t.Errorf("OK message missing %q; got: %q", want, msg)
		}
	}
}

// TestRunChecks_TagExistsFails pins that attempting to re-tag an existing
// release is rejected (defense against accidental re-release).
func TestRunChecks_TagExistsFails(t *testing.T) {
	dir := setupCleanRepo(t)
	mustGit(t, dir, "tag", "v0.1.0")
	chdir(t, dir)

	_, code, err := runChecks([]string{"releasecheck", "0.1.0", "main"})
	if code != 1 {
		t.Errorf("code = %d, want 1 (tag already exists)", code)
	}
	if err == nil || !strings.Contains(err.Error(), "v0.1.0") {
		t.Errorf("err should mention the conflicting tag, got: %v", err)
	}
}

// --- helpers ---

func setupCleanRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	dir := t.TempDir()
	mustGit(t, dir, "init", "-q", "-b", "main")
	mustGit(t, dir, "config", "user.email", "test@test")
	mustGit(t, dir, "config", "user.name", "test")
	mustGit(t, dir, "commit", "--allow-empty", "-q", "-m", "initial")
	return dir
}

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// chdir switches to dir for the duration of the test, restoring afterward.
// Required because runChecks reads CWD for git state.
func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}
