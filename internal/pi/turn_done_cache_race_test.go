// turn_done_cache_race_test.go — TDD concurrency test for TurnDoneCache.
//
// Ticket task/awp follow-up (post-audit): the audit's DEATH-phase
// finding flagged that TurnDoneCache has NO mutex even though:
//   1. Its file-header comment says "NOT goroutine-safe"
//   2. pollTurnDonesAsync's doc says "The cache itself has its own
//      mutex, so concurrent access from this goroutine and any
//      Update handler is safe."
//
// These are contradictory, and #2 is false. Today the race doesn't
// fire because only one poll goroutine runs at a time AND Update
// handlers only touch the wrapping sync.Map (Delete), not the
// cache fields. But that's an implicit invariant with no
// enforcement.
//
// This test makes the race visible: it spawns a writer and a
// reader goroutine that hammer Update/IsStale/Path simultaneously.
// Without a mutex, `go test -race` reports the conflict within
// ~10ms. With the mutex added, the test passes cleanly under
// `-race -count=1000`.
//
// Pre-fix expected: FAIL with "DATA RACE" output.
// Post-fix expected: PASS.

package pi

import (
	"sync"
	"testing"
	"time"
)

// TestTurnDoneCache_ConcurrentAccess exercises every non-trivial
// method from multiple goroutines simultaneously. With the mutex
// fix, it should pass cleanly under `go test -race -count=N` for
// any N. Without the fix, the race detector trips immediately.
func TestTurnDoneCache_ConcurrentAccess(t *testing.T) {
	c := NewTurnDoneCache("/test/path.jsonl")

	const writers = 4
	const readers = 8
	const iters = 200

	var wg sync.WaitGroup
	wg.Add(writers + readers)

	now := time.Now()
	stop := make(chan struct{})

	// Writers: Update the cache in tight loops, alternating
	// between "toolUse" and "stop" to exercise every transition.
	for i := 0; i < writers; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				select {
				case <-stop:
					return
				default:
				}
				sr := "toolUse"
				if (id+j)%2 == 0 {
					sr = "stop"
				}
				c.Update(sr, int64(j*100), now.Add(time.Duration(j)*time.Millisecond))
			}
		}(i)
	}

	// Readers: IsStale + Path in tight loops.
	for i := 0; i < readers; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				select {
				case <-stop:
					return
				default:
				}
				_ = c.IsStale(now, int64(j*100))
				_ = c.Path()
				_ = c.LastStopReason()
			}
		}(i)
	}

	wg.Wait()
}

// TestTurnDoneCache_UpdateRaceFromGoroutines is a tighter version
// that exercises just the Update path. Useful for pinpointing
// whether the race is in Update specifically (vs. cross-method
// races between Update and IsStale).
func TestTurnDoneCache_UpdateRaceFromGoroutines(t *testing.T) {
	c := NewTurnDoneCache("/test/path.jsonl")

	const goroutines = 16
	const iters = 500

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				sr := "toolUse"
				if i%3 == 0 {
					sr = "stop"
				}
				if id%2 == 0 {
					sr = "unknown" // some unknown value
				}
				c.Update(sr, int64(i), time.Now())
			}
		}(g)
	}
	wg.Wait()
}
