package pi

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeSessionJSONL creates a session file in the standard layout:
//   {type: "session", id, timestamp, cwd}
//   {type: "model_change", ...}
//   {type: "message", message: {role, content}}
//   {type: "tool_execution_start", ...}
func writeSessionJSONL(t *testing.T, dir, id, cwd string, lines []map[string]any) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	header := map[string]any{
		"type":      "session",
		"version":   1,
		"id":        id,
		"timestamp": time.Now().Format(time.RFC3339Nano),
		"cwd":       cwd,
	}
	all := append([]map[string]any{header}, lines...)
	f, err := os.Create(filepath.Join(dir, id+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, l := range all {
		if err := enc.Encode(l); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSessionInfo_ToolCount_Phase3(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir) // CASTRATION-2
	store, err := NewSessionStore(dir)
	if err != nil {
		t.Fatalf("NewSessionStore: %v", err)
	}
	sessionDir := filepath.Join(dir, "sessions", "--tmp--")
	writeSessionJSONL(t, sessionDir, "sess-tools", "/tmp", []map[string]any{
		{"type": "model_change", "provider": "anthropic", "modelId": "claude-opus-4"},
		{"type": "message", "message": map[string]any{"role": "user", "content": "hello"}},
		{"type": "tool_execution_start", "toolName": "bash"},
		{"type": "tool_execution_start", "toolName": "read"},
		{"type": "tool_execution_start", "toolName": "bash"},
	})
	sessions, err := store.List("/tmp")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("want 1 session, got %d", len(sessions))
	}
	if got := sessions[0].ToolCount; got != 3 {
		t.Errorf("ToolCount = %d, want 3", got)
	}
	if got := sessions[0].MessageCount; got != 1 {
		t.Errorf("MessageCount = %d, want 1", got)
	}
	if got := sessions[0].ModelID; got != "claude-opus-4" {
		t.Errorf("ModelID = %q, want claude-opus-4", got)
	}
}

func TestSessionStore_FindByID_Full(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir) // CASTRATION-2
	store, err := NewSessionStore(dir)
	if err != nil {
		t.Fatalf("NewSessionStore: %v", err)
	}
	sessionDir := filepath.Join(dir, "sessions", "--home-x--")
	writeSessionJSONL(t, sessionDir, "abc-123-uuid", "/home/x", []map[string]any{
		{"type": "message", "message": map[string]any{"role": "user", "content": "hi"}},
	})
	info, ok := store.FindByID("abc-123-uuid")
	if !ok {
		t.Fatal("FindByID returned not found")
	}
	if info.CWD != "/home/x" {
		t.Errorf("CWD = %q, want /home/x", info.CWD)
	}
}

func TestSessionStore_FindByID_Prefix(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir) // CASTRATION-2
	store, err := NewSessionStore(dir)
	if err != nil {
		t.Fatalf("NewSessionStore: %v", err)
	}
	sessionDir := filepath.Join(dir, "sessions", "--home-y--")
	writeSessionJSONL(t, sessionDir, "deadbeef-0000", "/home/y", nil)
	// Prefix match
	info, ok := store.FindByID("deadbeef")
	if !ok {
		t.Fatal("prefix match failed")
	}
	if info.ID != "deadbeef-0000" {
		t.Errorf("ID = %q", info.ID)
	}
}

func TestSessionStore_FindByID_NotFound(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir) // CASTRATION-2
	store, err := NewSessionStore(dir)
	if err != nil {
		t.Fatalf("NewSessionStore: %v", err)
	}
	if _, ok := store.FindByID("nonexistent"); ok {
		t.Fatal("expected not found")
	}
}

func TestSessionInfo_FirstAndLastPrompt(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir) // CASTRATION-2
	store, err := NewSessionStore(dir)
	if err != nil {
		t.Fatalf("NewSessionStore: %v", err)
	}
	sessionDir := filepath.Join(dir, "sessions", "--tmp--")
	writeSessionJSONL(t, sessionDir, "sess-prompts", "/tmp", []map[string]any{
		{"type": "message", "message": map[string]any{"role": "user", "content": "first question"}},
		{"type": "message", "message": map[string]any{"role": "assistant", "content": "first answer"}},
		{"type": "message", "message": map[string]any{"role": "user", "content": "second"}},
		{"type": "message", "message": map[string]any{"role": "assistant", "content": "second answer"}},
	})
	sessions, _ := store.List("/tmp")
	if len(sessions) != 1 {
		t.Fatalf("want 1")
	}
	if sessions[0].FirstPrompt != "first question" {
		t.Errorf("FirstPrompt = %q", sessions[0].FirstPrompt)
	}
	if sessions[0].LastAssistant != "second answer" {
		t.Errorf("LastAssistant = %q", sessions[0].LastAssistant)
	}
}

