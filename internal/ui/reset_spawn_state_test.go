package ui

import (
	"testing"

	"github.com/pi/awp/internal/board"
	"github.com/pi/awp/internal/config"
	"github.com/pi/awp/internal/project"
	"github.com/pi/awp/internal/terminal"
)

// TestResetSpawnState_StopsPane guards the fix for the user
// report "second spawn 界面停滞": when a spawn fails (e.g., pi
// crashes on an unknown flag), resetSpawnState must call Pane.Stop()
// to release the altScreenConsumer goroutine and PTY file descriptor.
// Without this, the goroutine leaks and blocks subsequent spawns.
//
// This test injects a Pane into m.panes, calls resetSpawnState, and
// verifies that the Pane's Running() flipped to false — which only
// happens inside Pane.Stop().
func TestResetSpawnState_StopsPane(t *testing.T) {
	tmpDir := t.TempDir()
	if err := initGitRepoForTest(t, tmpDir); err != nil {
		t.Fatalf("init git repo: %v", err)
	}

	registry := &project.ProjectRegistry{
		Projects: map[string]*project.Project{},
	}
	p := project.NewProject("test-proj", tmpDir)
	registry.Projects[p.ID] = p

	gts, err := project.LoadGlobalTicketStore(registry)
	if err != nil {
		t.Fatalf("LoadGlobalTicketStore: %v", err)
	}

	ticket := board.NewTicket("hello", p.ID)
	ticket.AgentSpawnedAt = nil // ensure resetSpawnState can write
	if err := gts.Add(ticket); err != nil {
		t.Fatalf("gts.Add: %v", err)
	}

	cfg := DefaultConfigForTest()
	model := NewModel(cfg, gts, registry, "", nil)

	// Inject a Pane into m.panes. Use New() (no Start) so we don't
	// actually need a PTY subprocess. New() initializes the pane
	// fields but p.running remains false. We call StartCmd with
	// empty command (render-only mode) to flip running to true
	// without needing a real pi process.
	pane := terminal.New(string(ticket.ID), 80, 24, 0)
	cmd := pane.StartCmd("", nil...)
	if cmd != nil {
		cmd()
	}
	// Verify pane is "running" before reset (StartCmd flips it)
	if !pane.Running() {
		t.Fatal("test setup: pane should be running after StartCmd")
	}

	model.panes[ticket.ID] = pane

	// Invoke resetSpawnState. Should:
	//   1. Call Pane.Stop() (pane.running → false)
	//   2. Delete m.panes[ticket.ID]
	model.resetSpawnState(ticket.ID)

	// CRITICAL: verify Pane was stopped (Running() == false).
	// If a future refactor removes the Stop() call, this assertion
	// fails and the leak bug returns.
	if pane.Running() {
		t.Error("resetSpawnState did not call Pane.Stop() — " +
			"altScreenConsumer goroutine and PTY fd will leak. " +
			"Regression: second spawn will stall.")
	}

	// And m.panes should no longer contain the ticket.
	if _, exists := model.panes[ticket.ID]; exists {
		t.Error("resetSpawnState did not delete m.panes[ticket.ID]")
	}
}

// DefaultConfigForTest returns a config suitable for tests that
// don't exercise config-related logic. We don't reuse
// config.DefaultConfig directly because that would couple this
// test to that function's evolution; tests should be self-contained.
func DefaultConfigForTest() *config.Config {
	return config.DefaultConfig()
}
