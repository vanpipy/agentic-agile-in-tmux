//go:build integration

package terminal_test

import (
	"os/exec"
	"testing"
	"time"

	"github.com/pi/awp/internal/terminal"
	testutil "github.com/pi/awp/internal/testutil"
)

// TestTripleSpawnPi_ManyTimes stresses the spawn-stop-spawn cycle
// to catch any goroutine/fd leaks that would build up over time.
func TestTripleSpawnPi_ManyTimes(t *testing.T) {
	testutil.RequireLinux(t)
	piPath, err := exec.LookPath("pi")
	if err != nil {
		t.Skipf("pi not in PATH: %v", err)
	}

	for i := 0; i < 3; i++ {
		t.Run("iteration", func(t *testing.T) {
			pane := terminal.New("iter", 80, 24, 0)
			pane.SetWorkdir(t.TempDir())
			cmd := pane.StartCmd(piPath, "--help")
			if cmd == nil {
				t.Fatal("Start nil")
			}
			deadline := time.Now().Add(3 * time.Second)
		loop:
			for time.Now().Before(deadline) {
				time.Sleep(50 * time.Millisecond)
				msg := cmd()
				if msg == nil {
					continue
				}
				switch m := msg.(type) {
				case terminal.ExitMsg:
					break loop
				case terminal.OutputMsg:
					_ = pane.Update(m)
				}
			}
			_ = pane.Stop()
		})
	}
}
