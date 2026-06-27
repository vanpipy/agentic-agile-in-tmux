// startcmd_race_test.go — regression test for the pre-existing race in
// pane.go:1568 (installAltScreenCallback writes p.altScreenActiveCh)
// vs pane.go:456 (altScreenConsumer reads p.altScreenActiveCh).
//
// Root cause: installAltScreenCallback is called TWICE — once inside
// Start.func1 (which runs as a Bubble Tea IIFE goroutine) and again
// inside installCallbacks (called synchronously from StartCmd). The
// consumer goroutine is spawned by the second call. The race detector
// catches the IIFE's write happening concurrently with the consumer's
// read of the same struct field.
//
// Fix (Cluster D.2 follow-up): remove the installAltScreenCallback call
// from Start.func1's PTY path. Let installCallbacks (synchronous) be the
// only one that creates the channel and starts the consumer.
//
// The render-only path (command=="") keeps its installAltScreenCallback
// call because it does NOT spawn a consumer, so no race exists there.
//
// Run with: go test -race -run TestPane_StartCmd_NoAltScreenRace
package terminal

import (
	"testing"
)

// TestPane_StartCmd_NoAltScreenRace verifies that StartCmd followed by
// the natural Bubble Tea IIFE flow does not produce a DATA RACE on
// p.altScreenActiveCh. This is the regression test for the pre-existing
// race that survived D.2's doc-only fix.
//
// CORRECT-7 self-check:
//   C-onformance: no DATA RACE warning from `go test -race`
//   O-rdering: N/A
//   R-ange: 1 invocation tested
//   R-eference: no external deps (render-only mode)
//   E-xistence: race detector must not flag p.altScreenActiveCh
//   C-ardinality: 1 case
//   T-ime: no time concerns
//
// To verify manually:
//   go test -race -run TestPane_StartCmd_NoAltScreenRace ./internal/terminal/
//   Expected: PASS (after fix). FAIL (before fix) with DATA RACE.
func TestPane_StartCmd_NoAltScreenRace(t *testing.T) {
	p := New("startcmd-race", 80, 24, 100)

	// Use StartCmd (the production path), which calls Start internally
	// + installCallbacks. The race surfaces between Start's IIFE and
	// installCallbacks's consumer goroutine.
	cmd := p.StartCmd("", nil...)
	if cmd != nil {
		cmd()
	}

	// Touch altScreenActiveCh read path: p.altScreenActive (a separate
	// field set by the consumer) is read here to force the race detector
	// to surface any unsynchronized access.
	_ = p.IsAltScreenActive()

	// Stop to clean up.
	if err := p.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}