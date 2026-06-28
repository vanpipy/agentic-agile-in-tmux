// Package release — release pipeline primitives for awp.
//
// Used by `make release` (via cmd/releasecheck) to gate a tagged
// build on:
//   - valid semver
//   - clean working tree
//   - expected branch
//   - tag not yet present
//
// Each Assert* function comes in two flavors:
//   - AssertX()       — operates on CWD (default; for Makefile use)
//   - AssertXIn(dir)  — operates on an explicit git repo dir (for tests)
//
// Pure helpers (ValidateSemver, Normalize, BuildTag) have no I/O.
package release

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// semverRe matches canonical semver 2.0.0 with optional 'v' prefix.
// Source: https://semver.org/#is-there-a-suggested-regular-expression-regex-to-check-a-semver-string
var semverRe = regexp.MustCompile(`^v?(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-[0-9A-Za-z-.]+)?(?:\+[0-9A-Za-z-.]+)?$`)

// reservedVersions are placeholders that must never be used as a release version.
// Allowing them would silently tag a build with no real version info.
var reservedVersions = map[string]bool{
	"latest": true,
	"dev":    true,
	"stable": true,
}

// ValidateSemver returns nil if v is a valid semver with or without 'v' prefix.
// Empty string, reserved placeholders, or malformed input returns an error.
func ValidateSemver(v string) error {
	if v == "" {
		return fmt.Errorf("version must not be empty")
	}
	if reservedVersions[v] {
		return fmt.Errorf("version %q is reserved (placeholder, not a real version)", v)
	}
	if !semverRe.MatchString(v) {
		return fmt.Errorf("version %q is not valid semver (expected e.g. 0.1.0 or v1.2.3-rc.1)", v)
	}
	return nil
}

// Normalize returns the version with leading 'v' stripped.
// Treats "v0.1.0" and "0.1.0" as the same version.
func Normalize(v string) string {
	return strings.TrimPrefix(v, "v")
}

// BuildTag returns the canonical tag for a version, always prefixed with 'v'.
// "0.1.0" → "v0.1.0", "v0.1.0" → "v0.1.0", "" → "".
// Empty input returns empty so callers can compose safely.
func BuildTag(v string) string {
	if v == "" {
		return ""
	}
	if strings.HasPrefix(v, "v") {
		return v
	}
	return "v" + v
}

// AssertGitClean fails if the working tree has uncommitted changes.
// Used by release pipelines to avoid baking unrelated WIP into a tag.
func AssertGitClean() error { return assertGitClean("") }

// AssertGitCleanIn is AssertGitClean with an explicit repo directory.
// Pass a t.TempDir() path in tests to isolate from the test process CWD.
func AssertGitCleanIn(dir string) error { return assertGitClean(dir) }

func assertGitClean(dir string) error {
	cmd := exec.Command("git", "status", "--porcelain")
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("git status: %w", err)
	}
	if strings.TrimSpace(string(out)) != "" {
		return fmt.Errorf("working tree has uncommitted changes:\n%s", out)
	}
	return nil
}

// AssertOnBranch fails if HEAD is not on the expected branch.
// Empty expected means "any branch" (no-op).
func AssertOnBranch(expected string) error { return assertOnBranch("", expected) }

// AssertOnBranchIn is AssertOnBranch with an explicit repo directory.
func AssertOnBranchIn(dir, expected string) error { return assertOnBranch(dir, expected) }

func assertOnBranch(dir, expected string) error {
	if expected == "" {
		return nil
	}
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("git rev-parse: %w", err)
	}
	branch := strings.TrimSpace(string(out))
	if branch != expected {
		return fmt.Errorf("expected branch %q, got %q", expected, branch)
	}
	return nil
}

// AssertTagAbsent fails if a local tag with the given name exists.
// Avoids accidentally re-tagging an old release.
func AssertTagAbsent(tag string) error { return assertTagAbsent("", tag) }

// AssertTagAbsentIn is AssertTagAbsent with an explicit repo directory.
func AssertTagAbsentIn(dir, tag string) error { return assertTagAbsent(dir, tag) }

func assertTagAbsent(dir, tag string) error {
	if tag == "" {
		return fmt.Errorf("tag must not be empty")
	}
	cmd := exec.Command("git", "rev-parse", "--verify", "--quiet", "refs/tags/"+tag)
	if dir != "" {
		cmd.Dir = dir
	}
	err := cmd.Run()
	if err == nil {
		return fmt.Errorf("tag %q already exists", tag)
	}
	// Non-zero exit from --quiet means tag not present (good).
	// Other errors (e.g. not a git repo) propagate.
	if _, ok := err.(*exec.ExitError); ok {
		return nil
	}
	return fmt.Errorf("git rev-parse: %w", err)
}
