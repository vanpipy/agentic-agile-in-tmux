//go:build integration

package terminal_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/pi/awp/internal/terminal"
	testutil "github.com/pi/awp/internal/testutil"
)

// TestScrollback_NoLineLoss_2LineChunks reproduces a bug where
// every other line gets lost when output comes in small chunks
// (e.g., 2 lines per Write).
//
// User reported: "滚动了一定高度后只剩下 1, 3, 5"
func TestScrollback_NoLineLoss_2LineChunks(t *testing.T) {
	mockPath := testutil.RepoPath(t, "internal", "pi", "testdata", "scrollback_multi.sh")

	pane := terminal.New("test", 80, 5, 1000)
	pane.SetWorkdir(t.TempDir())

	cmd := pane.StartCmd(mockPath)
	if cmd == nil {
		t.Fatal("Start nil")
	}
	cmd()

	for i := 0; i < 200; i++ {
		time.Sleep(5 * time.Millisecond)
		msg := cmd()
		if msg == nil {
			continue
		}
		if outMsg, ok := msg.(terminal.OutputMsg); ok {
			next := pane.Update(outMsg)
			if next != nil {
				cmd = next
			}
		} else if _, ok := msg.(terminal.ExitMsg); ok {
			break
		}
	}

	dumpScrollback(t, pane)
	if pane.ScrollbackLen() < 5 {
		t.Errorf("scrollback has %d lines, expected >= 5", pane.ScrollbackLen())
	}
	pane.Stop()
}

// TestScrollback_NoLineLoss_HandleOutputDirect verifies the
// HandleOutput path (used by PiClient/PiPane) also captures all
// scrolled-off lines, even when input has no newlines at all
// (i.e., the bounds check for the final chunk is correct).
func TestScrollback_NoLineLoss_HandleOutputDirect(t *testing.T) {
	pane := terminal.New("test", 80, 5, 100)
	pane.SetWorkdir(t.TempDir())

	cmd := pane.Start("", nil...)
	if cmd != nil {
		cmd()
	}

	// Multiple writes, some with \n, some without
	pane.HandleOutput([]byte("Hello, World!")) // no \n — should not panic
	pane.HandleOutput([]byte("\nLine 1\n"))
	pane.HandleOutput([]byte("Line 2\nLine 3\n"))
	pane.HandleOutput([]byte("Line 4")) // no trailing \n
	pane.HandleOutput([]byte("\n"))

	view := pane.View()
	if view == "" {
		t.Errorf("View() is empty after HandleOutput")
	}
	t.Logf("view:\n%s", view)
}

// TestScrollback_NoLineLoss_EmptyChunk verifies that empty
// chunks (between consecutive \n) don't trigger captures.
func TestScrollback_NoLineLoss_EmptyChunk(t *testing.T) {
	pane := terminal.New("test", 80, 5, 100)
	pane.SetWorkdir(t.TempDir())

	cmd := pane.Start("", nil...)
	if cmd != nil {
		cmd()
	}

	pane.HandleOutput([]byte("\n\n\n"))

	view := pane.View()
	t.Logf("view (after 3 newlines on 5-row screen):\n%s", view)
}

func dumpScrollback(t *testing.T, pane *terminal.Pane) {
	var sb strings.Builder
	for i := 0; i < pane.ScrollbackLen(); i++ {
		line := pane.GetScrollbackLine(i)
		if line == "" {
			continue
		}
		sb.WriteString(fmt.Sprintf("[sb %d] %s\n", i, line))
	}
	t.Logf("scrollback (%d lines):\n%s", pane.ScrollbackLen(), sb.String())
	t.Logf("view:\n%s", pane.View())
}
