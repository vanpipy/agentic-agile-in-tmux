package ui

import (
	"testing"
	"time"

	"github.com/pi/awp/internal/board"
	"github.com/pi/awp/internal/config"
	"github.com/pi/awp/internal/project"
)

// TestSpawnAgent_DoesNotBlock verifies that calling spawnAgent on
// a model doesn't block. This is what the user invokes by pressing
// 's' on an in-progress ticket.
func TestSpawnAgent_DoesNotBlock(t *testing.T) {
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
	ticket.Status = board.StatusInProgress
	ticket.UseWorktree = false
	if err := gts.Add(ticket); err != nil {
		t.Fatalf("gts.Add: %v", err)
	}

	model := NewModel(cfg, gts, registry, "", nil)
	_ = model.Init()

	// Select the ticket. The default activeColumn=0 is backlog; we
	// need activeColumn=1 (in_progress). Move to in_progress.
	model.activeColumn = 1
	model.refreshColumnTickets()

	done := make(chan struct{}, 1)
	go func() {
		_, _ = model.spawnAgent()
		done <- struct{}{}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("spawnAgent blocked — likely prepareSpawn deadlock")
	}
}
