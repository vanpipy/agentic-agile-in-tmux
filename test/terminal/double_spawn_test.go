//go:build integration

package terminal_test

import (
	"os/exec"
	"testing"
	"time"

	"github.com/pi/awp/internal/terminal"
	testutil "github.com/pi/awp/internal/testutil"
)

// TestDoubleSpawnPi_DoesNotDeadlock reproduces the user-reported
// "进入就直接阻塞" by doing two consecutive Pane.Start(pi) calls,
// each with a real pi subprocess. The first one exits when pi
// prints the banner; the second must not block.
func TestDoubleSpawnPi_DoesNotDeadlock(t *testing.T) {
	testutil.RequireLinux(t)
	piPath, err := exec.LookPath("pi")
	if err != nil {
		t.Skipf("pi not in PATH: %v", err)
	}

	// First spawn
	pane1 := terminal.New("first", 80, 24, 0)
	pane1.SetWorkdir(t.TempDir())
	cmd1 := pane1.StartCmd(piPath, "--help")
	if cmd1 == nil {
		t.Fatal("first StartCmd nil")
	}
	deadline1 := time.Now().Add(5 * time.Second)
loop1:
	for time.Now().Before(deadline1) {
		time.Sleep(50 * time.Millisecond)
		msg := cmd1()
		if msg == nil {
			continue
		}
		switch m := msg.(type) {
		case terminal.ExitMsg:
			break loop1
		case terminal.OutputMsg:
			_ = pane1.Update(m)
		}
	}
	_ = pane1.Stop()

	// Second spawn — must not block
	done := make(chan struct{}, 1)
	go func() {
		pane2 := terminal.New("second", 80, 24, 0)
		pane2.SetWorkdir(t.TempDir())
		cmd2 := pane2.StartCmd(piPath, "--help")
		if cmd2 == nil {
			t.Error("second StartCmd nil")
			done <- struct{}{}
			return
		}
		deadline2 := time.Now().Add(3 * time.Second)
	loop2:
		for time.Now().Before(deadline2) {
			time.Sleep(50 * time.Millisecond)
			msg := cmd2()
			if msg == nil {
				continue
			}
			switch m := msg.(type) {
			case terminal.ExitMsg:
				break loop2
			case terminal.OutputMsg:
				_ = pane2.Update(m)
			}
		}
		_ = pane2.Stop()
		done <- struct{}{}
	}()
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatal("second spawn deadlocked — likely consumer/Stop goroutine leak")
	}
}
