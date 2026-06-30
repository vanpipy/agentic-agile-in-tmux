// turn_done_cache_test.go — TDD tests for TurnDoneCache edge detector.
//
// Ticket task/awp PR 2 (step 3): a per-pane cache that tracks the
// last observed stopReason so the poll loop can fire ONE notification
// per "toolUse → stop" transition (not on every poll cycle that
// happens to see "stop").
//
// The cache also tracks the file's last mtime + byte offset so
// subsequent polls can avoid re-reading unchanged portions of the
// JSONL — see DONE_DETECTION_RESEARCH.md §5 R2.

package pi

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestTurnDoneCache_UpdateToolUseToStopFires pins the core contract:
// the FIRST time we see stopReason="stop" (after a non-stop value),
// Update returns true. This is the transition that fires the
// notification.
func TestTurnDoneCache_UpdateToolUseToStopFires(t *testing.T) {
	c := NewTurnDoneCache("/some/path.jsonl")

	// First observation: toolUse. Not yet done.
	if c.Update("toolUse", 1000, time.Now()) {
		t.Error("after observing 'toolUse', Update should return false")
	}

	// Second observation: stop. THIS is the transition.
	if !c.Update("stop", 2000, time.Now()) {
		t.Error("after transition toolUse → stop, Update should return true")
	}
}

// TestTurnDoneCache_UpdateStopToStopSilent pins the dedup contract:
// once we've fired a notification for "stop", subsequent readings of
// the same "stop" must NOT re-fire. Otherwise the user would get a
// toast every 5s for the same idle state.
func TestTurnDoneCache_UpdateStopToStopSilent(t *testing.T) {
	c := NewTurnDoneCache("/some/path.jsonl")
	if !c.Update("stop", 1000, time.Now()) {
		t.Fatal("first 'stop' should fire (from initial empty state)")
	}
	if c.Update("stop", 2000, time.Now()) {
		t.Error("second 'stop' on the same cycle should NOT re-fire (dedup)")
	}
}

// TestTurnDoneCache_UpdateStopToToolUseResets pins the resumption
// contract: if the agent re-engages after a stop, the next stop
// transition should fire again (it's a NEW turn).
func TestTurnDoneCache_UpdateStopToToolUseResets(t *testing.T) {
	c := NewTurnDoneCache("/some/path.jsonl")
	if !c.Update("stop", 1000, time.Now()) {
		t.Fatal("expected first stop to fire")
	}
	if c.Update("toolUse", 2000, time.Now()) {
		t.Error("transition stop → toolUse should NOT fire")
	}
	if !c.Update("stop", 3000, time.Now()) {
		t.Error("after re-engaging and finishing again, a new turn-done should fire")
	}
}

// TestTurnDoneCache_EmptyToStopFirstObservation pins the special
// case where the cache is fresh and we immediately see "stop"
// (e.g., resuming a session that was already idle). The user
// expects a notification in this case too — "your resumed session
// is already waiting for input".
func TestTurnDoneCache_EmptyToStopFirstObservation(t *testing.T) {
	c := NewTurnDoneCache("/some/path.jsonl")
	if !c.Update("stop", 1000, time.Now()) {
		t.Error("first observation of 'stop' on a fresh cache should fire " +
			"(resumed-idle-session is a notification-worthy event)")
	}
}

// TestTurnDoneCache_IsStaleByMtime pins the mtime staleness check
// the poll loop uses to skip work when the file hasn't been written.
func TestTurnDoneCache_IsStaleByMtime(t *testing.T) {
	c := NewTurnDoneCache("/some/path.jsonl")
	t1 := time.Now()
	c.Update("toolUse", 100, t1)

	// Same mtime → not stale.
	if c.IsStale(t1, 100) {
		t.Error("IsStale with same mtime + same offset should be false")
	}

	// Same mtime, larger offset → stale (file grew even if mtime
	// didn't tick because of coarse-resolution filesystems).
	if !c.IsStale(t1, 200) {
		t.Error("IsStale with same mtime but larger offset should be true")
	}

	// Larger mtime, same offset → stale.
	t2 := t1.Add(time.Second)
	if !c.IsStale(t2, 100) {
		t.Error("IsStale with larger mtime should be true")
	}

	// Smaller mtime and offset → not stale (clock drift / same-file).
	if c.IsStale(t1.Add(-time.Second), 50) {
		t.Error("IsStale with smaller mtime should be false (cache is at least as fresh)")
	}
}

// TestTurnDoneCache_NewFromExistingFile pins the "resume awp after
// restart" case: existing session file should populate the cache
// without firing a notification (the user was already aware of the
// state — no point re-pinging them on startup).
func TestTurnDoneCache_NewFromExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	// Write a small session whose last stopReason is "stop".
	writeSimpleSession(t, path, "user", "assistant:toolUse", "toolResult", "assistant:stop")

	stat, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	c, err := NewTurnDoneCacheFromFile(path)
	if err != nil {
		t.Fatalf("NewTurnDoneCacheFromFile: %v", err)
	}
	if c.lastStopReason != "stop" {
		t.Errorf("loaded cache lastStopReason = %q; want %q", c.lastStopReason, "stop")
	}
	if c.lastOffset != stat.Size() {
		t.Errorf("loaded cache lastOffset = %d; want %d", c.lastOffset, stat.Size())
	}
	if !c.lastMtime.Equal(stat.ModTime()) {
		t.Errorf("loaded cache lastMtime = %v; want %v", c.lastMtime, stat.ModTime())
	}
}

// TestTurnDoneCache_NewFromMissingFile pins the missing-file edge:
// awp started up, session hasn't been created yet (pi hasn't written
// anything). Cache should be empty (all zeros), no error.
func TestTurnDoneCache_NewFromMissingFile(t *testing.T) {
	c, err := NewTurnDoneCacheFromFile("/nonexistent/never/exists.jsonl")
	if err != nil {
		t.Fatalf("NewTurnDoneCacheFromFile(missing): %v", err)
	}
	if c.lastStopReason != "" {
		t.Errorf("missing-file cache lastStopReason = %q; want \"\"", c.lastStopReason)
	}
	if c.lastOffset != 0 {
		t.Errorf("missing-file cache lastOffset = %d; want 0", c.lastOffset)
	}
}

// writeSimpleSession creates a minimal session JSONL with the given
// entries. Each entry is "role" or "role:stopReason" (for assistant
// entries). Useful for cache tests where we don't care about content.
func writeSimpleSession(t *testing.T, path string, entries ...string) {
	t.Helper()
	content := `{"type":"session","version":3,"id":"x","timestamp":"t","cwd":"/x"}` + "\n"
	for _, e := range entries {
		role := e
		stopReason := ""
		if idx := indexByte(e, ':'); idx >= 0 {
			role = e[:idx]
			stopReason = e[idx+1:]
		}
		if stopReason == "" {
			content += `{"type":"message","message":{"role":"` + role + `"}}` + "\n"
		} else {
			content += `{"type":"message","message":{"role":"` + role + `","stopReason":"` + stopReason + `"}}` + "\n"
		}
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write session: %v", err)
	}
}

// indexByte is indexByte without importing "strings" (cheaper than the
// import for a one-call helper in a test file).
func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}
