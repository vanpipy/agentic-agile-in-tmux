// move_call_uniqueness_test.go — regression test for the duplicate-Move bug.
//
// During Cluster D.3 (state-transition validation), I added error-checking
// wrappers around 3 m.globalStore.Move() callsites in model.go. The Edit
// tool pattern used (insert-after-line) accidentally kept the original
// call alongside the new if-checked call. Result: Move() executes twice
// per drag/quickmove, and the second call may fail spuriously because
// the ticket is already in the target status.
//
// This test pins the source-level contract: exactly 3 m.globalStore.Move()
// calls in model.go (one per callsite: dropTicket, quickMoveTicket,
// quickMoveTicketBackward).
package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestModelGo_ExactlyThreeMoveCallsites pins the contract that prevents
// the duplicate-Move bug from regressing.
//
// CORRECT-7 self-check:
//   C-onformance: literal count = 3
//   O-rdering: N/A
//   R-ange: N/A
//   R-eference: source-file scan only
//   E-xistence: count must equal 3 (not 0, not 6)
//   C-ardinality: 1 dimension (count)
//   T-ime: no time concerns
func TestModelGo_ExactlyThreeMoveCallsites(t *testing.T) {
	src := readModelGoSource(t)

	// Count `m.globalStore.Move(` occurrences. The opening paren disambiguates
	// from any unrelated identifiers that happen to contain "Move".
	const pattern = "m.globalStore.Move("
	count := strings.Count(src, pattern)

	const want = 3
	if count != want {
		t.Errorf("model.go has %d %q callsites; want exactly %d.\n"+
			"This bug appeared during Cluster D.3 when adding error-checking wrappers:\n"+
			"the Edit tool kept the original line AND inserted the if-checked line.\n"+
			"Fix: remove the duplicate (un-wrapped) call, keeping only the if-checked one.\n"+
			"Callsites: dropTicket, quickMoveTicket, quickMoveTicketBackward.",
			count, pattern, want)
	}

	// Also verify each callsite has the if-checked wrapper. A callsite
	// missing the if-check would be a regression of the original bug
	// (silent error swallowing).
	for _, fn := range []string{"dropTicket", "quickMoveTicket", "quickMoveTicketBackward"} {
		// Find the function body in the source.
		idx := strings.Index(src, "func (m *Model) "+fn+"(")
		if idx < 0 {
			t.Errorf("function %s not found in model.go", fn)
			continue
		}
		// Find the next "func" after this one (or end of file) to bound the search.
		end := strings.Index(src[idx+1:], "\nfunc ")
		if end < 0 {
			end = len(src)
		} else {
			end += idx + 1
		}
		body := src[idx:end]
		if !strings.Contains(body, "if err := m.globalStore.Move(") {
			t.Errorf("function %s does not check Move() error; regression of D.3 contract.\n"+
				"All Move() calls must be wrapped: if err := m.globalStore.Move(...); err != nil",
				fn)
		}
	}
}

// --- helpers ---

func readModelGoSource(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			path := filepath.Join(dir, "internal", "ui", "model.go")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			return string(data)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}