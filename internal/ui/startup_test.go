package ui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pi/awp/internal/board"
	"github.com/pi/awp/internal/config"
	"github.com/pi/awp/internal/project"
)

// TestStartup_InitDoesNotBlock verifies NewModel + Init don't deadlock.
// User reported: "kill ./awp 进程, 还是进入就直接阻塞了"
//
// We test the Init path in isolation by running Init and a few
// Update messages with hard timeouts. If any of them blocks > 3s,
// we have a startup deadlock.
func TestStartup_InitDoesNotBlock(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("AWP_CONFIG_DIR", tmpDir)

	cfg := config.DefaultConfig()
	registry := &project.ProjectRegistry{
		Projects: map[string]*project.Project{},
	}
	gts, err := project.LoadGlobalTicketStore(registry)
	if err != nil {
		t.Fatalf("LoadGlobalTicketStore: %v", err)
	}

	model := NewModel(cfg, gts, registry, "", nil)

	// Init returns a batched cmd (spinner + tickAgentStatus + updates).
	// Invoking it should not block.
	done := make(chan struct{}, 1)
	go func() {
		_ = model.Init()
		done <- struct{}{}
	}()
	select {
	case <-done:
		// good
	case <-time.After(3 * time.Second):
		t.Fatal("Init blocked > 3s — startup deadlock")
	}
}

// TestStartup_AgentStatusTickDoesNotBlock verifies that the
// agentStatusMsg (from tickAgentStatus) is handled in Update
// without blocking. If pollAgentStatusesAsync holds a lock that
// another goroutine is waiting for, the second tick at 5s
// intervals would accumulate latency.
func TestStartup_AgentStatusTickDoesNotBlock(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("AWP_CONFIG_DIR", tmpDir)

	cfg := config.DefaultConfig()
	registry := &project.ProjectRegistry{
		Projects: map[string]*project.Project{},
	}
	gts, err := project.LoadGlobalTicketStore(registry)
	if err != nil {
		t.Fatalf("LoadGlobalTicketStore: %v", err)
	}

	model := NewModel(cfg, gts, registry, "", nil)
	_ = model.Init()

	// Simulate the agentStatusMsg Tick: this is what every 5s
	// fires. If pollAgentStatusesAsync inside Update blocks
	// (e.g., on a Pane lock that another goroutine holds),
	// this test times out.
	done := make(chan struct{}, 1)
	go func() {
		model.Update(agentStatusMsg(time.Now()))
		done <- struct{}{}
	}()
	select {
	case <-done:
		// good
	case <-time.After(3 * time.Second):
		t.Fatal("Update(agentStatusMsg) blocked > 3s — startup deadlock")
	}
}

// TestStartup_ViewDoesNotBlock ensures View() is non-blocking.
func TestStartup_ViewDoesNotBlock(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("AWP_CONFIG_DIR", tmpDir)

	cfg := config.DefaultConfig()
	registry := &project.ProjectRegistry{
		Projects: map[string]*project.Project{},
	}
	gts, err := project.LoadGlobalTicketStore(registry)
	if err != nil {
		t.Fatalf("LoadGlobalTicketStore: %v", err)
	}

	model := NewModel(cfg, gts, registry, "", nil)
	_ = model.Init()

	done := make(chan struct{}, 1)
	go func() {
		_ = model.View()
		done <- struct{}{}
	}()
	select {
	case <-done:
		// good
	case <-time.After(3 * time.Second):
		t.Fatal("View blocked > 3s — startup deadlock")
	}
}

var _ = tea.Cmd(nil) // keep import
var _ = board.AgentNone
