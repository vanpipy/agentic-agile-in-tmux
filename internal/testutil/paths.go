package testutil

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// RepoRoot returns the absolute path to the awp project root (the
// directory containing go.mod). Robust to any CWD and any test
// location, because it walks up from this source file rather than
// trusting the process working directory.
//
// Use this to anchor test-data paths (mock binaries, fixtures) that
// live in the repo at well-known locations relative to the project
// root, regardless of where `go test` is invoked from.
func RepoRoot(t *testing.T) string {
	t.Helper()
	// runtime.Caller(0) gives us this file's location. We start
	// from there and walk up looking for go.mod.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found above %s", filepath.Dir(thisFile))
		}
		dir = parent
	}
}

// RepoPath joins the project root with a relative path. Equivalent
// to filepath.Join(RepoRoot(t), parts...) but reads cleaner at the
// call site: testutil.RepoPath(t, "internal", "pi", "testdata", "mock-pi.sh").
func RepoPath(t *testing.T, parts ...string) string {
	t.Helper()
	return filepath.Join(RepoRoot(t), filepath.Join(parts...))
}