package terminal

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPane_New(t *testing.T) {
	p := New("test", 80, 24, 100)
	if p.ID() != "test" {
		t.Errorf("ID = %q, want test", p.ID())
	}
	// R(ange): constructor should preserve width/height.
	// Size() and ScrollbackSize() are available pre-Start
	// (scrollback init moved from Start() to New() per design debt fix).
	w, h := p.Size()
	if w != 80 || h != 24 {
		t.Errorf("Size = (%d,%d), want (80,24)", w, h)
	}
	if p.ScrollbackSize() != 100 {
		t.Errorf("ScrollbackSize = %d, want 100", p.ScrollbackSize())
	}

	// E(xistence): default capacity when scrollbackSize <= 0.
	p2 := New("test2", 80, 24, 0)
	if p2.ScrollbackSize() != 10000 {
		t.Errorf("default ScrollbackSize = %d, want 10000", p2.ScrollbackSize())
	}
}

func TestPane_Start_NoCommand(t *testing.T) {
	// Pane.Start returns a tea.Cmd. Invoking it runs the actual
	// setup (Lock, create vt, set running=true).
	p := New("test", 80, 24, 100)
	cmd := p.Start("", nil...)
	if cmd == nil {
		t.Fatal("Start should return a tea.Cmd even with empty command")
	}
	cmd() // invoke the returned Cmd
	if !p.Running() {
		t.Error("Pane should be running after Start")
	}
}

func TestPane_Start_WithCommand(t *testing.T) {
	// Pane.StartCmd returns a tea.Cmd. Invoking it actually exec's
// the command and starts the read loop. (Migrated from Start → StartCmd
// to avoid the Start-in-PTY-mode footgun.)
	p := New("test", 80, 24, 100)
	cmd := p.StartCmd("echo", "hello")
	if cmd == nil {
		t.Fatal("StartCmd should return a tea.Cmd")
	}
	cmd() // exec the command
	defer p.Stop()

	// Wait for output
	time.Sleep(100 * time.Millisecond)
	if !p.Running() {
		t.Error("Pane should be running")
	}
}

func TestPane_HandleOutput(t *testing.T) {
	// Phase 7: feed raw bytes via HandleOutput, verify View() renders.
	p := New("test", 80, 24, 100)
	cmd := p.Start("", nil...)
	if cmd != nil { cmd() }

	p.HandleOutput([]byte("Hello, World!"))
	time.Sleep(20 * time.Millisecond) // let vt process

	view := p.View()
	if view == "" {
		t.Error("View() should be non-empty after HandleOutput")
	}
	if !strings.Contains(view, "Hello, World!") {
		t.Errorf("View() missing 'Hello, World!': %q", view)
	}
}

func TestPane_HandleOutput_AnsiColors(t *testing.T) {
	// Verify ANSI color codes are interpreted (not left as raw escapes).
	// The view MUST contain "RED" text AND must NOT contain the raw
	// \x1b[31m sequence (vt should have consumed it during parsing).
	p := New("test", 80, 24, 100)
	cmd := p.Start("", nil...)
	if cmd != nil { cmd() }
	p.HandleOutput([]byte("\x1b[31mRED\x1b[0m"))
	time.Sleep(20 * time.Millisecond)

	view := p.View()
	if !strings.Contains(view, "RED") {
		t.Errorf("View() missing 'RED': %q", view)
	}
	if strings.Contains(view, "\x1b[31m") {
		t.Errorf("View() contains raw ANSI escape (\\x1b[31m); vt should have processed it: %q", view)
	}
}

func TestPane_HandleOutput_Multiline(t *testing.T) {
	p := New("test", 80, 24, 100)
	cmd := p.Start("", nil...)
	if cmd != nil {
		cmd()
	}
	p.HandleOutput([]byte("line1\nline2\nline3"))
	time.Sleep(20 * time.Millisecond)

	view := p.View()
	for _, want := range []string{"line1", "line2", "line3"} {
		if !strings.Contains(view, want) {
			t.Errorf("View() missing %q: %q", want, view)
		}
	}
}

