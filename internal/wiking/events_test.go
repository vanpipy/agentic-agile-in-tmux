// events_test.go — RED tests for the events protocol.
//
// Per SYSTEM_DESIGN.md §18.6: JSONL append-only log. Each line is one
// Event with envelope fields (v, id, ts, type) plus per-type optional
// payload fields. Atomic per-line Write + Sync. Schema-version gating
// on the reader side: v > 1 skipped (future-version), v < 1 rejected
// (incompatible).
//
// ULID-shaped ids are 26 chars: 10-char timestamp (ms, base32) +
// 16-char randomness (80 bits). Lex-sortable by time to ms resolution.

package wiking

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// ULID generation.

func TestNewID_LengthAndUniqueness(t *testing.T) {
	const n = 1000
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		id := NewID()
		if len(id) != 26 {
			t.Fatalf("iteration %d: id %q length %d, want 26", i, id, len(id))
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate id %q at iteration %d", id, i)
		}
		seen[id] = struct{}{}
	}
}

func TestNewID_LexSortableByTime(t *testing.T) {
	const n = 50
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		ids = append(ids, NewID())
		time.Sleep(1 * time.Millisecond)
	}
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	for i := 0; i < n; i++ {
		if ids[i] != sorted[i] {
			t.Fatalf("index %d: original %q vs sorted %q (not lex-monotone)",
				i, ids[i], sorted[i])
		}
	}
}

func TestNewID_DeterministicWithFixedRand(t *testing.T) {
	// A bytes.Reader with a known byte sequence; NewID must consume
	// 10 bytes per call from it. Verifies the SetRandForTests hook.
	fixed := bytes.NewReader([]byte("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdef"))
	restore := SetRandForTests(fixed)
	defer restore()

	id1 := NewID()
	id2 := NewID()
	if id1 == id2 {
		t.Fatalf("expected distinct ids, both %q", id1)
	}
	if len(id1) != 26 || len(id2) != 26 {
		t.Fatalf("id lengths: %d %d, want 26", len(id1), len(id2))
	}
}

// Log writer.

func TestOpenLog_CreatesParentDirs(t *testing.T) {
	deep := filepath.Join(t.TempDir(), "a", "b", "c", "events.jsonl")
	l, err := OpenLog(deep)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer l.Close()
	if _, err := os.Stat(deep); errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file not created: %v", err)
	}
}

func TestLog_AppendOneLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	l, err := OpenLog(path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	if err := l.Append(Event{Type: "round_started", Round: ptr(2), Article: "test.md"}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}

	var ev Event
	if err := json.Unmarshal([]byte(lines[0]), &ev); err != nil {
		t.Fatalf("json: %v", err)
	}
	if ev.Type != "round_started" {
		t.Fatalf("type %q want round_started", ev.Type)
	}
	if ev.V != SchemaVersion {
		t.Fatalf("v=%d want %d", ev.V, SchemaVersion)
	}
	if len(ev.ID) != 26 {
		t.Fatalf("id len %d want 26", len(ev.ID))
	}
	if ev.TS == "" {
		t.Fatalf("ts empty")
	}
	if ev.Round == nil || *ev.Round != 2 {
		t.Fatalf("round %v want 2", ev.Round)
	}
	if ev.Article != "test.md" {
		t.Fatalf("article %q", ev.Article)
	}
}

func TestLog_AppendMultipleAppendsAllLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	l, err := OpenLog(path)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 20; i++ {
		if err := l.Append(Event{Type: "wiking_done", Round: ptr(i)}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	l.Close()

	events, err := ReadLines(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 20 {
		t.Fatalf("got %d events, want 20", len(events))
	}
}

func TestLog_AppendAtomicAfterReturn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	l, err := OpenLog(path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	if err := l.Append(Event{Type: "round_started"}); err != nil {
		t.Fatal(err)
	}
	stat, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if stat.Size() == 0 {
		t.Fatal("expected non-zero size after Append return")
	}
}

func TestLog_AutoFillsEnvelopeFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	l, _ := OpenLog(path)
	defer l.Close()

	if err := l.Append(Event{Type: "loop", Round: ptr(2), Score: ptr(87)}); err != nil {
		t.Fatal(err)
	}
	events, err := ReadLines(path, "")
	if err != nil {
		t.Fatal(err)
	}
	ev := events[0]
	if ev.V != SchemaVersion {
		t.Fatalf("v=%d want %d", ev.V, SchemaVersion)
	}
	if len(ev.ID) != 26 {
		t.Fatalf("id len %d", len(ev.ID))
	}
	if ev.TS == "" {
		t.Fatal("ts empty")
	}
}

func TestLog_ReopenPreservesExistingContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	l1, _ := OpenLog(path)
	l1.Append(Event{Type: "round_started", Round: ptr(1)})
	l1.Close()

	l2, _ := OpenLog(path)
	defer l2.Close()
	l2.Append(Event{Type: "round_started", Round: ptr(2)})

	events, err := ReadLines(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d, want 2", len(events))
	}
}

