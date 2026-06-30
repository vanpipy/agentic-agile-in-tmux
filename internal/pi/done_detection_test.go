// done_detection_test.go — TDD tests for DetectLastStopReason.
//
// Ticket task/awp PR 2: parse pi session JSONL files to find the
// stopReason of the LAST assistant message, so the poll loop can fire
// per-turn notifications.
//
// Why a new function: parseSessionInfo (session.go:175) intentionally
// bounded its scan at 200 lines ("accuracy beyond summary is not
// needed for the picker UX"). For "find last assistant message" we
// need the entire file — real sessions reach 1226 lines / ~140 KB
// (verified in DONE_DETECTION_RESEARCH.md §2.1).
//
// Implementation note: the function scans from EOF backwards in
// bounded chunks (~4 KB) so we don't have to read the entire file
// each call. The CORRECT self-check below covers the edge cases.
//
// CORRECT-7 self-check:
//   C-onformance: literal stopReason values ("stop" / "toolUse" / "")
//   O-rdering: the LAST assistant message wins, not the first
//   R-ange: empty file, file with no assistant, file with one, file
//           with 1226 (worst observed)
//   R-eference: real filesystem (uses t.TempDir)
//   E-xistence: empty file, missing file
//   C-ardinality: 0/1/many assistant messages
//   T-ime: no time concerns
package pi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSession writes a synthetic pi session JSONL with the given
// entries (each entry is the raw "type" + "message.role" + "message.stopReason"
// tuple). The first line is always the session header.
func writeSession(t *testing.T, dir string, entries []sessionEntry) string {
	t.Helper()
	path := filepath.Join(dir, "session.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create session file: %v", err)
	}
	defer f.Close()

	header := map[string]any{
		"type":      "session",
		"version":   3,
		"id":        "00000000-0000-0000-0000-000000000001",
		"timestamp": "2026-06-28T10:00:00.000Z",
		"cwd":       "/tmp/test",
	}
	hb, _ := json.Marshal(header)
	f.Write(hb)
	f.Write([]byte("\n"))

	for _, e := range entries {
		msg := map[string]any{
			"type": "message",
			"message": map[string]any{
				"role": e.role,
			},
		}
		if e.stopReason != "" {
			msg["message"].(map[string]any)["stopReason"] = e.stopReason
		}
		if e.role == "assistant" && e.content != "" {
			msg["message"].(map[string]any)["content"] = []map[string]any{
				{"type": "text", "text": e.content},
			}
		}
		b, _ := json.Marshal(msg)
		f.Write(b)
		f.Write([]byte("\n"))
	}
	return path
}

type sessionEntry struct {
	role       string // "user" / "assistant" / "toolResult"
	stopReason string // empty for non-assistant messages
	content    string // optional assistant text
}

// TestDetectLastStopReason_LastAssistantWins pins the O-rdering rule:
// the LAST assistant message's stopReason is returned, not the first.
//
// Sequence: user, assistant(toolUse), toolResult, assistant(toolUse),
// toolResult, assistant(stop). Expected: "stop".
func TestDetectLastStopReason_LastAssistantWins(t *testing.T) {
	dir := t.TempDir()
	path := writeSession(t, dir, []sessionEntry{
		{role: "user"},
		{role: "assistant", stopReason: "toolUse", content: "let me think"},
		{role: "toolResult"},
		{role: "assistant", stopReason: "toolUse", content: "read file"},
		{role: "toolResult"},
		{role: "assistant", stopReason: "stop", content: "all done"},
	})

	got, err := DetectLastStopReason(path)
	if err != nil {
		t.Fatalf("DetectLastStopReason: %v", err)
	}
	if got != "stop" {
		t.Errorf("DetectLastStopReason = %q; want %q (must read LAST assistant message, not any)", got, "stop")
	}
}

// TestDetectLastStopReason_EndsWithToolUse guards against a bug where
// the function returns the first "stop" it sees instead of the last.
//
// Sequence: assistant(stop), assistant(toolUse). Expected: "toolUse".
// If implementation scans forward and bails on first match, it returns
// "stop" — wrong, this test would catch it.
func TestDetectLastStopReason_EndsWithToolUse(t *testing.T) {
	dir := t.TempDir()
	path := writeSession(t, dir, []sessionEntry{
		{role: "user"},
		{role: "assistant", stopReason: "stop", content: "first turn done"},
		{role: "user"},
		{role: "assistant", stopReason: "toolUse", content: "second turn still working"},
	})

	got, err := DetectLastStopReason(path)
	if err != nil {
		t.Fatalf("DetectLastStopReason: %v", err)
	}
	if got != "toolUse" {
		t.Errorf("DetectLastStopReason = %q; want %q (must read LAST, not first; turns mixed)", got, "toolUse")
	}
}

