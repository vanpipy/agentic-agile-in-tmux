package ui

import (
	"testing"
	"time"

	"github.com/pi/awp/internal/board"
	"github.com/pi/awp/internal/config"
	"github.com/pi/awp/internal/project"
	"github.com/pi/awp/internal/terminal"
)

// TestSpawnTwice_DoesNotDeadlock reproduces user report
// "kill ./awp 进程, 还是进入就直接阻塞了".
//
// The user spawns a ticket, pi dies, resetSpawnState runs, then
// the user re-attempts spawn — and the new attempt deadlocks.
//
// In production this would happen with a real PTY subprocess,
// but we can reproduce the deadlock with a "render-only" Pane
// (StartCmd with empty command) since the deadlock is in the
// goroutine + lock plumbing, not in pi-specific behavior.
func TestSpawnTwice_DoesNotDeadlock(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("AWP_CONFIG_DIR", tmpDir)

	cfg := config.DefaultConfig()
	registry := &project.ProjectRegistry{
		Projects: map[string]*project.Project{},
	}
	p := project.NewProject("test", tmpDir)
	registry.Projects[p.ID] = p
	gts, err := project.LoadGlobalTicketStore(registry)
	if err != nil {
		t.Fatalf("LoadGlobalTicketStore: %v", err)
	}

	ticket := board.NewTicket("hello", p.ID)
	ticket.UseWorktree = false
	if err := gts.Add(ticket); err != nil {
		t.Fatalf("gts.Add: %v", err)
	}

	model := NewModel(cfg, gts, registry, "", nil)

	// === First spawn ===
	firstCmd := model.prepareSpawn(ticket, p)
	if firstCmd == nil {
		t.Fatal("first prepareSpawn returned nil")
	}
	firstMsg := firstCmd()
	ready1, ok := firstMsg.(spawnReadyMsg)
	if !ok {
		// In test env, worktree-related errors are common. Skip test
		// gracefully since the deadlock path doesn't reach this far.
		t.Skipf("first prepareSpawn returned %T (likely worktree env issue); skipping", firstMsg)
	}
	// Drive StartCmd (sets running=true via installCallbacks)
	startCmd1 := ready1.pane.StartCmd(ready1.command, ready1.args...)
	if startCmd1 != nil {
		_ = startCmd1()
	}
	if !ready1.pane.Running() {
		t.Fatal("first pane should be running")
	}

	// === Failure: simulate spawn failure (e.g., pi crashed) ===
	// Inject the pane into m.panes and call resetSpawnState
	model.panes[ticket.ID] = ready1.pane
	model.spawningTicketID = ticket.ID
	model.resetSpawnState(ticket.ID)
	if ready1.pane.Running() {
		t.Error("first pane should be stopped after resetSpawnState")
	}

	// === Second spawn — must not deadlock ===
	done := make(chan struct{}, 1)
	go func() {
		secondCmd := model.prepareSpawn(ticket, p)
		if secondCmd == nil {
			t.Error("second prepareSpawn returned nil")
			done <- struct{}{}
			return
		}
		secondMsg := secondCmd()
		ready2, ok := secondMsg.(spawnReadyMsg)
		if !ok {
			t.Errorf("second prepareSpawn returned %T, want spawnReadyMsg: %+v", secondMsg, secondMsg)
			done <- struct{}{}
			return
		}
		// Drive StartCmd
		startCmd2 := ready2.pane.StartCmd(ready2.command, ready2.args...)
		if startCmd2 != nil {
			_ = startCmd2()
		}
		if !ready2.pane.Running() {
			t.Error("second pane should be running")
		}
		// Stop second pane to clean up
		_ = ready2.pane.Stop()
		done <- struct{}{}
	}()
	select {
	case <-done:
		// good
	case <-time.After(5 * time.Second):
		t.Fatal("second spawn deadlocked")
	}
}

var _ = terminal.New // keep import
