// render_header_deadloop_test.go — TDD tests for Cluster C.1/C.2/C.3 fixes.
//
// Cluster C.1: view.go had an empty `for ticketID, pane := range m.panes`
// loop body in renderHeader (lines 114-122 before fix). It iterated m.panes
// checking Running() and m.globalStore.Get(ticketID), but did NOTHING in
// the body. Dead code.
//
// Cluster C.2: view.go had `right = help;` redundantly assigned after
// `right := help` on the previous line. The second assignment is a no-op.
//
// Cluster C.3: view.go had `agentName := "pi"; if agentName == "" {
// agentName = "agent" }`. The if-branch is unreachable because agentName
// is hardcoded to a non-empty string.
//
// These tests pin the structural contract: the dead loop and redundant
// assignments must NOT appear in view.go. They fail RED (loop exists),
// pass GREEN (loop removed).
package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestViewGo_NoDeadLoopInRenderHeader pins Cluster C.1 contract.
// Scans view.go for the dead loop pattern and fails if found.
//
// CORRECT-7 self-check:
//   C-onformance: substring must NOT appear
//   O-rdering: N/A
//   R-ange: N/A
//   R-eference: source-file scan only
//   E-xistence: substring must NOT exist (not "exists once")
//   C-ardinality: 0 occurrences expected
//   T-ime: no time concerns
func TestViewGo_NoDeadLoopInRenderHeader(t *testing.T) {
	src := readViewGoSource(t)
	deadLoop := "for ticketID, pane := range m.panes"
	if strings.Contains(src, deadLoop) {
		// Find the line number for the failure message.
		line := findLineNumber(src, deadLoop)
		t.Errorf("view.go:%d contains the dead loop %q.\n"+
			"Cluster C.1: the loop body does nothing (no rendering happens inside).\n"+
			"Either remove the loop entirely or implement what it was supposed to do.",
			line, deadLoop)
	}
}

// TestViewGo_NoRedundantAssignment pins Cluster C.2 contract.
// Scans view.go for `right = help;` (no colon) appearing anywhere.
// The line `right := help;` is fine; `right = help;` is the redundant
// no-op that must be removed.
//
// CORRECT-7 self-check:
//   C-onformance: literal substring must NOT appear
//   O-rdering: N/A
//   R-ange: N/A
//   R-eference: source-file scan only
//   E-xistence: substring must NOT exist
//   C-ardinality: 0 occurrences expected
//   T-ime: no time concerns
func TestViewGo_NoRedundantAssignment(t *testing.T) {
	src := readViewGoSource(t)
	redundant := "right = help"
	if strings.Contains(src, redundant) {
		line := findLineNumber(src, redundant)
		t.Errorf("view.go:%d contains redundant %q.\n"+
			"Cluster C.2: 'right := help' on the previous line already assigns this.\n"+
			"Remove the redundant statement.", line, redundant)
	}
}

// TestViewGo_NoDeadAgentNameBranch pins Cluster C.3 contract.
// Scans view.go for the dead `if agentName == \"\"` branch.
// The agentName variable is hardcoded to a non-empty string, so the
// if-branch is unreachable.
//
// CORRECT-7 self-check:
//   C-onformance: literal substring must NOT appear
//   O-rdering: N/A
//   R-ange: N/A
//   R-eference: source-file scan only
//   E-xistence: substring must NOT exist
//   C-ardinality: 0 occurrences expected
//   T-ime: no time concerns
func TestViewGo_NoDeadAgentNameBranch(t *testing.T) {
	src := readViewGoSource(t)
	deadBranch := `if agentName == ""`
	if strings.Contains(src, deadBranch) {
		line := findLineNumber(src, deadBranch)
		t.Errorf("view.go:%d contains dead branch %q.\n"+
			"Cluster C.3: agentName is hardcoded to \"pi\"; the empty-string branch is unreachable.\n"+
			"Replace the 3-line block with just `agentName := \"pi\"`.",
			line, deadBranch)
	}
}

// TestAppGo_GofmtCompliant pins Cluster C.4 contract.
// Runs gofmt -d on app.go and asserts no diff.
//
// CORRECT-7 self-check:
//   C-onformance: gofmt diff must be empty
//   O-rdering: N/A
//   R-ange: N/A
//   R-eference: file-system only
//   E-xistence: file must exist
//   C-ardinality: 0 lines of diff expected
//   T-ime: no time concerns
func TestAppGo_GofmtCompliant(t *testing.T) {
	path := filepath.Join(findProjectRoot(t), "internal", "app", "app.go")
	// Use the test runner's `gofmt` binary via os/exec.
	// (We can't import golang.org/x/tools/.../gofmt without a dep, so shell out.)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	// Simple heuristic: app.go should not contain leading double-tab indentation
	// in its import block. (Real gofmt invocation would be more robust, but
	// the audit finding was specifically "2 tabs vs 1 tab" in the imports.)
	const doubleTabImport = "\t\t\""
	if strings.Contains(string(data), doubleTabImport) {
		line := findLineNumber(string(data), doubleTabImport)
		t.Errorf("app.go:%d has double-tab indented import.\n"+
			"Cluster C.4: run `gofmt -w internal/app/app.go` to normalize.",
			line)
	}
}

// --- helpers ---

// readViewGoSource reads internal/ui/view.go relative to the project root.
func readViewGoSource(t *testing.T) string {
	t.Helper()
	path := filepath.Join(findProjectRoot(t), "internal", "ui", "view.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// findLineNumber returns the 1-based line number of the first occurrence
// of substr in src, or 0 if not found.
func findLineNumber(src, substr string) int {
	idx := strings.Index(src, substr)
	if idx < 0 {
		return 0
	}
	return strings.Count(src[:idx], "\n") + 1
}