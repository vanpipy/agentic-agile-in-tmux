// turn_done_cache_lifecycle_test.go — TDD tests for turnDoneCaches
// lifecycle cleanup.
//
// Ticket task/awp follow-up: PR 2 added per-pane turn-done caches
// in m.turnDoneCaches. When panes are removed (process exit, ticket
// delete, stop), the cache entries MUST also be removed to avoid
// an unbounded map leak over long awp sessions.
//
// These tests pin the cleanup contract for the three common paths:
//
//   1. terminal.ExitMsg   (process exit / crash)
//   2. performTicketCleanup (user pressed 'd')
//   3. stopAgent          (user pressed 'S')
//
// The two rare error paths (spawn failure, resetSpawnState) are
// covered by their own existing tests; if they leak caches in
// practice we can add coverage later.

package ui

import (
	"testing"

	"github.com/pi/awp/internal/board"
	"github.com/pi/awp/internal/config"
	"github.com/pi/awp/internal/pi"
	"github.com/pi/awp/internal/project"
	"github.com/pi/awp/internal/terminal"
)

// TestTurnDoneCache_PrunedOnExitMsg: when a pane exits via
// terminal.ExitMsg, its turnDoneCaches entry is removed.
//
// Pre-fix bug: ExitMsg handler deleted m.panes[ticketID] but did
// not touch m.turnDoneCaches. After many exit cycles the map
// accumulated orphan entries.
func TestTurnDoneCache_PrunedOnExitMsg(t *testing.T) {
	m, ticketA, _ := setupPaneTurnDoneTestEnv(t)
	m.turnDoneCaches.Store(ticketA.ID, &pi.TurnDoneCache{})

	if _, loaded := m.turnDoneCaches.Load(ticketA.ID); !loaded {
		t.Fatal("test setup: cache should be present before ExitMsg")
	}

	_, _ = m.Update(terminal.ExitMsg{PaneID: string(ticketA.ID), Err: nil})

	if _, loaded := m.turnDoneCaches.Load(ticketA.ID); loaded {
		t.Error("ExitMsg handler did NOT prune m.turnDoneCaches for the exiting pane.\n" +
			"Pre-fix: cache entries leak — over a long session this is unbounded growth.\n" +
			"Post-fix: m.turnDoneCaches.Delete(ticketID) is called alongside m.panes.Delete.")
	}
}

// TestTurnDoneCache_PrunedOnPerformTicketCleanup: when the user
// deletes a ticket, its pane and cache are both removed. We invoke
// performTicketCleanup directly (it's the function that owns the
// cleanup logic) so we don't have to drive the full 'd' + confirm
// flow through Update.
func TestTurnDoneCache_PrunedOnPerformTicketCleanup(t *testing.T) {
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

	ticketA := board.NewTicket("Doomed ticket", p.ID)
	if err := gts.Add(ticketA); err != nil {
		t.Fatalf("gts.Add: %v", err)
	}

	cfg := config.DefaultConfig()
	m := NewModel(cfg, gts, registry, "", nil)

	// Inject a render-mode pane (mimics a running pi session).
	paneA := terminal.New(string(ticketA.ID), 80, 24, 0)
	if cmd := paneA.StartCmd("", nil...); cmd != nil {
		cmd()
	}
	m.panes[ticketA.ID] = paneA
	m.turnDoneCaches.Store(ticketA.ID, &pi.TurnDoneCache{})

	m.performTicketCleanup(ticketA)

	if _, loaded := m.turnDoneCaches.Load(ticketA.ID); loaded {
		t.Error("performTicketCleanup did NOT prune m.turnDoneCaches.\n" +
			"Same leak as the ExitMsg case — see TestTurnDoneCache_PrunedOnExitMsg.")
	}
	if _, exists := m.panes[ticketA.ID]; exists {
		t.Error("performTicketCleanup did NOT delete m.panes[ticketID] (test sanity check)")
	}
}

// TestTurnDoneCache_PrunedOnStopAgent: when the user explicitly
// stops a pane (key 'S'), the cache is pruned.
func TestTurnDoneCache_PrunedOnStopAgent(t *testing.T) {
	m, ticketA, _ := setupPaneTurnDoneTestEnv(t)
	m.turnDoneCaches.Store(ticketA.ID, &pi.TurnDoneCache{})

	// Setup: make A the selected ticket so stopAgent() targets it.
	// stopAgent reads m.selectedTicket() which inspects m.columnTickets.
	m.refreshColumnTickets()
	// Find the position of ticketA in the columns and select it.
	for col, tickets := range m.columnTickets {
		for i, t := range tickets {
			if t.ID == ticketA.ID {
				m.activeColumn = col
				m.activeTicket = i
				break
			}
		}
	}

	_, _ = m.stopAgent()

	if _, loaded := m.turnDoneCaches.Load(ticketA.ID); loaded {
		t.Error("stopAgent did NOT prune m.turnDoneCaches.")
	}
	if _, exists := m.panes[ticketA.ID]; exists {
		t.Error("stopAgent did NOT delete m.panes[ticketID] (test sanity check)")
	}
}