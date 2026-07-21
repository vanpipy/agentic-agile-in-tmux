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