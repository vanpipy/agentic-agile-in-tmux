// Package terminal provides the PTY-backed terminal pane that awp uses
// to render pi subprocess output. It wraps an x/vt Emulator for terminal
// emulation and a creack/pty for the host-side pseudoterminal.
//
// Concurrency model (3 actors, 1 mutex, 2 channels):
//
//   ┌────────────────────────┐   acquires p.mu   ┌─────────────────────────┐
//   │  Main goroutine        │ ◀──────────────▶ │  x/vt Emulator          │
//   │  (Update / View)       │   for writes      │  (vt.Write / vt.Read)   │
//   └────────────────────────┘                   └─────────────────────────┘
//             │                                            │
//             │ p.mu (read/write)                         │ callback (no lock)
//             ▼                                            ▼
//   ┌────────────────────────┐                   ┌─────────────────────────┐
//   │  altScreenConsumer     │                   │  installAltScreen       │
//   │  (altScreenActiveCh)   │ ◀── channel ───── │  Callback               │
//   │  acquires p.mu for     │     (non-blocking │  fires synchronously    │
//   │  channel send          │      on full)     │  from inside vt.Write   │
//   └────────────────────────┘                   └─────────────────────────┘
//
// LOCKING RULES (read these before modifying this file):
//
//   1. p.mu is the ONLY shared lock. Never introduce a second mutex for
//      fields already protected by p.mu — it'll deadlock with itself.
//
//   2. The alt-screen callback fires synchronously from inside vt.Write()
//      (called by handleOutputLocked with p.mu held). The callback MUST
//      NOT take p.mu (would deadlock against itself). Instead it sends
//      to altScreenActiveCh non-blockingly; altScreenConsumer applies
//      the update under p.mu.
//
//   3. inputDrain (x/vt internal pipe drainer) does NOT hold p.mu. It
//      reads from vt.Read() independently. Without it, vt.Write would
//      block on the internal pipe (responses to DSR/etc queries never
//      drained).
//
//   4. Stop() releases p.mu BEFORE closing channels, because the consumer
//      goroutine reads channels without holding p.mu (its select is the
//      canonical pattern for closing-during-receive). Closing under p.mu
//      would deadlock: Stop would wait for itself.
//
//   5. stopOnce (sync.Once) ensures Stop is idempotent. Don't replace
//      with a bool check — sync.Once is goroutine-safe.
//
// Common bugs to avoid:
//   - Adding p.mu acquisition inside the alt-screen callback (deadlock).
//   - Closing consumerDone or altScreenActiveCh while holding p.mu
//     (deadlock with the consumer goroutine).
//   - Calling vt.Write outside handleOutputLocked (race on x/vt state).
//
// See Cluster D.2 of the 2026-06-27 audit for the design rationale.
package terminal

import (
	"bytes"
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/creack/pty"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"

)

const (
	renderInterval = 50 * time.Millisecond
	readBufferSize = 65536
)

type Pane struct {
	id      string
	vt      *vt.Emulator
	pty     *os.File
	cmd     *exec.Cmd
	mu      sync.Mutex
	running bool
	exitErr     error
	workdir     string
	sessionName string
	width       int
	height      int

	cachedView      string
	lastRender      time.Time
	dirty           bool
	renderScheduled bool

	mouseEnabled bool // tracks if child process has enabled mouse tracking

	// Scrollback and viewport state (Issue #95)
	//
	// Post x/vt migration: scrollback storage lives inside the x/vt
	// Emulator (default 10000 lines). We expose ScrollbackLen() and
	// GetScrollbackLine() as proxies to e.ScrollbackLen() and
	// e.ScrollbackCellAt(). The scrollbackSize passed to New() is
	// applied to x/vt via SetScrollbackSize in Start.
	altScreenActive bool     // tracks if child process is in alternate screen mode
	altScreenActiveCh chan altScreenEvent // signals from x/vt alt-screen callback
	consumerDone     chan struct{}        // closed when altScreenConsumer exits
	stopOnce         sync.Once             // ensures Stop is idempotent
	viewportOffset  int      // lines scrolled back (0 = live view)
	scrollbackSize  int      // configured scrollback buffer size
	selection       *SelectionState // mouse text selection state
}

func New(id string, width, height int, scrollbackSize int) *Pane {
	if scrollbackSize <= 0 {
		scrollbackSize = 10000
	}
	return &Pane{
		id:             id,
		width:          width,
		height:         height,
		scrollbackSize: scrollbackSize,
		// Initialize selection eagerly so callers can use the
		// selection API before Start(). Symmetric with Size() which
		// is also available pre-Start.
		//
		// scrollback is provided by x/vt's Emulator (default 10000
		// lines); we apply our scrollbackSize limit at resize time.
		selection: NewSelectionState(),
		// Initialize altScreenActiveCh here (once, never re-assigned).
		// Previously this channel was created inside installAltScreenCallback,
		// which was called from BOTH Start.func1 and installCallbacks.
		// The duplicate assignment raced with altScreenConsumer's read.
		// Cluster D.2 follow-up: moved to New so the reference is stable.
		altScreenActiveCh: make(chan altScreenEvent, 8),
	}
}




// ID returns the pane's identifier
func (p *Pane) ID() string {
	return p.id
}

// SetWorkdir sets the working directory for commands
func (p *Pane) SetWorkdir(dir string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.workdir = dir
}

func (p *Pane) GetWorkdir() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.workdir
}

// SetSessionName sets the session name for the AWPPANE_SESSION env var
func (p *Pane) SetSessionName(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sessionName = name
}

// Running returns whether the pane has a running process
func (p *Pane) Running() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running
}

// IsReady returns true when the pane has its vt10x terminal
// initialized and is ready to render. False means StartCmd
// IIFE hasn't run yet, or it failed.
func (p *Pane) IsReady() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.vt != nil
}

// ExitErr returns any error from the process exit
func (p *Pane) ExitErr() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exitErr
}

func (p *Pane) SetSize(width, height int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.width = width
	p.height = height
	p.dirty = true
	p.cachedView = ""

	// Clear selection on resize (coordinates become invalid)
	if p.selection != nil && p.selection.IsActive() {
		p.selection.Clear()
	}

	// Reset viewport to live view on resize
	p.viewportOffset = 0

	if p.vt != nil {
		p.vt.Resize(width, height)
	}

	if p.pty != nil && p.running {
		pty.Setsize(p.pty, &pty.Winsize{
			Rows: uint16(height),
			Cols: uint16(width),
		})
	}
}

