package terminal

// This file pins the test antipattern spelled out in pane.Update's
// doc-comment and AGENTS.md §5.4. It is white-box (package terminal)
// because it asserts pane.Update's return semantics, which is internal
// API.
//
// The two patterns for driving a Pane's readLoop in tests:
//
//   1. CORRECT — pane.Update(outMsg) for side effect only; cmd stays
//      as the readLoop from pane.StartCmd(). Accumulates scrollback
//      across all chunks.
//
//   2. ANTIPATTERN — cmd = pane.Update(outMsg). Replaces cmd with
//      tea.Batch(readOutput, scheduleRenderTick); subsequent cmd()
//      returns BatchMsg instead of OutputMsg. Only the first chunk
//      feeds handleOutput; everything after is silently dropped.
//
// See pane.Update's doc-comment for the full explanation and the
// reference to commit 6a121d5 which fixed the scrollback test that
// had been hanging 10/10 times because of this antipattern.
//
// This file deliberately avoids invoking cmd() on Update's return
// value in render-only mode — that path eventually depends on
// Bubble Tea's Batch goroutine scheduling which has nondeterministic
// teardown. Pinning the antipattern via test would require either
// a real PTY (slow, integration-only) or accepting flaky timing.
// Instead we pin the CORRECT pattern (which is what the documentation
// tells contributors to copy) and leave the antipattern as a
// pure documentation rule.

import (
	"strings"
	"testing"
)

// TestPane_UpdatePattern_CorrectAccumulatesScrollback pins the
// correct pattern. Drives 10 simulated OutputMsg chunks into a Pane
// via the canonical readLoop-style loop (without actually assigning
// pane.Update's return back to cmd), and asserts ScrollbackLen >= 5.
//
// If this test ever fails because scrollback doesn't accumulate, the
// most likely cause is handleOutput breaking — but the test is
// intentionally structured to mirror the correct pattern from the
// doc-comment, so a regression here also signals that the documented
// pattern itself has drifted.
//
// Render-only mode (Start("", nil...)) is used so the test is fully
// synchronous and doesn't depend on PTY / subprocess lifecycle.
func TestPane_UpdatePattern_CorrectAccumulatesScrollback(t *testing.T) {
	const lines = 10
	p := New("correct", 80, 5, 100)
	cmd := p.Start("", nil...) // render-only, no PTY
	if cmd != nil {
		cmd()
	}
	defer p.Stop()

	// Construct OutputMsg directly — no readLoop needed because we
	// already know what the chunks would look like. This isolates
	// the readLoop-vs-Batch distinction from PTY timing.
	//
	// Mirror the CORRECT pattern: call pane.Update for side effect
	// only. Do NOT assign its return to a variable.
	for i := 0; i < lines; i++ {
		msg := OutputMsg{
			PaneID: p.id,
			Data:   []byte("Line " + strings.Repeat("x", i+1) + "\n"),
		}
		p.Update(msg) // ← correct: side effect only
	}

	got := p.ScrollbackLen()
	if got < 5 {
		t.Errorf("correct pattern: scrollback has %d lines after %d writes, "+
			"expected >= 5 (cursor should have scrolled 5+ lines off "+
			"the 5-row viewport). If this fails, handleOutput or "+
			"x/vt scrollback capture has regressed.",
			got, lines)
	}
}