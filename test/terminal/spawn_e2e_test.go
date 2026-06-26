//go:build integration

package terminal_test

import (
	"strings"
	"testing"
	"time"

	"github.com/pi/awp/internal/terminal"

	testutil "github.com/pi/awp/internal/testutil"
)

// TestSpawn_EndToEnd exercises the full spawn flow:
// prepareSpawn → spawnReadyMsg → terminal.Pane.Start → pi subprocess.
//
// Uses a mock pi binary that emits agent_start JSONL. Verifies:
// 1. Pane.Start actually launches the subprocess
// 2. PTY output contains the expected JSONL
// 3. Pane.GetContent() returns the captured output
func TestSpawn_EndToEnd(t *testing.T) {
	testutil.RequireLinux(t)

	mockPath := testutil.RepoPath(t, "internal", "pi", "testdata", "mock-pi.sh")

	pane := terminal.New("test-ticket", 80, 24, 10000)
	pane.SetWorkdir(t.TempDir())

	// Drive the readOutput self-cycle
	cmd := pane.Start(mockPath, "--mode", "rpc")
	if cmd == nil {
		t.Fatal("pane.Start returned nil — should return readOutput Cmd")
	}
	for i := 0; i < 20; i++ {
		time.Sleep(50 * time.Millisecond)
		msg := cmd()
		if msg == nil {
			continue
		}
		if _, ok := msg.(terminal.ExitMsg); ok {
			break
		}
		if outMsg, ok := msg.(terminal.OutputMsg); ok {
			next := pane.Update(outMsg)
			if next == nil {
				break
			}
			cmd = next
		}
	}

	// Read captured content
	content := pane.GetContent()
	if content == "" {
		t.Fatal("pane captured no output — mock pi didn't write to PTY")
	}

	if !strings.Contains(content, "agent_start") {
		t.Errorf("content missing agent_start JSONL; got: %q", content[:min(200, len(content))])
	}

	if !pane.Running() {
		t.Errorf("pane.Running()=false; want true")
	}

	if err := pane.Stop(); err != nil {
		t.Errorf("pane.Stop: %v", err)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
