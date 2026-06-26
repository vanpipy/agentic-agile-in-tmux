package terminal

import (
	"testing"
	"time"
)

// TestInstallCallbacks_DrainsInputPipe reproduces the deadlock
// reported by user: "spawn 之后, 在 loading 之后, 整个界面就无法响应了"
//
// Root cause (from runtime.Stack dump on user's actual machine):
//
//   x/vt's CSI handler writes Device Attributes responses to
//   an internal io.Pipe (pr/pw). If nothing reads from the pipe,
//   the writer blocks forever, freezing vt.Write() and the entire
//   awp UI.
//
// This test sends an ESC [ c (Device Attributes) query into a
// Pane, then verifies HandleOutput returns within 2s. Without
// the inputDrain goroutine in installCallbacks, it deadlocks.
func TestInstallCallbacks_DrainsInputPipe(t *testing.T) {
	p := New("drain-test", 80, 24, 100)
	cmd := p.Start("", nil...)
	if cmd != nil {
		cmd()
	}
	p.installCallbacks()

	done := make(chan struct{})
	go func() {
		p.HandleOutput([]byte("\x1b[c"))
		close(done)
	}()
	select {
	case <-done:
		// good
	case <-time.After(2 * time.Second):
		t.Fatal("HandleOutput deadlocked — inputDrain is not consuming the x/vt internal pipe")
	}
}

// TestInstallCallbacks_DrainsMultipleDASQueries verifies the drain
// works for repeated queries (pi sends many of these on startup).
func TestInstallCallbacks_DrainsMultipleDASQueries(t *testing.T) {
	p := New("drain-multi", 80, 24, 100)
	cmd := p.Start("", nil...)
	if cmd != nil {
		cmd()
	}
	p.installCallbacks()

	for i := 0; i < 10; i++ {
		t.Logf("iteration %d", i)
		done := make(chan struct{})
		go func() {
			p.HandleOutput([]byte("\x1b[c"))
			close(done)
		}()
		select {
		case <-done:
			t.Logf("iter %d done", i)
		case <-time.After(2 * time.Second):
			t.Fatalf("iteration %d: HandleOutput deadlocked", i)
		}
	}
}

