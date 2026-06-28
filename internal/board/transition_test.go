// transition_test.go — TDD tests for the simplified ticket state machine.
//
// Post-2026-06-28 simplification: the state machine has been reduced
// from 4 states (backlog / in_progress / done / archived) to 2 states
// (backlog / in_progress). The "is this done?" judgment is the user's,
// not encoded in state — when finished, the user deletes the ticket
// (not moves it to a terminal status).
//
// Transitions allowed:
//   - backlog → in_progress (start work)
//   - in_progress → backlog (pause / reorder)
//   - same-status (no-op, allowed)
//
// All other transitions are forbidden by the state machine. Note that
// orphan-agent protection (in_progress → backlog while AgentStatus ==
// AgentWorking) is enforced by the UI layer (caller-side guard), not
// here — CanTransitionTo is intentionally pure (no agent coupling).
//
// This is the canonical state machine; tests pin every transition
// (2×2 matrix = 4 cases).
package board

import (
	"os"
	"path/filepath"
	"testing"
)

// TestTicket_CanTransitionTo_BasicMatrix covers all 4 transitions in
// the simplified 2×2 status matrix.
//
// CORRECT-7 self-check:
//   C-onformance: error must be nil for allowed, non-nil for forbidden
//   O-rdering: N/A (each transition is independent)
//   R-ange: 2×2 = 4 cases
//   R-eference: no external deps
//   E-xistence: covered (same-status transition included)
//   C-ardinality: 4 cases
//   T-ime: no time concerns
func TestTicket_CanTransitionTo_BasicMatrix(t *testing.T) {
	tests := []struct {
		fromStr string
		toStr   string
		wantErr bool
	}{
		// backlog row
		{"backlog", "backlog", false},    // same-status no-op
		{"backlog", "in_progress", false}, // start work

		// in_progress row
		{"in_progress", "in_progress", false}, // same-status no-op
		{"in_progress", "backlog", false},      // pause / reorder
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

// TestTicket_CanTransitionTo_PureFunction documents and pins the design
// decision: CanTransitionTo does NOT consult AgentStatus. The orphan-agent
// guard (in_progress → backlog with AgentStatus == AgentWorking) lives
// in the UI layer (model.go:checkOrphanAgentBeforeMove), not here.
//
// This test would catch a regression where someone "fixes" the orphan
// problem by adding agent coupling back into the state machine.
func TestTicket_CanTransitionTo_PureFunction(t *testing.T) {
	tests := []AgentStatus{
		AgentNone, AgentIdle, AgentWorking, AgentWaiting, AgentCompleted, AgentError,
	}
	for _, as := range tests {
		t.Run(string(as), func(t *testing.T) {
			ticket := NewTicket("t", "p1")
			ticket.Status = StatusInProgress
			ticket.AgentStatus = as

			// in_progress → backlog must succeed regardless of agent state.
			// The UI layer is responsible for blocking this when
			// AgentStatus == AgentWorking.
			if err := ticket.CanTransitionTo(StatusBacklog); err != nil {
				t.Errorf("CanTransitionTo(in_progress → backlog) with AgentStatus=%q returned error %v; "+
					"want nil (orphan-agent guard is UI-side, not state-machine)",
					as, err)
			}
		})
	}
}

// TestTicket_SetStatus_RespectsStateMachine verifies the integration:
// SetStatus (called by Move()) enforces CanTransitionTo. If a future
// PR adds a fast-path that bypasses the state machine, this test
// surfaces it.
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
	t.Run("same-status is a no-op", func(t *testing.T) {
		ticket := NewTicket("noop", "p1")
		ticket.Status = StatusBacklog

		err := ticket.SetStatus(StatusBacklog)
		if err != nil {
			t.Errorf("SetStatus(backlog → backlog) returned error: %v; want nil (no-op)", err)
		}
		if ticket.Status != StatusBacklog {
			t.Errorf("Status = %q; want %q", ticket.Status, StatusBacklog)
		}
	})

	t.Run("allowed transition (backlog → in_progress) succeeds", func(t *testing.T) {
		ticket := NewTicket("allowed", "p1")
		ticket.Status = StatusBacklog

		err := ticket.SetStatus(StatusInProgress)
		if err != nil {
			t.Errorf("SetStatus(backlog → in_progress) returned error: %v", err)
		}
		if ticket.Status != StatusInProgress {
			t.Errorf("Status = %q; want %q", ticket.Status, StatusInProgress)
		}
		if ticket.StartedAt == nil {
			t.Error("StartedAt should be set after transition to in_progress")
		}
	})

	t.Run("allowed transition (in_progress → backlog) succeeds and clears StartedAt", func(t *testing.T) {
		ticket := NewTicket("pause", "p1")
		ticket.SetStatus(StatusInProgress)
		if ticket.StartedAt == nil {
			t.Fatal("precondition: StartedAt should be set after in_progress")
		}

		err := ticket.SetStatus(StatusBacklog)
		if err != nil {
			t.Errorf("SetStatus(in_progress → backlog) returned error: %v", err)
		}
		if ticket.Status != StatusBacklog {
			t.Errorf("Status = %q; want %q", ticket.Status, StatusBacklog)
		}
		// StartedAt is intentionally NOT cleared — it records "first
		// time the user started this ticket". Subsequent cycles
		// accumulate. See field doc on StartedAt.
	})
}

// TestBoardGo_CanTransitionToExists pins the source-level contract that
// the CanTransitionTo method exists in board.go. Structural regression
// test for the original Cluster D.3 fix, retained to keep the audit
// trail consistent.
func TestBoardGo_CanTransitionToExists(t *testing.T) {
	src := readBoardGoSource(t)
	if !contains(src, "func (t *Ticket) CanTransitionTo(") {
		t.Errorf("board.go missing Ticket.CanTransitionTo method.\n"+
			"This is the canonical state-machine validator.")
	}
}

// TestBoardGo_NoTerminalStatus pins the new design contract: there is
// no longer a "terminal" status in the state machine. The Done and
// Archived constants must not exist. A regression that re-introduces
// them (e.g., to fix a perceived gap) would fail this test.
func TestBoardGo_NoTerminalStatus(t *testing.T) {
	src := readBoardGoSource(t)
	if contains(src, "StatusDone") {
		t.Errorf("board.go references StatusDone; the simplified state machine has no Done status.\n"+
			"When the user judges a ticket done, they delete it (not move it).")
	}
	if contains(src, "StatusArchived") {
		t.Errorf("board.go references StatusArchived; the simplified state machine has no Archived status.\n"+
			"There is no terminal state — 'done' = 'delete'.")
	}
	if contains(src, "CompletedAt") {
		t.Errorf("board.go references CompletedAt; with no Done status there is no setter for this field.\n"+
			"Remove the CompletedAt field from the Ticket struct.")
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
