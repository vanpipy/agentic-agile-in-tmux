// Package wiking — events.go implements §18.6 events protocol.
//
// Each event is a single JSONL line in ~/.awp/cycle/{id}/events.jsonl.
// Envelope: v (schema version), id (ULID), ts (RFC3339Nano UTC), type
// (snake_case stable enum from §18.7). Payload fields are per-type
// and optional, all serialized with `omitempty` so a partial struct
// renders as the right subset of fields.
//
// Witness = the marker protocol; news = this file. Events about
// `wiking_done` / `coding_done` / `score_parsed` are emitted only
// after the corresponding marker is parsed (§18.5, §18.6).
package wiking

import (
	"bufio"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// SchemaVersion is the version of the events.jsonl schema this writer
// emits and this reader accepts. Newer versions (v > SchemaVersion)
// are silently skipped by ReadLines with a stderr warning. Older
// versions (v < SchemaVersion) cause a hard error.
const SchemaVersion = 1

// Event is one row in events.jsonl. Envelope fields V/ID/TS are
// auto-populated by Log.Append if the caller leaves them empty
// (they should leave them empty). Type is set by the producer;
// payload fields are filled per Type (the rest serialize as
// `omitempty` and stay absent).
type Event struct {
	V    int    `json:"v"`
	ID   string `json:"id"`
	TS   string `json:"ts"`
	Type string `json:"type"`

	// round_started, loop
	Round   *int   `json:"round,omitempty"`
	Article string `json:"article,omitempty"`

	// wiking_spawned, coding_spawned
	PID       *int   `json:"pid,omitempty"`
	CWD       string `json:"cwd,omitempty"`
	PromptSHA string `json:"prompt_sha,omitempty"`

	// wiking_done, coding_done, cycle_accepted, phase_timeout
	DurationMS *int64 `json:"duration_ms,omitempty"`
	MarkerPath string `json:"marker_path,omitempty"`

	// score_parsed, score_above_threshold, loop, coding_done
	Score     *int `json:"score,omitempty"`
	Threshold *int `json:"threshold,omitempty"`

	// loop
	NextRound *int `json:"next_round,omitempty"`

	// synced
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`

	// cycle_accepted
	Rounds     *int `json:"rounds,omitempty"`
	FinalRound *int `json:"final_round,omitempty"`
	FinalScore *int `json:"final_score,omitempty"`

	// phase_timeout, no_progress
	Phase string `json:"phase,omitempty"`
	Ticks *int   `json:"ticks,omitempty"`

	// cycle_failed, error, terminated
	Kind      string `json:"kind,omitempty"`
	Details   string `json:"details,omitempty"`
	Reason    string `json:"reason,omitempty"`
	LastRound *int   `json:"last_round,omitempty"`
	LastPhase string `json:"last_phase,omitempty"`
}

// Events is a sortable slice of Event. Sorting is by ID (lex) which
// is also chronological order at ms resolution.
type Events []Event

func (es Events) Len() int           { return len(es) }
func (es Events) Less(i, j int) bool { return es[i].ID < es[j].ID }
func (es Events) Swap(i, j int)      { es[i], es[j] = es[j], es[i] }

// Sort sorts Events by ID, which is lex-sortable time order.
func (es Events) Sort() { sort.Sort(es) }

// Log writes Events to a JSONL file with atomic per-line writes.
type Log struct {
	path string
	mu   sync.Mutex
	f    *os.File
}

// OpenLog opens path for append, creating parent directories as
// needed. Existing content is preserved (O_APPEND).
func OpenLog(path string) (*Log, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("wiking: mkdir %s: %w", dir, err)
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("wiking: open %s: %w", path, err)
	}
	return &Log{path: path, f: f}, nil
}

// Append writes one event as a JSONL line. V/ID/TS auto-fill if
// empty. The line is Sync()'d before return so the on-disk truth
// matches in-memory state (§18.6 — atomic per-line writes).
func (l *Log) Append(ev Event) error {
	if l == nil {
		return errors.New("wiking: Append on nil Log")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return errors.New("wiking: Append on closed Log")
	}

	if ev.V == 0 {
		ev.V = SchemaVersion
	}
	if ev.ID == "" {
		ev.ID = NewID()
	}
	if ev.TS == "" {
		ev.TS = NowRFC3339Nano()
	}

	data, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("wiking: marshal event %q: %w", ev.Type, err)
	}
	data = append(data, '\n')

	if _, err := l.f.Write(data); err != nil {
		return fmt.Errorf("wiking: write: %w", err)
	}
	if err := l.f.Sync(); err != nil {
		return fmt.Errorf("wiking: sync: %w", err)
	}
	return nil
}

// Path returns the file path the Log was opened for.
func (l *Log) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

// Close syncs and closes the underlying file. Idempotent — safe to
// call from a different goroutine than the one that called Append.
func (l *Log) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return nil
	}
	err := l.f.Sync()
	if cerr := l.f.Close(); cerr != nil && err == nil {
		err = cerr
	}
	l.f = nil
	return err
}

// Crockford base32 alphabet (the canonical ULID/UUID-short alphabet,
// excluding I/L/O/U to avoid visual confusion).
const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

var crockfordEncoding = base32.NewEncoding(crockfordAlphabet).WithPadding(base32.NoPadding)

// randMu guards newIDRand for the SetRandForTests swap. The mutex
// matters because tests may swap while a goroutine is mid-NewID.
var (
	randMu sync.Mutex
	randR  io.Reader = rand.Reader
)

// NewID returns a fresh 26-character ULID-shaped ID:
//   - 10 chars: 48-bit UTC timestamp in milliseconds, big-endian, base32
//   - 16 chars: 80 bits of cryptographic randomness, base32
//
// Lexicographic order matches chronological order at ms resolution.
// IDs in the same millisecond differ in the random portion.
func NewID() string {
	return newIDAt(time.Now())
}

func newIDAt(t time.Time) string {
	ms := uint64(t.UnixMilli())

	// Encode 48-bit timestamp as 10 chars (base32: 5 bits/char).
	// We pack into 8 bytes (zero-extended), encode, then trim/pad
	// to exactly 10 chars from the right (so the high bits zero
	// out cleanly).
	timeBytes := make([]byte, 8)
	for i := 7; i >= 0; i-- {
		timeBytes[i] = byte(ms)
		ms >>= 8
	}
	timeEncoded := crockfordEncoding.EncodeToString(timeBytes)
	switch {
	case len(timeEncoded) < 10:
		timeEncoded = strings.Repeat("0", 10-len(timeEncoded)) + timeEncoded
	case len(timeEncoded) > 10:
		timeEncoded = timeEncoded[len(timeEncoded)-10:]
	}

	randBytes := make([]byte, 10)
	randMu.Lock()
	_, _ = io.ReadFull(randR, randBytes)
	randMu.Unlock()
	randEncoded := crockfordEncoding.EncodeToString(randBytes)
	if len(randEncoded) > 16 {
		randEncoded = randEncoded[:16]
	}

	return timeEncoded + randEncoded
}

// NowRFC3339Nano returns the current UTC time formatted as RFC3339Nano.
func NowRFC3339Nano() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

// SetRandForTests replaces the entropy source for ULID generation.
// Returns a function that restores the default. Test-only.
func SetRandForTests(r io.Reader) func() {
	randMu.Lock()
	defer randMu.Unlock()
	prev := randR
	randR = r
	return func() {
		randMu.Lock()
		defer randMu.Unlock()
		randR = prev
	}
}

// ReadLines reads events from path, returning those with v ==
// SchemaVersion and id > afterULID. Lines with v > SchemaVersion
// (future schema) are silently skipped with a stderr warning. Lines
// with v < SchemaVersion, malformed JSON, or any other parse
// failure cause a hard error — the audit log is authoritative and
// we don't lie about its contents.
//
// afterULID == "" returns all valid events from the start.
//
// Callers should keep their own cursor (last seen id) and pass it
// here as afterULID; the cyclepane does this with `seenEvents`
// (post-P6.1).
func ReadLines(path, afterULID string) ([]Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("wiking: read %s: %w", path, err)
	}
	defer f.Close()

	var events []Event
	var skippedFuture int
	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var ev Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			return nil, fmt.Errorf("wiking: parse %s:%d: %w", path, lineNo, err)
		}
		switch {
		case ev.V > SchemaVersion:
			skippedFuture++
		case ev.V < SchemaVersion:
			return nil, fmt.Errorf("wiking: %s:%d unsupported schema v=%d (this reader: v=%d)",
				path, lineNo, ev.V, SchemaVersion)
		default:
			if afterULID != "" && ev.ID <= afterULID {
				continue
			}
			events = append(events, ev)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("wiking: scan %s: %w", path, err)
	}
	if skippedFuture > 0 {
		fmt.Fprintf(os.Stderr, "wiking: %s: skipped %d future-schema events (v>%d, ignored)\n",
			filepath.Base(path), skippedFuture, SchemaVersion)
	}
	return events, nil
}
