package terminal

import (
	"strings"
	"testing"
)

// TestPane_View_CursorHidden_ByChildSequence_SuppressesBlock
// verifies that when the child process asks to hide the cursor
// (DECTCEM reset, \x1b[?25l), pane.View() omits the reverse-video
// cursor block from the rendered output.
//
// Pre-fix, the cursor was always drawn whenever x/vt reported a
// non-negative cursor position, regardless of the Hidden flag.
// x/vt's Emulator API does not expose Cursor.Hidden via
// CursorPosition() (only Position), so awp tracks the change
// itself by byte-scanning for the DECTCEM sequences in
// detectCursorVisibilityChanges.
func TestPane_View_CursorHidden_ByChildSequence_SuppressesBlock(t *testing.T) {
	p := wideCharPane(t, 10, 3)
	p.HandleOutput([]byte("hello"))     // cursor at (5, 0)
	p.HandleOutput([]byte("\x1b[?25l")) // hide cursor (DECTCEM reset)

	view := p.RenderNow()

	// Cursor block must NOT appear in the rendered output.
	if strings.Contains(view, "\x1b[7m") {
		t.Errorf("cursor block rendered despite \\x1b[?25l hide sequence; view=%q", view)
	}
	// Sanity: the text is still emitted (we only suppressed the
	// cursor block, not the underlying text content).
	if !strings.Contains(view, "hello") {
		t.Errorf("text content missing; view=%q", view)
	}
}

// TestPane_View_CursorShown_ByChildSequence_RestoresBlock is the
// companion to the hide test: after \x1b[?25l, sending \x1b[?25h
// (DECTCEM set) re-enables cursor rendering.
func TestPane_View_CursorShown_ByChildSequence_RestoresBlock(t *testing.T) {
	p := wideCharPane(t, 10, 3)
	p.HandleOutput([]byte("hello"))     // cursor at (5, 0)
	p.HandleOutput([]byte("\x1b[?25l")) // hide
	p.HandleOutput([]byte("\x1b[?25h")) // show again

	view := p.RenderNow()

	if !strings.Contains(view, "\x1b[7m") {
		t.Errorf("cursor block missing after \\x1b[?25h show sequence; view=%q", view)
	}
}

// TestPane_View_CursorHidden_StateResetsWithEmptyPane verifies
// that the cursorHidden state defaults to false on a fresh pane.
// (Sanity check that no initialization bug causes an "always hidden"
// baseline.)
func TestPane_View_CursorHidden_StateResetsWithEmptyPane(t *testing.T) {
	p := wideCharPane(t, 10, 3)
	p.HandleOutput([]byte("hello"))

	view := p.RenderNow()
	if !strings.Contains(view, "\x1b[7m") {
		t.Errorf("cursor missing on a fresh pane (should default to visible); view=%q", view)
	}
}

// TestPane_View_CursorHidden_ToggleMultipleTimes verifies that
// repeated hide/show toggles each take effect (no state sticking).
func TestPane_View_CursorHidden_ToggleMultipleTimes(t *testing.T) {
	p := wideCharPane(t, 10, 3)

	// Initial: visible.
	p.HandleOutput([]byte("hello"))
	if !strings.Contains(p.RenderNow(), "\x1b[7m") {
		t.Fatalf("step 0: expected cursor visible; view=%q", p.RenderNow())
	}

	// Hide → not visible.
	p.HandleOutput([]byte("\x1b[?25l"))
	if strings.Contains(p.RenderNow(), "\x1b[7m") {
		t.Fatalf("step 1: expected cursor hidden; view=%q", p.RenderNow())
	}

	// Show → visible again.
	p.HandleOutput([]byte("\x1b[?25h"))
	if !strings.Contains(p.RenderNow(), "\x1b[7m") {
		t.Fatalf("step 2: expected cursor visible; view=%q", p.RenderNow())
	}

	// Hide again → not visible.
	p.HandleOutput([]byte("\x1b[?25l"))
	if strings.Contains(p.RenderNow(), "\x1b[7m") {
		t.Fatalf("step 3: expected cursor hidden; view=%q", p.RenderNow())
	}
}

// TestPane_View_CursorHidden_LastSequenceInChunkWins pins the
// ordering semantics of detectCursorVisibilityChanges: when BOTH
// \x1b[?25l and \x1b[?25h appear in the same chunk, the LAST one
// wins (matching VT parser semantics — the most recent DECTCEM
// sequence determines the state).
//
// The previous implementation used bytes.Contains with an early
// `return`, which made it FIRST-wins — silently dropping later
// sequences in the same chunk. This test catches that bug.
//
// Pre-fix trace (show-then-hide in same chunk):
//   Contains(hide) → true → cursorHidden = true → return
//   Final state: hidden (matches LAST-wins for THIS direction).
//
// Pre-fix trace (hide-then-show in same chunk):
//   Contains(hide) → true → cursorHidden = true → return
//   Show check never runs.
//   Final state: hidden (BUG — LAST was show, expected visible).
func TestPane_View_CursorHidden_LastSequenceInChunkWins(t *testing.T) {
	// Direction 1: hide then show in one chunk → final = show.
	p := wideCharPane(t, 10, 3)
	p.HandleOutput([]byte("hello"))
	p.HandleOutput([]byte("intro\x1b[?25lmid\x1b[?25houtro"))
	if !strings.Contains(p.RenderNow(), "\x1b[7m") {
		t.Errorf("hide-then-show in same chunk: expected cursor visible (last wins); view=%q",
			p.RenderNow())
	}

	// Direction 2: show then hide in one chunk → final = hide.
	p2 := wideCharPane(t, 10, 3)
	p2.HandleOutput([]byte("hello"))
	p2.HandleOutput([]byte("\x1b[?25hprefix\x1b[?25lsuffix"))
	if strings.Contains(p2.RenderNow(), "\x1b[7m") {
		t.Errorf("show-then-hide in same chunk: expected cursor hidden (last wins); view=%q",
			p2.RenderNow())
	}
}
