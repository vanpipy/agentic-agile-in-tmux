// can_transition_purity_test.go — TDD test pinning the pure state-machine
// contract for CanTransitionTo.
//
// Finding FOOT-3 from post-P3P4 audit: CanTransitionTo's
// "in_progress → backlog blocked if AgentWorking" rule couples board
// (state machine) with the semantic of "agent working". The board
// package shouldn't know about agent semantics.
//
// Fix: CanTransitionTo is now PURE — it only knows about TicketStatus
// transitions. The agent-coupling check moves to the caller (UI layer).
//
// This test pins the new contract: CanTransitionTo must NOT inspect
// AgentStatus. A test ticket in (in_progress, AgentWorking) state can
// transition to backlog at the state-machine level; the caller decides
// whether to allow it.
package board

import "testing"

// TestCanTransitionTo_PureDoesNotInspectAgentStatus pins the contract
// that CanTransitionTo has no agent coupling. AgentStatus is a separate
// concern that callers (UI layer) handle.
//
// CORRECT-7 self-check:
//   C-onformance: transition allowed regardless of AgentStatus
//   O-rdering: N/A
//   R-ange: 3 cases (AgentNone, AgentWorking, AgentError)
//   R-eference: no external deps
//   E-xistence: AgentStatus field is irrelevant
//   C-ardinality: 3 cases
//   T-ime: no time concerns
func TestCanTransitionTo_PureDoesNotInspectAgentStatus(t *testing.T) {
	for _, status := range []AgentStatus{AgentNone, AgentWorking, AgentError, AgentIdle} {
		t.Run(string(status), func(t *testing.T) {
			ticket := NewTicket("t", "p1")
			ticket.Status = StatusInProgress
			ticket.AgentStatus = status

			// Pure state machine: in_progress → backlog is allowed.
			// Agent-coupling check belongs in the caller.
			if err := ticket.CanTransitionTo(StatusBacklog); err != nil {
				t.Errorf("CanTransitionTo(in_progress → backlog, AgentStatus=%s) = %v; "+
					"want nil (pure state-machine check).\n"+
					"AgentStatus coupling belongs in the caller (UI layer).",
					status, err)
			}
		})
	}
}
