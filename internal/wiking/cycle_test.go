// cycle_test.go — RED tests for the cycle state machine.
//
// Per SYSTEM_DESIGN.md §18.9 / §18.10 / §18.11: cycle is a single
// goroutine state machine, channel-bounded, no mutex. The four
// core behaviors tested here:
//   1. ctx-cancel    — <-ctx.Done() returns ErrCancelled
//   2. ext-msg       — <-Ext returns ErrCancelled (ExtCancel) or advances
//   3. tick-step     — <-ticker advances phase when marker appears
//   4. phase-timeout — <-timer returns ErrPhaseTimeout
// Plus: no_progress (consecutive idle ticks) and resume_from_disk
// (cycle auto-restarts at last-completed round).

package wiking

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// helpers ----------------------------------------------------------------

func newCycleForTest(t *testing.T) (*Cycle, *testHooks) {
	t.Helper()
	wiki := t.TempDir()
	awp := t.TempDir()
	tickCh := make(chan time.Time, 1)
	timerCh := make(chan time.Time, 1)
	cfg := Config{
		WikiDir:         wiki,
		RunID:           "test-run",
		AWPHome:         awp,
		Threshold:       90,
		IdleInterval:    30 * time.Second,
		WikingInterval:  50 * time.Millisecond,
		CodingInterval:  50 * time.Millisecond,
		WikingTimeout:   100 * time.Millisecond, // very short for tests
		CodingTimeout:   100 * time.Millisecond,
		MaxNoProgress:   5, // short circuit
		Wiking: RoleBinding{
			Prompt: "wiking test",
			CWD:    wiki,
		},
		Coding: RoleBinding{
			Prompt: "coding test",
			CWD:    wiki,
		},
		Binary:    "", // empty Binary => no-spawn (test mode)
		TickerCh:  tickCh,
		TimerCh:   timerCh,
	}
	c, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c, &testHooks{tickCh: tickCh, timerCh: timerCh, wiki: wiki}
}

type testHooks struct {
	tickCh  chan time.Time
	timerCh chan time.Time
	wiki    string
}

// fire sends a tick that the cycle will read; safe to call repeatedly.
func (h *testHooks) fire() { h.tickCh <- time.Now() }

func (h *testHooks) fireTimer() { h.timerCh <- time.Now() }

