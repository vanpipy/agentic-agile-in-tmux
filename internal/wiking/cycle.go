// Package wiking — cycle.go is the heart of the postman.
//
// The Cycle is a single goroutine state machine that drives one
// wiking↔coding 2-cycle to acceptance (or timeout, cancel, or
// no-progress). All state is touched only by the tick goroutine;
// external commands arrive on Ext (chan ExtMsg); events flow out
// on Events (chan Event); Run()'s exit is signaled on Done (chan error).
//
// Per SYSTEM_DESIGN.md §18.9 / §18.10 / §18.11:
//   - parent Model (in TUI) or headless cmd/awp/cycle owns the
//     Cycle; cyclepane is a viewer over Events
//   - no mutex on *Cycle's FSM fields (phase/roundN/etc.); channels
//     are the boundary
//   - injectable clock/ticker for deterministic tests
//   - SIGTERM-then-WaitDelay-then-SIGKILL dispatch (per AGENTS.md §5.4)

package wiking

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Phase is the cycle's state. Transitions per §18.10:
//
//	Idle --[first tick]--> WikingRun --[marker found]--> CodingRun
//	                                          |
//	                                          +--[marker+score]--> Decide
//	                                                              |
//	                                                              +--[score>=thr]--> Sync --> Done
//	                                                              +--[score<thr] ---> WikingRun (round+1)
//	                                                                   (timeout / cancel / no_progress also reach Done)
type Phase string

const (
	PhaseIdle      Phase = "idle"
	PhaseWikingRun Phase = "wiking_run"
	PhaseCodingRun Phase = "coding_run"
	PhaseDecide    Phase = "decide"
	PhaseSync      Phase = "sync"
	PhaseDone      Phase = "done"
)

// ExtKind discriminates Ext messages from outside the cycle.
type ExtKind int

const (
	ExtCancel ExtKind = iota
	ExtSkip
	ExtForceScore
)

// ExtMsg carries an external command into the cycle.
type ExtMsg struct {
	Kind       ExtKind
	ForceScore int // used when Kind == ExtForceScore
}

// Sentinel errors.
var (
	ErrCancelled     = errors.New("wiking: cycle cancelled")
	ErrPhaseTimeout  = errors.New("wiking: phase timed out")
	ErrNoProgress    = errors.New("wiking: no progress detected")
	ErrAlreadySynced = errors.New("wiking: cycle already accepted")
)

// RoleBinding is the per-role configuration.
type RoleBinding struct {
	Prompt       string
	CWD          string
	AllowedTools []string
}

// Config is the cycle's input. See §18.10 for defaults.
type Config struct {
	WikiDir   string
	RunID     string
	AWPHome   string
	Threshold int

	Wiking RoleBinding
	Coding RoleBinding

	IdleInterval   time.Duration
	WikingInterval time.Duration
	CodingInterval time.Duration
	WikingTimeout  time.Duration
	CodingTimeout  time.Duration
	MaxNoProgress  int

	// Binary path for subprocess spawn. Empty = no-spawn (test mode
	// or dry-run). Production sets this to "pi" via NewDispatch.
	Binary string

	// Logger. Nil falls back to slog.Default().
	Logger *slog.Logger

	// Test hooks: nil = use real time.Ticker/time.NewTimer.
	TickerCh <-chan time.Time // optional override of the tick source
	TimerCh  <-chan time.Time // optional override of the per-phase timeout source
}

// Cycle is the postman state machine.
type Cycle struct {
	cfg Config

	ws       *Workspace
	log      *Log
	dispatch *Dispatch

	phase     Phase
	roundN    int
	lastScore int
	startedAt time.Time

	// Per-phase state for no-progress detection.
	lastFileMtime  time.Time
	noProgressRuns int

	// Active subprocesses; nil between phases. mu guards these from
	// races with killActiveSpawns called from a different goroutine
	// (ext-cancel / ctx-cancel).
	mu        sync.Mutex
	wikingCmd *exec.Cmd
	codingCmd *exec.Cmd

	// runCtx is the ctx passed to Run, exposed to spawn helpers so
	// they cancel with the cycle.
	runCtx context.Context

	// Channels (created in New, owned by *Cycle).
	Events chan Event
	Ext    chan ExtMsg
	Done   chan error
}

