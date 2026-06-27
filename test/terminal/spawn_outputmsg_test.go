//go:build integration

package terminal_test

import (
	"testing"
	"time"

	"github.com/pi/awp/internal/terminal"

	testutil "github.com/pi/awp/internal/testutil"
)

// TestSpawn_TriggersOutputMsg verifies that Pane.Start causes
// Bubble Tea to receive terminal.OutputMsg — the signal
// the Model uses to switch from ModeSpawning to ModeAgentView.
//
// This was the root cause of "spawn doesn't work" — Pane.Start
// returned nil (Phase 8 refactor: readLoop self-driven), so
// no OutputMsg was ever dispatched, ModeAgentView never
// activated, user stuck on spinner.
func TestSpawn_TriggersOutputMsg(t *testing.T) {
	testutil.RequireLinux(t)

	mockPath := testutil.RepoPath(t, "internal", "pi", "testdata", "mock-pi.sh")

	pane := terminal.New("test", 80, 24, 0)
	pane.SetWorkdir(t.TempDir())

	// Start should return a Cmd that produces OutputMsg
	// (Pane.Start returns p.readOutputUnlocked()())
	cmd := pane.StartCmd(mockPath, "--mode", "rpc")
	if cmd == nil {
		t.Fatal("Pane.Start returned nil — no Cmd to trigger OutputMsg")
		t.FailNow()
	}

	// Run the Cmd; it should return a tea.Msg
	msg := cmd()
	if msg == nil {
		t.Fatal("Cmd returned nil message — no OutputMsg dispatched")
	}

	// Verify it's an OutputMsg
	if _, ok := msg.(terminal.OutputMsg); !ok {
		t.Errorf("Cmd returned %T, want terminal.OutputMsg", msg)
	}
	_ = time.Second // unused import
}
