// cycle_model_test.go — TDD tests for the cycle slot in Model.
//
// Per SYSTEM_DESIGN.md §18.9, the cycle lives at the parent Model with
// three channels (Events, Ext, Done). cyclepane is a viewer (P6.2).
// These tests cover the parent-model surface only.
//
// CORRECT-7 self-check on this test file:
//   C-onformance: literal mode + field equality
//   O-rdering:    N/A (no map iteration in tests)
//   R-ange:       stem value (empty vs non-empty); cycle nil/non-nil
//   R-eference:   no external deps beyond wiking.New (uses t.TempDir)
//   E-xistence:   precondition checks before each action
//   C-ardinality: 1 cycle per process; not tested for concurrency
//   T-ime:        1s timeouts on channel reads; no wall-clock reliance
package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pi/awp/internal/config"
	"github.com/pi/awp/internal/project"
	"github.com/pi/awp/internal/wiking"
)

// newModelForCycleTest is a minimal Model fixture for cycle tests.
// Mirrors the pattern from spawn_twice_test.go: tmp config dir, one
// project, empty global store.
func newModelForCycleTest(t *testing.T) *Model {
	t.Helper()
	tmpDir := t.TempDir()
	t.Setenv("AWP_CONFIG_DIR", tmpDir)

	cfg := config.DefaultConfig()
	registry := &project.ProjectRegistry{
		Projects: map[string]*project.Project{},
	}
	p := project.NewProject("test", tmpDir)
	registry.Projects[p.ID] = p
	gts, err := project.LoadGlobalTicketStore(registry)
	if err != nil {
		t.Fatalf("LoadGlobalTicketStore: %v", err)
	}

	return NewModel(cfg, gts, registry, "", nil)
}

// TestCycle_CHotkeyStartsCycle — pressing 'c' in ModeNormal creates a
// *wiking.Cycle, stores it in m.activeCycle, wires the three channels
// (Events/Ext/Done), transitions to ModeCycle, and populates
// cycleStem for the chip.
//
// Per §18.9: the cycle slot is process-lifetime; this is the entry point.
func TestCycle_CHotkeyStartsCycle(t *testing.T) {
	m := newModelForCycleTest(t)

	// Preconditions
	if m.activeCycle != nil {
		t.Fatalf("precondition: activeCycle = %v, want nil", m.activeCycle)
	}
	if m.mode != ModeNormal {
		t.Fatalf("precondition: mode = %v, want ModeNormal", m.mode)
	}

	// Press 'c'
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})

	// Postconditions
	if m.activeCycle == nil {
		t.Fatal("activeCycle should be non-nil after pressing 'c'")
	}
	if m.cycleEvents == nil {
		t.Error("cycleEvents channel should be wired to activeCycle.Events")
	}
	if m.cycleExt == nil {
		t.Error("cycleExt channel should be wired to activeCycle.Ext")
	}
	if m.cycleDone == nil {
		t.Error("cycleDone channel should be wired to activeCycle.Done")
	}
	if m.mode != ModeCycle {
		t.Errorf("mode = %v, want ModeCycle", m.mode)
	}
	if m.cycleStem == "" {
		t.Error("cycleStem should be populated for the header chip")
	}

	// Cleanup: cancel the cycle so the goroutine exits cleanly.
	// Without this, the test process leaks the goroutine until exit.
	t.Cleanup(func() {
		if m.activeCycle != nil {
			select {
			case m.cycleExt <- wiking.ExtMsg{Kind: wiking.ExtCancel}:
			case <-time.After(100 * time.Millisecond):
			}
		}
	})
}

// TestCycle_EscLeavesModeCycleWithoutKillingCycle — pressing esc
// from ModeCycle returns to ModeNormal (the kanban) but keeps the
// cycle running. This is the load-bearing 18.9 invariant: "the
// cycle is not bound to mode lifetime" — a 30-min cycle cannot
// require 30 min of ModeCycle focus.
//
// Pre-conditions: a cycle has been started (fields populated).
// Action: send esc.
// Post-conditions: mode == ModeNormal, activeCycle still non-nil,
// channels still wired, cycleStem preserved.
func TestCycle_EscLeavesModeCycleWithoutKillingCycle(t *testing.T) {
	m := newModelForCycleTest(t)
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if m.mode != ModeCycle {
		t.Fatalf("precondition: mode = %v, want ModeCycle", m.mode)
	}
	originalCycle := m.activeCycle
	originalStem := m.cycleStem

	// Press esc.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if m.mode != ModeNormal {
		t.Errorf("mode after esc = %v, want ModeNormal", m.mode)
	}
	if m.activeCycle == nil {
		t.Error("activeCycle should remain non-nil after esc (cycle keeps running)")
	}
	if m.activeCycle != originalCycle {
		t.Error("activeCycle pointer changed — cycle was killed and replaced")
	}
	if m.cycleStem != originalStem {
		t.Errorf("cycleStem = %q, want preserved %q", m.cycleStem, originalStem)
	}
	if m.cycleEvents == nil {
		t.Error("cycleEvents channel should remain wired after esc")
	}
	if m.cycleDone == nil {
		t.Error("cycleDone channel should remain wired after esc")
	}

	// Cleanup
	t.Cleanup(func() {
		if m.activeCycle != nil {
			select {
			case m.cycleExt <- wiking.ExtMsg{Kind: wiking.ExtCancel}:
			case <-time.After(100 * time.Millisecond):
			}
		}
	})
}

