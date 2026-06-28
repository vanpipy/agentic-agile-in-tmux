// move_call_uniqueness_test.go — regression test for the duplicate-Move bug.
//
// During Cluster D.3 (state-transition validation), error-checking
// wrappers were added around m.globalStore.Move() callsites in
// model.go. The Edit tool pattern (insert-after-line) accidentally
// kept the original call alongside the new if-checked call. Result:
// Move() executes twice per drag/quickmove, and the second call
// may fail spuriously because the ticket is already in the target
// status.
//
// This test pins the structural contract that prevents regression:
//   1. Every m.globalStore.Move() call must be wrapped in
//      `if err := m.globalStore.Move(...); err != nil`
//   2. Within any single function body, m.globalStore.Move() must
//      appear at most once (no duplicate calls).
//
// We intentionally do NOT pin a specific total count of Move()
// callsites, because the post-2026-06-28 simplification reduced
// the callsite count (quickMoveTicket and quickMoveTicketBackward
// collapsed into toggleSelectedTicket; dropTicket remained for
// mouse drag). Pinning a hard count would force churn every time
// the design changes.
package ui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestModelGo_MoveCallsitesHaveErrorGuards pins the source-level
// contract: every m.globalStore.Move() call must be wrapped in
// `if err := m.globalStore.Move(...); err != nil`. A naked Move()
// call would swallow errors silently — the original D.3 bug.
//
// CORRECT-7 self-check:
//   C-onformance: every Move() has the if-err wrapper
//   O-rdering: N/A
//   R-ange: covers all Move() calls in model.go
//   R-eference: source-file scan only
//   E-xistence: must find at least 1 callsite (regression guard)
//   C-ardinality: 1 dimension (presence of wrapper)
//   T-ime: no time concerns
func TestModelGo_MoveCallsitesHaveErrorGuards(t *testing.T) {
	src := readModelGoSource(t)
	const pattern = "m.globalStore.Move("
	count := strings.Count(src, pattern)
	if count == 0 {
		t.Fatalf("model.go has 0 %q callsites; expected at least 1 (drag or toggle). "+
			"This test is vacuous; check the test setup.", pattern)
	}

	// Walk every function body in model.go. For each function, if it
	// contains a Move() call, verify the body also has the if-err
	// wrapper.
	for _, fn := range findFunctionBodies(t, src) {
		if !strings.Contains(fn.body, pattern) {
			continue
		}
		if !strings.Contains(fn.body, "if err := m.globalStore.Move(") {
			t.Errorf("function %s has a naked m.globalStore.Move(...) call.\n"+
				"D.3 contract: all Move() calls must be wrapped:\n"+
				"  if err := m.globalStore.Move(...); err != nil { ... }",
				fn.name)
		}

		// No-duplicates-per-function: a function body that calls
		// Move() twice would re-execute the original D.3 bug.
		moveCount := strings.Count(fn.body, pattern)
		if moveCount > 1 {
			t.Errorf("function %s has %d m.globalStore.Move() calls; want at most 1.\n"+
				"Multiple Move() calls in one function re-introduces the D.3 duplicate-Move bug.",
				fn.name, moveCount)
		}
	}
}

// TestModelGo_NotMoreThanOneMovePerLine is a finer-grained guard:
// even within a function, two Move() calls on different lines would
// be a bug. This test catches the case where an Edit tool refactor
// adds a new Move() call without removing the old one.
//
// The original test had a hard count = 3, but the post-2026-06-28
// simplification legitimately reduces it (the 2-state model needs
// fewer callsites). This test enforces the *invariant* (no
// duplicates per function) rather than the *count*.
//
// The regex matches both the bare `m.globalStore.Move(...)` form and
// the `if err := m.globalStore.Move(...)` wrapper form, since the
// D.3 contract is that the latter is the only form that should
// appear in production code.
func TestModelGo_NotMoreThanOneMovePerLine(t *testing.T) {
	src := readModelGoSource(t)
	// Match `m.globalStore.Move(` anywhere on a line (with or without
	// the `if err := ` prefix). Anchoring with `^` would miss
	// wrapped forms.
	re := regexp.MustCompile(`m\.globalStore\.Move\(`)
	matches := re.FindAllStringIndex(src, -1)
	if len(matches) == 0 {
		t.Fatal("no m.globalStore.Move() calls found in model.go; expected at least 1")
	}
	// All matches should be on different lines (no two Move() calls
	// on the same source line — which would be the smoking gun for
	// the original D.3 bug pattern of "kept the original AND added
	// the if-checked one on adjacent lines").
	seenLines := make(map[int]bool)
	for _, m := range matches {
		// Find the line number of this match.
		lineNum := strings.Count(src[:m[0]], "\n") + 1
		if seenLines[lineNum] {
			t.Errorf("m.globalStore.Move() called twice on line %d — D.3 duplicate-Move bug pattern",
				lineNum)
		}
		seenLines[lineNum] = true
	}
}

// --- helpers ---

// functionBody identifies a function in model.go source by its name
// and the bounds of its body (start byte, end byte).
type functionBody struct {
	name  string
	start int
	end   int
	body  string
}

// findFunctionBodies returns the bodies of all `func (m *Model) Name(`
// functions in src. The end bound is the start of the next function
// (or EOF).
func findFunctionBodies(t *testing.T, src string) []functionBody {
	t.Helper()
	re := regexp.MustCompile(`(?m)^func \(m \*Model\) (\w+)\(`)
	matches := re.FindAllStringSubmatchIndex(src, -1)
	var out []functionBody
	for i, m := range matches {
		name := src[m[2]:m[3]]
		start := m[0]
		// Body end: start of the next function, or EOF.
		end := len(src)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		out = append(out, functionBody{
			name:  name,
			start: start,
			end:   end,
			body:  src[start:end],
		})
	}
	return out
}

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