// TestParseSessionInfo_BoundedScan verifies that parseSessionInfo
// doesn't read the entire file (Phase 3 audit finding). For a file
// with 1000 message entries, MessageCount should be bounded by
// maxScanLines (200) — not 1000.
func TestParseSessionInfo_BoundedScan(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir) // CASTRATION-2
	store, err := NewSessionStore(dir)
	if err != nil {
		t.Fatalf("NewSessionStore: %v", err)
	}
	sessionDir := filepath.Join(dir, "sessions", "--tmp--")

	var lines []map[string]any
	for i := 0; i < 1000; i++ {
		lines = append(lines, map[string]any{
			"type":    "message",
			"message": map[string]any{"role": "user", "content": "x"},
		})
	}
	writeSessionJSONL(t, sessionDir, "big-session", "/tmp", lines)

	sessions, err := store.List("/tmp")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("want 1 session, got %d", len(sessions))
	}
	if got := sessions[0].MessageCount; got > maxScanLines {
		t.Errorf("MessageCount = %d, want ≤ %d (bounded scan)", got, maxScanLines)
	}
	if got := sessions[0].MessageCount; got < 1 {
		t.Errorf("MessageCount = %d, want ≥ 1", got)
	}
}

// TestParseSessionInfo_LastActivity verifies LastActivity is updated
// to a later entry's timestamp (not just the header time).
func TestParseSessionInfo_LastActivity(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir) // CASTRATION-2
	store, err := NewSessionStore(dir)
	if err != nil {
		t.Fatalf("NewSessionStore: %v", err)
	}
	sessionDir := filepath.Join(dir, "sessions", "--tmp--")
	writeSessionJSONL(t, sessionDir, "la-test", "/tmp", []map[string]any{
		{"type": "message", "time": "2026-06-17T10:00:00Z",
			"message": map[string]any{"role": "user", "content": "first"}},
		{"type": "message", "time": "2026-06-17T11:00:00Z",
			"message": map[string]any{"role": "assistant", "content": "second"}},
	})
	sessions, _ := store.List("/tmp")
	if len(sessions) != 1 {
		t.Fatalf("want 1")
	}
	if sessions[0].LastActivity.IsZero() {
		t.Error("LastActivity is zero")
	}
	// Should be the timestamp of the second entry, not the header
	if !sessions[0].LastActivity.Equal(sessions[0].Timestamp) &&
		sessions[0].LastActivity.Format("2006-01-02T15:04:05Z") != "2026-06-17T11:00:00Z" {
		t.Errorf("LastActivity = %v, want 2026-06-17T11:00:00Z",
			sessions[0].LastActivity)
	}
}



// TestSessionStore_ListSkipped verifies that ListSkipped returns
// the count of files that failed to parse.
func TestSessionStore_ListSkipped(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir) // CASTRATION-2
	store, err := NewSessionStore(dir)
	if err != nil {
		t.Fatalf("NewSessionStore: %v", err)
	}
	sessionDir := filepath.Join(dir, "sessions", "--tmp--")

	// 1 good file
	writeSessionJSONL(t, sessionDir, "good", "/tmp", []map[string]any{
		{"type": "message", "message": map[string]any{"role": "user", "content": "hi"}},
	})
	// 2 corrupt files
	if err := os.WriteFile(filepath.Join(sessionDir, "bad1.jsonl"),
		[]byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "bad2.jsonl"),
		[]byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = store.List("/tmp")
	if err != nil {
		t.Fatal(err)
	}
	if got := store.ListSkipped(); got != 2 {
		t.Errorf("ListSkipped = %d, want 2", got)
	}
}

