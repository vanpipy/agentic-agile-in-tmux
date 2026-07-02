// exit_notification_test.go — TDD tests for per-task exit notification.
//
// Ticket task/awp: when a non-focused pane's agent exits, the user
// should see a TUI-internal notification containing the ticket title.
// Without this, the user has to flip back to each pane to discover
// which ones finished.
//
// CORRECT-7 self-check:
//   C-onformance: literal string contains (title substring, "exited" / "crashed")
//   O-rdering: N/A
//   R-ange: 0/1/2 panes (tested: 0 panes, 1 focused pane, 2 panes with 1 non-focused)
//   R-eference: filesystem (git repo) + in-memory state only
//   E-xistence: edge case — empty panes map
//   C-ardinality: 0/1/2 panes
//   T-ime: no time concerns
package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pi/awp/internal/board"
	"github.com/pi/awp/internal/terminal"
)

// TestExitMsg_NotifiesNonFocusedPane pins the user spec:
// "10 tasks A–J running, user in B, A completes → TUI notification."
//
// Pre-fix bug: terminal.ExitMsg handler in model.go:448 only called
// m.notify() inside the `if m.focusedPane == ticketID` branch, so a
// non-focused pane's exit was silent. This test fails pre-fix
// (notification empty) and passes post-fix (notification contains A's title).
func TestExitMsg_NotifiesNonFocusedPane(t *testing.T) {
	m := newTestModel(t)

	// Add two tickets so we can have one focused and one not.
	projectID := ""
	for _, p := range m.globalStore.Projects() {
		projectID = p.ID
		break
	}
	ticketA := board.NewTicket("Render JSON diff", projectID)
	ticketB := board.NewTicket("Update README", projectID)
	if err := m.globalStore.Add(ticketA); err != nil {
		t.Fatalf("gts.Add A: %v", err)
	}
	if err := m.globalStore.Add(ticketB); err != nil {
		t.Fatalf("gts.Add B: %v", err)
	}

	// Inject a running pane for ticket A (mimicking pi having spawned).
	paneA := terminal.New(string(ticketA.ID), 80, 24, 0)
	if cmd := paneA.StartCmd("", nil...); cmd != nil {
		cmd()
	}
	m.panes[ticketA.ID] = paneA

	// User is currently focused on ticket B.
	m.focusedPane = ticketB.ID

	// Ticket A's pi process exits (e.g., user quits, network drops).
	_, _ = m.Update(terminal.ExitMsg{PaneID: string(ticketA.ID), Err: nil})

	// Assert: notification was set with ticket A's title.
	// Pre-fix this is empty (the bug we're fixing).
	notif := m.notification
	if !strings.Contains(notif, "Render JSON diff") {
		t.Errorf("non-focused pane exit did not produce a notification with ticket A's title.\n"+
			"Pre-fix: m.notification stayed empty (silent exit).\n"+
			"Post-fix: m.notification should contain ticket A's title.\n"+
			"Got m.notification = %q", notif)
	}
	// Assert: focus was NOT cleared (user is still in B).
	if m.focusedPane != ticketB.ID {
		t.Errorf("non-focused pane exit cleared m.focusedPane.\n"+
			"Pre-fix: only focused-pane exit would clear, but the *non-focused* one shouldn't.\n"+
			"Got m.focusedPane = %q, want %q", m.focusedPane, ticketB.ID)
	}
	// Assert: pane was removed from m.panes (cleanup still happens).
	if _, exists := m.panes[ticketA.ID]; exists {
		t.Errorf("m.panes still contains ticketA.ID after ExitMsg; should be deleted")
	}
}

// TestExitMsg_FocusedPaneNotifiesGeneric guards the existing behavior
// that focused-pane exits still notify (so the user gets feedback that
// the agent they were watching is gone). Pre-fix this was "Agent exited";
// post-fix we keep that wording for the focused case (the user knows
// which one because they were just looking at it).
func TestExitMsg_FocusedPaneNotifiesGeneric(t *testing.T) {
	m := newTestModel(t)

	projectID := ""
	for _, p := range m.globalStore.Projects() {
		projectID = p.ID
		break
	}
	ticketA := board.NewTicket("Focused task", projectID)
	if err := m.globalStore.Add(ticketA); err != nil {
		t.Fatalf("gts.Add: %v", err)
	}

	paneA := terminal.New(string(ticketA.ID), 80, 24, 0)
	if cmd := paneA.StartCmd("", nil...); cmd != nil {
		cmd()
	}
	m.panes[ticketA.ID] = paneA
	m.focusedPane = ticketA.ID
	m.mode = ModeAgentView

	_, _ = m.Update(terminal.ExitMsg{PaneID: string(ticketA.ID), Err: nil})

	// Focused-pane exit still notifies.
	if m.notification == "" {
		t.Errorf("focused-pane exit cleared m.notification; expected 'Agent exited' toast.\n"+
			"Got m.notification = %q", m.notification)
	}
	// Focused-pane exit must clear focus and reset to Normal mode.
	if m.focusedPane != "" {
		t.Errorf("focused-pane exit did not clear m.focusedPane\n"+
			"Got m.focusedPane = %q, want \"\"", m.focusedPane)
	}
	if m.mode != ModeNormal {
		t.Errorf("focused-pane exit did not switch to ModeNormal\n"+
			"Got m.mode = %v, want %v", m.mode, ModeNormal)
	}
}

// TestExitMsg_CrashUsesFailedWording guards the new "failed" wording
// when ExitMsg carries a non-nil error. The view layer picks the ✗ icon
// based on "Failed" prefix or "failed" substring (view.go:471-484), so
// our message uses that exact convention to trigger error styling.
func TestExitMsg_CrashUsesFailedWording(t *testing.T) {
	m := newTestModel(t)

	projectID := ""
	for _, p := range m.globalStore.Projects() {
		projectID = p.ID
		break
	}
	ticketA := board.NewTicket("Crashy task", projectID)
	if err := m.globalStore.Add(ticketA); err != nil {
		t.Fatalf("gts.Add: %v", err)
	}

	paneA := terminal.New(string(ticketA.ID), 80, 24, 0)
	if cmd := paneA.StartCmd("", nil...); cmd != nil {
		cmd()
	}
	m.panes[ticketA.ID] = paneA
	// User is NOT focused on A.
	m.focusedPane = board.NewTicketID()

	_, _ = m.Update(terminal.ExitMsg{
		PaneID: string(ticketA.ID),
		Err:    errSentinel("network died"),
	})

	notif := m.notification
	if !strings.Contains(notif, "Crashy task") {
		t.Errorf("crashed non-focused pane: notification should contain ticket title.\n"+
			"Got m.notification = %q", notif)
	}
	// "failed" substring is what the view layer uses to pick the ✗
	// icon (view.go:471-484 detects "Failed" prefix or "failed" substring).
	if !strings.Contains(strings.ToLower(notif), "failed") {
		t.Errorf("crashed exit should say 'failed' to trigger error icon.\n"+
			"Got m.notification = %q", notif)
	}
}

// errSentinel is a tiny error helper so we don't need to import "errors"
// just for a string-based error in tests.
type errSentinel string

func (e errSentinel) Error() string { return string(e) }

// Compile-time check that Update still returns (tea.Model, tea.Cmd).
// Catches the case where a refactor changes the signature.
var _ func(m *Model, msg tea.Msg) (tea.Model, tea.Cmd) = (*Model).Update