// session_model_change_first_wins_test.go — TDD test for parseSessionInfo's
// "first model_change wins" semantics.
//
// Mutation testing (gremlins, full-mutator run) identified a NOT COVERED
// CONDITIONALS_NEGATION mutation at session.go:297:26 — the
// `if info.ModelProvider == ""` guard is never tested under the condition
// where ModelProvider is already non-empty (because no test writes a JSONL
// with two `model_change` entries).
//
// Original behavior (verified by this test):
//   First model_change entry sets ModelProvider / ModelID.  Subsequent
//   model_change entries are ignored.
//
// Mutated behavior (would fail this test):
//   `== ""` → `!= ""` would skip the first entry (ModelProvider is empty)
//   and adopt the second entry's provider instead.
//
// Per AGENTS.md §2.2 CORRECT-7 self-check:
//   C-onformance: assert exact provider/model strings
//   O-rdering: N/A (set, not sequence)
//   R-ange: 2 model_change entries; one before, one after; one provider each
//   R-eference: t.TempDir() + t.Setenv("HOME", dir) — CASTRATION-2 pattern
//   E-xistence: provider/model both empty → not covered
//   C-ardinality: 2 entries; one expects set, other expects ignored
//   T-ime: no time concerns
package pi

import (
	"path/filepath"
	"testing"
)

// TestParseSessionInfo_ModelChange_FirstWins verifies that the FIRST
// `model_change` entry's provider/model populate SessionInfo, and a
// subsequent `model_change` entry does NOT overwrite them.
//
// This test was added after gremlins reported session.go:297:26
// (CONDITIONALS_NEGATION: `== ""` vs `!= ""`) as NOT COVERED — i.e.
// no existing test exercised the branch where ModelProvider is already
// non-empty when a second model_change entry is encountered.
func TestParseSessionInfo_ModelChange_FirstWins(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir) // CASTRATION-2 — keep HOME-relative lookups inside tmp

	store, err := NewSessionStore(dir)
	if err != nil {
		t.Fatalf("NewSessionStore: %v", err)
	}
	sessionDir := filepath.Join(dir, "sessions", "--tmp--")

	writeSessionJSONL(t, sessionDir, "model-change-wins", "/tmp", []map[string]any{
		// First model_change: this is the one that should "stick".
		{"type": "model_change", "provider": "anthropic", "modelId": "claude-opus-4"},
		// Second model_change: must be IGNORED. Under the gremlins
		// CONDITIONALS_NEGATION mutation `== ""` → `!= ""`, this one
		// would clobber the first.
		{"type": "model_change", "provider": "openai", "modelId": "gpt-4o"},
	})

	sessions, err := store.List("/tmp")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("want 1 session, got %d", len(sessions))
	}

	got := sessions[0]
	if got.ModelProvider != "anthropic" {
		t.Errorf("ModelProvider = %q, want %q (first model_change wins)",
			got.ModelProvider, "anthropic")
	}
	if got.ModelID != "claude-opus-4" {
		t.Errorf("ModelID = %q, want %q (first model_change wins)",
			got.ModelID, "claude-opus-4")
	}
}

// TestParseSessionInfo_ThinkingLevel_FirstWins — same pattern, for the
// thinking_level_change branch at session.go:302:9 (also a NOT COVERED
// CONDITIONALS_NEGATION in the all-mutator gremlins run).
//
// Pins the contract: the FIRST `thinking_level_change` entry wins.
// Subsequent entries are ignored. This is the same "first wins" pattern
// as model_change and exists for the same reason — if a session changes
// its model mid-flight, the recorded provider in the index reflects the
// initial model, not the most recent one.
func TestParseSessionInfo_ThinkingLevel_FirstWins(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir) // CASTRATION-2

	store, err := NewSessionStore(dir)
	if err != nil {
		t.Fatalf("NewSessionStore: %v", err)
	}
	sessionDir := filepath.Join(dir, "sessions", "--tmp--")

	writeSessionJSONL(t, sessionDir, "thinking-first-wins", "/tmp", []map[string]any{
		{"type": "thinking_level_change", "level": "high"},
		{"type": "thinking_level_change", "level": "minimal"},
	})

	sessions, err := store.List("/tmp")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("want 1 session, got %d", len(sessions))
	}

	got := sessions[0]
	if got.ThinkingLevel != "high" {
		t.Errorf("ThinkingLevel = %q, want %q (first thinking_level_change wins)",
			got.ThinkingLevel, "high")
	}
}