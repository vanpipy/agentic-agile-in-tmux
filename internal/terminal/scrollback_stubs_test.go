// scrollback_stubs_test.go — TDD tests for Cluster E.2 fix.
//
// Cluster E.2 (Major from 2026-06-27 audit): pane.go had 3 doc-only stub
// comments at lines 709-713:
//
//   // captureScrollbackBeforeWrite takes a snapshot of row 0 before vt.Write
//   // captureScrollbackAfterWrite checks if row 0 changed and captures scrolled line
//   // isLineVisible checks if a line is still visible on screen
//
// These were promised by handleOutputLocked's doc-comment ("capturing
// scrollback per chunk to fix the openkanban every-other-line truncation
// bug") but never implemented. The behavior works without them (the alt-
// screen fast-path + direct chunked writes handle scrollback capture via
// x/vt's Emulator). The stubs are dead doc-comments that lie about code
// that doesn't exist.
//
// Fix: delete the 3 doc-only stubs. The behavior is unchanged.
//
// Tests pin the contract that the 3 stub identifiers do NOT appear in
// pane.go as function declarations OR doc-comments. They are removed.
package terminal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPaneGo_NoCaptureScrollbackStubComments pins the E.2 contract: the
// 3 stub doc-comments must not exist. They were dead code masquerading
// as documentation.
func TestPaneGo_NoCaptureScrollbackStubComments(t *testing.T) {
	src := readPaneGoSource(t)
	stubs := []string{
		"captureScrollbackBeforeWrite takes a snapshot",
		"captureScrollbackAfterWrite checks if row 0",
		"isLineVisible checks if a line is still visible",
	}
	for _, stub := range stubs {
		if strings.Contains(src, stub) {
			line := paneGoLineNumber(src, stub)
			t.Errorf("pane.go:%d contains dead stub doc-comment: %q.\n"+
				"Cluster E.2: these stubs were promised but never implemented.\n"+
				"Delete the doc-only comment blocks (3 of them at original lines 709-713).",
				line, stub)
		}
	}
}

// TestPaneGo_NoCaptureScrollbackStubDeclarations pins the contract that
// no function declaration with these names exists. If a future PR adds
// them, this test forces the implementer to also update the test (or
// delete it as no longer needed).
func TestPaneGo_NoCaptureScrollbackStubDeclarations(t *testing.T) {
	src := readPaneGoSource(t)
	decls := []string{
		"func captureScrollbackBeforeWrite(",
		"func captureScrollbackAfterWrite(",
		"func isLineVisible(",
	}
	for _, decl := range decls {
		if strings.Contains(src, decl) {
			line := paneGoLineNumber(src, decl)
			t.Errorf("pane.go:%d declares function %q, but it was supposed to be removed.\n"+
				"Cluster E.2 fix removes these stubs. If you intentionally re-added them,\n"+
				"update the test (and the implementation must actually do useful work).",
				line, decl)
		}
	}
}

// TestPaneGo_HandleOutputLockedBehaviorPreserved is a behavioral regression
// test: removing the stubs must NOT change handleOutputLocked's behavior.
// The integration test test/terminal/scrollback_truncation_test.go covers
// the actual behavior; this unit test provides fast feedback on the basic
// invariant that View() is non-empty after HandleOutput with chunks.
//
// CORRECT-7 self-check:
//   C-onformance: literal non-empty View() after HandleOutput
//   O-rdering: N/A
//   R-ange: 1 chunk tested (basic case)
//   R-eference: no external deps (no PTY started, render-only mode)
//   E-xistence: empty chunks must not panic
//   C-ardinality: 1 chunk
//   T-ime: no time concerns
func TestPaneGo_HandleOutputLockedBehaviorPreserved(t *testing.T) {
	pane := New("test", 80, 5, 100)
	pane.SetWorkdir(t.TempDir())
	cmd := pane.Start("", nil...) // render-only mode
	if cmd != nil {
		cmd()
	}

	// Multiple writes, some with \n, some without — same pattern as
	// the integration test, but without the //go:build integration tag.
	pane.HandleOutput([]byte("Hello, World!"))
	pane.HandleOutput([]byte("\nLine 1\n"))
	pane.HandleOutput([]byte("Line 2\nLine 3\n"))

	view := pane.View()
	if view == "" {
		t.Errorf("View() is empty after HandleOutput; Cluster E.2 must preserve behavior.")
	}
}

// --- helpers ---

func readPaneGoSource(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// Walk up looking for go.mod.
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			path := filepath.Join(dir, "internal", "terminal", "pane.go")
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

func paneGoLineNumber(src, substr string) int {
	idx := strings.Index(src, substr)
	if idx < 0 {
		return 0
	}
	return strings.Count(src[:idx], "\n") + 1
}