// New constructs a Cycle. Validates config and creates the
// workspace + events.jsonl log.
func New(cfg Config) (*Cycle, error) {
	cfg = withDefaults(cfg)

	ws, err := NewWorkspace(WorkspaceConfig{
		WikiDir: cfg.WikiDir,
		RunID:   cfg.RunID,
		AWPHome: cfg.AWPHome,
	})
	if err != nil {
		return nil, fmt.Errorf("wiking: workspace: %w", err)
	}

	logFile, err := OpenLog(ws.EventsPath())
	if err != nil {
		return nil, fmt.Errorf("wiking: open log: %w", err)
	}

	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	c := &Cycle{
		cfg:       cfg,
		ws:        ws,
		log:       logFile,
		dispatch:  NewDispatch().WithBinary(cfg.Binary),
		phase:     PhaseIdle,
		roundN:    0, // resume happens at Run() so pre-existing files are seen
		startedAt: time.Now(),
		Events:    make(chan Event, 32),
		Ext:       make(chan ExtMsg, 1),
		Done:      make(chan error, 1),
	}

	return c, nil
}

// withDefaults fills in zero-value duration fields with the §18.10 defaults.
func withDefaults(cfg Config) Config {
	if cfg.Threshold <= 0 || cfg.Threshold > 100 {
		cfg.Threshold = 90
	}
	if cfg.IdleInterval <= 0 {
		cfg.IdleInterval = 30 * time.Second
	}
	if cfg.WikingInterval <= 0 {
		cfg.WikingInterval = 5 * time.Second
	}
	if cfg.CodingInterval <= 0 {
		cfg.CodingInterval = 10 * time.Second
	}
	if cfg.WikingTimeout <= 0 {
		cfg.WikingTimeout = 30 * time.Minute
	}
	if cfg.CodingTimeout <= 0 {
		cfg.CodingTimeout = 60 * time.Minute
	}
	if cfg.MaxNoProgress <= 0 {
		cfg.MaxNoProgress = 20
	}
	return cfg
}

// Workspace returns the cycle's Workspace.
func (c *Cycle) Workspace() *Workspace { return c.ws }

// Config returns the cycle's effective config (post-defaults).
func (c *Cycle) Config() Config { return c.cfg }

// RoundN returns the current round (1-based; 0 = no round started).
func (c *Cycle) RoundN() int { return c.roundN }

// Phase returns the cycle's current phase. Racy if read concurrently
// with Run(); intended for diagnostics or the post-run view.
func (c *Cycle) Phase() Phase { return c.phase }

// tickInterval returns the per-phase tick interval.
func (c *Cycle) tickInterval() time.Duration {
	switch c.phase {
	case PhaseWikingRun:
		return c.cfg.WikingInterval
	case PhaseCodingRun:
		return c.cfg.CodingInterval
	default:
		return c.cfg.IdleInterval
	}
}

// phaseTimeout returns the per-phase timeout.
func (c *Cycle) phaseTimeout() time.Duration {
	switch c.phase {
	case PhaseWikingRun:
		return c.cfg.WikingTimeout
	case PhaseCodingRun:
		return c.cfg.CodingTimeout
	default:
		// Idle/Decide/Sync are one-shot, no real timeout.
		return 1 * time.Hour
	}
}

