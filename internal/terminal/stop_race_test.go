// stop_race_test.go — TDD tests for Cluster D.2 fix.
//
// Cluster D.2 (Major from 2026-06-27 audit): pane.go:390 close pattern for
// altScreenActiveCh and consumerDone. Currently -race is clean per memory,
// but the lock state machine is undocumented — future refactors could
// easily introduce deadlocks.
//
// Fix:
//   1. Add a top-of-file doc-comment in pane.go describing the 3 actors
//      and the locks they hold.
//   2. Add TestPane_Stop_DoubleCallIsIdempotent: calling Stop() twice must
//      not panic (verifies sync.Once discipline).
//   3. Add TestPane_Stop_RaceWithOutput: concurrent Stop + HandleOutput
//      must not deadlock or race (run under -race).
//
// These tests pin the contract so future changes to the close-pattern
// surface the behavior change for review.
package terminal

import (
	"sync"
	"testing"
	"time"
)

// TestPane_Stop_DoubleCallIsIdempotent pins the contract that Stop() can
// be called multiple times safely. The implementation uses sync.Once;
// this test verifies the contract from the outside.
//
// CORRECT-7 self-check:
//   C-onformance: no panic, no error on second call
//   O-rdering: N/A
//   R-ange: 2 calls (minimum to test idempotency)
//   R-eference: no external deps
//   E-xistence: pane must be stopped; not stopped is also valid
//   C-ardinality: 1 pane, 2 Stop calls
//   T-ime: no time concerns
func TestPane_Stop_DoubleCallIsIdempotent(t *testing.T) {
	p := New("double-stop", 80, 24, 100)
	cmd := p.Start("", nil...)
	if cmd != nil {
		cmd()
	}

	// First Stop: must not panic.
	if err := p.Stop(); err != nil {
		t.Fatalf("first Stop: %v", err)
	}

	// Second Stop: must NOT panic and should return nil (or whatever
	// the first call returned; we don't pin a specific value here).
	// The contract is: no panic, no deadlock.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("second Stop panicked: %v", r)
		}
	}()
	if err := p.Stop(); err != nil {
		t.Errorf("second Stop returned error: %v (expected idempotent)", err)
	}

	// Third Stop for good measure.
	if err := p.Stop(); err != nil {
		t.Errorf("third Stop returned error: %v", err)
	}
}

// TestPane_Stop_RaceWithOutput verifies that concurrent Stop and
// HandleOutput calls don't race or deadlock. Run with `go test -race`
// to actually detect races.
//
// CORRECT-7 self-check:
//   C-onformance: no panic, no deadlock
//   O-rdering: N/A (concurrent)
//   R-ange: 100 Stop+HandleOutput iterations
//   R-eference: no external deps
//   E-xistence: N/A
//   C-ardinality: 2 goroutines + 1 main
//   T-ime: bounded (1s timeout via goroutine + channel)
func TestPane_Stop_RaceWithOutput(t *testing.T) {
	p := New("race-stop-output", 80, 24, 100)
	cmd := p.Start("", nil...)
	if cmd != nil {
		cmd()
	}

	// Race test pattern: writer goroutine + Stop goroutine.
	// The race detector (when run via `go test -race`) catches any
	// unsynchronized access to p.vt or other shared state.
	var wg sync.WaitGroup
	stopped := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			p.HandleOutput([]byte("data\n"))
			time.Sleep(time.Microsecond)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		// Stop while the writer is active.
		time.Sleep(time.Millisecond)
		p.Stop()
		close(stopped)
	}()

	// Wait for the writer to finish OR for Stop to complete.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Either the writer finished before Stop, or Stop won the race.
		// Both outcomes are valid.
	case <-time.After(2 * time.Second):
		t.Fatal("deadlock: Stop + HandleOutput did not complete in 2s")
	}

	// After the test, the pane should be in a Stopped state.
	<-stopped
	// Calling Stop again must be idempotent.
	if err := p.Stop(); err != nil {
		t.Errorf("post-race Stop: %v", err)
	}
}