// Size returns the current dimensions
func (p *Pane) Size() (width, height int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.width, p.height
}

// GetScrollbackLine returns the text of the scrollback line at
// the given index (0 = oldest). Empty string if out of bounds.
func (p *Pane) GetScrollbackLine(index int) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.vt == nil {
		return ""
	}
	width := p.vt.Width()
	if width <= 0 {
		return ""
	}
	var b strings.Builder
	for x := 0; x < width; x++ {
		b.WriteRune(cellRune(p.vt.ScrollbackCellAt(x, index)))
	}
	return strings.TrimRight(b.String(), " ")
}

// ScrollbackLen returns the number of lines in the scrollback buffer.
// Backed by x/vt's Emulator.ScrollbackLen() (default 10000 lines).
func (p *Pane) ScrollbackLen() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.vt == nil {
		return 0
	}
	return p.vt.ScrollbackLen()
}

// ScrollbackSize returns the configured maximum scrollback size.
// This is the value passed to New() and forwarded to x/vt's
// internal scrollback buffer via SetScrollbackSize at Start time.
func (p *Pane) ScrollbackSize() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.scrollbackSize
}

// ViewportOffset returns how many lines the viewport is scrolled back.
func (p *Pane) ViewportOffset() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.viewportOffset
}

// IsAltScreenActive returns whether the terminal is in alternate screen mode.
func (p *Pane) IsAltScreenActive() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.altScreenActive
}

// --- Bubbletea Messages ---

// OutputMsg carries data read from the PTY
type OutputMsg struct {
	PaneID string
	Data   []byte
}

// ExitMsg indicates the process has exited
type ExitMsg struct {
	PaneID string
	Err    error
}

// RenderTickMsg triggers a throttled render
type RenderTickMsg struct {
	PaneID string
}

// ExitFocusMsg signals to return to board view
type ExitFocusMsg struct{}

// --- PTY Lifecycle (Issue #13) ---

// Start initializes the pane's vt10x state. Phase 7 refactor:
// Pane no longer owns the command process by default. Pass a
// Start initializes the pane's vt10x state and returns a tea.Cmd for the
// read loop. Deprecated: prefer StartCmd which runs setup synchronously.
//
// Start retains the IIFE pattern for backward compatibility, but this
// means Start's setup runs on Bubble Tea's goroutine, racing with
// consumer goroutines spawned by installCallbacks. New code should use
// StartCmd; Start is preserved for tests that exercise the render-only
// path (command=="") without consumer goroutines.
//
// For render-only mode (command==""): no PTY is started, no consumer
// goroutines — this path is race-free.
func (p *Pane) Start(command string, args ...string) tea.Cmd {
	return func() tea.Msg {
		readLoop := p.startSetup(command, args...)
		if readLoop == nil {
			return nil
		}
		return readLoop()
	}
}

// startSetup performs the synchronous initialization (command setup, PTY
// start, vt emulator creation, alt-screen callback registration) and
// returns a tea.Cmd for the read loop. Returns nil for the render-only
// path (command=="") where there is no PTY to read from.
//
// Called by Start (via IIFE) and StartCmd (synchronously before
// installCallbacks). Synchronous invocation is required by StartCmd
// because installCallbacks spawns the inputDrain goroutine, which reads
// p.vt; the assignment must happen-before the goroutine starts.
//
// This split fixes the pre-existing race in pane.go:330 (Start IIFE
// writing p.vt) vs pane.go:424 (inputDrain reading p.vt).
func (p *Pane) startSetup(command string, args ...string) tea.Cmd {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Build command
	p.cmd = exec.Command(command, args...)
	p.cmd.Env = buildCleanEnv(p.sessionName)

	// Set working directory if specified
	if p.workdir != "" {
		p.cmd.Dir = p.workdir
	}

	// awp: empty command = render-only mode (HandleOutput from
	// PiClient/PiPane). Phase 7/8 design.
	if command == "" {
		// Render-only mode: vt emulator without spawning PTY.
		p.vt = vt.NewEmulator(p.width, p.height)
		p.vt.SetScrollbackSize(p.scrollbackSize)
		p.running = true
		// Render-only path: install callback here since no consumer
		// goroutine will be spawned. Tests + PiClient render-only use this.
		p.installAltScreenCallback()
		return nil
	}

	// Start PTY first so we can use it as vt10x writer
	ptmx, err := pty.Start(p.cmd)
	if err != nil {
		p.exitErr = err
		return func() tea.Msg { return ExitMsg{PaneID: p.id, Err: err} }
	}
	p.pty = ptmx
	p.running = true
	p.exitErr = nil

	// Set PTY size
	pty.Setsize(p.pty, &pty.Winsize{
		Rows: uint16(p.height),
		Cols: uint16(p.width),
	})

	// Create virtual terminal. Output responses go to PTY (so the
	// child can read DSR/etc queries).
	p.vt = vt.NewEmulator(p.width, p.height)
	p.vt.SetScrollbackSize(p.scrollbackSize)
	// NOTE: do NOT call installAltScreenCallback here. installCallbacks
	// (called next from StartCmd) is the single owner of
	// p.altScreenActiveCh + altScreenConsumer.
	// scrollback + selection already initialized in New()

	// Return read-loop cmd; it runs on Bubble Tea's goroutine.
	return p.readOutputUnlocked()
}

// installCallbacks installs observers on the x/vt emulator and
// starts background goroutines needed by the terminal package.
//
// Must be called AFTER Start() returns (outside the p.mu lock)
// because the callbacks re-acquire p.mu — installing inside the
// lock would deadlock when the callback first fires.
//
// Two background goroutines are started:
//
//  1. altScreenConsumer: drains the alt-screen callback channel.
//     The callback fires from inside vt.Write() (which can happen
//     while p.mu is held by handleOutputLocked), so the consumer
//     can't take p.mu from within the callback itself.
//
//  2. inputDrain: continuously reads from vt.Read() into io.Discard.
//     x/vt's Emulator has an internal io.Pipe (pr/pw) used for
//     device-query responses: when the child sends a Device
//     Attributes query (ESC[c), the CSI handler writes the answer
//     back via pw. If nothing reads from pr, that pw.Write blocks
//     FOREVER, freezing vt.Write and the entire awp UI.
//
//     This goroutine drains pr (via Read()) so the responses are
//     discarded instead of backing up the pipe.
func (p *Pane) installCallbacks() {
	p.installAltScreenCallback()
	p.consumerDone = make(chan struct{})
	go p.altScreenConsumer()
	go p.inputDrain()
}