// Run drives the cycle to completion (or termination). Returns the
// terminal error; also writes the same error to Done.
func (c *Cycle) Run(ctx context.Context) (runErr error) {
	c.runCtx = ctx
	defer func() {
		if r := recover(); r != nil {
			c.cfg.Logger.Error("cycle panic", "panic", r)
			c.killActiveSpawns()
			runErr = fmt.Errorf("wiking: cycle panic: %v", r)
		}
		if c.log != nil {
			_ = c.log.Close()
		}
		c.Done <- runErr
	}()

	// Resume-from-disk at run start: if any article-N.md on disk has a
	// valid wiking-end marker, set roundN to N (PhaseIdle will
	// increment to N+1 on the first tick — i.e., start fresh beyond
	// the highest wiking-completed round).
	if n, _ := c.ws.ResumeRound(); n > 0 {
		c.roundN = n
	}

	// Tick + timer setup. We may reinstall these on phase transitions
	// (different interval per phase).
	var tickerC <-chan time.Time
	var stopTicker func()
	if c.cfg.TickerCh != nil {
		tickerC = c.cfg.TickerCh
		stopTicker = func() {}
	} else {
		real := time.NewTicker(c.tickInterval())
		tickerC = real.C
		stopTicker = real.Stop
	}
	defer stopTicker()

	var timerC <-chan time.Time
	var resetTimer func(d time.Duration) bool
	if c.cfg.TimerCh != nil {
		timerC = c.cfg.TimerCh
		resetTimer = func(time.Duration) bool { return false }
	} else {
		timer := time.NewTimer(c.phaseTimeout())
		timerC = timer.C
		resetTimer = timer.Reset
		defer timer.Stop()
	}

	for {
		select {
		case <-ctx.Done():
			c.appendEvent(Event{Type: "terminated", Reason: "context_cancelled"})
			c.killActiveSpawns()
			return ErrCancelled

		case msg := <-c.Ext:
			if c.handleExt(msg) {
				c.killActiveSpawns()
				return ErrCancelled
			}

		case <-timerC:
			c.appendEvent(Event{Type: "phase_timeout", Phase: string(c.phase)})
			c.killActiveSpawns()
			return ErrPhaseTimeout

		case <-tickerC:
			done, terminalErr := c.tick()
			if terminalErr != nil {
				return terminalErr
			}
			// Reinstall ticker + timer with the (possibly new) phase's
			// interval/timeout.
			if c.cfg.TickerCh == nil {
				stopTicker()
				real := time.NewTicker(c.tickInterval())
				tickerC = real.C
				stopTicker = real.Stop
			}
			if c.cfg.TimerCh == nil {
				resetTimer(c.phaseTimeout())
			}
			if done {
				return nil
			}
		}
	}
}

// tick advances the state machine by one step for the current phase.
// Returns (done, terminalErr) — done=true means Run() should exit.
func (c *Cycle) tick() (bool, error) {
	switch c.phase {
	case PhaseIdle:
		c.roundN++
		c.noProgressRuns = 0
		c.lastFileMtime = time.Time{}
		c.appendEvent(Event{Type: "round_started", Article: "article"})
		c.spawnWiking()
		c.phase = PhaseWikingRun

	case PhaseWikingRun:
		ok, mtime := c.checkWikingMarker()
		if ok {
			c.appendEvent(Event{
				Type:       "wiking_done",
				MarkerPath: c.ws.WikingPath(c.roundN),
			})
			c.noProgressRuns = 0
			c.lastFileMtime = mtime
			c.spawnCoding()
			c.phase = PhaseCodingRun
			return false, nil
		}
		if c.tallyNoProgress(mtime) {
			c.appendEvent(Event{
				Type: "no_progress", Phase: string(c.phase),
				Ticks: &c.noProgressRuns,
			})
			c.killActiveSpawns()
			return true, ErrNoProgress
		}

	case PhaseCodingRun:
		score, parsed, mtime := c.checkCodingMarker()
		if parsed {
			c.lastScore = score
			c.appendEvent(Event{
				Type: "coding_done", Score: &score,
				MarkerPath: c.ws.FeedbackPath(c.roundN),
			})
			c.appendEvent(Event{Type: "score_parsed", Score: &score})
			c.noProgressRuns = 0
			c.lastFileMtime = mtime
			c.phase = PhaseDecide
			return false, nil
		}
		if c.tallyNoProgress(mtime) {
			c.appendEvent(Event{
				Type: "no_progress", Phase: string(c.phase),
				Ticks: &c.noProgressRuns,
			})
			c.killActiveSpawns()
			return true, ErrNoProgress
		}

	case PhaseDecide:
		if c.lastScore >= c.cfg.Threshold {
			c.appendEvent(Event{
				Type: "score_above_threshold",
				Score: &c.lastScore, Threshold: &c.cfg.Threshold,
			})
			c.phase = PhaseSync
			return false, nil
		}
		next := c.roundN + 1
		c.appendEvent(Event{
			Type: "loop", NextRound: &next,
			Score: &c.lastScore, Threshold: &c.cfg.Threshold,
		})
		// Loop: start next wiking on a fresh round.
		c.roundN++
		c.noProgressRuns = 0
		c.lastFileMtime = time.Time{}
		c.appendEvent(Event{Type: "round_started", Article: "article"})
		c.spawnWiking()
		c.phase = PhaseWikingRun

	case PhaseSync:
		if err := c.ws.SyncOnAccept(c.roundN); err != nil {
			details := err.Error()
			c.appendEvent(Event{Type: "error", Kind: "sync_failed", Details: details})
			return true, err
		}
		c.appendEvent(Event{
			Type: "synced",
			From:  c.ws.WikingPath(c.roundN),
			To:    c.ws.CanonicalPath(),
		})
		c.appendEvent(Event{
			Type: "cycle_accepted",
			Rounds: &c.roundN, FinalRound: &c.roundN,
			FinalScore: &c.lastScore,
		})
		c.phase = PhaseDone
		return true, nil
	}
	return false, nil
}

