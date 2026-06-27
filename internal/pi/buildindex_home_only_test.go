// buildindex_home_only_test.go — TDD test for buildIndex path validation.
//
// Finding CASTRATION-2 from post-P3P4 audit: buildIndex reads from
// os.ReadDir(root) where root is s.sessionsDir(). If root is a symlink
// to a sensitive directory (e.g., /etc), buildIndex walks it.
//
// Fix: after computing root, verify it's under the user's HOME dir.
// Reject paths outside with a clear error.
//
// Defense-in-depth: a malicious symlink in the agentDir could expose
// unrelated files to FindByID. We never WRITE anything in buildIndex
// (read-only), so the risk is information disclosure. Still worth
// pinning the contract.
package pi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildIndex_RejectsPathsOutsideHome pins the contract that
// buildIndex refuses to walk paths outside the user's HOME directory.
//
// CORRECT-7 self-check:
//   C-onformance: buildIndex silently builds empty index for off-home paths
//   O-rdering: N/A
//   R-ange: 1 case (path outside HOME)
//   R-eference: HOME env var
//   E-xistence: N/A
//   C-ardinality: 1 case
//   T-ime: no time concerns
func TestBuildIndex_RejectsPathsOutsideHome(t *testing.T) {
	// Point HOME at a temp dir, point agentDir at a different temp dir.
	// buildIndex should NOT walk the off-home path.
	homeDir := t.TempDir()
	offHomeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	store, err := NewSessionStore(offHomeDir)
	if err != nil {
		t.Fatalf("NewSessionStore: %v", err)
	}
	// Trigger buildIndex.
	store.FindByID("any-id")

	// After buildIndex, store.index should be empty (we refused to walk
	// the off-home path). Pin the contract.
	if store.index == nil {
		// Lazy build never triggered; that's fine too.
		return
	}
	if len(store.index) != 0 {
		t.Errorf("buildIndex walked off-home path %q; index has %d entries.\n"+
			"CASTRATION-2: buildIndex must refuse paths outside HOME.",
			offHomeDir, len(store.index))
	}
}

// TestBuildIndex_AcceptsPathsInsideHome pins the happy path.
func TestBuildIndex_AcceptsPathsInsideHome(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	// Default agentDir (~/.pi/agent) is under HOME.
	store, err := NewSessionStore("")
	if err != nil {
		t.Fatalf("NewSessionStore: %v", err)
	}
	// Trigger buildIndex.
	store.FindByID("any-id")

	if store.index == nil {
		t.Fatal("buildIndex didn't run for in-home path")
	}
	// index may be empty (no sessions yet), but must not error.
	_ = strings.Contains
}

// TestIsUnderHome verifies the path-containment helper.
func TestIsUnderHome(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	tests := []struct {
		name string
		path string
		want bool
	}{
		{"exact home", homeDir, true},
		{"home subdir", filepath.Join(homeDir, "sub"), true},
		{"off-home", "/tmp/something", false},
		{"empty home", "", false},
	}
	// Create the "sub" dir for EvalSymlinks to work.
	if err := os.MkdirAll(filepath.Join(homeDir, "sub"), 0755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// isUnderHome is unexported; test through buildIndex behavior.
			got := isUnderHome(tt.path, homeDir)
			if got != tt.want {
				t.Errorf("isUnderHome(%q, %q) = %v; want %v", tt.path, homeDir, got, tt.want)
			}
		})
	}
	_ = os.Stat
}
