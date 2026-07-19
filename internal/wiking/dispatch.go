// Package wiking — dispatch.go implements §18.3 dispatch: spawning
// the pi subprocess under a role binding. The dispatch layer is
// generic — it knows how to run a subprocess under a context, but
// not what role-specific args pi expects. The cycle driver (cycle.go)
// computes the role binding + args and passes them here.

package wiking

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// DefaultShutdownGrace is the maximum time a subprocess gets to
// clean up after SIGTERM before SIGKILL (Cmd.WaitDelay). Matches
// AGENTS.md §5.4 cleanup window.
const DefaultShutdownGrace = 5 * time.Second

// Dispatch builds and runs subprocesses for the postman. It's a
// thin layer over os/exec — its job is to enforce:
//   1. The subprocess runs under the parent's context
//      (cancellable; ctx-cancel sends SIGTERM)
//   2. Stdout/stderr are silenced by default — the marker file is
//      the witness, the events.jsonl is the news. stdout leaking
//      into a TTY would pollute the cyclepane.
//   3. SIGTERM-then-WaitDelay-then-SIGKILL on cancellation, so pi
//      has a chance to flush its session file before being killed.
type Dispatch struct {
	binary        string
	stdout        io.Writer // default: io.Discard
	stderr        io.Writer // default: io.Discard
	shutdownGrace time.Duration
}

// NewDispatch returns a Dispatch that defaults to the `pi` binary
// (resolved through $PATH at Start() time), with stdout/stderr
// silenced and a 5s SIGTERM→SIGKILL grace window. Tests should
// call WithBinary (and WithStdout/WithStderr if they need to read
// subprocess output) to override.
func NewDispatch() *Dispatch {
	return &Dispatch{
		binary:        "pi",
		stdout:        io.Discard,
		stderr:        io.Discard,
		shutdownGrace: DefaultShutdownGrace,
	}
}

// WithBinary overrides the binary path. Returns self for chaining.
func (d *Dispatch) WithBinary(path string) *Dispatch {
	d.binary = path
	return d
}

// WithStdout routes the subprocess's stdout to w instead of
// io.Discard. Useful in tests that want to assert on output.
func (d *Dispatch) WithStdout(w io.Writer) *Dispatch {
	if w != nil {
		d.stdout = w
	}
	return d
}

// WithStderr is the stderr counterpart of WithStdout.
func (d *Dispatch) WithStderr(w io.Writer) *Dispatch {
	if w != nil {
		d.stderr = w
	}
	return d
}

// WithShutdownGrace overrides the SIGTERM-to-SIGKILL grace window.
func (d *Dispatch) WithShutdownGrace(g time.Duration) *Dispatch {
	if g > 0 {
		d.shutdownGrace = g
	}
	return d
}

// SpawnArgs is the input to Spawn. Callers construct it from a
// RoleBinding (cycle.go, P2.5) or directly from config (cmd/awp/cycle.go).
type SpawnArgs struct {
	// Binary, if non-empty, overrides the Dispatch's binary for
	// this one call. Empty means "use Dispatch's binary".
	Binary string

	// Args is the full argv passed to the subprocess (excluding the
	// binary path). The dispatcher doesn't add defaults — the caller
	// decides what flags pi should see.
	Args []string

	// Dir, if non-empty, sets the subprocess's working directory.
	// Empty means inherit parent process CWD (the standard os/exec
	// behavior). The cycle driver always passes Dir because each
	// role has a wiki-repo CWD.
	Dir string

	// StdinPayload, if non-empty, is written to the subprocess's
	// stdin then closed. Useful for one-shot prompts or scripts.
	StdinPayload string
}

// Spawn returns a configured *exec.Cmd that the caller must Start
// and Wait. Spawn performs no validation beyond non-empty binary;
// the caller's Start() will surface binary-not-found errors.
//
// The returned cmd is configured for graceful shutdown:
//   - cmd.Cancel: SIGTERM on ctx cancel
//   - cmd.WaitDelay: SIGKILL escalation after shutdownGrace
//   - cmd.Stdout/Stderr: Dispatch.stdout/stderr (default io.Discard)
func (d *Dispatch) Spawn(ctx context.Context, args SpawnArgs) (*exec.Cmd, error) {
	bin := d.binary
	if args.Binary != "" {
		bin = args.Binary
	}
	if bin == "" {
		return nil, errors.New("wiking: dispatch binary not set")
	}

	cmd := exec.CommandContext(ctx, bin, args.Args...)
	if args.Dir != "" {
		cmd.Dir = args.Dir
	}

	// Always set Cancel + WaitDelay so ctx cancellation sends
	// SIGTERM first (allowing pi to flush its session file), and
	// escalates to SIGKILL after the grace window.
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return cmd.Process.Signal(syscall.SIGTERM)
	}
	cmd.WaitDelay = d.shutdownGrace

	// Quiet the subprocess by default — the marker file is the
	// single source of truth.
	cmd.Stdout = d.stdout
	cmd.Stderr = d.stderr

	if args.StdinPayload != "" {
		cmd.Stdin = strings.NewReader(args.StdinPayload)
	}

	return cmd, nil
}