// tallyNoProgress updates the no-progress counter based on whether
// the marker file's mtime has changed since the last tick. Returns
// true when the counter has reached the threshold (caller should
// exit with ErrNoProgress).
func (c *Cycle) tallyNoProgress(mtime time.Time) bool {
	if mtime.IsZero() {
		// File missing — wiking isn't writing. Count as no progress.
		c.noProgressRuns++
	} else if !mtime.Equal(c.lastFileMtime) {
		c.lastFileMtime = mtime
		c.noProgressRuns = 0
		return false
	} else {
		c.noProgressRuns++
	}
	return c.noProgressRuns >= c.cfg.MaxNoProgress
}

// handleExt processes one external command. Returns true if cycle
// should terminate (ExtCancel).
func (c *Cycle) handleExt(msg ExtMsg) bool {
	switch msg.Kind {
	case ExtCancel:
		c.appendEvent(Event{Type: "terminated", Reason: "user_cancelled"})
		return true
	case ExtSkip:
		switch c.phase {
		case PhaseWikingRun, PhaseCodingRun:
			c.appendEvent(Event{Type: "skipped", Phase: string(c.phase)})
			c.lastScore = 0
			c.phase = PhaseDecide
		}
		return false
	case ExtForceScore:
		c.appendEvent(Event{Type: "forced_score", Score: &msg.ForceScore})
		c.lastScore = msg.ForceScore
		c.phase = PhaseDecide
		return false
	}
	return false
}

// checkWikingMarker wraps CheckEnd and returns (markerFound, mtime).
func (c *Cycle) checkWikingMarker() (bool, time.Time) {
	path := c.ws.WikingPath(c.roundN)
	ok, err := CheckEnd(path)
	mtime := c.statMtime(path)
	if err != nil || !ok {
		return false, mtime
	}
	return true, mtime
}

// checkCodingMarker wraps CheckScore and returns (score, parsed, mtime).
func (c *Cycle) checkCodingMarker() (int, bool, time.Time) {
	path := c.ws.FeedbackPath(c.roundN)
	score, err := CheckScore(path)
	mtime := c.statMtime(path)
	if err != nil {
		return 0, false, mtime
	}
	return score, true, mtime
}