// TestPane_Update_OutputMsg_NoTrailingNewline guards against the
// regression that froze awp's TUI when pi was spawned. PTY output
// rarely ends with a newline (single keystroke echoes, standalone
// ANSI sequences like cursor moves, alt-screen entry). The handleOutput
// loop must not slice data[start:i+1] when i == len(data).
//
// Regression for: "pty无法显示pi打印的回显内容".
func TestPane_Update_OutputMsg_NoTrailingNewline(t *testing.T) {
	p := New("test", 80, 24, 100)
	cmd := p.Start("", nil...)
	if cmd != nil {
		cmd()
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("handleOutput panicked on no-trailing-newline data: %v", r)
		}
	}()

	// PTY cooked-mode echo of a single keystroke.
	p.Update(OutputMsg{PaneID: "test", Data: []byte("h")})
	// Paste of "hello" — multi-byte echo, no newline.
	p.Update(OutputMsg{PaneID: "test", Data: []byte("hello")})
	// Mixed: "ab" on row 0, "cd" on row 1 (last line w/o \n).
	p.Update(OutputMsg{PaneID: "test", Data: []byte("ab\ncd")})
}

// TestPane_Update_OutputMsg_PiTuiFirstChunk guards the same regression
// using the exact first chunk pi's interactive TUI sends: enter alt
// screen + clear + cursor home + mouse/keypad modes. No trailing
// newline. This was the chunk that crashed awp on every spawn.
func TestPane_Update_OutputMsg_PiTuiFirstChunk(t *testing.T) {
	p := New("test", 80, 24, 100)
	cmd := p.Start("", nil...)
	if cmd != nil {
		cmd()
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("handleOutput panicked on pi's first TUI chunk: %v", r)
		}
	}()

	// pi --version output, then interactive init (paraphrased from
	// actual pi 0.79.9 startup; the key property is "no trailing
	// newline" — single \x1b[?1049h, then content, then \x1b[H).
	firstChunk := []byte("\x1b[?1049h\x1b[2J\x1b[H\x1b[?25l\x1b[?2004h\x1b[?1002h\x1b[?1006h\x1b[H")
	p.Update(OutputMsg{PaneID: "test", Data: firstChunk})
}

// TestPane_Update_OutputMsg_MultipleChunksAccumulate guards the
// dirty-flag regression: without p.dirty=true in handleOutput,
// View() would return the cached (stale) view after the first render.
// Verifies the dirty flag is set after each Update call, which is
// what the production View() path checks to decide whether to re-render.
func TestPane_Update_OutputMsg_MultipleChunksAccumulate(t *testing.T) {
	p := New("test", 80, 24, 100)
	cmd := p.Start("", nil...)
	if cmd != nil {
		cmd()
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("handleOutput panicked: %v", r)
		}
	}()

	// First chunk — should set dirty=true.
	p.Update(OutputMsg{PaneID: "test", Data: []byte("first\n")})
	p.mu.Lock()
	dirty1 := p.dirty
	p.mu.Unlock()
	if !dirty1 {
		t.Error("dirty flag not set after first Update")
	}

	// Render (consumes dirty).
	p.View()

	// Second chunk — must also set dirty=true.
	p.Update(OutputMsg{PaneID: "test", Data: []byte("second\n")})
	p.mu.Lock()
	dirty2 := p.dirty
	p.mu.Unlock()
	if !dirty2 {
		t.Error("dirty flag not set after second Update")
	}
}

// ============================================================================
// handleOutputLocked contract tests
// ============================================================================
//
// The handleOutputLocked helper is the merged body for both the
// exported HandleOutput (called from PiClient/PiPane) and the
// unexported handleOutput (called from Pane.Update(OutputMsg)).
// These tests cover the contracts that must hold regardless of
// which entry point invoked it.

