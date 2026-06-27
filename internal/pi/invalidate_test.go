// invalidate_test.go — TDD tests for SessionStore.Invalidate() and IsIndexed().
//
// Finding DEATH-1 follow-up: add explicit Invalidate() method so callers
// can force a rebuild if the index goes stale (e.g., long-running
// processes where files are added/removed externally).
package pi

import (
	"testing"
)

// TestSessionStore_InvalidateForcesRebuild pins the contract that
// Invalidate() resets the index state so the next FindByID rebuilds.
func TestSessionStore_InvalidateForcesRebuild(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	store, err := NewSessionStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSessionStore: %v", err)
	}

	// Initially not indexed.
	if store.IsIndexed() {
		t.Error("store.IsIndexed() = true before any FindByID; want false")
	}

	// Trigger a build.
	_, _ = store.FindByID("nonexistent")
	if !store.IsIndexed() {
		t.Error("store.IsIndexed() = false after FindByID; want true (index was built)")
	}

	// Invalidate.
	store.Invalidate()
	if store.IsIndexed() {
		t.Error("store.IsIndexed() = true after Invalidate; want false (index reset)")
	}

	// Next FindByID rebuilds.
	_, _ = store.FindByID("nonexistent")
	if !store.IsIndexed() {
		t.Error("store.IsIndexed() = false after FindByID following Invalidate; want true (rebuilt)")
	}
}
