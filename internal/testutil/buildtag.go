package testutil

import (
	"runtime"
	"testing"
)

// RequireLinux skips a test if it requires Linux/macOS-only behavior.
// Available unconditionally (no build tag) so unit and integration
// tests can both use it.
//
// Use as the first line in Test functions that need POSIX-specific
// features (PTY, signals, etc.).
func RequireLinux(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skipf("requires Linux/macOS, got %s", runtime.GOOS)
	}
}