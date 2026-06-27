// transition_test.go — TDD tests for Cluster D.3 fix.
//
// Cluster D.3 (Major from 2026-06-27 audit): Ticket.Move() and SetStatus()
// accept any transition, including invalid ones. A drag-drop can move a
// ticket from in_progress back to backlog while a pi agent is running,
// leaving the UI in an inconsistent state.
//
// Fix: add Ticket.CanTransitionTo(target TicketStatus) error in board
// package. UI calls it before Move()/SetStatus(). Transitions are:
//   - backlog ↔ in_progress (allowed)
//   - in_progress → done (allowed; marks CompletedAt)
//   - done → backlog or done → in_progress (allowed; user reopens)
//   - any → archived (allowed except from archived)
//   - archived → * (FORBIDDEN: archived is terminal)
//
// Additionally, transitions FROM in_progress TO backlog are blocked if
// AgentStatus is AgentWorking (would orphan the running pi).
//
// This is the canonical state machine; tests pin all 16 transitions
// plus the AgentWorking guard.
package board

import (
	"os"
	"path/filepath"
	"testing"
)

// TestTicket_CanTransitionTo_BasicMatrix covers all 16 transitions in
// the 4×4 status matrix. Most are allowed; archived → * is forbidden.
//
// CORRECT-7 self-check:
//   C-onformance: error must be nil for allowed, non-nil for forbidden
//   O-rdering: N/A (each transition is independent)
//   R-ange: 4×4 = 16 cases
//   R-eference: no external deps
//   E-xistence: covered (same-status transition included)
//   C-ardinality: 16 cases
//   T-ime: no time concerns
func TestTicket_CanTransitionTo_BasicMatrix(t *testing.T) {
	tests := []struct {
		fromStr string
		toStr   string
		wantErr bool
	}{
		// FROM backlog
		{"backlog", "backlog", false}, // same-status is OK (no-op)
		{"backlog", "in_progress", false},
		{"backlog", "done", false},
		{"backlog", "archived", false},

		// FROM in_progress (no running agent)
		{"in_progress", "backlog", false}, // reopen allowed when no agent
		{"in_progress", "in_progress", false},
		{"in_progress", "done", false},
		{"in_progress", "archived", false},

		// FROM done
		{"done", "backlog", false}, // reopen
		{"done", "in_progress", false}, // restart
		{"done", "done", false},
		{"done", "archived", false},

		// FROM archived (terminal — all transitions forbidden)
		{"archived", "backlog", true},
		{"archived", "in_progress", true},
		{"archived", "done", true},
		{"archived", "archived", false}, // same-status no-op
	}

	for _, tt := range tests {
		t.Run(tt.fromStr+"_to_"+tt.toStr, func(t *testing.T) {
			ticket := NewTicket("t", "p1")
			ticket.Status = TicketStatus(tt.fromStr)
			// Force a clean AgentStatus (no running agent).
			ticket.AgentStatus = AgentNone

			err := ticket.CanTransitionTo(TicketStatus(tt.toStr))
			gotErr := err != nil
			if gotErr != tt.wantErr {
				t.Errorf("CanTransitionTo(%s → %s): gotErr=%v, wantErr=%v (err=%v)",
					tt.fromStr, tt.toStr, gotErr, tt.wantErr, err)
			}
		})
	}
}

// TestTicket_CanTransitionTo_RejectsOrphanAgent was REMOVED in the FOOT-3
// follow-up (post-P3P4 audit): CanTransitionTo is now PURE (no agent
// coupling). The orphan-agent guard moved to the UI layer (see
// model.go:dropTicket / quickMoveTicket which check AgentWorking
// before calling Move()).

// TestTicket_CanTransitionTo_AllowsReopenWhenAgentIdle was REMOVED:
// with CanTransitionTo now pure, this test is trivially true for
// all AgentStatus values. The caller-side guard (UI layer) is what
// distinguishes AgentWorking (blocked) from AgentIdle (allowed).

// TestTicket_CanTransitionTo_RejectsArchivedTerminal pins the contract
// that archived is terminal. From archived, you cannot transition to any
// other status (the only "valid" target is itself, as a no-op).
func TestTicket_CanTransitionTo_RejectsArchivedTerminal(t *testing.T) {
	ticket := NewTicket("archived-ticket", "p1")
	ticket.Status = StatusArchived
	ticket.AgentStatus = AgentNone

	for _, target := range []TicketStatus{StatusBacklog, StatusInProgress, StatusDone} {
		err := ticket.CanTransitionTo(target)
		if err == nil {
			t.Errorf("CanTransitionTo(archived → %s) returned nil; want error (archived is terminal)", target)
		}
	}
}

// TestTicket_SetStatus_RespectsStateMachine verifies the integration:
// SetStatus (called by Move()) should also enforce CanTransitionTo.
// If a future PR adds a fast-path that bypasses the state machine,
// this test surfaces it.
//
// Currently SetStatus unconditionally applies the new status; this
// test pins the contract that the new validation gate MUST be in
// place. If the gate isn't there, the test fails RED.
//
// CORRECT-7 self-check:
//   C-onformance: SetStatus returns error for forbidden transition
//   O-rdering: N/A
//   R-ange: 1 forbidden + 1 allowed case
//   R-eference: no external deps
//   E-xistence: ticket retains original Status after rejected SetStatus
//   C-ardinality: 2 cases
//   T-ime: no time concerns
func TestTicket_SetStatus_RespectsStateMachine(t *testing.T) {
	t.Run("forbidden transition returns error", func(t *testing.T) {
		ticket := NewTicket("forbidden", "p1")
		ticket.Status = StatusArchived

		err := ticket.SetStatus(StatusInProgress)
		if err == nil {
			t.Fatal("SetStatus(archived → in_progress) returned nil; want error")
		}
		// Ticket status should NOT have changed.
		if ticket.Status != StatusArchived {
			t.Errorf("Status = %q after rejected SetStatus; want %q (unchanged)",
				ticket.Status, StatusArchived)
		}
	})

	t.Run("allowed transition succeeds", func(t *testing.T) {
		ticket := NewTicket("allowed", "p1")
		ticket.Status = StatusBacklog

		err := ticket.SetStatus(StatusInProgress)
		if err != nil {
			t.Errorf("SetStatus(backlog → in_progress) returned error: %v", err)
		}
		if ticket.Status != StatusInProgress {
			t.Errorf("Status = %q; want %q", ticket.Status, StatusInProgress)
		}
	})
}

// TestBoardGo_CanTransitionToExists pins the source-level contract that
// the CanTransitionTo method exists in board.go. Structural regression
// test for Cluster D.3.
func TestBoardGo_CanTransitionToExists(t *testing.T) {
	src := readBoardGoSource(t)
	if !contains(src, "func (t *Ticket) CanTransitionTo(") {
		t.Errorf("board.go missing Ticket.CanTransitionTo method.\n"+
			"Cluster D.3: this is the canonical state-machine validator.")
	}
}

// --- helpers ---

// contains is a tiny substring helper.
func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func readBoardGoSource(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			path := filepath.Join(dir, "internal", "board", "board.go")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			return string(data)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}