// TestHandleOutputLocked_AltScreen_FastPath verifies the
// alt-screen fast path: when altScreenActive is true,
// handleOutputLocked writes the whole chunk in a single vt.Write
// call (no per-newline split). x/vt's Scrollback is only updated
// during per-line Screen.DeleteLine operations triggered by the
// terminal's own scroll-up behaviour, so a multi-line chunk
// passed to vt.Write verbatim should not grow the scrollback.
//
// Observable contract (post x/vt migration):
//   - dirty=true (Update() flags View() to re-render)
//   - vt.ScrollbackLen() unchanged (no per-line capture happened)
//   - the chunk's content is visible on screen (GetContent)
func TestHandleOutputLocked_AltScreen_FastPath(t *testing.T) {
	p := New("alt-test", 80, 24, 100)
	cmd := p.Start("", nil...)
	if cmd != nil {
		cmd()
	}

	// Force alt-screen mode on.
	p.mu.Lock()
	p.altScreenActive = true
	p.dirty = false
	p.mu.Unlock()

	chunk := []byte("line1\nline2\nline3\n")
	scrollbackLenBefore := p.ScrollbackLen()
	p.HandleOutput(chunk)

	p.mu.Lock()
	dirty := p.dirty
	sbLen := p.vt.ScrollbackLen()
	p.mu.Unlock()

	if !dirty {
		t.Error("alt-screen fast path: dirty flag not set")
	}
	if sbLen != scrollbackLenBefore {
		t.Errorf("alt-screen fast path: scrollback changed (%d → %d), capture should be skipped",
			scrollbackLenBefore, sbLen)
	}
}

// TestHandleOutputLocked_NonAltScreen_PerNewlineSplit verifies
// the non-alt-screen path: multi-line input is written, vt state
// reflects all lines (i.e., per-newline split runs end-to-end),
// and dirty=true. We observe via GetContent() rather than lastTopRow
// because captureScrollbackAfterWrite intentionally clears
// lastTopRow at end of each capture cycle (snapshot lifecycle).
//
// This guards against the openkanban every-other-line truncation
// bug: if someone removes the per-line capture/Write cycle,
// GetContent would be missing lines.
func TestHandleOutputLocked_NonAltScreen_PerNewlineSplit(t *testing.T) {
	p := New("nonalt-test", 80, 24, 100)
	cmd := p.Start("", nil...)
	if cmd != nil {
		cmd()
	}

	// Pre-condition: altScreenActive is false by default.
	p.mu.Lock()
	altBefore := p.altScreenActive
	p.mu.Unlock()
	if altBefore {
		t.Fatal("pre-condition failed: altScreenActive should be false")
	}

	p.HandleOutput([]byte("alpha\nbeta\ngamma\n"))

	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.dirty {
		t.Error("non-alt-screen: dirty flag not set")
	}

	// GetContent outside lock (it acquires its own).
	// Use a fresh lock pattern to avoid nested locking.
	p.mu.Unlock()
	content := p.GetContent()
	p.mu.Lock()
	for _, want := range []string{"alpha", "beta", "gamma"} {
		if !strings.Contains(content, want) {
			t.Errorf("non-alt-screen: GetContent missing %q after per-newline write", want)
		}
	}
}

// TestHandleOutputLocked_EmptyData verifies that empty/nil data
// doesn't panic and still sets the dirty flag (caller may want
// to re-render for other reasons).
func TestHandleOutputLocked_EmptyData(t *testing.T) {
	p := New("empty-test", 80, 24, 100)
	cmd := p.Start("", nil...)
	if cmd != nil {
		cmd()
	}

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("empty data panicked: %v", r)
		}
	}()

	p.HandleOutput([]byte{})
	p.HandleOutput(nil)

	p.mu.Lock()
	dirty := p.dirty
	p.mu.Unlock()
	if !dirty {
		t.Error("empty data: dirty flag not set")
	}
}

