package terminal

import (
	"strings"
	"testing"
)

// TestPane_View_CursorOnWideCharPlaceholder_RendersBlock is a
// regression test for the wrong-cursor bug.
//
// When x/vt's CursorPosition() reports a position that falls on
// a wide-char placeholder cell (the zero-value Cell{} that x/vt
// inserts after a wide character's main cell), awp's renderer
// must still emit a visible cursor block at that position.
//
// Why this matters (the user's bug report):
//   - "the cursor cannot point to the right position"
//   - "the cn typing cannot follow the cursor"
//
// In real pi sessions, the cursor lands on a placeholder when:
//   - The user types a CJK character, then BS — cursor moves
//     back by 1 onto the placeholder of the previous CJK char.
//   - pi issues a CUP (\x1b[r;cH) whose column happens to be a
//     placeholder (e.g., when redrawing a prompt that contains
//     CJK).
//   - CUF / CUB navigates the cursor onto a placeholder.
//
// If we skip the placeholder via isPlaceholder, the cursor block
// is never emitted. The user sees no cursor where they expect one,
// and the next typed character (which x/vt writes at the
// placeholder position) appears "out of sync" with the missing
// cursor — exactly the reported symptom.
//
// Fix expectation: when x/vt reports cursorPos.X == placeholder
// column, render a reverse-video block (or the underlying wide
// char with reverse video, depending on terminal semantics) at
// that column so the cursor is visible.
func TestPane_View_CursorOnWideCharPlaceholder_RendersBlock(t *testing.T) {
	p := wideCharPane(t, 10, 3)

	// Write "中" — creates main cell at col 0 (Width=2) and
	// placeholder at col 1. Cursor advances to (2, 0).
	p.HandleOutput([]byte("中"))

	// Move cursor to (1, 0) — the placeholder of '中'.
	// CUP uses 1-indexed coordinates, so (row=1, col=2) maps to
	// 0-indexed (1, 0).
	p.HandleOutput([]byte("\x1b[1;2H"))

	view := p.View()

	// The rendered output MUST contain a reverse-video cursor
	// block (\x1b[7m...\x1b[27m) somewhere in row 0. Currently
	// the placeholder is skipped, so NO cursor block is emitted
	// and the cursor appears invisible to the user.
	lines := strings.Split(view, "\n")
	if len(lines) == 0 {
		t.Fatalf("View() returned no lines; view=%q", view)
	}
	row0 := lines[0]

	if !strings.Contains(row0, "\x1b[7m") {
		t.Errorf("BUG: cursor on wide-char placeholder (col 1) not rendered; row=%q; full view=%q",
			row0, view)
	}
}

// TestPane_View_CursorAfterBackspaceOnCJK_RendersBlock covers the
// most common user-facing trigger: typing a CJK character then BS.
// x/vt moves the cursor back by 1 onto the placeholder; without
// the fix, the cursor disappears.
func TestPane_View_CursorAfterBackspaceOnCJK_RendersBlock(t *testing.T) {
	p := wideCharPane(t, 10, 3)

	// Simulate pi echoing a typed CJK char: "中" then BS.
	// After BS, cursor is at (1, 0) — the placeholder.
	p.HandleOutput([]byte("中\b"))

	view := p.View()

	lines := strings.Split(view, "\n")
	if len(lines) == 0 {
		t.Fatalf("View() returned no lines; view=%q", view)
	}
	row0 := lines[0]

	if !strings.Contains(row0, "\x1b[7m") {
		t.Errorf("BUG: cursor invisible after typing CJK + BS; row=%q; full view=%q",
			row0, view)
	}
}

// TestPane_View_CursorOnMainCellOfCJK_RendersBlock ensures the
// existing "cursor on main cell" behavior still works after the
// fix. This guards against an over-correction that would shift
// the cursor from the main cell to the placeholder (or vice
// versa) in a way that breaks the common case.
func TestPane_View_CursorOnMainCellOfCJK_RendersBlock(t *testing.T) {
	p := wideCharPane(t, 10, 3)

	p.HandleOutput([]byte("中")) // main at col 0, placeholder at col 1
	// Move cursor back to (0, 0) — the main cell of '中'.
	p.HandleOutput([]byte("\x1b[1;1H")) // CUP 1;1 → (0, 0)

	view := p.View()
	lines := strings.Split(view, "\n")
	row0 := lines[0]

	// Cursor block must appear at col 0, on the main cell of '中'.
	// Expected pattern: \x1b[7m中\x1b[27m (reverse-video 中).
	if !strings.Contains(row0, "\x1b[7m") {
		t.Fatalf("cursor block missing; row=%q", row0)
	}
	// The wide char '中' itself must be rendered (either
	// standalone or inside the cursor block).
	plain := stripANSI(row0)
	if !strings.Contains(plain, "中") {
		t.Errorf("wide char '中' missing from rendered row; plain=%q", plain)
	}
}