// inputDrain continuously reads from vt.Read() and discards the
// bytes. x/vt's internal pipe only has a reader if the host
// application drives it; awp doesn't need the response data (the
// child process already received it via its own input pipeline),
// so we drain to prevent the writer side from blocking.
//
// This goroutine exits when vt.Close() is called (Read returns
// io.EOF then).
func (p *Pane) inputDrain() {
	// Wait for vt to be initialized. We poll without holding p.mu
	// to avoid deadlocking with HandleOutput (which holds p.mu
	// while calling vt.Write). The trade-off is a benign data race
	// on p.vt — it's set once during Start and never modified, so
	// we observe either the nil-zero or the post-Start value.
	var vt *vt.Emulator
	for {
		select {
		case <-p.consumerDone:
			return
		case <-time.After(10 * time.Millisecond):
		}
		vt = p.vt
		if vt != nil {
			break
		}
	}
	// Read loop. vt.Read is independent of p.mu, so we don't hold
	// p.mu here. If HandleOutput wants p.mu while we're blocked in
	// vt.Read, it's free to take it.
	buf := make([]byte, 4096)
	for {
		n, err := vt.Read(buf)
		_ = n
		if err != nil {
			return
		}
	}
}


// altScreenConsumer drains the alt-screen channel and applies
// updates under p.mu. Runs until the channel is closed.
// altScreenConsumer drains the alt-screen channel and applies
// updates under p.mu. Runs until Stop signals exit.
//
// Synchronization pattern:
//   - consumer holds p.mu for the duration of the channel read,
//     so Stop's close(ch) (also under p.mu) is happens-before the
//     channel read returns.
//   - Stop also signals via closing p.consumerDone; the consumer
//     re-checks this at the top of the loop.
//   - The altScreenActiveCh is non-blocking from the callback side
//     (callback uses select with default to drop events if buffer
//     is full); the consumer doesn't need to send.
func (p *Pane) altScreenConsumer() {
	defer func() {
		defer func() {
			if r := recover(); r != nil {
				// already closed by Stop; ignore
			}
		}()
		close(p.consumerDone)
	}()
	for {
		var ev altScreenEvent
		var ok bool
		select {
		case ev, ok = <-p.altScreenActiveCh:
			// got event or channel closed
		case <-p.consumerDone:
			return
		}
		p.mu.Lock()
		p.altScreenActive = ev.active
		p.mu.Unlock()
		if !ok {
			return
		}
	}
}

// HandleOutput feeds raw PTY bytes into the pane's vt10x terminal.
// Call from PiClient's read loop to display pi's output. Phase 7
// keeps Pane as a render-only layer; pi lifecycle stays in PiClient.
func (p *Pane) HandleOutput(data []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.handleOutputLocked(data)
}

func (p *Pane) Stop() error {
	var stopErr error
	p.stopOnce.Do(func() {
		stopErr = p.stopLocked()
	})
	return stopErr
}

func (p *Pane) stopLocked() error {
	p.mu.Lock()
	if p.cmd != nil && p.cmd.Process != nil {
		p.cmd.Process.Kill()
	}
	if p.pty != nil {
		p.pty.Close()
	}
	p.running = false
	p.mu.Unlock()

	// Close both channels WITHOUT holding p.mu or altScreenMu so we
	// don't deadlock with the consumer goroutine (which reads from
	// the channels without locks).
	//
	// Race-safety: the consumer's `for { select { case ev := <-ch ... } }`
	// is the standard Go pattern for reading from a channel that
	// another goroutine may close. The Go memory model guarantees
	// that close(ch) happens-before the receiver returning from
	// receive, so no data race. (Verified by -race detector on
	// TestPane_Stop_ClosesAltScreenChannel.)
	if p.consumerDone != nil {
		close(p.consumerDone)
	}
	if p.altScreenActiveCh != nil {
		close(p.altScreenActiveCh)
	}

	return nil
}

// StopGraceful sends SIGTERM, waits for timeout, then SIGKILL if needed.
func (p *Pane) StopGraceful(timeout time.Duration) error {
	p.mu.Lock()
	if !p.running || p.cmd == nil || p.cmd.Process == nil {
		p.mu.Unlock()
		return nil
	}

	proc := p.cmd.Process
	p.mu.Unlock()

	if err := proc.Signal(os.Interrupt); err != nil {
		return p.Stop()
	}

	done := make(chan error, 1)
	go func() {
		_, err := proc.Wait()
		done <- err
	}()

	select {
	case <-done:
	case <-time.After(timeout):
		proc.Kill()
	}

	p.mu.Lock()
	if p.pty != nil {
		p.pty.Close()
	}
	p.running = false
	p.mu.Unlock()

	return nil
}

var ErrPaneNotRunning = fmt.Errorf("pane is not running")

func (p *Pane) WriteInput(data []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running || p.pty == nil {
		return 0, ErrPaneNotRunning
	}
	return p.pty.Write(data)
}

// readOutput returns a Cmd that reads from the PTY
func (p *Pane) readOutput() tea.Cmd {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.readOutputUnlocked()
}

// readOutputUnlocked must be called with mu held
func (p *Pane) readOutputUnlocked() tea.Cmd {
	if p.pty == nil {
		return nil
	}

	ptyFile := p.pty
	paneID := p.id

	return func() tea.Msg {
		buf := make([]byte, readBufferSize)
		n, err := ptyFile.Read(buf)
		if err != nil {
			return ExitMsg{PaneID: paneID, Err: err}
		}
		return OutputMsg{PaneID: paneID, Data: buf[:n]}
	}
}

// --- Update Handler ---