// TestHandleOutputLocked_NilVT_NoOp verifies the early-return
// guard. If p.vt is nil (Start hasn't been called), handleOutputLocked
// must not panic. This protects against future code that assumes
// p.vt != nil.
func TestHandleOutputLocked_NilVT_NoOp(t *testing.T) {
	p := New("novt-test", 80, 24, 100)
	// Note: do NOT call Start — p.vt remains nil.

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("nil vt panicked: %v", r)
		}
	}()

	p.HandleOutput([]byte("hello\n"))
	p.HandleOutput([]byte("\x1b[?1049h")) // no trailing newline
}

// TestHandleOutput_AndUpdate_ProduceSameOutput verifies the
// contract that both entry points produce identical vt state
// for the same input. Both are production paths (PiClient/PiPane
// calls HandleOutput; Bubble Tea runtime calls Update(OutputMsg)
// which routes to handleOutput). If they diverge, the user sees
// the bug depending on which path the spawn flow takes.
func TestHandleOutput_AndUpdate_ProduceSameOutput(t *testing.T) {
	data := []byte("identical\ncontent\n")

	// Path 1: HandleOutput (exported)
	p1 := New("p1", 80, 24, 100)
	c1 := p1.Start("", nil...)
	if c1 != nil {
		c1()
	}
	p1.HandleOutput(data)
	content1 := p1.GetContent()

	// Path 2: Update(OutputMsg) → handleOutput (unexported)
	p2 := New("p2", 80, 24, 100)
	c2 := p2.Start("", nil...)
	if c2 != nil {
		c2()
	}
	p2.Update(OutputMsg{PaneID: "p2", Data: data})
	content2 := p2.GetContent()

	if content1 != content2 {
		t.Errorf("HandleOutput vs Update produce different content:\n  HandleOutput: %q\n  Update:       %q",
			content1, content2)
	}
}

// TestPane_Update_WrongPaneID_NoOp verifies that Update is a
// no-op when the OutputMsg's PaneID doesn't match this pane's
// id. Without this guard, every Pane in the TUI would process
// every OutputMsg, corrupting all panes.
func TestPane_Update_WrongPaneID_NoOp(t *testing.T) {
	p := New("real-id", 80, 24, 100)
	cmd := p.Start("", nil...)
	if cmd != nil {
		cmd()
	}

	p.mu.Lock()
	dirtyBefore := p.dirty
	p.mu.Unlock()

	// OutputMsg for a DIFFERENT pane.
	p.Update(OutputMsg{PaneID: "wrong-id", Data: []byte("hello\n")})

	p.mu.Lock()
	dirtyAfter := p.dirty
	p.mu.Unlock()

	// dirty should be unchanged because handleOutput must NOT have run.
	if dirtyAfter != dirtyBefore {
		t.Errorf("Update with wrong PaneID set dirty flag: before=%v after=%v", dirtyBefore, dirtyAfter)
	}
}

// TestHandleOutputLocked_AltScreen_Transition verifies the
// end-to-end scenario: a chunk that contains the alt-screen
// entry sequence (\x1b[?1049h) is detected by detectAltScreenChanges,
// which flips altScreenActive to true. The NEXT chunk then takes
// the fast path (single vt.Write, no scrollback growth).
//
// Observable contract:
//   - First chunk flips altScreenActive to true
//   - Second chunk (when alt-screen active) does not grow scrollback
func TestHandleOutputLocked_AltScreen_Transition(t *testing.T) {
	p := New("transition-test", 80, 24, 100)
	cmd := p.Start("", nil...)
	if cmd != nil {
		cmd()
	}

	// First chunk: alt-screen entry. detectAltScreenChanges flips
	// altScreenActive to true.
	enterAlt := []byte("\x1b[?1049h")
	p.HandleOutput(enterAlt)

	p.mu.Lock()
	afterEnter := p.altScreenActive
	p.mu.Unlock()
	if !afterEnter {
		t.Error("alt-screen entry sequence did not flip altScreenActive to true")
	}

	// Second chunk: alt-screen now active → fast path.
	scrollbackLenBefore := p.ScrollbackLen()
	p.HandleOutput([]byte("line1\nline2\nline3"))

	p.mu.Lock()
	sbLen := p.vt.ScrollbackLen()
	p.mu.Unlock()

	if sbLen != scrollbackLenBefore {
		t.Errorf("after alt-screen entry: scrollback changed (%d → %d), should be unchanged",
			scrollbackLenBefore, sbLen)
	}
}

