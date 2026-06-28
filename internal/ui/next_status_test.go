// next_status_test.go — TDD regression test for the keyboard navigation
// state-machine mapping in (Model).nextStatus / (Model).previousStatus.
//
// Bug (ticket "Fix awp - the task in done column cannot move"):
// nextStatus(StatusDone) fell into the `default:` branch and returned
// `current` (i.e. StatusDone). The keyboard handler in quickMoveTicket
// then short-circuits on `if nextStatus == ticket.Status { return m, nil }`
// — pressing space on a done ticket silently did nothing. No
// notification, no move, no obvious feedback. Users reported the
// ticket "couldn't move" from the done column.
//
// The underlying state machine (board.Ticket.CanTransitionTo) ALLOWS
// `done → archived` and `done → backlog/in_progress` per SYSTEM_DESIGN.md
// §3 and AGENTS.md §5.5. The bug was purely in the keyboard UI mapping.
//
// Fix: nextStatus(StatusDone) must return StatusArchived (the natural
// forward step in the lifecycle: done → archive). We pin every
// transition with a unit test so a future refactor can't silently
// regress the keyboard-driven movement.
//
// CORRECT-7 self-check on this test file:
//   C-onformance: literal equality of status values
//   O-rdering:    N/A (each case is independent)
//   R-ange:       all 4 statuses × 2 directions = 8 cases
//   R-eference:   no external deps; methods are pure (don't read m.*)
//   E-xistence:   StatusDone is the regression target; covered explicitly
//   C-ardinality: 4 statuses, 2 methods; boundary StatusArchived covered
//   T-ime:        no time concerns
package ui

import (
	"testing"

	"github.com/pi/awp/internal/board"
	"github.com/pi/awp/internal/config"
	"github.com/pi/awp/internal/project"
)

// nextStatus / previousStatus are pure functions on the receiver
// (don't read m.*), so we use a zero-value Model.
func newPureModelForStatusTest() *Model { return &Model{} }

// TestNextStatus_Mapping pins every transition of the keyboard forward
// (space) mapping. The original bug was nextStatus(StatusDone) ==
// StatusDone (no-op). This test enforces the corrected behavior: done
// advances to archived.
func TestNextStatus_Mapping(t *testing.T) {
	m := newPureModelForStatusTest()

	tests := []struct {
		name    string
		current board.TicketStatus
		want    board.TicketStatus
	}{
		// Forward through the visible columns (Backlog → InProgress → Done).
		{"backlog_advances_to_in_progress", board.StatusBacklog, board.StatusInProgress},
		{"in_progress_advances_to_done", board.StatusInProgress, board.StatusDone},

		// REGRESSION: done must NOT be a dead-end. It advances to archived,
		// which is the natural "clean up finished work" step documented in
		// SYSTEM_DESIGN.md (line ~436: done → archived | ✅ | 归档).
		{"done_advances_to_archived", board.StatusDone, board.StatusArchived},

		// Archived is terminal — forward from archived is a no-op.
		// (board.CanTransitionTo also forbids archived → anything; the UI
		// must not invent a transition the state machine rejects.)
		{"archived_is_terminal_forward", board.StatusArchived, board.StatusArchived},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.nextStatus(tt.current)
			if got != tt.want {
				t.Errorf("nextStatus(%q) = %q; want %q", tt.current, got, tt.want)
			}
		})
	}
}

// TestPreviousStatus_Mapping pins every transition of the keyboard
// backward (-/backspace) mapping. previousStatus(StatusDone) returning
// StatusInProgress was already correct (the user can reopen a done
// ticket). We pin it here so a future refactor doesn't break the
// reopen flow while fixing the forward direction.
func TestPreviousStatus_Mapping(t *testing.T) {
	m := newPureModelForStatusTest()

	tests := []struct {
		name    string
		current board.TicketStatus
		want    board.TicketStatus
	}{
		// Backward through the visible columns.
		{"done_back_to_in_progress", board.StatusDone, board.StatusInProgress},
		{"in_progress_back_to_backlog", board.StatusInProgress, board.StatusBacklog},

		// Backlog has no earlier state — backward is a no-op.
		{"backlog_is_terminal_backward", board.StatusBacklog, board.StatusBacklog},

		// Archived is terminal — backward is a no-op.
		{"archived_is_terminal_backward", board.StatusArchived, board.StatusArchived},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.previousStatus(tt.current)
			if got != tt.want {
				t.Errorf("previousStatus(%q) = %q; want %q", tt.current, got, tt.want)
			}
		})
	}
}

// TestQuickMoveTicket_DoneIsNotNoOp is the end-to-end regression test:
// selecting a ticket in StatusDone and calling quickMoveTicket MUST
// invoke globalStore.Move with a different status (so the ticket
// actually moves and the user sees a notification). Before the fix,
// quickMoveTicket returned early with `return m, nil` because
// nextStatus(StatusDone) == StatusDone, leaving the user with no
// feedback that anything was attempted.
func TestQuickMoveTicket_DoneIsNotNoOp(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("AWP_CONFIG_DIR", cfgDir)

	cfg := config.DefaultConfig()
	reg, err := project.LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	proj := project.NewProject("done-move-test", cfgDir)
	if err := reg.Add(proj); err != nil {
		t.Fatalf("reg.Add: %v", err)
	}
	gts, err := project.LoadGlobalTicketStore(reg)
	if err != nil {
		t.Fatalf("LoadGlobalTicketStore: %v", err)
	}

	// Create a single ticket and place it in the Done column.
	ticket := board.NewTicket("Done ticket", proj.ID)
	ticket.Status = board.StatusDone
	if err := gts.Add(ticket); err != nil {
		t.Fatalf("gts.Add: %v", err)
	}

	m := NewModel(cfg, gts, reg, "", nil)
	m.refreshColumnTickets()

	// Find the Done column index so we can select the ticket there.
	doneCol := -1
	for i, c := range m.columns {
		if c.Status == board.StatusDone {
			doneCol = i
			break
		}
	}
	if doneCol < 0 {
		t.Fatal("Done column not found in m.columns")
	}
	m.activeColumn = doneCol
	m.activeTicket = 0
	if len(m.columnTickets[doneCol]) == 0 {
		t.Fatal("ticket not present in Done column after refresh")
	}

	// Sanity: pre-state is Done.
	pre, _ := gts.Get(ticket.ID)
	if pre.Status != board.StatusDone {
		t.Fatalf("pre-state Status = %q; want %q", pre.Status, board.StatusDone)
	}

	// Press space (quickMoveTicket). It's synchronous (returns nil cmd).
	_, _ = m.quickMoveTicket()

	// Post-state must NOT be Done — the bug was that it stayed Done silently.
	post, _ := gts.Get(ticket.ID)
	if post.Status == board.StatusDone {
		t.Errorf("quickMoveTicket on a Done ticket was a no-op; ticket still in Done column.\n"+
			"This is the original bug: nextStatus(StatusDone) == StatusDone, so the\n"+
			"`if nextStatus == ticket.Status { return m, nil }` short-circuit fired\n"+
			"and the user saw no movement and no notification.\n"+
			"Expected the ticket to advance (e.g. to Archived).")
	}
}