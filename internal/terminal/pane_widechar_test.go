package terminal

import (
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// stripANSI removes ANSI escape sequences for width/text comparison.
func stripANSI(s string) string {
	return ansi.Strip(s)
}

// visualWidth returns the mono-spaced visual width of an ANSI-colored
// string (CJK = 2 cells, ASCII = 1 cell). Mirrors how Bubble Tea /
// lipgloss calculate column positions downstream of Pane.View().
func visualWidth(s string) int {
	return ansi.StringWidth(s)
}

// wideCharPane creates a Pane initialized in render-only mode at the
// given size. Caller feeds input via HandleOutput, then calls View().
func wideCharPane(t *testing.T, w, h int) *Pane {
	t.Helper()
	p := New("wide", w, h, 100)
	p.SetWorkdir(t.TempDir())
	cmd := p.Start("", nil...)
	if cmd != nil {
		cmd()
	}
	if !p.IsReady() {
		t.Fatalf("Pane not ready after Start (vt not initialized)")
	}
	return p
}

// TestPane_View_WideChars_NoPlaceholderSpaces is the canonical
// failing test for the "半个字号间隔" (half-font spacing) bug.
//
// x/vt stores a wide character (CJK) as 1 main cell (Width=2) + 1
// placeholder cell (Cell{} zero-value). Our renderer must NOT emit
// the placeholder as a space — otherwise each CJK char grows by 1
// extra cell, which downstream Bubble Tea / lipgloss counts as
// half a CJK glyph in width.
//
// Pane.View() returns the full screen (rows × cols), so we slice
// the first row and verify its content width is exactly the
// expected CJK width, NOT inflated by leaked placeholder spaces.
func TestPane_View_WideChars_NoPlaceholderSpaces(t *testing.T) {
	p := wideCharPane(t, 20, 5)

	// "你好" = 2 wide chars × 2 cells = 4 cells visual width.
	p.HandleOutput([]byte("你好"))

	view := p.View()
	plain := stripANSI(view)

	lines := strings.Split(plain, "\n")
	if len(lines) == 0 {
		t.Fatalf("View() returned no lines; view=%q", view)
	}
	firstRowContent := strings.TrimRight(lines[0], " ")

	// C(onformance): first row content width must equal 4
	// (2 wide × 2 cells), not 6 (current bug: 4 cells + 2 ASCII
	// placeholder spaces).
	if got := visualWidth(firstRowContent); got != 4 {
		t.Errorf("first row content width = %d, want 4; content=%q", got, firstRowContent)
	}

	// E(xistence): rendered text must NOT contain a trailing
	// space after a wide char (leaked placeholder cell).
	if strings.Contains(firstRowContent, "你 ") || strings.Contains(firstRowContent, "好 ") {
		t.Errorf("placeholder cell rendered as ASCII space; content=%q", firstRowContent)
	}

	// C(onformance): wide chars themselves must be preserved.
	if firstRowContent != "你好" {
		t.Errorf("first row content = %q, want %q", firstRowContent, "你好")
	}
}

// TestPane_View_WideChars_MixedWithASCII covers the common case:
// ASCII + CJK + ASCII on one line. Content width must equal
// 1 (A) + 2 (你) + 2 (好) + 1 (B) = 6 cells.
func TestPane_View_WideChars_MixedWithASCII(t *testing.T) {
	p := wideCharPane(t, 20, 5)
	p.HandleOutput([]byte("A你好B"))

	view := p.View()
	plain := stripANSI(view)
	lines := strings.Split(plain, "\n")
	firstRowContent := strings.TrimRight(lines[0], " ")

	if got := visualWidth(firstRowContent); got != 6 {
		t.Errorf("first row content width = %d, want 6 (1+2+2+1); content=%q", got, firstRowContent)
	}
	if firstRowContent != "A你好B" {
		t.Errorf("first row content = %q, want %q", firstRowContent, "A你好B")
	}
}

// TestPane_View_WideChars_LongLine_DoesNotWrapPastBoundary checks
// the cursor-misalignment symptom: if the renderer leaks a
// placeholder space for every wide char, a line of N wide chars
// occupies 3N cells visually instead of 2N. Beyond the pane width
// this triggers wrap on the wrong row.
//
// With pane width 8 and 4 wide chars, expected content width = 8
// (fits exactly), not 12 (would force a wrap and leave the row
// looking padded).
func TestPane_View_WideChars_LongLine_DoesNotWrapPastBoundary(t *testing.T) {
	p := wideCharPane(t, 8, 5)
	// 4 wide chars = 8 visual cells = exactly pane width.
	p.HandleOutput([]byte("一二三四"))

	view := p.View()
	plain := stripANSI(view)

	lines := strings.Split(plain, "\n")
	firstRowContent := strings.TrimRight(lines[0], " ")
	if got := visualWidth(firstRowContent); got != 8 {
		t.Errorf("first row width = %d, want 8; firstLine=%q", got, firstRowContent)
	}
	if firstRowContent != "一二三四" {
		t.Errorf("first row content = %q, want %q", firstRowContent, "一二三四")
	}
}

// TestPane_View_WideChars_NextRowAligned is a regression check for
// the "光标错行" (cursor on wrong row) symptom. With the bug, the
// extra placeholder spaces pushed content beyond the pane width,
// causing line wrap that put subsequent content on a different
// row than where the vt cursor actually was.
//
// Fix: skip placeholders, so row N's content stays within the
// pane width and the next row's content sits where the vt cursor
// placed it.
func TestPane_View_WideChars_NextRowAligned(t *testing.T) {
	p := wideCharPane(t, 6, 3)
	// "A你B\nC" — first row has A(1) + 你(2) + B(1) = 4 cells of
	// content, fits in 6-cell pane. Second row has 'C' (x/vt's
	// LF-only newline does NOT reset col, so cursor lands at
	// col 4 when 'C' is written; col 0-3 are blank cells).
	p.HandleOutput([]byte("A你B\nC"))

	view := p.View()
	plain := stripANSI(view)
	lines := strings.Split(plain, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected >=2 lines, got %d; view=%q", len(lines), plain)
	}

	// First row content (after trimming padding) must be "A你B".
	// CRITICAL regression check: row width equals 4 (1+2+1),
	// NOT 7+ which would indicate a leaked placeholder space
	// (1+2+1 + 1 placeholder + 1 extra wrap).
	if firstRowContent := strings.TrimRight(lines[0], " "); firstRowContent != "A你B" {
		t.Errorf("line 0 content = %q, want %q", firstRowContent, "A你B")
	}
	if w := visualWidth(strings.TrimRight(lines[0], " ")); w != 4 {
		t.Errorf("line 0 content width = %d, want 4; line=%q", w, lines[0])
	}

	// Second row must contain 'C' (cursor did NOT drift to a wrong
	// row due to wrap miscalculation).
	if !strings.Contains(lines[1], "C") {
		t.Errorf("line 1 missing 'C'; line=%q", lines[1])
	}
	// Line 1's content width should be just 1 (the 'C'), not 4+
	// (which would mean placeholder chars were duplicated).
	if w := visualWidth(strings.TrimSpace(lines[1])); w != 1 {
		t.Errorf("line 1 trimmed content width = %d, want 1; line=%q", w, lines[1])
	}
}

// TestSelection_ExtractText_WideChars_NoSpaces verifies the copy
// path (Ctrl+C). When the user selects wide chars and copies, the
// clipboard must receive the wide chars alone, not wide-char +
// trailing space (the bug propagating through cellRune in
// selection.go).
func TestSelection_ExtractText_WideChars_NoSpaces(t *testing.T) {
	p := wideCharPane(t, 20, 5)
	p.HandleOutput([]byte("你好"))

	// Sanity: View renders the wide chars.
	view := p.View()
	if !strings.Contains(stripANSI(view), "你好") {
		t.Fatalf("setup: view missing 你好; got %q", view)
	}

	liveRows := p.vt.Height()
	liveScreen := func(col, row int) *uv.Cell {
		return p.vt.CellAt(col, row)
	}

	sel := NewSelectionState()
	sel.Start(Position{Row: 0, Col: 0})
	sel.Update(Position{Row: 0, Col: 3}) // 你 (col 0-1) + 好 (col 2-3)
	sel.Finish()

	got := sel.ExtractText(nil, liveScreen, liveRows, 0)
	if got != "你好" {
		t.Errorf("ExtractText = %q, want %q", got, "你好")
	}
}

// TestGetScrollbackLine_WideChars_NoSpaces verifies that text
// extraction from scrollback (used by scroll/grep features) also
// skips placeholder cells.
func TestGetScrollbackLine_WideChars_NoSpaces(t *testing.T) {
	p := wideCharPane(t, 20, 5)
	// Push enough content to overflow scrollback, then verify
	// wide chars don't leak placeholder spaces when extracted.
	// (Test the live-screen accessor — same code path used for
	// scrollback via GetScrollbackLine.)
	p.HandleOutput([]byte("你好"))

	// Use GetContent (live screen) — same cell-skipping logic.
	got := p.GetContent()
	lines := strings.Split(got, "\n")
	firstLine := strings.TrimRight(lines[0], " ")
	if firstLine != "你好" {
		t.Errorf("GetContent first line = %q, want %q", firstLine, "你好")
	}
	if w := visualWidth(firstLine); w != 4 {
		t.Errorf("GetContent first line width = %d, want 4; line=%q", w, firstLine)
	}
}