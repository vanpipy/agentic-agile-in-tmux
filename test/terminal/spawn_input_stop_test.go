//go:build integration

package terminal_test

import (
	"os/exec"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pi/awp/internal/terminal"
	testutil "github.com/pi/awp/internal/testutil"
)

// readWithTimeout runs cmd in a goroutine and waits up to timeout for
// its result. Returns nil on timeout. Necessary because readOutput
// blocks indefinitely on the PTY when the subprocess is idle
// (interactive pi in TUI mode produces no further output). Without
// this wrapper, the test loop would block on cmd() forever.
func readWithTimeout(cmd tea.Cmd, timeout time.Duration) tea.Msg {
	if cmd == nil {
		return nil
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case msg := <-ch:
		return msg
	case <-time.After(timeout):
		return nil
	}
}

// TestSpawnWriteInputStopThenSpawn mirrors the user scenario:
//   1. spawn pi
//   2. wait for some output
//   3. write some input
//   4. wait for echo
//   5. stop the pane
//   6. spawn another pi
//   7. verify second spawn works (not deadlocked)
//
// This is closer to the user's "first spawn 闪退, second spawn
// 界面停滞" pattern. Without the fix, the consumer goroutine
// from the first spawn leaks and may block the second.
func TestSpawnWriteInputStopThenSpawn(t *testing.T) {
	testutil.RequireLinux(t)
	piPath, err := exec.LookPath("pi")
	if err != nil {
		t.Skipf("pi not in PATH: %v", err)
	}

	// === Phase 1: first spawn ===
	pane1 := terminal.New("first", 100, 30, 0)
	pane1.SetWorkdir(t.TempDir())
	cmd1 := pane1.StartCmd(piPath, "--append-system-prompt", "test")
	if cmd1 == nil {
		t.Fatal("first Start nil")
	}

	// Drive the readOutput cycle for up to 2 seconds (let pi initialize).
	// Each cmd1() call uses readWithTimeout because once pi enters its
	// TUI, it produces no more output and the underlying pty.Read
	// would block indefinitely.
	time.Sleep(2 * time.Second)
	gotOutput1 := false
	phase1Deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(phase1Deadline) {
		msg := readWithTimeout(cmd1, 200*time.Millisecond)
		if msg == nil {
			continue
		}
		if outMsg, ok := msg.(terminal.OutputMsg); ok {
			gotOutput1 = true
			cmd1 = pane1.Update(outMsg)
		} else if _, ok := msg.(terminal.ExitMsg); ok {
			t.Logf("first pi exited early")
			break
		}
	}
	if !gotOutput1 {
		t.Skip("first pi produced no output; cannot test full cycle")
	}

	// === Phase 2: write some input + drain echo ===
	if _, err := pane1.WriteInput([]byte("h")); err != nil {
		t.Logf("WriteInput err: %v (may be expected)", err)
	}
	time.Sleep(200 * time.Millisecond)
	for i := 0; i < 10; i++ {
		msg := readWithTimeout(cmd1, 100*time.Millisecond)
		if msg == nil {
			break
		}
		if outMsg, ok := msg.(terminal.OutputMsg); ok {
			cmd1 = pane1.Update(outMsg)
		} else if _, ok := msg.(terminal.ExitMsg); ok {
			break
		}
	}

	// === Phase 3: stop the first pane ===
	if err := pane1.Stop(); err != nil {
		t.Logf("first Stop err: %v", err)
	}

	// === Phase 4: second spawn (must not deadlock) ===
	done := make(chan struct{}, 1)
	go func() {
		pane2 := terminal.New("second", 100, 30, 0)
		pane2.SetWorkdir(t.TempDir())
		cmd2 := pane2.StartCmd(piPath, "--append-system-prompt", "second test")
		if cmd2 == nil {
			t.Error("second Start nil")
			done <- struct{}{}
			return
		}

		sawOutput := false
		// Give second pi 1.5s to initialize before the loop starts.
		// Without this, readWithTimeout(200ms) fires before pi's first
		// output arrives and we report 'second pi produced no output'.
		// Phase 1 has the same 2s pre-loop sleep; mirror it here.
		time.Sleep(1500 * time.Millisecond)
		deadline := time.Now().Add(3 * time.Second)
	loop:
		for time.Now().Before(deadline) {
			msg := readWithTimeout(cmd2, 200*time.Millisecond)
			if msg == nil {
				continue
			}
			switch m := msg.(type) {
			case terminal.ExitMsg:
				t.Errorf("second pi exited: %v", m.Err)
				break loop
			case terminal.OutputMsg:
				sawOutput = true
			}
		}
		if !sawOutput {
			t.Error("second pi produced no output")
		}
		_ = pane2.Stop()
		done <- struct{}{}
	}()
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatal("second spawn deadlocked after first spawn + input + stop")
	}
}
