package terminal

import (
	"strings"
	"testing"
)

// TestPane_View_CursorBlock_PreservesCellStyle is a regression test
// for the "cursor position is still not right" issue.
//
// When x/vt reports the cursor position on a cell that has SGR styling
// (fg color, bg color, bold, italic, etc.), the rendered cursor block
// MUST preserve that styling in addition to applying reverse video.
//
// Pre-fix behavior: the cursor block was emitted as `\x1b[7m{CHAR}\x1b[27m`
// — reverse video only, with the previous batch's SGR already reset by
// flushBatch's trailing `\x1b[0m`. The cell's color (e.g., red fg) was
// LOST in the cursor block, so the cursor appeared with default colors
// while the surrounding styled text had specific colors. Visually this
// made the cursor "stand out" from the cell instead of appearing AT
// the cell — exactly the user-reported "cursor position is still not
// right" symptom.
//
// Fix expectation: the cursor block on a styled cell becomes
// `\x1b[<cellStyle>m\x1b[7m{CHAR}\x1b[27m`. The cell's fg becomes the
// cursor's bg via reverse video, so the cursor visually integrates with
// the styled cell (matches what real terminals like xterm do).
//
// This test pins the contract: on a styled cell, the cursor block's
// SGR prefix MUST contain both the cell's fg color and the reverse
// video (7) attribute, in that order.
func TestPane_View_CursorBlock_PreservesCellStyle(t *testing.T) {
	p := wideCharPane(t, 10, 3)

	// Red FG '中文' (4 cells), then CUB 2 to put cursor on
	// 文's main cell (col 2), which has red fg.
	p.HandleOutput([]byte("\x1b[31m中文\x1b[2D"))

	view := p.RenderNow()

	// Locate the cursor block — it must contain reverse video (7)
	// AND the cell's red fg color (38;2;128;0;0).
	//
	// The contract is that the cursor block's SGR prefix carries
	// BOTH attributes. We search for the substring "38;2;128;0;0" (red)
	// followed by ";7" (reverse video) before the next "m".
	//
	// Pre-fix: cursor block is `\x1b[7m文\x1b[27m` — no red color.
	// Post-fix: cursor block is `\x1b[38;2;128;0;0m\x1b[7m文\x1b[27m`
	//           — red fg applied before reverse video.
	if !strings.Contains(view, "38;2;128;0;0m\x1b[7m") {
		t.Errorf("BUG: cursor block does not preserve cell's fg color (red). "+
			"Pre-fix the cursor block was just \\x1b[7m{CHAR}\\x1b[27m with no fg. "+
			"Post-fix it should be \\x1b[38;2;128;0;0m\\x1b[7m{CHAR}\\x1b[27m. "+
			"Got view=%q", view)
	}
}

// TestPane_View_CursorBlock_PreservesBGColor pins the contract for
// background color: a cursor on a yellow-bg cell should keep the yellow
// bg in its rendered SGR, so the cursor block visually merges with the
// yellow background (with fg swapped to yellow via reverse video).
func TestPane_View_CursorBlock_PreservesBGColor(t *testing.T) {
	p := wideCharPane(t, 10, 3)

	// Yellow BG 'ABCDE', then CUB 1 to put cursor on 'E' (col 4).
	p.HandleOutput([]byte("\x1b[43mABCDE\x1b[1D"))

	view := p.RenderNow()

	// Cursor block must contain yellow bg (48;2;128;128;0) and reverse
	// video (7).
	if !strings.Contains(view, "48;2;128;128;0m\x1b[7m") {
		t.Errorf("BUG: cursor block does not preserve cell's bg color (yellow). "+
			"Got view=%q", view)
	}
}

// TestPane_View_CursorBlock_PreservesBold checks bold is preserved on
// the cursor block (cursor block character should remain bold).
func TestPane_View_CursorBlock_PreservesBold(t *testing.T) {
	p := wideCharPane(t, 10, 3)

	// Bold + red 'ABCDE', then CUB 1 to put cursor on 'E'.
	p.HandleOutput([]byte("\x1b[1;31mABCDE\x1b[1D"))

	view := p.RenderNow()

	// Cursor block must contain red (38;2;128;0;0), bold (1), and
	// reverse (7) in its SGR. The order matters for compactness but
	// SGR semantics are order-independent, so we just check all three
	// are present in the cursor block's SGR prefix.
	//
	// Find the position of `\x1b[7m` (the cursor block's reverse
	// video introducer) and look at the SGR immediately before it.
	idx := strings.Index(view, "\x1b[7m")
	if idx < 0 {
		t.Fatalf("cursor block missing entirely; view=%q", view)
	}
	// The SGR preceding `\x1b[7m` should contain red fg (38;2;128;0;0)
	// and bold (1).
	prefix := view[:idx]
	// Walk back to find the most recent SGR introducer (\x1b[)
	lastEsc := strings.LastIndex(prefix, "\x1b[")
	if lastEsc < 0 {
		t.Fatalf("no SGR introducer found before cursor block; view=%q", view)
	}
	cursorSGR := prefix[lastEsc:]
	if !strings.Contains(cursorSGR, "38;2;128;0;0") {
		t.Errorf("cursor block's preceding SGR missing red fg (38;2;128;0;0); got %q", cursorSGR)
	}
	if !strings.Contains(cursorSGR, ";1") && !strings.HasPrefix(cursorSGR[2:], "1;") {
		t.Errorf("cursor block's preceding SGR missing bold (1); got %q", cursorSGR)
	}
}

// TestPane_View_CursorBlock_NoStyle_Unchanged verifies that the fix
// for styled cells does NOT regress the unstyled-cell case: when the
// cell at the cursor position has no style (empty cell after plain
// text), the cursor block is still `\x1b[7m{CHAR}\x1b[27m` — no
// extra SGR prefix, no SGR suffix after the reverse-off.
//
// This guards against an over-correction that would emit something
// like `\x1b[m\x1b[7m \x1b[27m` (empty SGR + reverse) for unstyled
// cells — a subtle output-size regression that would bloat the rendered
// stream for the common empty-cell case.
func TestPane_View_CursorBlock_NoStyle_Unchanged(t *testing.T) {
	p := wideCharPane(t, 10, 3)

	// Plain 'hello' — cursor at col 5 on an empty (default-style) cell.
	p.HandleOutput([]byte("hello"))

	view := p.RenderNow()

	// The cursor block must start with `\x1b[7m ` (reverse + space) —
	// i.e., the SGR introducer immediately before the reverse video is
	// exactly `\x1b[7m`, not `\x1b[<empty>m\x1b[7m`.
	//
	// Concretely: after the previous batch's flush `\x1b[0m`, the very
	// next bytes are `\x1b[7m ` (no other SGR in between).
	if !strings.Contains(view, "\x1b[0m\x1b[7m ") {
		t.Errorf("unstyled-cell cursor block format regressed; expected "+
			"\\x1b[0m\\x1b[7m , got view=%q", view)
	}
}
