// new_model_orphan_test.go — TDD tests for Cluster D.1 fix.
//
// Cluster D.1 (Major from 2026-06-27 audit): NewModel's startup reset
// iterated globalStore.All() and unconditionally reset every ticket's
// AgentStatus to AgentNone, then Save() to disk. This means if pi was
// still running in the background (orphaned PTY from a crashed awp), the
// next awp launch would silently downgrade the ticket's on-disk state
// to "no agent running" — even though pi was actually still alive.
//
// Fix (Phase 1): stop the disk write on startup. The in-memory reset
// still happens (so the UI doesn't show stale "working" badges), but
// disk state is preserved. Phase 2 (deferred) will add real orphan
// detection via PID lookup.
//
// Tests pin the contract: NewModel resets in-memory AgentStatus but does
// NOT overwrite the on-disk AgentStatus.
package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pi/awp/internal/board"
	"github.com/pi/awp/internal/config"
	"github.com/pi/awp/internal/project"
)

// TestNewModel_DoesNotOverwriteDiskAgentStatus pins the D.1 contract:
//   - In-memory: AgentStatus is reset to AgentNone (UX: no stale "working"
//     badges after restart).
//   - On-disk: AgentStatus is preserved (data integrity: orphan-PTY tickets
//     retain their "working" status so a future PR can detect them).
//
// CORRECT-7 self-check:
//   C-onformance: literal value comparisons (AgentNone vs AgentWorking)
//   O-rdering: N/A (single ticket)
//   R-ange: 1 ticket
//   R-eference: filesystem only
//   E-xistence: disk file must exist before and after NewModel
//   C-ardinality: 1 ticket tested (the simplify-pass pattern)
//   T-ime: no time concerns
func TestNewModel_DoesNotOverwriteDiskAgentStatus(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("AWP_CONFIG_DIR", cfgDir)

	// Phase 1: create a registry with one project, save a ticket with
	// AgentStatus=AgentWorking to disk (simulating a previous awp session
	// that crashed mid-spawn, leaving the ticket on disk with a working agent).
	reg, err := project.LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	proj := project.NewProject("test-repo", cfgDir)
	if err := reg.Add(proj); err != nil {
		t.Fatalf("reg.Add: %v", err)
	}

	ticket := board.NewTicket("orphan-ticket", proj.ID)
	ticket.AgentStatus = board.AgentWorking // <-- simulate orphan-PTY state
	ticket.PiState = board.PiStateStreaming

	store, err := project.LoadTicketStore(proj)
	if err != nil {
		t.Fatalf("LoadTicketStore: %v", err)
	}
	store.Add(ticket)
	if err := store.Save(); err != nil {
		t.Fatalf("ticket store Save: %v", err)
	}

	// Sanity check: disk has AgentWorking.
	diskPath := filepath.Join(cfgDir, "tickets", proj.ID+".json")
	diskData, err := os.ReadFile(diskPath)
	if err != nil {
		t.Fatalf("read disk ticket: %v", err)
	}
	if !contains(diskData, "working") {
		t.Fatalf("precondition: disk ticket should contain 'working' status; got: %s", diskData)
	}

	// Phase 2: build a NewModel. This is where the disk-overwrite bug lived.
	gts, err := project.LoadGlobalTicketStore(reg)
	if err != nil {
		t.Fatalf("LoadGlobalTicketStore: %v", err)
	}
	_ = NewModel(testConfig(), gts, reg, "", nil)

	// Phase 3: re-read disk and verify AgentStatus is preserved.
	diskDataAfter, err := os.ReadFile(diskPath)
	if err != nil {
		t.Fatalf("read disk ticket after NewModel: %v", err)
	}
	if !contains(diskDataAfter, "working") {
		t.Errorf("D.1 bug: NewModel overwrote disk AgentStatus to 'none'.\n"+
			"Before fix: on-disk ticket state was clobbered on every restart.\n"+
			"After fix: disk state is preserved; only in-memory AgentStatus is reset.\n"+
			"Disk now: %s", diskDataAfter)
	}

	// Also verify the in-memory ticket was reset (UX contract).
	// (The ticket is loaded into gts.allTickets from disk; after NewModel,
	// the same memory reference should now show AgentNone.)
	memTicket, _ := gts.Get(ticket.ID)
	if memTicket == nil {
		t.Fatal("ticket not in global store after NewModel")
	}
	if memTicket.AgentStatus != board.AgentNone {
		t.Errorf("in-memory AgentStatus = %q; want %q (UX: stale 'working' badges should clear on restart)",
			memTicket.AgentStatus, board.AgentNone)
	}
}

// contains is a tiny helper because strings.Contains requires a string,
// not a byte slice.
func contains(data []byte, substr string) bool {
	for i := 0; i+len(substr) <= len(data); i++ {
		if string(data[i:i+len(substr)]) == substr {
			return true
		}
	}
	return false
}
// testConfig returns a minimal Config for testing (no file I/O).
func testConfig() *config.Config { return config.DefaultConfig() }