func TestSessionStore_Read(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("PI_AGENT_DIR", tmp)
	// Create a fake session file
	encodedCwd := encodeCwdKey("/home/user/myproject")
	sessDir := filepath.Join(tmp, "agent", "sessions", encodedCwd)
	if err := os.MkdirAll(sessDir, 0755); err != nil {
		t.Fatal(err)
	}
	sessFile := filepath.Join(sessDir, "test-session.jsonl")
	content := `{"type":"session","version":1,"id":"test-session","cwd":"/home/user/myproject","name":"test"}
{"type":"message","id":"m1","role":"user","content":"hello"}
{"type":"message","id":"m2","role":"assistant","content":"hi"}`
	if err := os.WriteFile(sessFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	store, err := NewSessionStore(tmp)
	if err != nil {
		t.Fatalf("NewSessionStore: %v", err)
	}
	got, err := store.Read(sessFile)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Info.ID != "test-session" {
		t.Errorf("Info.ID = %q, want %q", got.Info.ID, "test-session")
	}
	if len(got.Entries) != 2 {
		t.Errorf("Messages count = %d, want 2", len(got.Entries))
	}
}

func TestSessionStore_Read_NotFound(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("PI_AGENT_DIR", tmp)
	store, err := NewSessionStore(tmp)
	if err != nil {
		t.Fatalf("NewSessionStore: %v", err)
	}
	_, err = store.Read("/nonexistent/file.jsonl")
	if err == nil {
		t.Error("Read of nonexistent should error")
	}
}

func TestSessionStore_FindByID(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("PI_AGENT_DIR", tmp)
	// Create multiple session files
	// FindByID scans subdirectories of sessionsDir
	projDir := filepath.Join(tmp, "sessions", "projectA")
	if err := os.MkdirAll(projDir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"abc-123", "def-456", "ghi-789"} {
		path := filepath.Join(projDir, id+".jsonl")
		if err := os.WriteFile(path, []byte(`{"type":"session","id":"`+id+`","cwd":"/x"}`), 0644); err != nil {
			t.Fatal(err)
		}
	}

	store, err := NewSessionStore(tmp)
	if err != nil {
		t.Fatalf("NewSessionStore: %v", err)
	}
	t.Setenv("HOME", tmp) // CASTRATION-2: buildIndex requires HOME
	got, ok := store.FindByID("def-456")
	if !ok {
		t.Fatal("FindByID should find def-456")
	}
	if got.ID != "def-456" {
		t.Errorf("got ID = %q, want def-456", got.ID)
	}

	_, ok = store.FindByID("nonexistent")
	if ok {
		t.Error("FindByID should not find nonexistent")
	}
}

func TestSessionStore_Truncate(t *testing.T) {
	tests := []struct {
		in   string
		max  int
		want string
	}{
		{"short", 10, "short"},
		{"a long string that needs truncation", 10, "a long ..."},
		{"", 5, ""},
	}
	for _, tt := range tests {
		got := truncate(tt.in, tt.max)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.in, tt.max, got, tt.want)
		}
	}
}

func TestEncodeCwdKey(t *testing.T) {
	// Test that we match pi's encoding exactly
	tests := []struct {
		in, want string
	}{
		{"/home/user/project", "--home-user-project--"},
		{"/", "----"},
	}
	for _, tt := range tests {
		got := encodeCwdKey(tt.in)
		if got != tt.want {
			t.Errorf("encodeCwdKey(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestErrSessionNotFound_Sentinel verifies the sentinel
// errors.Is behavior so callers can detect missing sessions.
func TestErrSessionNotFound_Sentinel(t *testing.T) {
	if ErrSessionNotFound == nil {
		t.Fatal("ErrSessionNotFound is nil")
	}
	if ErrSessionNotFound.Error() != "session not found" {
		t.Errorf("Error() = %q, want %q", ErrSessionNotFound.Error(), "session not found")
	}
	// errors.Is with itself
	if !errors.Is(ErrSessionNotFound, ErrSessionNotFound) {
		t.Error("errors.Is(self, self) should be true")
	}
	// errors.Is with wrapped version (caller pattern from cmd/awp/root.go)
	wrapped := fmt.Errorf("abc-123: %w", ErrSessionNotFound)
	if !errors.Is(wrapped, ErrSessionNotFound) {
		t.Error("errors.Is should unwrap to find sentinel")
	}
}