// statMtime returns the file's mod time, or zero on error.
func (c *Cycle) statMtime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// spawnWiking starts a wiking-role subprocess. With cfg.Binary==""
// (test mode), no spawn is performed; the cycle relies on tests or
// external actors to write the marker file.
func (c *Cycle) spawnWiking() {
	if c.cfg.Binary == "" {
		c.appendEvent(Event{Type: "wiking_spawned", CWD: c.cfg.Wiking.CWD})
		return
	}
	args := c.wikingSpawnArgs()
	cmd, err := c.dispatch.Spawn(c.runCtx, SpawnArgs{
		Args: args,
		Dir:  c.cfg.Wiking.CWD,
	})
	if err != nil {
		details := err.Error()
		c.appendEvent(Event{Type: "error", Kind: "wiking_spawn_failed", Details: details})
		return
	}
	if err := cmd.Start(); err != nil {
		details := err.Error()
		c.appendEvent(Event{Type: "error", Kind: "wiking_start_failed", Details: details})
		return
	}
	c.mu.Lock()
	c.wikingCmd = cmd
	c.mu.Unlock()
	pid := cmd.Process.Pid
	c.appendEvent(Event{
		Type: "wiking_spawned", PID: &pid, CWD: c.cfg.Wiking.CWD,
	})
}

// spawnCoding: like spawnWiking, for the coding role.
func (c *Cycle) spawnCoding() {
	if c.cfg.Binary == "" {
		c.appendEvent(Event{Type: "coding_spawned", CWD: c.cfg.Coding.CWD})
		return
	}
	args := c.codingSpawnArgs()
	cmd, err := c.dispatch.Spawn(c.runCtx, SpawnArgs{
		Args: args,
		Dir:  c.cfg.Coding.CWD,
	})
	if err != nil {
		details := err.Error()
		c.appendEvent(Event{Type: "error", Kind: "coding_spawn_failed", Details: details})
		return
	}
	if err := cmd.Start(); err != nil {
		details := err.Error()
		c.appendEvent(Event{Type: "error", Kind: "coding_start_failed", Details: details})
		return
	}
	c.mu.Lock()
	c.codingCmd = cmd
	c.mu.Unlock()
	pid := cmd.Process.Pid
	c.appendEvent(Event{
		Type: "coding_spawned", PID: &pid, CWD: c.cfg.Coding.CWD,
	})
}

// wikingSpawnArgs builds the argv for wiking.
//
// NOTE: pi's exact CLI flags for one-shot prompt + cwd are out of
// scope here. We pass --mode rpc (consistent with awp's other pi
// spawning). Production will fill in the exact flags once pi's CLI
// is finalized; for v1 this is a stub.
func (c *Cycle) wikingSpawnArgs() []string {
	out := []string{"--mode", "rpc"}
	if c.cfg.Wiking.Prompt != "" {
		out = append(out, "--prompt", c.cfg.Wiking.Prompt)
	}
	return out
}

// codingSpawnArgs builds the argv for coding.
func (c *Cycle) codingSpawnArgs() []string {
	out := []string{"--mode", "rpc"}
	if c.cfg.Coding.Prompt != "" {
		out = append(out, "--prompt", c.cfg.Coding.Prompt)
	}
	return out
}

// killActiveSpawns terminates any in-flight subprocesses. Safe to
// call multiple times. Called on ctx-cancel, ext-cancel, and
// phase-timeout. P4.1 polish can verify all paths are reached.
func (c *Cycle) killActiveSpawns() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, cmd := range []*exec.Cmd{c.wikingCmd, c.codingCmd} {
		if cmd != nil && cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}
	c.wikingCmd = nil
	c.codingCmd = nil
}

// appendEvent writes one event to the JSONL log AND to the Events
// channel (non-blocking if the channel is buffered and consumer is
// slow). Best-effort: a dropped event from a full Events buffer
// doesn't fail the cycle — the JSONL log still has it for audit.
func (c *Cycle) appendEvent(ev Event) {
	n := c.roundN
	if ev.Round == nil {
		ev.Round = &n
	}
	if c.cfg.Logger != nil {
		c.cfg.Logger.Info("cycle event", "type", ev.Type, "round", *ev.Round)
	}
	if c.log != nil {
		_ = c.log.Append(ev)
	}
	select {
	case c.Events <- ev:
	default:
		// Drop on full buffer; audit log still has it.
	}
}
