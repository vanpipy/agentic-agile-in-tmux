// poll_turn_done_test.go — TDD tests for the poll-driven turn-done
// notification path (PR 2, step 5+).
//
// The poll goroutine (pollTurnDonesAsync) detects a "toolUse → stop"
// transition via per-pane TurnDoneCache (tested separately in
// internal/pi/turn_done_cache_test.go) and emits a paneTurnDoneMsg.
// These tests pin the handler that processes that Msg — basically:
//
//   handlePaneTurnDone(msg paneTurnDoneMsg) {
//       if msg.paneID == string(m.focusedPane) return  // silent
//       title := msg.title (fallback to ticket ID)
//       m.notify(title + " finished a turn")
//   }
//
// Coverage:
//   - Non-focused pane → toast fires with ticket title.
//   - Focused pane → silent.
//   - Empty title → handler falls back to ticket ID.

package ui

import (
	"strings"
	"testing"

	"github.com/pi/awp/internal/board"
	"github.com/pi/awp/internal/config"
	"github.com/pi/awp/internal/project"
	"github.com/pi/awp/internal/terminal"
)

// TestHandlePaneTurnDone_NonFocusedFires pins the user spec:
// "user in B, A finishes a turn → TUI notification".
func TestHandlePaneTurnDone_NonFocusedFires(t *testing.T) {
	m, ticketA, ticketB := setupPaneTurnDoneTestEnv(t)
	m.focusedPane = ticketB.ID

	prev := m.notification
	m.handlePaneTurnDone(paneTurnDoneMsg{
		ticketID: ticketA.ID,
		paneID:   string(ticketA.ID),
		title:    ticketA.Title,
	})

	notif := m.notification
	if notif == prev {
		t.Fatal("non-focused pane turn-done did NOT produce a notification")
	}
	if !strings.Contains(notif, ticketA.Title) {
		t.Errorf("non-focused pane turn-done: toast should contain ticket A title.\n"+
			"Got m.notification = %q, want substring %q", notif, ticketA.Title)
	}
}

// TestHandlePaneTurnDone_FocusedSilent: user is already in the pane
// that finished — no toast needed (they can see the pane state).
func TestHandlePaneTurnDone_FocusedSilent(t *testing.T) {
	m, ticketA, _ := setupPaneTurnDoneTestEnv(t)
	m.focusedPane = ticketA.ID

	prev := m.notification
	m.handlePaneTurnDone(paneTurnDoneMsg{
		ticketID: ticketA.ID,
		paneID:   string(ticketA.ID),
		title:    ticketA.Title,
	})

	if m.notification != prev {
		t.Errorf("focused pane turn-done: notification should NOT change.\n"+
			"Got %q, want %q (unchanged)", m.notification, prev)
	}
}

// TestHandlePaneTurnDone_EmptyTitleFallback: if the ticket has no
// title, the handler still notifies using the ticket ID.
func TestHandlePaneTurnDone_EmptyTitleFallback(t *testing.T) {
	m, ticketA, ticketB := setupPaneTurnDoneTestEnv(t)
	m.focusedPane = ticketB.ID

	prev := m.notification
	m.handlePaneTurnDone(paneTurnDoneMsg{
		ticketID: ticketA.ID,
		paneID:   string(ticketA.ID),
		title:    "", // empty
	})

	notif := m.notification
	if notif == prev {
		t.Fatal("empty-title turn-done did NOT produce a notification")
	}
	if !strings.Contains(notif, string(ticketA.ID)) {
		t.Errorf("empty-title turn-done: toast should fall back to ticket ID.\n"+
			"Got m.notification = %q, want substring %q", notif, string(ticketA.ID))
	}
}

// TestHandlePaneTurnDone_NoFocusedPaneStaysSilent: if focus is
// empty (e.g., user is in Normal mode browsing tickets), no pane is
// focused, so any pane's turn-done should fire.
func TestHandlePaneTurnDone_NoFocusedPaneFires(t *testing.T) {
	m, ticketA, _ := setupPaneTurnDoneTestEnv(t)
	m.focusedPane = "" // no focus

	prev := m.notification
	m.handlePaneTurnDone(paneTurnDoneMsg{
		ticketID: ticketA.ID,
		paneID:   string(ticketA.ID),
		title:    ticketA.Title,
	})

	if m.notification == prev {
		t.Errorf("no-focus state: turn-done should still produce a notification.\n"+
			"Got m.notification = %q (unchanged from %q)", m.notification, prev)
	}
}

// setupPaneTurnDoneTestEnv builds a minimal Model with two tickets
// and a render-mode pane for one of them. Mirrors the env set up in
// exit_notification_test.go but kept independent so failures
// diagnose cleanly.
func setupPaneTurnDoneTestEnv(t *testing.T) (*Model, *board.Ticket, *board.Ticket) {
	t.Helper()
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

	ticketA := board.NewTicket("Render JSON diff", p.ID)
	ticketB := board.NewTicket("Update README", p.ID)
	if err := gts.Add(ticketA); err != nil {
		t.Fatalf("gts.Add A: %v", err)
	}
	if err := gts.Add(ticketB); err != nil {
		t.Fatalf("gts.Add B: %v", err)
	}

	cfg := config.DefaultConfig()
	m := NewModel(cfg, gts, registry, "", nil)

	// Inject a render-mode pane for A (mimics a running pi).
	paneA := terminal.New(string(ticketA.ID), 80, 24, 0)
	if cmd := paneA.StartCmd("", nil...); cmd != nil {
		cmd()
	}
	m.panes[ticketA.ID] = paneA

	return m, ticketA, ticketB
}