// TestCycle_ViewShowsCycleChipWhenActive — View() renders the cycle
// chip in the header when activeCycle is set. The chip shows the
// article stem (T2 Q4) so the user can tell which cycle is running
// when not focused on the cyclepane.
//
// Implementation: render a known-width header, scan for the cycle
// stem text. The exact chip format is not pinned here — only that
// the stem appears in the header.
func TestCycle_ViewShowsCycleChipWhenActive(t *testing.T) {
	m := newModelForCycleTest(t)
	m.width = 120
	m.height = 40
	m.refreshColumnTickets()

	// Baseline: no cycle, view should not contain a "cycle" hint.
	viewBaseline := m.View()
	if strings.Contains(strings.ToLower(viewBaseline), "cycle:") {
		t.Fatalf("baseline view should not contain a 'cycle:' chip; got: %.300q", viewBaseline)
	}

	// Start a cycle with a known stem.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if m.activeCycle == nil {
		t.Fatal("precondition: cycle should have started")
	}
	if m.cycleStem == "" {
		t.Fatal("precondition: cycleStem should be populated")
	}

	// Header should now mention the cycle.
	viewActive := m.View()
	if !strings.Contains(viewActive, m.cycleStem) {
		t.Errorf("view with active cycle should contain stem %q in header; got header slice: %.300q",
			m.cycleStem, firstNLines(viewActive, 3))
	}
	if !strings.Contains(strings.ToLower(viewActive), "cycle") {
		t.Errorf("view with active cycle should contain the word 'cycle' in the chip; got header slice: %.300q",
			firstNLines(viewActive, 3))
	}

	// Cleanup
	t.Cleanup(func() {
		if m.activeCycle != nil {
			select {
			case m.cycleExt <- wiking.ExtMsg{Kind: wiking.ExtCancel}:
			case <-time.After(100 * time.Millisecond):
			}
		}
	})
}

// TestCycle_XSendsExtCancel — pressing 'x' in any mode with an
// active cycle sends wiking.ExtCancel via m.cycleExt (non-blocking).
// Per 18.12, 'x' works in any active cycle, not just ModeCycle, so
// the user can cancel from the kanban without focusing the
// cyclepane first.
//
// Implementation lives in handleKey's x branch (not
// handleNormalMode) so it's mode-agnostic. The cycle's Run
// goroutine reads cycleExt on each tick; cancel causes it to
// exit, the defer writes the error to cycleDone, and
// handleCycleDoneMsg clears the slot.
func TestCycle_XSendsExtCancel(t *testing.T) {
	m := newModelForCycleTest(t)
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if m.activeCycle == nil {
		t.Fatal("precondition: cycle should have started")
	}

	// Drain any other Ext traffic (none expected on a fresh
	// cycle, but be defensive — channels cap 1 so a stale value
	// would block this test's send).
	select {
	case <-m.activeCycle.Ext:
	default:
	}

	// Press 'x' from ModeCycle (the active mode after 'c').
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})

	// Read the cancel from cycleExt. Timeout guards against the
	// 'send fell into a closed channel' or 'send was dropped'
	// regressions; 1s is generous for a buffered cap-1 channel.
	select {
	case msg := <-m.activeCycle.Ext:
		if msg.Kind != wiking.ExtCancel {
			t.Errorf("ExtMsg.Kind = %v, want ExtCancel", msg.Kind)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("no ExtCancel received on cycleExt within 1s")
	}

	// Cleanup: the cancel should make the cycle exit; flush the
	// goroutine via t.Cleanup with another ExtCancel in case the
	// first was already consumed.
	t.Cleanup(func() {
		if m.activeCycle != nil {
			select {
			case m.cycleExt <- wiking.ExtMsg{Kind: wiking.ExtCancel}:
			case <-time.After(100 * time.Millisecond):
			}
		}
	})
}

// TestCycle_DoneCleansUpActiveCycle — handleCycleDoneMsg clears
// the cycle slot when invoked. This is the cleanup half of the
// cycle lifecycle: cyc.Run returns → defer writes to cycleDone →
// pollCycleDoneAsync (P6.1c) emits cycleDoneMsg → Update dispatches
// to handleCycleDoneMsg → slot cleared.
//
// This test invokes handleCycleDoneMsg directly (without going
// through the polling path) so the cleanup logic is testable in
// isolation. The polling integration lands in P6.1c.
func TestCycle_DoneCleansUpActiveCycle(t *testing.T) {
	m := newModelForCycleTest(t)
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if m.activeCycle == nil {
		t.Fatal("precondition: cycle should have started")
	}
	originalCycle := m.activeCycle
	originalStem := m.cycleStem

	// Simulate the cycle's defer firing: send an error to the
	// underlying Done channel (m.cycleDone is the receive-only
	// view per 18.11; the writable side is on the cycle itself).
	// In production, the cycleDoneMsg delivery to Update comes
	// from pollCycleDoneAsync, not from this direct send.
	m.activeCycle.Done <- nil

	// Dispatch the cleanup as Update would.
	_, _ = m.Update(cycleDoneMsg{stem: originalStem, err: nil})

	if m.activeCycle != nil {
		t.Errorf("activeCycle = %v, want nil after done cleanup", m.activeCycle)
	}
	if m.activeCycle == originalCycle {
		t.Error("activeCycle pointer was not cleared (should be nil, not the old value)")
	}
	if m.cycleEvents != nil {
		t.Error("cycleEvents should be nil after done cleanup")
	}
	if m.cycleExt != nil {
		t.Error("cycleExt should be nil after done cleanup")
	}
	if m.cycleDone != nil {
		t.Error("cycleDone should be nil after done cleanup")
	}
	if m.cycleStem != "" {
		t.Errorf("cycleStem = %q, want empty", m.cycleStem)
	}
}
// for compact error messages so the test failure shows the header
// row (where the chip lives) without dumping the full kanban view.
func firstNLines(s string, n int) string {
	out := []string{}
	for i, line := range strings.Split(s, "\n") {
		if i >= n {
			break
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}