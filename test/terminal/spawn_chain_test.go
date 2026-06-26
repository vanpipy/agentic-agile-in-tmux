//go:build integration

package terminal_test

import (
	"strings"
	"testing"
	"time"

	"github.com/pi/awp/internal/board"
	"github.com/pi/awp/internal/terminal"

	testutil "github.com/pi/awp/internal/testutil"
)

// TestSpawn_OpenkanbanStyle exercises the prepareSpawn
// → spawnReadyMsg → Pane.StartCmd chain that our Model uses.
//
// Reproduces: awp's spawnAgent() (in internal/ui/model.go) creates a
// terminal.Pane, builds pi args, and returns spawnReadyMsg.
//
// Verifies: Pane.StartCmd launches the subprocess, mock pi's
// JSONL reaches GetContent (via readOutput Cmd).
func TestSpawn_OpenkanbanStyle(t *testing.T) {
	testutil.RequireLinux(t)

	mockPath := testutil.RepoPath(t, "internal", "pi", "testdata", "mock-pi.sh")

	// === prepareSpawn equivalent ===
	ticketID := board.TicketID("ticket-abc-123")
	width, height := 80, 22

	pane := terminal.New(string(ticketID), width, height, 0)
	pane.SetWorkdir(t.TempDir())
	pane.SetSessionName(string(ticketID))

	// case "pi" args (from prepareSpawn)
	args := []string{"--mode", "rpc"}

	// === spawnReadyMsg handler equivalent ===
	panes := map[board.TicketID]*terminal.Pane{
		ticketID: pane,
	}

	// Pane.Start returns readOutput Cmd.
// Bubble Tea would feed back the OutputMsg to Pane.Update,
// which writes to vt and returns the next readOutput.
// We simulate that loop manually here.
	cmd := pane.StartCmd(mockPath, args...)
	if cmd == nil {
		t.Fatal("Pane.StartCmd returned nil")
	}

	// Drive the self-cycle: each readOutput returns one OutputMsg.
	// Pane.Update(OutputMsg) writes to vt + returns next readOutput.
	for i := 0; i < 20; i++ {
		time.Sleep(50 * time.Millisecond)
		msg := cmd()
		if msg == nil {
			continue
		}
		if exitMsg, ok := msg.(terminal.ExitMsg); ok {
			t.Logf("pane exited early: %v", exitMsg.Err)
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

	// === verify spawn actually worked ===
	if _, ok := panes[ticketID]; !ok {
		t.Fatal("pane not stored in m.panes")
	}

	if !pane.Running() {
		t.Errorf("pane.Running()=false; want true")
	}

	content := pane.GetContent()
	if !strings.Contains(content, "agent_start") {
		t.Errorf("missing agent_start in pane content; got: %q", content[:min(200, len(content))])
	}

	if err := pane.Stop(); err != nil {
		t.Errorf("pane.Stop: %v", err)
	}
}