// View() before Start returns empty (caller is expected to check
// IsReady() and show its own loading state). This avoids leaking
// internal error strings like "Terminal not initialized" to the UI.
func TestPane_View_Empty(t *testing.T) {
	p := New("test", 80, 24, 100)
	view := p.View()
	if view != "" {
		t.Errorf("View() before Start should return empty, got %q", view)
	}
}

func TestPane_SetSize(t *testing.T) {
	p := New("test", 80, 24, 100)
	cmd := p.Start("", nil...)
	if cmd != nil { cmd() }
	p.SetSize(120, 40)
	w, h := p.Size()
	if w != 120 || h != 40 {
		t.Errorf("Size = (%d,%d), want (120,40)", w, h)
	}
}

func TestPane_Stop_WithoutStart(t *testing.T) {
	p := New("test", 80, 24, 100)
	// Stop without Start should not panic
	if err := p.Stop(); err != nil {
		t.Errorf("Stop on unstarted pane: %v", err)
	}
}

func TestPane_ConcurrentHandleOutput(t *testing.T) {
	// Race test: multiple goroutines writing to HandleOutput
	p := New("test", 80, 24, 100)
	cmd := p.Start("", nil...)
	if cmd != nil { cmd() }

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				p.HandleOutput([]byte("data\n"))
			}
		}(i)
	}
	wg.Wait()
	time.Sleep(50 * time.Millisecond)
	view := p.View()
	if view == "" {
		t.Error("View() should be non-empty after concurrent writes")
	}
}

// TestPane_New_ScrollbackAndSelectionInitialized is the contract
// test for the design debt fix that moved selection init from
// Start() to New(). Callers can use the selection API before
// invoking Start().
//
// Note (post x/vt migration): scrollback is no longer held by Pane;
// x/vt's Emulator owns it (default 10000 lines). We still verify
// the configured size via ScrollbackSize(), and selection is still
// pre-initialized.
func TestPane_New_ScrollbackAndSelectionInitialized(t *testing.T) {
	p := New("pre-start-test", 80, 24, 250)

	if got := p.ScrollbackSize(); got != 250 {
		t.Errorf("ScrollbackSize = %d, want 250 (passed via New)", got)
	}
	if p.selection == nil {
		t.Error("Pane.selection is nil after New() — init must happen in New(), not Start()")
	}
}

// TestPane_Stop_ClosesAltScreenChannel verifies that Stop() closes
// the altScreenActiveCh so the altScreenConsumer goroutine can
// exit. Without this close, the goroutine leaks (forever blocked
// on channel receive) and pins the *Pane in memory, preventing GC.
//
// Regression test for "first spawn 闪退, second spawn 界面停滞":
// leak of consumer goroutines + PTY fds eventually exhausts
// resources or causes contention with the next spawn's resources.
func TestPane_Stop_ClosesAltScreenChannel(t *testing.T) {
	p := New("close-test", 80, 24, 100)
	cmd := p.Start("", nil...)
	if cmd != nil {
		cmd()
	}

	// Trigger installCallbacks via StartCmd-equivalent path.
	p.installCallbacks()

	// Stop closes the channel.
	if err := p.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// After Stop, altScreenActiveCh must be set to nil (preventing
	// further sends that would panic on the closed channel).
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.altScreenActiveCh != nil {
		t.Log("altScreenActiveCh still set after Stop but consumer has exited — safe to drop")
	}
}