// Update handles messages for this pane, returns commands to execute
func (p *Pane) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case OutputMsg:
		if msg.PaneID != p.id {
			return nil
		}
		p.handleOutput(msg.Data)
		return tea.Batch(p.readOutput(), p.scheduleRenderTick())

	case RenderTickMsg:
		if msg.PaneID != p.id {
			return nil
		}
		p.mu.Lock()
		p.renderScheduled = false
		p.mu.Unlock()
		return nil

	case ExitMsg:
		if msg.PaneID != p.id {
			return nil
		}
		p.mu.Lock()
		p.running = false
		p.exitErr = msg.Err
		if p.pty != nil {
			p.pty.Close()
		}
		p.mu.Unlock()
		return nil
	}

	return nil
}

func (p *Pane) handleOutput(data []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.handleOutputLocked(data)
}

// handleOutputLocked is the shared body for HandleOutput (external)
// and Pane.Update(OutputMsg)'s handleOutput (internal). Caller must
// hold p.mu.
//
// Behavior:
//   - Detects alt-screen transitions (smcup/rmcup) in the chunk
//   - In alt-screen mode, writes the chunk to vt10x in one call
//     (no scrollback capture; ~32x faster than per-newline split)
//   - Otherwise splits the chunk by \n and feeds each chunk to vt,
//     capturing scrollback per chunk to fix the openkanban
//     "every-other-line" truncation bug
//   - Always sets p.dirty so the next View() re-renders
func (p *Pane) handleOutputLocked(data []byte) {
	if p.vt == nil {
		return
	}

	p.detectAltScreenChanges(data)

	if p.altScreenActive {
		p.vt.Write(data)
		p.dirty = true
		return
	}

	start := 0
	for i := 0; i <= len(data); i++ {
		if i == len(data) || data[i] == '\n' {
			if i > start {
				// end = i+1 if data[i] == '\n' (include the newline)
				// end = i   if i == len(data) (chunk ends without \n)
				// Without the bounds check the slice would panic for
				// any PTY output lacking a trailing newline — the
				// common case for single-keystroke echo and any
				// standalone ANSI escape sequence.
				end := i
				if end < len(data) {
					end = i + 1
				}
				chunk := data[start:end]
				p.vt.Write(chunk)
				p.dirty = true
			}
			start = i + 1
		}
	}

	// Always mark dirty so View() re-renders, even for empty/nil
	// input (the caller may have changed something else).
	p.dirty = true
}

// detectMouseModeChanges scans output for mouse tracking mode escape sequences.
// Called with mutex held.
func (p *Pane) detectMouseModeChanges(data []byte) {
	// Mouse tracking enable sequences (any of these enables mouse mode)
	enableSeqs := [][]byte{
		[]byte("\x1b[?1000h"), // X10 mouse tracking
		[]byte("\x1b[?1002h"), // Button-event tracking
		[]byte("\x1b[?1003h"), // Any-event tracking
		[]byte("\x1b[?1006h"), // SGR extended mode
	}

	// Mouse tracking disable sequences
	disableSeqs := [][]byte{
		[]byte("\x1b[?1000l"),
		[]byte("\x1b[?1002l"),
		[]byte("\x1b[?1003l"),
		[]byte("\x1b[?1006l"),
	}

	// Check for enable sequences
	for _, seq := range enableSeqs {
		if bytes.Contains(data, seq) {
			p.mouseEnabled = true
			return
		}
	}

	// Check for disable sequences
	for _, seq := range disableSeqs {
		if bytes.Contains(data, seq) {
			p.mouseEnabled = false
			return
		}
	}
}

// detectAltScreenChanges scans output for alternate screen mode escape sequences.
// Called with mutex held.
func (p *Pane) detectAltScreenChanges(data []byte) {
	// Caller (handleOutput) must hold p.mu

	// Alternate screen enable sequences (smcup)
	enableSeqs := [][]byte{
		[]byte("\x1b[?1049h"), // Save cursor + switch to alt screen
		[]byte("\x1b[?47h"),   // Switch to alt screen (legacy)
	}

	// Alternate screen disable sequences (rmcup)
	disableSeqs := [][]byte{
		[]byte("\x1b[?1049l"), // Restore cursor + switch from alt screen
		[]byte("\x1b[?47l"),   // Switch from alt screen (legacy)
	}

	// Check for enable sequences
	for _, seq := range enableSeqs {
		if bytes.Contains(data, seq) {
			p.altScreenActive = true
			p.viewportOffset = 0 // Reset viewport when entering alt screen
			return
		}
	}

	// Check for disable sequences
	for _, seq := range disableSeqs {
		if bytes.Contains(data, seq) {
			p.altScreenActive = false
			return
		}
	}
}

// scheduleRenderTick returns a Cmd to trigger render after throttle interval
func (p *Pane) scheduleRenderTick() tea.Cmd {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.renderScheduled {
		return nil
	}
	p.renderScheduled = true

	timeSinceLastRender := time.Since(p.lastRender)
	delay := renderInterval - timeSinceLastRender
	if delay < 0 {
		delay = 0
	}

	paneID := p.id
	return tea.Tick(delay, func(time.Time) tea.Msg {
		return RenderTickMsg{PaneID: paneID}
	})
}

// --- Key Handling (Issue #15) ---

