// toggle_status_test.go — TDD tests for the simplified keyboard
// navigation after the 2026-06-28 state-machine simplification.
//
// The keyboard Space handler toggles a ticket's status between
// Backlog and In Progress (bidirectional). There is no `-`/backspace
// key for the reverse direction (in 2-state model, Space is its own
// inverse).
//
// The toggle logic is a pure function on the receiver (doesn't read
// m.*), so we test it with a zero-value Model. The end-to-end
// quickMoveTicket (now: toggleSelectedTicket) behavior is tested in
// TestQuickMoveTicket_TogglesStatus which exercises the full
// select → space → verify path.
//
// CORRECT-7 self-check on this test file:
//   C-onformance: literal status equality
//   O-rdering:    N/A (pure function)
//   R-ange:       both states × 2 directions = 4 cases
//   R-eference:   no external deps (pure function)
//   E-xistence:   regression target: Space on a non-empty ticket
//   C-ardinality: 2 states + 1 end-to-end
//   T-ime:        no time concerns
package ui

import (
	"testing"

	"github.com/pi/awp/internal/board"
)

// TestToggleTicketStatus_PureFunction pins the bidirectional toggle
// mapping. The 2-state model collapses the original 4×4 matrix to
// 2 states × 2 directions = 4 cases.
func TestToggleTicketStatus_PureFunction(t *testing.T) {
	m := &Model{}

	tests := []struct {
		name    string
		current board.TicketStatus
		want    board.TicketStatus
	}{
		{"backlog_advances_to_in_progress", board.StatusBacklog, board.StatusInProgress},
		{"in_progress_reverts_to_backlog", board.StatusInProgress, board.StatusBacklog},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.toggleTicketStatus(tt.current)
			if got != tt.want {
				t.Errorf("toggleTicketStatus(%q) = %q; want %q",
					tt.current, got, tt.want)
			}
		})
	}
}

// TestToggleTicketStatus_IsOwnInverse is a structural property test:
// applying toggle twice returns the original. This pins the
// "bidirectional" semantic — pressing Space twice should leave the
// ticket where it started.
func TestToggleTicketStatus_IsOwnInverse(t *testing.T) {
	m := &Model{}
	for _, s := range []board.TicketStatus{board.StatusBacklog, board.StatusInProgress} {
		t.Run(string(s), func(t *testing.T) {
			toggled := m.toggleTicketStatus(s)
			back := m.toggleTicketStatus(toggled)
			if back != s {
				t.Errorf("toggle is not its own inverse: toggle(%q)=%q, toggle(%q)=%q; want %q",
					s, toggled, toggled, back, s)
			}
		})
	}
}
