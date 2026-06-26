//go:build e2e

package testutil

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// repoRoot walks up from this source file until it finds a go.mod
// and returns the containing directory. Robust to test being run
// from any CWD (project root, subdir, or elsewhere) and to the
// helper package being in a different directory than the test
// package calling it.
func repoRoot(t *testing.T) string {
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

// AwpBin returns the path to the awp binary, building it from source.
// Used by e2e tests that exercise the full CLI surface.
//
// Build tag //go:build e2e: this helper is only compiled into test
// binaries that opt in via `go test -tags e2e`. Unit/integration
// test runs don't pay the import cost.
func AwpBin(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "awp")
	root := repoRoot(t)
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build awp (in %s): %v\n%s", root, err, out)
	}
	return bin
}

// RunAwp runs the awp binary with the given args and returns
// stdout, stderr, and exit code. Uses a clean env per call so
// tests don't pollute each other (each call gets its own AWP_CONFIG_DIR).
func RunAwp(t *testing.T, bin string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(),
		"AWP_CONFIG_DIR="+t.TempDir(),
		"PATH="+os.Getenv("PATH"),
	)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	} else if err == nil {
		exitCode = 0
	} else {
		t.Fatalf("run awp: %v", err)
	}
	return out.String(), errOut.String(), exitCode
}