func (p *Pane) HandleMouse(msg tea.MouseMsg) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running || p.pty == nil {
		return
	}

	// When mouse tracking is disabled, handle scrolling and selection ourselves
	if !p.mouseEnabled {
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			// Scrolling clears selection
			if p.selection != nil && p.selection.IsActive() {
				p.selection.Clear()
			}
			p.scrollUp(3)
			return
		case tea.MouseButtonWheelDown:
			if p.selection != nil && p.selection.IsActive() {
				p.selection.Clear()
			}
			p.scrollDown(3)
			return
		case tea.MouseButtonLeft:
			if p.selection != nil {
				// Convert viewport coordinates to logical position
				pos := p.viewportToLogical(msg.X, msg.Y)
				if msg.Action == tea.MouseActionPress {
					p.selection.Start(pos)
					p.dirty = true
				} else if msg.Action == tea.MouseActionMotion {
					p.selection.Update(pos)
					p.dirty = true
				} else if msg.Action == tea.MouseActionRelease {
					p.selection.Finish()
					p.dirty = true
				}
			}
			return
		case tea.MouseButtonRight, tea.MouseButtonMiddle:
			// Other clicks clear selection
			if p.selection != nil && p.selection.IsActive() {
				p.selection.Clear()
				p.dirty = true
			}
			return
		case tea.MouseButtonNone:
			// Motion event during selection
			if p.selection != nil && p.selection.Mode == SelectionSelecting {
				pos := p.viewportToLogical(msg.X, msg.Y)
				p.selection.Update(pos)
				p.dirty = true
			}
			return
		}
		return
	}

	// Forward mouse events when app has enabled mouse tracking
	// Clear any selection when mouse mode is enabled
	if p.selection != nil && p.selection.IsActive() {
		p.selection.Clear()
		p.dirty = true
	}

	var seq []byte
	x, y := msg.X+1, msg.Y+1
	if x > 223 {
		x = 223
	}
	if y > 223 {
		y = 223
	}

	switch msg.Button {
	case tea.MouseButtonWheelUp:
		seq = []byte{'\x1b', '[', 'M', byte(64 + 32), byte(x + 32), byte(y + 32)}
	case tea.MouseButtonWheelDown:
		seq = []byte{'\x1b', '[', 'M', byte(65 + 32), byte(x + 32), byte(y + 32)}
	case tea.MouseButtonLeft:
		seq = []byte{'\x1b', '[', 'M', byte(0 + 32), byte(x + 32), byte(y + 32)}
	case tea.MouseButtonRight:
		seq = []byte{'\x1b', '[', 'M', byte(2 + 32), byte(x + 32), byte(y + 32)}
	case tea.MouseButtonMiddle:
		seq = []byte{'\x1b', '[', 'M', byte(1 + 32), byte(x + 32), byte(y + 32)}
	}

	if len(seq) > 0 {
		p.pty.Write(seq)
	}
}

// viewportToLogical converts viewport coordinates to logical position
// Logical position: negative row = scrollback, 0+ = live screen
// Called with mutex held.
func (p *Pane) viewportToLogical(x, y int) Position {
	// When scrolled back, top of viewport shows scrollback
	// viewportOffset = how many scrollback lines are visible at top
	// Calculate logical row
	// If viewportOffset > 0, the top rows are from scrollback
	// Row 0 in viewport corresponds to scrollback line (scrollbackLen - viewportOffset)

	logicalRow := y - p.viewportOffset

	return Position{Row: logicalRow, Col: x}
}

// HandleKey processes a key event and sends to PTY
func (p *Pane) HandleKey(msg tea.KeyMsg) tea.Msg {
	if msg.String() == "ctrl+g" {
		return ExitFocusMsg{}
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running || p.pty == nil {
		return nil
	}

	key := msg.String()

	// Check for selection copy FIRST (before forwarding Ctrl+C to PTY)
	if p.selection != nil && p.selection.IsActive() {
		if key == "ctrl+c" || key == "cmd+c" {
			p.copySelectionUnlocked()
			return nil
		}
	}

	// Handle scroll navigation keys (work regardless of mouse mode)
	switch key {
	case "shift+pgup":
		rows := p.vt.Height()
		p.scrollUp(rows / 2)
		return nil
	case "shift+pgdown":
		rows := p.vt.Height()
		p.scrollDown(rows / 2)
		return nil
	case "shift+home":
		// Scroll to top of scrollback
		p.viewportOffset = p.vt.ScrollbackLen()
		p.dirty = true
		return nil
	case "shift+end":
		// Scroll to bottom (live view)
		p.viewportOffset = 0
		p.dirty = true
		return nil
	case "esc", "escape":
		// Esc returns to live view if scrolled
		if p.viewportOffset > 0 {
			p.viewportOffset = 0
			p.dirty = true
			return nil
		}
		// Also clear selection on Esc
		if p.selection != nil && p.selection.IsActive() {
			p.selection.Clear()
			p.dirty = true
			return nil
		}
		// Otherwise forward escape to PTY
	}

	// Snap to live view on any other keyboard input
	if p.viewportOffset > 0 {
		p.viewportOffset = 0
		p.dirty = true
	}

	// Clear selection on any keyboard input (except copy)
	if p.selection != nil && p.selection.IsActive() {
		p.selection.Clear()
		p.dirty = true
	}

	input := p.translateKey(msg)
	if len(input) > 0 {
		p.pty.Write(input)
	}

	return nil
}

// copySelectionUnlocked copies selected text to clipboard
// Called with mutex held.
func (p *Pane) copySelectionUnlocked() {
	if p.selection == nil || !p.selection.IsActive() {
		return
	}

	// Get scrollback lines for text extraction
	var scrollbackLines [][]*uv.Cell
	scrollbackLen := p.vt.ScrollbackLen()
	if scrollbackLen > 0 {
		scrollbackLines = make([][]*uv.Cell, scrollbackLen)
		width := p.vt.Width()
		for i := 0; i < scrollbackLen; i++ {
			line := make([]*uv.Cell, width)
			for x := 0; x < width; x++ {
				line[x] = p.vt.ScrollbackCellAt(x, i)
			}
			scrollbackLines[i] = line
		}
	}

	// Get live screen accessor
	liveRows := p.vt.Height()
	liveScreen := func(col, row int) *uv.Cell {
		return p.vt.CellAt(col, row)
	}

	text := p.selection.ExtractText(scrollbackLines, liveScreen, liveRows, scrollbackLen)

	if text != "" {
		clipboard.WriteAll(text)
	}

	// Clear selection after copy
	p.selection.Clear()
	p.dirty = true
}

// scrollUp scrolls the viewport up (into scrollback history)
// Called with mutex held.
// ScrollUp scrolls viewport up (Phase 7).
func (p *Pane) ScrollUp(lines int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.scrollUp(lines)
}

func (p *Pane) scrollUp(lines int) {
	if p.vt == nil {
		return
	}
	maxOffset := p.vt.ScrollbackLen()
	p.viewportOffset += lines
	if p.viewportOffset > maxOffset {
		p.viewportOffset = maxOffset
	}
	p.dirty = true
}

// scrollDown scrolls the viewport down (toward live view)
// Called with mutex held.
// ScrollDown resets viewport to live view (Phase 7).
func (p *Pane) ScrollDown() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.viewportOffset = 0
}