// TestPane_View_CursorOnPlaceholder_NoNulByteInOutput is a guard
// against a regression in the opposite direction: when the cursor
// lands on a placeholder and we fall through to emit it, we must
// substitute a space for the NUL that cellRune returns. A stray
// NUL byte in the terminal stream corrupts subsequent output and
// is hard to debug.
func TestPane_View_CursorOnPlaceholder_NoNulByteInOutput(t *testing.T) {
	p := wideCharPane(t, 10, 3)

	p.HandleOutput([]byte("中"))               // main at col 0, placeholder at col 1
	p.HandleOutput([]byte("\x1b[1;2H"))        // CUP → (1, 0) = placeholder

	view := p.View()

	if strings.ContainsRune(view, 0) {
		t.Errorf("rendered output contains NUL byte — placeholder cell leaked into cursor block; view=%q", view)
	}
}

// TestPane_View_CursorFlowWithCJK_RealisticFlow simulates a realistic
// pi echo scenario:
//
//	> 你      (prompt with CJK)
//	> 你好    (user adds 好)
//	> 你     (user BS to delete 好)
//
// At each step the cursor must be visible in the rendered output.
// Before the fix, the BS step placed the cursor on the placeholder
// of '你', so no cursor block was emitted — the user could not see
// where they were typing.
//
// Uses RenderNow() to bypass the 16ms View() throttle deterministically
// (no wall-clock sleep). Without this, a tight HandleOutput + View
// sequence would race the throttle and observe stale cached output.
func TestPane_View_CursorFlowWithCJK_RealisticFlow(t *testing.T) {
	p := wideCharPane(t, 20, 3)

	// Step 1: prompt with initial CJK.
	p.HandleOutput([]byte("> 你"))

	view1 := p.RenderNow()
	if !strings.Contains(view1, "\x1b[7m") {
		t.Fatalf("step 1: cursor missing; view=%q", view1)
	}

	// Step 2: append another CJK.
	p.HandleOutput([]byte("好"))

	view2 := p.RenderNow()
	if !strings.Contains(view2, "\x1b[7m") {
		t.Fatalf("step 2: cursor missing after appending CJK; view=%q", view2)
	}
	// Both chars must still be present (no truncation, no overwrite).
	plain2 := stripANSI(view2)
	if !strings.Contains(plain2, "你好") {
		t.Errorf("step 2: chars missing; plain=%q", plain2)
	}

	// Step 3: BS — cursor moves back onto '好's placeholder.
	p.HandleOutput([]byte("\b"))

	view3 := p.RenderNow()
	// BUG (pre-fix): cursor invisible here because x/vt reports
	// cursor at col 4 (placeholder of 好 at col 3).
	if !strings.Contains(view3, "\x1b[7m") {
		t.Fatalf("step 3 (BS): cursor missing — wrong-cursor bug; view=%q", view3)
	}
	// Verify no NUL leaked into the stream.
	if strings.ContainsRune(view3, 0) {
		t.Errorf("step 3: NUL byte in rendered output; view=%q", view3)
	}
}

// TestPane_RenderNow_BypassesViewThrottle pins the RenderNow
// contract: RenderNow always produces a fresh view of the current
// x/vt state, even when View() within the 16ms throttle window
// would return the cached view.
func TestPane_RenderNow_BypassesViewThrottle(t *testing.T) {
	p := wideCharPane(t, 10, 3)

	p.HandleOutput([]byte("abc"))
	p.RenderNow() // establish a render timestamp (sets p.lastRender = now)

	// Immediately (well within 16ms) change state and call RenderNow.
	p.HandleOutput([]byte("D"))
	rendered := p.RenderNow()

	// RenderNow must reflect the post-HandleOutput state, not the
	// stale cached view from before "D".
	plain := stripANSI(rendered)
	if !strings.Contains(plain, "abcD") {
		t.Errorf("RenderNow did not include post-throttle update; plain=%q", plain)
	}
}

// TestPane_View_GetContent_CursorOnPlaceholder_StillSucceeds ensures
// the text-extraction path (used by /copy, scrollback introspection)
// also tolerates a cursor-on-placeholder state without crashing or
// emitting NUL bytes. The cursor itself is NOT rendered in this path,
// but the surrounding text must still be cleanly extracted.
func TestPane_View_GetContent_CursorOnPlaceholder_StillSucceeds(t *testing.T) {
	p := wideCharPane(t, 10, 3)

	p.HandleOutput([]byte("中"))
	p.HandleOutput([]byte("\x1b[1;2H")) // cursor on placeholder

	// GetContent must succeed and contain '中' (no NUL byte).
	got := p.GetContent()
	if strings.ContainsRune(got, 0) {
		t.Errorf("GetContent contains NUL; got=%q", got)
	}
	// The content row width at the wide-char position must be exactly
	// 2 cells (the wide char), not 3 (which would indicate the
	// placeholder leaked as an extra space). We assert by checking
	// that the first row, with trailing whitespace trimmed, is "中"
	// (no trailing space from the placeholder).
	lines := strings.Split(got, "\n")
	if len(lines) == 0 {
		t.Fatalf("GetContent returned no lines")
	}
	firstLine := strings.TrimRight(lines[0], " ")
	if firstLine != "中" {
		t.Errorf("GetContent first row (trimmed) = %q, want %q", firstLine, "中")
	}
	if w := visualWidth(firstLine); w != 2 {
		t.Errorf("GetContent first row width = %d, want 2 (wide char only)", w)
	}
}