func TestLog_CloseIdempotent(t *testing.T) {
	l, _ := OpenLog(filepath.Join(t.TempDir(), "e.jsonl"))
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestLog_AppendAfterCloseFailsCleanly(t *testing.T) {
	l, _ := OpenLog(filepath.Join(t.TempDir(), "e.jsonl"))
	l.Close()
	err := l.Append(Event{Type: "round_started"})
	if err == nil {
		t.Fatal("expected error appending to closed log")
	}
}

// Reader.

func TestReadLines_FutureVersionSkipped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	writeRaw(t, path, []string{
		`{"v":2,"id":"01HXXFUTURE0000000000000000","ts":"2026-07-18T03:21:44.123Z","type":"future_event"}`,
		`{"v":1,"id":"01HXXY00000000000000000001","ts":"2026-07-18T03:21:44.123Z","type":"round_started"}`,
	})

	events, err := ReadLines(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1 (future v=2 should be skipped)", len(events))
	}
	if events[0].Type != "round_started" {
		t.Fatalf("type %q want round_started", events[0].Type)
	}
}

func TestReadLines_OldVersionErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	writeRaw(t, path, []string{
		`{"v":0,"id":"01HXXY00000000000000000001","ts":"2026-07-18T03:21:44.123Z","type":"old_event"}`,
	})

	_, err := ReadLines(path, "")
	if err == nil {
		t.Fatal("expected error for v=0")
	}
	if !strings.Contains(err.Error(), "v=0") {
		t.Fatalf("error %q should mention v=0", err)
	}
}

func TestReadLines_MalformedJSONErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	writeRaw(t, path, []string{
		`{this is not valid JSON`,
	})

	_, err := ReadLines(path, "")
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestReadLines_CursorFiltersByULID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	l, _ := OpenLog(path)
	for i := 0; i < 5; i++ {
		if err := l.Append(Event{Type: "round_started", Round: ptr(i)}); err != nil {
			t.Fatal(err)
		}
	}
	l.Close()

	events, err := ReadLines(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 5 {
		t.Fatalf("got %d, want 5", len(events))
	}

	cursor := events[2].ID
	filtered, err := ReadLines(path, cursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 2 {
		t.Fatalf("got %d, want 2 (after cursor %q)", len(filtered), cursor)
	}
	for _, ev := range filtered {
		if ev.ID <= cursor {
			t.Fatalf("filtered event id %q <= cursor %q", ev.ID, cursor)
		}
	}
}

func TestReadLines_EmptyFileReturnsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	os.WriteFile(path, []byte(""), 0o644)

	events, err := ReadLines(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("got %d, want 0", len(events))
	}
}

func TestReadLines_MissingFileReturnsError(t *testing.T) {
	_, err := ReadLines(filepath.Join(t.TempDir(), "absent.jsonl"), "")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

// Concurrency: multiple goroutines all Appending to the same Log must
// produce all lines, no ID collisions.

func TestLog_ConcurrentAppendsAllWritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	l, _ := OpenLog(path)

	const goroutines = 10
	const perGoroutine = 20
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				if err := l.Append(Event{Type: "wiking_done"}); err != nil {
					t.Errorf("append: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
	l.Close()

	events, err := ReadLines(path, "")
	if err != nil {
		t.Fatal(err)
	}
	want := goroutines * perGoroutine
	if len(events) != want {
		t.Fatalf("got %d, want %d", len(events), want)
	}

	// All IDs unique (within a process, cryptographic randomness).
	seen := make(map[string]struct{}, want)
	for _, ev := range events {
		if _, dup := seen[ev.ID]; dup {
			t.Fatalf("duplicate id %q", ev.ID)
		}
		seen[ev.ID] = struct{}{}
	}
}

// Sanity: SetRandForTests restores on the returned function call.
func TestSetRandForTests_Restores(t *testing.T) {
	fixed := bytes.NewReader([]byte("AAAAAAAAAAAAAAAAAAAA"))
	restore := SetRandForTests(fixed)
	idFixed := NewID()
	restore()

	// After restore, NewID uses real crypto/rand. Just confirm it's
	// 26 chars and doesn't panic / reuse the fixed reader.
	idReal := NewID()
	if len(idReal) != 26 {
		t.Fatalf("post-restore id len %d", len(idReal))
	}
	if idFixed == idReal {
		t.Fatalf("expected distinct ids: %q == %q", idFixed, idReal)
	}
}

// Helpers.

func ptr[T any](v T) *T { return &v }

func writeRaw(t *testing.T, path string, lines []string) {
	t.Helper()
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write raw: %v", err)
	}
}
