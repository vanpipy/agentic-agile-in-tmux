//go:build integration

package terminal_test

import (
	"os/exec"
	"testing"
	"time"

	"github.com/pi/awp/internal/terminal"
	testutil "github.com/pi/awp/internal/testutil"
)

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
	cmd1 := pane1.Start(piPath, "--append-system-prompt", "test")
	if cmd1 == nil {
		t.Fatal("first Start nil")
	}

	// Drive the readOutput cycle for 2 seconds (let pi initialize)
	time.Sleep(2 * time.Second)
	gotOutput1 := false
	for i := 0; i < 20; i++ {
		msg := cmd1()
		if msg == nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		if outMsg, ok := msg.(terminal.OutputMsg); ok {
			gotOutput1 = true
			_ = pane1.Update(outMsg)
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
		msg := cmd1()
		if msg == nil {
			break
		}
		if outMsg, ok := msg.(terminal.OutputMsg); ok {
			_ = pane1.Update(outMsg)
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
		cmd2 := pane2.Start(piPath, "--append-system-prompt", "second test")
		if cmd2 == nil {
			t.Error("second Start nil")
			done <- struct{}{}
			return
		}

		sawOutput := false
		deadline := time.Now().Add(3 * time.Second)
	loop:
		for time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
			msg := cmd2()
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
