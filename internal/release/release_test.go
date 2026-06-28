package release

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// --- Pure helpers: ValidateSemver ---

func TestValidateSemver_Valid(t *testing.T) {
	cases := []string{
		"0.0.0",
		"0.1.0",
		"1.2.3",
		"10.20.30",
		"v0.1.0",
		"v1.2.3",
		"1.2.3-rc.1",
		"v1.2.3-rc.1",
		"1.2.3-rc.1+build.42",
		"1.0.0-alpha",
		"1.0.0-0.3.7",
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			if err := ValidateSemver(in); err != nil {
				t.Errorf("ValidateSemver(%q) = %v, want nil", in, err)
			}
		})
	}
}

func TestValidateSemver_Invalid(t *testing.T) {
	cases := map[string]string{
		"empty":              "",
		"reserved_latest":    "latest",
		"reserved_dev":       "dev",
		"reserved_stable":    "stable",
		"missing_patch":      "1.2",
		"only_v":             "v",
		"non_numeric":        "abc",
		"leading_zero_major": "01.0.0",
		"leading_zero_minor": "1.02.0",
		"leading_zero_patch": "1.0.02",
		"four_segments":      "1.0.0.0",
		"negative_major":     "-1.0.0",
		"whitespace":         " 0.1.0",
		"trailing_whitespace": "0.1.0 ",
		"plus_prefix":        "+0.1.0",
		"newer_than_v":       "vv0.1.0",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateSemver(in); err == nil {
				t.Errorf("ValidateSemver(%q) = nil, want error", in)
			}
		})
	}
}

// --- Pure helpers: Normalize ---

func TestNormalize(t *testing.T) {
	cases := []struct{ in, want string }{
		{"0.1.0", "0.1.0"},
		{"v0.1.0", "0.1.0"},
		{"v1.2.3-rc.1", "1.2.3-rc.1"},
		{"latest", "latest"}, // unchanged — Normalize is dumb about content
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := Normalize(tc.in); got != tc.want {
				t.Errorf("Normalize(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// --- Pure helpers: BuildTag ---

func TestBuildTag(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"0.1.0", "v0.1.0"},
		{"v0.1.0", "v0.1.0"},
		{"1.2.3-rc.1", "v1.2.3-rc.1"},
		{"v1.2.3-rc.1", "v1.2.3-rc.1"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := BuildTag(tc.in); got != tc.want {
				t.Errorf("BuildTag(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// --- Git-backed assertions: use t.TempDir() to isolate from test process ---

// setupTestRepo creates a fresh git repo in a temp dir with one empty commit
// on `main`, returns the dir path. Skips the test if git is unavailable.
func setupTestRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	dir := t.TempDir()
	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append([]string{},
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test",
		)
		// Inherit PATH so git itself is found.
		for _, e := range []string{"PATH", "HOME"} {
			if v, ok := os.LookupEnv(e); ok {
				cmd.Env = append(cmd.Env, e+"="+v)
			}
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit("init", "-q", "-b", "main")
	runGit("config", "user.email", "test@test")
	runGit("config", "user.name", "test")
	runGit("commit", "--allow-empty", "-q", "-m", "initial")
	return dir
}

func writeFile(dir, name, body string) error {
	return os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644)
}

// mustGit runs git in dir with the given args and fatals on failure.
// Used by tests that need a tagged commit without going through setupTestRepo.
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

func TestAssertGitCleanIn_CleanRepo(t *testing.T) {
	dir := setupTestRepo(t)
	if err := AssertGitCleanIn(dir); err != nil {
		t.Errorf("AssertGitCleanIn(clean repo) = %v, want nil", err)
	}
}

func TestAssertGitCleanIn_DirtyRepo(t *testing.T) {
	dir := setupTestRepo(t)
	// Create an untracked file to dirty the tree.
	if err := writeFile(dir, "untracked.txt", "dirty"); err != nil {
		t.Fatal(err)
	}
	err := AssertGitCleanIn(dir)
	if err == nil {
		t.Fatal("AssertGitCleanIn(dirty repo) = nil, want error")
	}
	if !strings.Contains(err.Error(), "uncommitted") {
		t.Errorf("error should mention uncommitted changes, got: %v", err)
	}
}

func TestAssertOnBranchIn_Matches(t *testing.T) {
	dir := setupTestRepo(t)
	if err := AssertOnBranchIn(dir, "main"); err != nil {
		t.Errorf("AssertOnBranchIn(main) = %v, want nil", err)
	}
}

func TestAssertOnBranchIn_WrongBranch(t *testing.T) {
	dir := setupTestRepo(t)
	err := AssertOnBranchIn(dir, "release")
	if err == nil {
		t.Fatal("AssertOnBranchIn(release on main) = nil, want error")
	}
	if !strings.Contains(err.Error(), "main") {
		t.Errorf("error should mention current branch, got: %v", err)
	}
}

func TestAssertOnBranchIn_EmptyMeansAny(t *testing.T) {
	dir := setupTestRepo(t)
	if err := AssertOnBranchIn(dir, ""); err != nil {
		t.Errorf("AssertOnBranchIn(\"\") = %v, want nil (any branch)", err)
	}
}

func TestAssertTagAbsentIn_NoSuchTag(t *testing.T) {
	dir := setupTestRepo(t)
	if err := AssertTagAbsentIn(dir, "v0.1.0"); err != nil {
		t.Errorf("AssertTagAbsentIn(missing) = %v, want nil", err)
	}
}

func TestAssertTagAbsentIn_TagExists(t *testing.T) {
	dir := setupTestRepo(t)
	mustGit(t, dir, "tag", "v0.1.0")
	err := AssertTagAbsentIn(dir, "v0.1.0")
	if err == nil {
		t.Fatal("AssertTagAbsentIn(existing tag) = nil, want error")
	}
	if !strings.Contains(err.Error(), "v0.1.0") {
		t.Errorf("error should mention tag name, got: %v", err)
	}
}

func TestAssertTagAbsentIn_EmptyTagRejected(t *testing.T) {
	dir := setupTestRepo(t)
	err := AssertTagAbsentIn(dir, "")
	if err == nil {
		t.Fatal("AssertTagAbsentIn(\"\") = nil, want error")
	}
}

// --- Sanity: composite error messages are actionable ---

func TestAssertGitCleanIn_ErrorMentionsFix(t *testing.T) {
	dir := setupTestRepo(t)
	_ = writeFile(dir, "x.txt", "x")
	err := AssertGitCleanIn(dir)
	if err == nil {
		t.Fatal("expected error")
	}
	// Error must name the offending file so the user knows what to clean up,
	// and must not be a bare "exit status 128" from git.
	msg := err.Error()
	if !strings.Contains(msg, "x.txt") {
		t.Errorf("error should list dirty files (x.txt), got: %v", msg)
	}
	if !strings.Contains(msg, "uncommitted") {
		t.Errorf("error should hint at the fix ('uncommitted changes'), got: %v", msg)
	}
}