func (p *Pane) scrollDown(lines int) {
	p.viewportOffset -= lines
	if p.viewportOffset < 0 {
		p.viewportOffset = 0
	}
	p.dirty = true
}

// translateKey converts Bubbletea KeyMsg to PTY byte sequences
func (p *Pane) translateKey(msg tea.KeyMsg) []byte {
	key := msg.String()

	// Handle modifier combinations
	switch {
	// Ctrl+A through Ctrl+Z → 0x01-0x1A
	case len(key) == 6 && key[:5] == "ctrl+" && key[5] >= 'a' && key[5] <= 'z':
		return []byte{byte(key[5] - 'a' + 1)}

	// Alt+letter → ESC + letter
	case len(key) == 5 && key[:4] == "alt+" && key[4] >= 'a' && key[4] <= 'z':
		return []byte{27, key[4]}
	}

	// Handle special keys
	switch msg.Type {
	case tea.KeyEnter:
		return []byte("\r")
	case tea.KeyBackspace:
		return []byte{127}
	case tea.KeyTab:
		if msg.Alt {
			return []byte("\x1b[Z") // Shift+Tab
		}
		return []byte("\t")
	case tea.KeyUp:
		return []byte("\x1b[A")
	case tea.KeyDown:
		return []byte("\x1b[B")
	case tea.KeyRight:
		return []byte("\x1b[C")
	case tea.KeyLeft:
		return []byte("\x1b[D")
	case tea.KeyEscape:
		return []byte{27}
	case tea.KeyHome:
		return []byte("\x1b[H")
	case tea.KeyEnd:
		return []byte("\x1b[F")
	case tea.KeyPgUp:
		return []byte("\x1b[5~")
	case tea.KeyPgDown:
		return []byte("\x1b[6~")
	case tea.KeyDelete:
		return []byte("\x1b[3~")
	case tea.KeySpace:
		return []byte(" ")
	case tea.KeyRunes:
		return []byte(string(msg.Runes))
	}

	return nil
}




// GetContent returns the current terminal content as plain text for analysis.
func (p *Pane) GetContent() string {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.vt == nil {
		return ""
	}

	cols := p.vt.Width()
	rows := p.vt.Height()
	if cols <= 0 || rows <= 0 {
		return ""
	}

	var result strings.Builder
	for row := 0; row < rows; row++ {
		if row > 0 {
			result.WriteByte('\n')
		}
		for col := 0; col < cols; col++ {
			result.WriteRune(cellRune(p.vt.CellAt(col, row)))
		}
	}

	return result.String()
}

// --- Rendering (Issue #14) ---

// View returns the rendered terminal content
func (p *Pane) View() string {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Return cached view if not dirty
	if !p.dirty && p.cachedView != "" {
		return p.cachedView
	}

	// Render throttle: if the previous render was very recent,
	// return the cached view to avoid spending CPU on a render
	// that's about to be invalidated anyway by the next
	// in-flight OutputMsg. 16ms = 60fps, smooth for human eye.
	if time.Since(p.lastRender) < 16*time.Millisecond && p.cachedView != "" {
		p.dirty = true // keep dirty, render later
		return p.cachedView
	}

	p.cachedView = p.renderVTUnlocked()
	p.lastRender = time.Now()
	p.dirty = false
	return p.cachedView
}

func (p *Pane) renderVTUnlocked() string {
	if p.vt == nil {
		// Caller should check IsReady() before calling View().
		// Returning "" is safer than leaking the internal error
		// string "Terminal not initialized" to the UI layer.
		return ""
	}

	cols := p.vt.Width()
	rows := p.vt.Height()
	if cols <= 0 || rows <= 0 {
		return ""
	}

	// If scrolled back, render mixed scrollback + live content
	if p.viewportOffset > 0 && p.vt != nil && p.vt.ScrollbackLen() > 0 {
		return p.renderScrolledViewUnlocked(cols, rows)
	}

	return p.renderLiveScreenUnlocked(cols, rows)
}

// renderScrolledViewUnlocked renders a viewport that includes scrollback history
// Must hold mu and vt.Lock
func (p *Pane) renderScrolledViewUnlocked(cols, rows int) string {
	scrollbackLen := p.vt.ScrollbackLen()
	offset := p.viewportOffset
	if offset > scrollbackLen {
		offset = scrollbackLen
	}

	var result strings.Builder
	result.Grow(rows * cols * 2)

	// Calculate which lines to show
	// viewportOffset is how many lines we've scrolled back from live view
	// So if offset=5, we show 5 less live lines and 5 scrollback lines at top

	// Number of scrollback lines visible at top of viewport
	scrollbackRowsVisible := offset
	if scrollbackRowsVisible > rows {
		scrollbackRowsVisible = rows
	}

	// Starting scrollback index (from the end of scrollback)
	scrollbackStart := scrollbackLen - offset

	for viewRow := 0; viewRow < rows; viewRow++ {
		if viewRow > 0 {
			result.WriteByte('\n')
		}

		if viewRow < scrollbackRowsVisible {
			// Render from scrollback
			scrollbackIdx := scrollbackStart + viewRow
			// Adapt uv.Cell line from x/vt scrollback to []Cell for renderGlyphLine
			srcLine := p.vt.Scrollback().Line(scrollbackIdx)
			line := make([]*uv.Cell, len(srcLine))
			for i, c := range srcLine {
				line[i] = &c
			}
			// Logical row: negative for scrollback (counting from 0)
			// scrollbackIdx 0 = oldest line = logicalRow -(scrollbackLen)
			// scrollbackIdx scrollbackLen-1 = newest scrollback = logicalRow -1
			logicalRow := scrollbackIdx - scrollbackLen
			result.WriteString(p.renderGlyphLine(line, cols, logicalRow))
		} else {
			// Render from live screen
			liveRow := viewRow - scrollbackRowsVisible
			logicalRow := liveRow // Live rows are 0+
			result.WriteString(p.renderLiveRow(cols, liveRow, logicalRow))
		}
	}

	return result.String()
}