// TestDetectLastStopReason_EmptyFile pins the E-xistence edge case.
// An empty file (just the header) is valid — no assistant yet, so ""
// is returned, not an error.
func TestDetectLastStopReason_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	// Write just the header, no entries.
	path := filepath.Join(dir, "session.jsonl")
	header := map[string]any{
		"type":      "session",
		"version":   3,
		"id":        "00000000-0000-0000-0000-000000000001",
		"timestamp": "2026-06-28T10:00:00.000Z",
		"cwd":       "/tmp/test",
	}
	hb, _ := json.Marshal(header)
	if err := os.WriteFile(path, append(hb, '\n'), 0644); err != nil {
		t.Fatalf("write empty session: %v", err)
	}

	got, err := DetectLastStopReason(path)
	if err != nil {
		t.Fatalf("DetectLastStopReason(empty): %v", err)
	}
	if got != "" {
		t.Errorf("DetectLastStopReason on empty session = %q; want \"\"", got)
	}
}

// TestDetectLastStopReason_NoAssistant guards the no-assistant case.
// If the file has only user messages, return "" (not an error).
func TestDetectLastStopReason_NoAssistant(t *testing.T) {
	dir := t.TempDir()
	path := writeSession(t, dir, []sessionEntry{
		{role: "user"},
		{role: "user"},
		{role: "user"},
	})

	got, err := DetectLastStopReason(path)
	if err != nil {
		t.Fatalf("DetectLastStopReason(no assistant): %v", err)
	}
	if got != "" {
		t.Errorf("DetectLastStopReason with no assistant = %q; want \"\"", got)
	}
}

// TestDetectLastStopReason_LongFile guards the R-ange edge case: a
// session with 1226 entries (the worst case observed in real
// ~/.pi/agent/sessions/). Must return the last assistant's stopReason.
func TestDetectLastStopReason_LongFile(t *testing.T) {
	dir := t.TempDir()
	entries := make([]sessionEntry, 0, 1500)
	// 1226 alternating messages — last assistant is "stop".
	// Pattern: user, assistant(toolUse), toolResult, ..., last=user, assistant(stop).
	for i := 0; i < 1224; i++ {
		switch i % 3 {
		case 0:
			entries = append(entries, sessionEntry{role: "user"})
		case 1:
			entries = append(entries, sessionEntry{role: "assistant", stopReason: "toolUse"})
		case 2:
			entries = append(entries, sessionEntry{role: "toolResult"})
		}
	}
	// Last three entries: user, assistant(stop).
	entries = append(entries, sessionEntry{role: "user"})
	entries = append(entries, sessionEntry{role: "assistant", stopReason: "stop", content: "final summary"})
	path := writeSession(t, dir, entries)

	got, err := DetectLastStopReason(path)
	if err != nil {
		t.Fatalf("DetectLastStopReason(long): %v", err)
	}
	if got != "stop" {
		t.Errorf("DetectLastStopReason on 1226-entry session = %q; want %q", got, "stop")
	}
}

// TestDetectLastStopReason_MissingFile ensures missing files return
// an error (not a panic or empty string by accident). The poll loop
// relies on this error to skip the pane's notification gracefully.
func TestDetectLastStopReason_MissingFile(t *testing.T) {
	_, err := DetectLastStopReason("/nonexistent/path/never/exists.jsonl")
	if err == nil {
		t.Error("DetectLastStopReason on missing file returned nil; want non-nil error")
	}
}

// TestDetectLastStopReason_MalformedLine ensures lines that don't
// parse as JSON are skipped (not fatal). Real pi sessions
// occasionally have partial writes that get flushed later; we should
// not crash the poll loop on them.
func TestDetectLastStopReason_MalformedLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer f.Close()

	header := `{"type":"session","version":3,"id":"x","timestamp":"t","cwd":"/x"}`
	f.WriteString(header + "\n")
	// Garbage line.
	f.WriteString("{not valid json\n")
	// Valid user.
	f.WriteString(`{"type":"message","message":{"role":"user"}}` + "\n")
	// Garbage line again.
	f.WriteString("another garbage line\n")
	// Valid assistant with stop.
	f.WriteString(`{"type":"message","message":{"role":"assistant","stopReason":"stop"}}` + "\n")

	got, err := DetectLastStopReason(path)
	if err != nil {
		t.Fatalf("DetectLastStopReason(malformed): %v", err)
	}
	if got != "stop" {
		t.Errorf("DetectLastStopReason with mixed garbage = %q; want %q (must skip malformed lines, not crash)", got, "stop")
	}
}

// TestDetectLastStopReason_IgnoresToolResultStopReason verifies the
// function only inspects assistant messages, not toolResult ones.
// (toolResult can technically carry a stopReason field in some pi
// versions, but it doesn't represent "the agent's turn ended".)
func TestDetectLastStopReason_IgnoresToolResultStopReason(t *testing.T) {
	dir := t.TempDir()
	path := writeSession(t, dir, []sessionEntry{
		{role: "user"},
		{role: "assistant", stopReason: "toolUse"},
		{role: "toolResult", stopReason: "stop"}, // pathological — should be ignored
	})

	got, err := DetectLastStopReason(path)
	if err != nil {
		t.Fatalf("DetectLastStopReason: %v", err)
	}
	if got != "toolUse" {
		t.Errorf("DetectLastStopReason = %q; want %q (toolResult stopReason must be ignored; only assistant role counts)", got, "toolUse")
	}
}

// helper assertion to keep t.Errorf messages terse
func mustContain(t *testing.T, haystack, needle, label string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("%s: got %q, expected to contain %q", label, haystack, needle)
	}
}
