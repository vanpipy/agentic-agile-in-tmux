// turn_done_cache.go — Ticket task/awp PR 2 (step 4): per-pane
// cache + edge detector.
//
// TurnDoneCache tracks the last observed stopReason + file (mtime,
// offset) for one pane's pi session JSONL. The poll loop in
// pollAgentStatusesAsync calls Update() each cycle; Update() returns
// true iff this observation is a transition INTO "stop" since the
// last observation. That single bool is the signal that fires a
// "<ticket> finished a turn" notification.
//
// Why a dedicated cache (vs scanning each time and comparing):
//
//   - Avoids re-firing every 5 s for an idle session (stop → stop
//     is silent, by design).
//   - Cheap staleness check via (mtime, offset) without parsing the
//     file when nothing has changed (DONE_DETECTION_RESEARCH.md §5 R2).
//   - One cache per pane; the loop iterates a map and updates each.
//
// API contract:
//
//   NewTurnDoneCache(path)         — fresh cache; no observations
//   Update(stopReason, off, mtime) — record + report transition
//   IsStale(mtime, offset)         — does the file have new content?
//   LastStopReason() string         — getter for callers that want
//                                     the current state (e.g., for
//                                     initial UI badge on resume)
//   NewTurnDoneCacheFromFile(path) — load existing session into the
//                                     cache WITHOUT firing (user
//                                     was already aware of the state)
//
// Concurrency: NOT goroutine-safe. All callers serialize through the
// Bubble Tea Update loop, so we don't need a mutex.

package pi

import (
	"errors"
	"os"
	"time"
)

// TurnDoneCache is the per-pane edge detector. See file header for
// the API contract.
type TurnDoneCache struct {
	path string

	// lastStopReason is the most recent stopReason observed. Empty
	// string means "no observation yet" (initial state) or "the
	// last assistant message had no stopReason field".
	lastStopReason string

	// lastOffset / lastMtime are the byte offset and modification
	// time of the JSONL file at the time of the last observation.
	// Used by IsStale to skip work when pi hasn't written anything
	// since the previous poll.
	lastOffset int64
	lastMtime  time.Time
}

// NewTurnDoneCache returns a fresh cache for the given JSONL path.
// No observations have been recorded yet, so Update("stop", ...)
// will fire on the first call (transition from empty → stop).
func NewTurnDoneCache(path string) *TurnDoneCache {
	return &TurnDoneCache{path: path}
}

// Update records a new observation. Returns true iff this observation
// represents a transition INTO the "stop" state — i.e., the previous
// observation was NOT "stop" (could be empty, could be "toolUse",
// could be any other value) and this one is "stop". The poll loop
// fires the notification when (and only when) this returns true.
//
// Callers MUST call this every poll cycle (not just when IsStale)
// because the returned bool must be inspected even when the file
// hasn't changed (transitions are remembered across polls but NOT
// across Update calls — the cache alone tracks state).
//
// Update also stores the offset + mtime so subsequent IsStale
// queries can compare cheaply.
func (c *TurnDoneCache) Update(stopReason string, offset int64, mtime time.Time) bool {
	fired := stopReason == "stop" && c.lastStopReason != "stop"
	c.lastStopReason = stopReason
	c.lastOffset = offset
	c.lastMtime = mtime
	return fired
}

// IsStale reports whether the file at (mtime, offset) likely has new
// content since the last Update. Caller passes the result of
// os.Stat(file); we compare.
//
// Both mtime and offset are checked because:
//
//   - mtime-after → file was written to.
//   - same mtime, larger offset → file grew (some filesystems have
//     1-second mtime resolution; ext3 is the worst offender. Checking
//     offset catches writes within the same mtime tick).
//
// If the file shrank (smaller mtime OR smaller offset), we treat the
// cache as at-least-as-fresh (clock skew, file rewritten). The caller
// could be wrong here, but the cost of being wrong is one extra scan,
// not a missed notification.
func (c *TurnDoneCache) IsStale(mtime time.Time, offset int64) bool {
	if mtime.After(c.lastMtime) {
		return true
	}
	if offset > c.lastOffset {
		return true
	}
	return false
}

// LastStopReason returns the most recent observed stopReason. Empty
// string means "no observation yet" or "assistant wrote no
// stopReason field on the last message". Callers that want to render
// a UI badge can use this directly.
func (c *TurnDoneCache) LastStopReason() string {
	return c.lastStopReason
}

// Path returns the JSONL file the cache is tracking.
func (c *TurnDoneCache) Path() string {
	return c.path
}

// NewTurnDoneCacheFromFile loads an existing session file into the
// cache so the poll loop can resume tracking without re-firing any
// transition. The user's spec is per-task stop notifications: they
// only want to know about NEW stops, not re-pinged for stops that
// were already there when awp started.
//
// Returns a usable (empty) cache + nil if the file doesn't exist
// yet (pi hasn't created it). The poll loop will retry next cycle.
//
// All other I/O / parse errors are returned to the caller.
func NewTurnDoneCacheFromFile(path string) (*TurnDoneCache, error) {
	c := NewTurnDoneCache(path)
	stat, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return c, nil
		}
		return nil, err
	}
	sr, err := DetectLastStopReason(path)
	if err != nil {
		return nil, err
	}
	c.lastStopReason = sr
	c.lastOffset = stat.Size()
	c.lastMtime = stat.ModTime()
	return c, nil
}