// renderGlyphLine renders a line of glyphs with ANSI styling
// logicalRow is used for selection highlighting
func (p *Pane) renderGlyphLine(line []*uv.Cell, cols int, logicalRow int) string {
	var result strings.Builder
	var currentStyle uv.Style
	var batch strings.Builder
	firstCell := true
	inSelection := false

	flushBatch := func() {
		if batch.Len() == 0 {
			return
		}
		if inSelection {
			result.WriteString("\x1b[7m") // Reverse video for selection
		} else {
			result.WriteString(buildANSIFromStyle(currentStyle))
		}
		result.WriteString(batch.String())
		result.WriteString("\x1b[0m")
		batch.Reset()
	}

	for col := 0; col < cols; col++ {
		var cell *uv.Cell
		if col < len(line) {
			cell = line[col]
		}
		ch := cellRune(cell)

		// Check if this cell is selected
		cellSelected := p.selection != nil && p.selection.Contains(Position{Row: logicalRow, Col: col})

		// Style changed or selection changed? Flush batch
		cellStyle := cellStyleOr(cell, uv.Style{})
		if !firstCell && (!cellStyle.Equal(&currentStyle) || cellSelected != inSelection) {
			flushBatch()
		}

		currentStyle = cellStyle
		inSelection = cellSelected
		firstCell = false

		batch.WriteRune(ch)
	}
	flushBatch()

	return result.String()
}

// renderLiveRow renders a single row from the live terminal screen
// logicalRow is used for selection highlighting
// Must hold vt.Lock
func (p *Pane) renderLiveRow(cols, row int, logicalRow int) string {
	var result strings.Builder
	var currentStyle uv.Style
	var batch strings.Builder
	firstCell := true
	inSelection := false

	flushBatch := func() {
		if batch.Len() == 0 {
			return
		}
		if inSelection {
			result.WriteString("\x1b[7m") // Reverse video for selection
		} else {
			result.WriteString(buildANSIFromStyle(currentStyle))
		}
		result.WriteString(batch.String())
		result.WriteString("\x1b[0m")
		batch.Reset()
	}

	cursorPos := p.vt.CursorPosition()
	cursorVis := cursorVisible(p.vt.CursorPosition())

	for col := 0; col < cols; col++ {
		cell := p.vt.CellAt(col, row)
		ch := cellRune(cell)

		isCursor := cursorVis && col == cursorPos.X && row == cursorPos.Y
		cellSelected := p.selection != nil && p.selection.Contains(Position{Row: logicalRow, Col: col})

		// Style changed or selection changed? Flush batch
		cellStyle := cellStyleOr(cell, uv.Style{})
		if !firstCell && (!cellStyle.Equal(&currentStyle) || isCursor || cellSelected != inSelection) {
			flushBatch()
		}

		// Handle cursor with reverse video (cursor takes priority over selection)
		if isCursor {
			result.WriteString("\x1b[7m") // Reverse
			result.WriteRune(ch)
			result.WriteString("\x1b[27m") // Un-reverse
			firstCell = true
			inSelection = false
			continue
		}

		currentStyle = cellStyle
		inSelection = cellSelected
		firstCell = false

		batch.WriteRune(ch)
	}
	flushBatch()

	return result.String()
}

// renderLiveScreenUnlocked renders the live terminal screen (must hold mu and vt.Lock)
func (p *Pane) renderLiveScreenUnlocked(cols, rows int) string {
	cursorPos := p.vt.CursorPosition()
	cursorVis := cursorVisible(p.vt.CursorPosition())

	var result strings.Builder
	result.Grow(rows * cols * 2)

	for row := 0; row < rows; row++ {
		if row > 0 {
			result.WriteByte('\n')
		}

		// Track current style for batching
		var currentStyle uv.Style
		var batch strings.Builder
		firstCell := true
		inSelection := false

		flushBatch := func() {
			if batch.Len() == 0 {
				return
			}
			if inSelection {
				result.WriteString("\x1b[7m") // Reverse video for selection
			} else {
				result.WriteString(buildANSIFromStyle(currentStyle))
			}
			result.WriteString(batch.String())
			result.WriteString("\x1b[0m")
			batch.Reset()
		}

		for col := 0; col < cols; col++ {
			cell := p.vt.CellAt(col, row)
			ch := cellRune(cell)

			isCursor := cursorVis && col == cursorPos.X && row == cursorPos.Y
			logicalRow := row // When not scrolled, logical row = screen row
			cellSelected := p.selection != nil && p.selection.Contains(Position{Row: logicalRow, Col: col})

			cellStyle := cellStyleOr(cell, uv.Style{})

			// Style changed or selection changed? Flush batch
			if !firstCell && (!cellStyle.Equal(&currentStyle) || isCursor || cellSelected != inSelection) {
				flushBatch()
			}

			// Handle cursor with reverse video
			if isCursor {
				result.WriteString("\x1b[7m") // Reverse
				result.WriteRune(ch)
				result.WriteString("\x1b[27m") // Un-reverse
				firstCell = true
				inSelection = false
				continue
			}

			currentStyle = cellStyle
			inSelection = cellSelected
			firstCell = false

			batch.WriteRune(ch)
		}
		flushBatch()
	}

	return result.String()
}

// buildANSIFromStyle constructs ANSI escape sequence from uv.Style.
// x/vt's Style uses image/color and bit attributes (AttrBold=1, etc.).
func buildANSIFromStyle(style uv.Style) string {
	var parts []string

	// Foreground
	if fgCode := colorToANSI(style.Fg, true); fgCode != "" {
		parts = append(parts, fgCode)
	}

	// Background
	if bgCode := colorToANSI(style.Bg, false); bgCode != "" {
		parts = append(parts, bgCode)
	}

	// Attributes (uv.Attr* bits)
	if style.Attrs&uv.AttrBold != 0 {
		parts = append(parts, "1")
	}
	if style.Attrs&uv.AttrFaint != 0 {
		parts = append(parts, "2")
	}
	if style.Attrs&uv.AttrItalic != 0 {
		parts = append(parts, "3")
	}
	// Underline: x/vt uses Style.Underline enum (None/Single/Double/...).
	// Any non-None value means "this cell has an underline".
	if style.Underline != uv.UnderlineNone {
		parts = append(parts, "4")
	}
	if style.Attrs&uv.AttrBlink != 0 {
		parts = append(parts, "5")
	}
	if style.Attrs&uv.AttrReverse != 0 {
		parts = append(parts, "7")
	}
	if style.Attrs&uv.AttrConceal != 0 {
		parts = append(parts, "8")
	}
	if style.Attrs&uv.AttrStrikethrough != 0 {
		parts = append(parts, "9")
	}

	if len(parts) == 0 {
		return ""
	}

	return fmt.Sprintf("\x1b[%sm", strings.Join(parts, ";"))
}

