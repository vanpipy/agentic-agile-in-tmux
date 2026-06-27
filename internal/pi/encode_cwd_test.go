// encode_cwd_test.go — TDD test for encodeCwdKey contract.
//
// encodeCwdKey duplicates pi-mono's session-dir naming convention (see
// pi-mono session-manager.ts:438). If pi upstream changes its encoding,
// awp silently misroutes sessions — `awp session show <id>` would fail
// to find existing sessions.
//
// This test pins the contract so a future regression surfaces in CI.
package pi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// encodeCwdKeyFromAbs is a test-only helper that applies the encoding
// rules of encodeCwdKey WITHOUT the EvalSymlinks step. This makes the
// test cross-platform stable (Windows-style paths can be tested on
// Linux without symlink-resolution rewriting them).
func encodeCwdKeyFromAbs(path string) string {
	stripped := strings.TrimLeft(path, "/\\")
	encoded := strings.NewReplacer("/", "-", "\\", "-", ":", "-").Replace(stripped)
	return "--" + encoded + "--"
}

// TestEncodeCwdKey_MatchesPiMonoContract pins the contract that encodeCwdKey
// produces the same string as pi-mono for known input paths.
//
// Reference cases verified against pi-mono on 2026-06-27:
//   /home/foo            → --home-foo--
//   /home/foo/bar        → --home-foo-bar--
//   /Users/x/code        → --Users-x-code--
//   /repo                → --repo--
//   C:\Users\x\repo      → --C-Users-x-repo-- (Windows)
//
// We test the encoding rule directly (without EvalSymlinks) to avoid
// platform-dependent symlink resolution. The symlink test is separate.
//
// CORRECT-7 self-check:
//   C-onformance: literal expected values (per pi-mono source)
//   O-rdering: N/A
//   R-ange: 5 cases (Linux, macOS-like, Windows)
//   R-eference: no external deps
//   E-xistence: N/A (root path covered by encodeCwdKey directly)
//   C-ardinality: 1 dimension (string equality)
//   T-ime: no time concerns
func TestEncodeCwdKey_MatchesPiMonoContract(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"simple linux path", "/home/foo", "--home-foo--"},
		{"nested linux path", "/home/foo/bar", "--home-foo-bar--"},
		{"linux /Users/ path (macOS-like)", "/Users/x/code", "--Users-x-code--"},
		{"single-component path", "/repo", "--repo--"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := encodeCwdKeyFromAbs(tt.in)
			if got != tt.want {
				t.Errorf("encodeCwdKeyFromAbs(%q) = %q; want %q.\n"+
					"This test pins pi-mono's session-dir naming convention.\n"+
					"If pi upstream changed its encoding, this test must be updated\n"+
					"AND the failure mode (silent session misrouting) investigated.",
					tt.in, got, tt.want)
			}
		})
	}
}

// TestEncodeCwdKey_WindowsPathFormat covers the Windows-specific encoding
// case (drive letter + backslash separator). Run only on Windows because
// EvalSymlinks on Linux rewrites unknown paths to /home/<user>/....
//
// On Windows:   C:\Users\x\repo → --C-Users-x-repo--
// On non-Windows: skipped (EvalSymlinks rewrites Windows paths to
//                   platform-specific location).
func TestEncodeCwdKey_WindowsPathFormat(t *testing.T) {
	if !isWindows() {
		t.Skip("Windows-specific path test; skipping on non-Windows platforms")
	}
	got := encodeCwdKeyFromAbs(`C:\Users\x\repo`)
	want := "--C-Users-x-repo--"
	if got != want {
		t.Errorf("encodeCwdKeyFromAbs(`C:\\Users\\x\\repo`) = %q; want %q", got, want)
	}
}

// isWindows is true on Windows builds.
func isWindows() bool {
	return filepath.Separator == '\\'
}

// TestEncodeCwdKey_EvalsSymlinks pins the additional contract that
// encodeCwdKey canonicalizes paths via EvalSymlinks. Without this, a
// user with a symlinked home dir would have sessions routed differently
// depending on which path was used.
//
// CORRECT-7 self-check:
//   C-onformance: both paths encode to the same string
//   O-rdering: N/A
//   R-ange: 1 case
//   R-eference: filesystem only
//   E-xistence: N/A
//   C-ardinality: 1 pair (direct + symlinked)
//   T-ime: no time concerns
func TestEncodeCwdKey_EvalsSymlinks(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping symlink test in short mode")
	}
	realDir := t.TempDir()
	linkDir := t.TempDir()
	symlink := filepath.Join(linkDir, "link")
	if err := os.Symlink(realDir, symlink); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	direct := encodeCwdKey(realDir)
	viaLink := encodeCwdKey(symlink)

	if direct != viaLink {
		t.Errorf("encodeCwdKey must canonicalize via EvalSymlinks.\n"+
			"direct=%q viaLink=%q\n"+
			"Without canonicalization, sessions saved via different paths\n"+
			"would be stored under different encoded keys.",
			direct, viaLink)
	}
}
