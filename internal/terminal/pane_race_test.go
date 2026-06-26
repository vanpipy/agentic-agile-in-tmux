package terminal

import (
	"sync"
	"testing"
)

// TestPane_SetWorkdir_ConcurrentRace guards against data race in
// SetWorkdir / GetWorkdir / SetSessionName. Before the fix, these
// methods accessed p.workdir / p.sessionName without holding p.mu
// while other methods (SetSize, View, etc.) held the lock.
//
// Run with `go test -race` to verify no data race detected.
func TestPane_SetWorkdir_ConcurrentRace(t *testing.T) {
	p := New("race-test", 80, 24, 100)

	const goroutines = 8
	const iterations = 200

	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	// Writers
	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				p.SetWorkdir("/workdir/" + string(rune('a'+n)))
				p.SetSessionName("session-" + string(rune('a'+n)))
			}
		}(i)
	}

	// Readers
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = p.GetWorkdir()
				_ = p.Running()
				_, _ = p.Size()
			}
		}()
	}

	wg.Wait()
}