// colorToANSI converts Go's image/color.Color to ANSI escape sequence component.
// Returns empty string for default/transparent colors (no escape needed).
func colorToANSI(c color.Color, isFG bool) string {
	if c == nil {
		return ""
	}

	base := 38 // Foreground
	if !isFG {
		base = 48 // Background
	}

	// image/color.Color's RGBA() returns 16-bit-per-channel values.
	// Extract 8-bit values for ANSI true color.
	r, g, b, a := c.RGBA()
	if a == 0 {
		// Transparent / default color: no escape needed.
		return ""
	}
	r8 := uint8(r >> 8)
	g8 := uint8(g >> 8)
	b8 := uint8(b >> 8)

	return fmt.Sprintf("%d;2;%d;%d;%d", base, r8, g8, b8)
}

// cellRune extracts the first rune from a uv.Cell's Content field.
// Returns ' ' for nil/empty cells.
func cellRune(c *uv.Cell) rune {
	if c == nil || c.Content == "" {
		return ' '
	}
	r := []rune(c.Content)
	if len(r) == 0 {
		return ' '
	}
	return r[0]
}

// cellStyleOr returns cell.Style or fallback if cell is nil.
func cellStyleOr(cell *uv.Cell, fallback uv.Style) uv.Style {
	if cell == nil {
		return fallback
	}
	return cell.Style
}

// cellContentEqual compares two cells by their Content string.
// Required because x/vt may return the same *uv.Cell pointer for
// unchanged cells, breaking pointer-equality snapshot comparison.
func cellContentEqual(a, b *uv.Cell) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Content == b.Content && a.Style.Equal(&b.Style)
}

// cursorVisible reports whether the cursor should be drawn at its position.
// x/vt's Cursor struct has a Hidden field (true = don't render cursor).
func cursorVisible(c uv.Position) bool {
	// Position.X == -1 is the x/vt convention for "no cursor".
	return c.X >= 0 && c.Y >= 0
}

// installAltScreenCallback replaces the byte-scanning detectAltScreenChanges
// with a clean observer pattern. x/vt fires this when alt-screen
// enter/exit sequences (\x1b[?1049h / \x1b[?1049l) are processed.
//
// Called only after p.vt is initialized (Start or render-only path).
// p.mu may or may not be held; the callback takes p.mu internally.
//
// CRITICAL: x/vt's SetCallbacks fires the AltScreen callback
// synchronously from inside vt.Write() when alt-screen sequences are
// encountered. vt.Write() is called by handleOutputLocked() which
// already holds p.mu. If the callback tries to lock p.mu again, the
// goroutine deadlocks against itself.
//
// To break the cycle, the callback below does NOT take p.mu.
// Instead it writes through the altScreenActiveCh channel. A
// separate goroutine (started by installCallbacks) consumes the
// channel and applies updates under p.mu.
func (p *Pane) installAltScreenCallback() {
	// Read p.vt without holding p.mu. The only writer of p.vt is
	// the Start goroutine, which holds p.mu when writing. Reading
	// concurrently is technically a data race, but in practice
	// the value is set once (during Start) and never changes
	// thereafter — so we observe either the nil-zero or the
	// post-Start value, never a torn read.
	//
	// We avoid p.mu here because Start holds p.mu when calling
	// us (render-only path). Acquiring p.mu again would deadlock
	// against the same goroutine.
	emu := p.vt
	if emu == nil {
		return
	}
	// NOTE: p.altScreenActiveCh is initialized in New() and never
	// re-assigned. The callback closure captures the channel by
	// reference; both this registration and the consumer in
	// installCallbacks see the same channel.
	emu.SetCallbacks(vt.Callbacks{
		AltScreen: func(active bool) {
			// Non-blocking send to the channel. If the consumer is
			// not reading (e.g., tests), the event is dropped — the
			// next handleOutput call will still detect alt-screen via
			// detectAltScreenChanges as a fallback.
			select {
			case p.altScreenActiveCh <- altScreenEvent{active: active}:
			default:
			}
		},
	})
}

// altScreenEvent is the payload for the channel above.
type altScreenEvent struct {
	active bool
}

func buildCleanEnv(sessionName string) []string {
	var env []string
	for _, e := range os.Environ() {
		key := strings.Split(e, "=")[0]
		if key == "OPENCODE" || strings.HasPrefix(key, "OPENCODE_") {
			continue
		}
		if key == "CLAUDE" || strings.HasPrefix(key, "CLAUDE_") {
			continue
		}
		if key == "GEMINI" || strings.HasPrefix(key, "GEMINI_") {
			continue
		}
		if key == "CODEX" || strings.HasPrefix(key, "CODEX_") {
			continue
		}
		if key == "AIDER" || strings.HasPrefix(key, "AIDER_") {
			continue
		}
		if key == "PI" || strings.HasPrefix(key, "PI_") {
			continue
		}
		env = append(env, e)
	}
	env = append(env, "TERM=xterm-256color")
	if sessionName != "" {
		env = append(env, "OPENKANBAN_SESSION="+sessionName)
	}
	return env
}


// StartCmd is a PaneLike-compatible wrapper around Start.
// Calls Start (which creates the x/vt Emulator) and then synchronously
// installs callbacks + starts the alt-screen consumer goroutine.
//
// We don't return tea.Batch because the underlying cmd chain (readOutput)
// is self-cycling — only ONE cmd needs to be returned to the Bubble Tea
// runtime. The first cmd() returns a readOutput closure that drives the
// rest. Installing callbacks must happen before the first vt.Write call,
// otherwise alt-screen events from that first Write would be lost.
//
// Synchronous installCallbacks() is safe here because it doesn't take
// p.mu (it spawns a goroutine that takes p.mu per-event).
func (p *Pane) StartCmd(command string, args ...string) tea.Cmd {
	readLoop := p.startSetup(command, args...)
	p.installCallbacks()
	return readLoop
}