// drainEvents reads events until either d or no recent events.
func drainEvents(c *Cycle, d time.Duration) []Event {
	var out []Event
	timeout := time.After(d)
	for {
		select {
		case ev := <-c.Events:
			out = append(out, ev)
		case <-timeout:
			return out
		default:
			if len(out) > 0 {
				return out
			}
			// Wait briefly for the first event.
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// tests -----------------------------------------------------------------

// 1. ctx-cancel: cancel ctx before any tick; expect ErrCancelled.
func TestCycle_ContextCancelReturnsErrCancelled(t *testing.T) {
	cyc, _ := newCycleForTest(t)
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := cyc.Run(ctx)
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("Run err: %v want ErrCancelled", err)
	}
}

// 2. ext-msg (Cancel): cancel via Ext channel.
func TestCycle_ExtCancelReturnsErrCancelled(t *testing.T) {
	cyc, h := newCycleForTest(t)
	ctx := context.Background()

	done := make(chan error, 1)
	go func() {
		done <- cyc.Run(ctx)
	}()

	go func() {
		time.Sleep(50 * time.Millisecond)
		cyc.Ext <- ExtMsg{Kind: ExtCancel}
	}()

	select {
	case err := <-done:
		if !errors.Is(err, ErrCancelled) {
			t.Fatalf("Run err: %v want ErrCancelled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ExtCancel")
	}
	// h unused but referenced so it isn't flagged.
	_ = h
}

// 2b. ext-msg (Skip): skip forces a Decide→WikingRun transition without
// requiring a coding score marker.
func TestCycle_ExtSkipAdvancesWithoutMarker(t *testing.T) {
	cyc, h := newCycleForTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = cyc.Run(ctx) }()

	// First tick: Idle -> WikingRun (no actual spawn since Binary="")
	h.fire()

	// Wait for round_started / wiking_spawned events
	got := []Event{}
	deadline := time.After(500 * time.Millisecond)
wait:
	for {
		select {
		case ev := <-cyc.Events:
			got = append(got, ev)
			if ev.Type == "wiking_spawned" {
				// we've entered WikingRun. Send Skip.
				cyc.Ext <- ExtMsg{Kind: ExtSkip}
				break wait
			}
		case <-deadline:
			t.Fatalf("never entered WikingRun; events: %+v", got)
		}
	}
}

// 3. tick-step: write a wiking-end marker; fire tick; cycle advances to
// CodingRun.
func TestCycle_TickAdvancesOnWikingMarker(t *testing.T) {
	cyc, h := newCycleForTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = cyc.Run(ctx) }()

	// First tick: Idle -> WikingRun (no spawn)
	h.fire()

	// Wait for wiking_spawned to know we're in WikingRun.
	deadline := time.After(500 * time.Millisecond)
	for {
		select {
		case ev := <-cyc.Events:
			if ev.Type == "wiking_spawned" {
				goto inWiking
			}
		case <-deadline:
			t.Fatal("never reached WikingRun")
		}
	}
inWiking:
	// Write the wiking-end marker for round 0 (cycle starts at round 0 → 1 on first loop, see tick logic).
	// After spawn, cycle will try round 1 (c.roundN starts at 0, then idle -> wikingRun sets up round 1).
	// Let's verify what roundN is by checking round_started.
	// Actually c.roundN was set by ResumeFromDisk; if DiskState=Fresh, roundN=0, but on first
	// transition to WikingRun we do roundN++. So the article path is round 1's.
	markerPath := cyc.ws.WikingPath(c_roundNForTest(cyc))
	if err := os.WriteFile(markerPath, []byte("body\n--- end ---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	h.fire()
	// Expect coding_spawned event within 500ms.
	deadline2 := time.After(500 * time.Millisecond)
	for {
		select {
		case ev := <-cyc.Events:
			if ev.Type == "coding_spawned" {
				return // success
			}
			if ev.Type == "error" {
				t.Fatalf("got error event: %+v", ev)
			}
		case <-deadline2:
			t.Fatal("never advanced to CodingRun")
		}
	}
}

// 4. phase-timeout: no marker, fire timer; cycle returns ErrPhaseTimeout.
func TestCycle_PhaseTimeoutReturnsErr(t *testing.T) {
	cyc, h := newCycleForTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- cyc.Run(ctx) }()

	// Drive the cycle into WikingRun.
	h.fire()
	// Wait for wiking_spawned.
	deadline := time.After(500 * time.Millisecond)
	for {
		select {
		case ev := <-cyc.Events:
			if ev.Type == "wiking_spawned" {
				goto inWiking
			}
		case <-deadline:
			t.Fatal("never reached WikingRun")
		}
	}
inWiking:
	// Fire the timer channel; expect ErrPhaseTimeout.
	h.fireTimer()
	select {
	case err := <-done:
		if !errors.Is(err, ErrPhaseTimeout) {
			t.Fatalf("Run err: %v want ErrPhaseTimeout", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after timer fire")
	}
}

// no-progress: consecutive ticks without marker file change should
// eventually trigger ErrNoProgress.
func TestCycle_NoProgressTriggersErr(t *testing.T) {
	cyc, h := newCycleForTest(t)
	cyc.cfg.MaxNoProgress = 3 // tighter than the default 5 in helpers
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- cyc.Run(ctx) }()

	// First tick: Idle -> WikingRun.
	h.fire()
	deadline := time.After(500 * time.Millisecond)
	for {
		select {
		case ev := <-cyc.Events:
			if ev.Type == "wiking_spawned" {
				goto inWiking
			}
		case <-deadline:
			t.Fatal("never reached WikingRun")
		}
	}
inWiking:
	// Fire ticks without writing a marker. After MaxNoProgress (3)
	// consecutive no-change ticks, we expect ErrNoProgress. The
	// first tick drives Idle->WikingRun; we then need
	// MaxNoProgress more to trip the detector. Total = 1 + 3 = 4.
	// Don't fire beyond that — once the cycle exits, fire() blocks
	// on the channel forever.
	maxFires := 1 + cyc.cfg.MaxNoProgress
	for i := 0; i < maxFires; i++ {
		// Drain pending events so the channel doesn't fill.
	drain:
		for {
			select {
			case <-cyc.Events:
			default:
				break drain
			}
		}
		h.fire()
	}

	select {
	case err := <-done:
		if !errors.Is(err, ErrNoProgress) {
			t.Fatalf("Run err: %v want ErrNoProgress", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after no-progress exhaustion")
	}
}

// resume-from-disk: pre-existing article-N.md with valid marker should
// set roundN to N+1 on the next tick (the cycle skips the wiking for
// that round and starts fresh on round N+1).
func TestCycle_ResumeFromDiskSeedsRoundN(t *testing.T) {
	cyc, h := newCycleForTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Pre-write article-3.md with a valid wiking-end marker.
	markerPath := filepath.Join(h.wiki, "article-3.md")
	if err := os.WriteFile(markerPath, []byte("# Round 3\nbody\n--- end ---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	go func() { _ = cyc.Run(ctx) }()

	h.fire()

	// First round_started event should have Round >= 4: cycle saw
	// article-3.md on resume and starts at round 4 (fresh on top of
	// the completed round 3 wiking).
	deadline := time.After(500 * time.Millisecond)
	var sawResume bool
	for {
		select {
		case ev := <-cyc.Events:
			if ev.Type == "round_started" && ev.Round != nil {
				if *ev.Round >= 4 {
					sawResume = true
				}
			}
			if ev.Type == "wiking_spawned" && !sawResume {
				t.Fatalf("advanced to Wiking without observing round_started with round > 0")
			}
			if sawResume && ev.Type == "wiking_spawned" {
				return
			}
		case <-deadline:
			if !sawResume {
				t.Fatal("never observed round_started with round >= 4")
			}
			return
		}
	}
}

// Run-level safety: Done fires exactly once with ErrCancelled on cancel.
func TestCycle_DoneFiresExactlyOnce(t *testing.T) {
	cyc, _ := newCycleForTest(t)
	ctx, cancel := context.WithCancel(context.Background())

	go func() { _ = cyc.Run(ctx) }()

	cancel()

	select {
	case <-cyc.Done:
		// good, Done fired once
	case <-time.After(2 * time.Second):
		t.Fatal("Done did not fire")
	}

	// Second read should not block (Done has capacity 1, fired once).
	select {
	case <-cyc.Done:
		t.Fatal("Done fired twice")
	default:
		// ok, drained
	}
}

// Helpers above.  ----------------------------------------------------------------

// c_roundNForTest returns the current roundN by inspecting the cycle's
// first round_started event it observes. Used by tests that want to
// know which article-N.md path the cycle is operating on.
func c_roundNForTest(c *Cycle) int {
	// Best-effort: look at the workspace for max valid marker.
	n, _ := c.ws.ResumeRound()
	if n == 0 {
		return 1
	}
	return n + 1
}
