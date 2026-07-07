package ui

import (
	"sync"
	"testing"

	"github.com/pi/awp/internal/board"
	"github.com/pi/awp/internal/pi"
)

// TestTurnDoneCache_NewPatternIsSafe is a regression test for the
// sync.Map race that crashed awp on 2026-07-07 (see
// ~/.awp/logs/awp-crash-2026-07-07-180225-*.log for the original
// crash report).
//
// The old code in pollTurnDonesAsync used a two-phase sync.Map
// pattern with a nil placeholder:
//
//	cacheI, _ := m.turnDoneCaches.LoadOrStore(ticketID, nil)  // step 1
//	if cacheI == nil {
//	    fresh, _ := pi.NewTurnDoneCacheFromFile(jsonlPath)
//	    actual, _ := m.turnDoneCaches.LoadOrStore(ticketID, fresh)  // step 2
//	    cache = actual.(*pi.TurnDoneCache)  // PANIC if actual is nil
//	}
//
// The race: goroutine A does step 1, stores nil. Goroutine B then
// does step 2 BEFORE A does its own step 2. B's LoadOrStore sees
// the key exists with value nil, returns (nil, true), does NOT
// store fresh. B's `actual.(*pi.TurnDoneCache)` on nil panics.
//
// The fix in model.go removes the nil placeholder: Load first
// (no write), then LoadOrStore with the fresh value. This test
// pins that the new pattern is safe under concurrent access.
func TestTurnDoneCache_NewPatternIsSafe(t *testing.T) {
	var m sync.Map
	key := board.TicketID("test-ticket")

	getOrInit := func(build func() *pi.TurnDoneCache) *pi.TurnDoneCache {
		if existing, ok := m.Load(key); ok && existing != nil {
			return existing.(*pi.TurnDoneCache)
		}
		fresh := build()
		if fresh == nil {
			return nil
		}
		actual, loaded := m.LoadOrStore(key, fresh)
		if loaded && actual != nil {
			return actual.(*pi.TurnDoneCache)
		}
		return fresh
	}

	// Run 100 concurrent goroutines. None should panic.
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cache := getOrInit(func() *pi.TurnDoneCache {
				return &pi.TurnDoneCache{}
			})
			if cache == nil {
				t.Error("getOrInit returned nil")
			}
		}()
	}
	wg.Wait()

	// Final state: map has a non-nil value.
	v, ok := m.Load(key)
	if !ok {
		t.Fatal("expected key in map after concurrent getOrInit")
	}
	if v == nil {
		t.Fatal("expected non-nil value, got nil (race regression)")
	}
}
