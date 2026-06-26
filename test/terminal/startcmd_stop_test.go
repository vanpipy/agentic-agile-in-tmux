package terminal_test

import (
	"testing"
	"time"

	"github.com/pi/awp/internal/terminal"
)

// TestStartCmd_DoesNotDeadlockOnStop reproduces user report:
// "进入就直接阻塞" after a previous awp process was killed.
//
// Scenario: Start a Pane via StartCmd (which triggers installCallbacks
// → altScreenConsumer goroutine), then immediately Stop. Stop must
// close the consumer's channel and let it exit.
//
// If the consumer's select loop holds a lock that Stop also takes,
// Stop deadlocks waiting for the lock; the consumer deadlocks
// waiting for the close. Test fails by timeout.
func TestStartCmd_DoesNotDeadlockOnStop(t *testing.T) {
	pane := terminal.New("startcmd-stop", 80, 24, 0)
	pane.SetWorkdir(t.TempDir())

	// StartCmd path (production): Start + installCallbacks.
	cmd := pane.StartCmd("/bin/echo", "hello")
	if cmd == nil {
		t.Fatal("StartCmd returned nil")
	}
	// Drive the start (echo will exit fast).
	_ = cmd()

	done := make(chan error, 1)
	go func() {
		done <- pane.Stop()
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Stop: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Stop deadlocked — likely consumer/Stop mutex cycle")
	}
}
