// findbyid_index_test.go — TDD test pinning FindByID's lazy index contract.
//
// FindByID now uses a lazy in-memory index (built once via sync.Once)
// for O(1) lookups. This test pins:
//   - The index is built on first call (state change observable via
//     repeated FindByID calls returning same result)
//   - Prefix matching (≥8 chars) still works
//   - Non-existent IDs return (zero, false)
package pi

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestFindByID_BuildsIndexOnFirstCall pins the lazy-index contract.
// After the first FindByID, subsequent calls return the same SessionInfo
// (proving the index is being reused).
func TestFindByID_BuildsIndexOnFirstCall(t *testing.T) {
	// Set up a fake pi agent dir with one session file.
	tmpDir := t.TempDir()
	writeFakeSessionFile(t, tmpDir, "--fake--", "fake-uuid-1234567890ab")

	store, err := NewSessionStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSessionStore: %v", err)
	}

	// First call: builds index, returns the session.
	info1, ok := store.FindByID("fake-uuid-1234567890ab")
	if !ok {
		t.Fatal("first FindByID failed; expected to find fake session")
	}

	// Second call: should return the same SessionInfo via the index.
	info2, ok := store.FindByID("fake-uuid-1234567890ab")
	if !ok {
		t.Fatal("second FindByID failed; expected to find same session via index")
	}
	if info1.ID != info2.ID || info1.CWD != info2.CWD {
		t.Errorf("index returned different SessionInfo:\n"+
			"first:  %+v\n"+
			"second: %+v", info1, info2)
	}

	// The index map must be non-nil after at least one FindByID call.
	if store.index == nil {
		t.Error("store.index is nil after FindByID; expected lazy initialization")
	}
	if len(store.index) == 0 {
		t.Error("store.index is empty after FindByID; expected ≥1 entry")
	}
}

// TestFindByID_PrefixMatchStillWorks pins the backward-compatible prefix
// matching (≥8 chars) after the lazy-index refactor.
func TestFindByID_PrefixMatchStillWorks(t *testing.T) {
	tmpDir := t.TempDir()
	writeFakeSessionFile(t, tmpDir, "--fake--", "abcdef1234567890xyz")

	store, err := NewSessionStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSessionStore: %v", err)
	}

	// Prefix "abcdef12" (8 chars) should match.
	info, ok := store.FindByID("abcdef12")
	if !ok {
		t.Fatal("prefix match failed; expected to find session by 8-char prefix")
	}
	if info.ID != "abcdef1234567890xyz" {
		t.Errorf("got ID %q; want %q", info.ID, "abcdef1234567890xyz")
	}

	// 7-char prefix should NOT match (ambiguity guard).
	if _, ok := store.FindByID("abcdef1"); ok {
		t.Error("7-char prefix should not match; the contract is ≥8 chars")
	}
}

// TestFindByID_NotFound pins the miss contract.
func TestFindByID_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	writeFakeSessionFile(t, tmpDir, "--fake--", "real-session-id-1234")

	store, err := NewSessionStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSessionStore: %v", err)
	}

	if _, ok := store.FindByID("nonexistent-id-xyz"); ok {
		t.Error("FindByID returned ok=true for non-existent ID")
	}
}

// writeFakeSessionFile creates a minimal valid session .jsonl file in the
// expected directory layout (sessions/<encoded-cwd>/<id>.jsonl).
func writeFakeSessionFile(t *testing.T, agentDir, encodedCwd, sessionID string) {
	t.Helper()
	dir := filepath.Join(agentDir, "sessions", encodedCwd)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, sessionID+".jsonl")
	// Minimal session header (pi-mono format).
	now := time.Now().UTC().Format(time.RFC3339Nano)
	content := `{"type":"session","version":1,"id":"` + sessionID + `","timestamp":"` + now + `","cwd":"/test/repo"}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write session: %v", err)
	}
}