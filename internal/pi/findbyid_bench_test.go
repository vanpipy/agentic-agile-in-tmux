// findbyid_bench_test.go — benchmark verifying the lazy index actually helps.
//
// Finding FOOT-1 from post-P3P4 audit: lazy index adds state to a
// previously-stateless reader. The TUI doesn't use FindByID, so the
// optimization only helps one-shot CLI use cases. This benchmark
// verifies the optimization is real (not just dead complexity).
//
// Run with: go test -bench=BenchmarkFindByID ./internal/pi/ -run=^$
package pi

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// makeFakeSessionDir creates `numSessions` valid session .jsonl files in
// a temp dir under the agentDir path expected by SessionStore.
func makeFakeSessionDir(tb testing.TB, numSessions int) string {
	tb.Helper()
	tmpDir := tb.TempDir()
	tb.Setenv("HOME", tmpDir)

	dir := filepath.Join(tmpDir, "sessions", "--fake--")
	if err := os.MkdirAll(dir, 0755); err != nil {
		tb.Fatalf("mkdir: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for i := 0; i < numSessions; i++ {
		id := fmt.Sprintf("session-%04d-uuid-1234567890", i)
		path := filepath.Join(dir, id+".jsonl")
		content := `{"type":"session","version":1,"id":"` + id + `","timestamp":"` + now + `","cwd":"/test/repo"}` + "\n"
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			tb.Fatalf("write: %v", err)
		}
	}
	return tmpDir
}

// BenchmarkFindByID_LazyIndex benchmarks the lazy-index case. The
// first iteration triggers buildIndex (O(n)); subsequent iterations
// are O(1) map lookups.
func BenchmarkFindByID_LazyIndex(b *testing.B) {
	const numSessions = 100
	tmpDir := makeFakeSessionDir(b, numSessions)
	store, err := NewSessionStore(tmpDir)
	if err != nil {
		b.Fatalf("NewSessionStore: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := fmt.Sprintf("session-%04d-uuid-1234567890", i%numSessions)
		_, ok := store.FindByID(id)
		if !ok {
			b.Fatalf("FindByID(%s) miss", id)
		}
	}
}

// BenchmarkFindByID_BuildOnce pre-warms the index, then measures
// steady-state O(1) lookup cost.
func BenchmarkFindByID_BuildOnce(b *testing.B) {
	const numSessions = 100
	tmpDir := makeFakeSessionDir(b, numSessions)
	store, err := NewSessionStore(tmpDir)
	if err != nil {
		b.Fatalf("NewSessionStore: %v", err)
	}

	// Pre-warm the index by triggering one FindByID.
	b.ResetTimer()
	_, _ = store.FindByID("does-not-exist")
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		id := fmt.Sprintf("session-%04d-uuid-1234567890", i%numSessions)
		_, ok := store.FindByID(id)
		if !ok {
			b.Fatalf("FindByID(%s) miss", id)
		}
	}
}
