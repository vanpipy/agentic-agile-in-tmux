//go:build integration

package terminal_test

import (
	"runtime/debug"
	"testing"
	"time"

	"github.com/pi/awp/internal/terminal"

	testutil "github.com/pi/awp/internal/testutil"
)

// TestSetSize_ResizesVTAndPTY verifies that calling Pane.SetSize
// after spawn keeps vt10x and the pty in sync with the new size.
//
// Before the fix:
//   - Pane.SetSize only updated p.width/p.height
//   - vt and pty kept the old size
//   - When the user's terminal was resized (tea.WindowSizeMsg),
//     the next View() call panicked with "index out of range [N]"
//   - This was the user's [131] panic
func TestSetSize_ResizesVTAndPTY(t *testing.T) {
	testutil.RequireLinux(t)

	mockPath := testutil.RepoPath(t, "internal", "pi", "testdata", "mock-pi.sh")

	pane := terminal.New("test", 100, 24, 1000)
	pane.SetWorkdir(t.TempDir())

	cmd := pane.Start(mockPath, "--mode", "rpc")
	if cmd == nil {
		t.Fatal("pane.Start returned nil")
	}
	defer pane.Stop()

	// Drive a few output reads
	for i := 0; i < 5; i++ {
		time.Sleep(50 * time.Millisecond)
		msg := cmd()
		if msg == nil {
			continue
		}
		if outMsg, ok := msg.(terminal.OutputMsg); ok {
			next := pane.Update(outMsg)
			if next == nil {
				break
			}
			cmd = next
		}
	}

	// Resize to a new size (this is what tea.WindowSizeMsg triggers)
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("SetSize panicked: %v\n%s", r, debug.Stack())
			}
		}()
		pane.SetSize(131, 30)
	}()

	// View should still work after resize
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("View panicked after SetSize(131, 30): %v\n%s", r, debug.Stack())
			}
		}()
		view := pane.View()
		if view == "" {
			t.Error("View returned empty after SetSize")
		}
	}()

	// Resize again to smaller
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Second SetSize panicked: %v\n%s", r, debug.Stack())
			}
		}()
		pane.SetSize(80, 24)
	}()

	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Second View panicked: %v\n%s", r, debug.Stack())
			}
		}()
		_ = pane.View()
	}()
}
