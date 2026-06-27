// view_purity_test.go — TDD test for AGENTS.md §5.3 TEA contract: View() is pure.
//
// The current renderTicketForm has logic that mutates m.formScrollOffset
// during render. AGENTS.md §5.3 says: "View() is a pure render of model
// state, no side effects."
//
// This test pins the contract: rendering the ticket form twice with the
// same model state must produce identical output. If View() mutates state,
// the second render would differ (because the mutated state affects the
// second render's output).
//
// Fix: extract scroll-offset adjustment to a pure helper function.
// View() calls the helper, uses the result for rendering, but does NOT
// mutate m.formScrollOffset. Mouse-wheel scroll (via Update) is the only
// legitimate writer of m.formScrollOffset.
package ui

import (
	"testing"

	"github.com/pi/awp/internal/config"
	"github.com/pi/awp/internal/project"
)

// TestRenderTicketForm_IsPure verifies that View() does not mutate model
// state. Calling View() twice with the same model produces identical output.
//
// We force the scroll-adjustment path to fire by pre-populating
// formFieldLines with an active field that is "off-screen" relative
// to the current scroll offset. This makes the View()-time mutation
// observable in test runs.
//
// CORRECT-7 self-check:
//   C-onformance: literal string equality between two consecutive renders
//   O-rdering: N/A (renders are sequential)
//   R-ange: 1 model state, 2 renders
//   R-eference: no external deps (no PTY started)
//   E-xistence: not relevant
//   C-ardinality: 1 case (two renders compared)
//   T-ime: no time concerns
func TestRenderTicketForm_IsPure(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("AWP_CONFIG_DIR", cfgDir)
	cfg := config.DefaultConfig()
	reg, err := project.LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	proj := project.NewProject("pure-test", cfgDir)
	if err := reg.Add(proj); err != nil {
		t.Fatalf("reg.Add: %v", err)
	}
	store := project.NewGlobalTicketStore(reg)

	m := NewModel(cfg, store, reg, "", nil)
	m.width = 100
	m.height = 5 // very small — forces scroll for any non-trivial form
	m.mode = ModeCreateTicket
	m.ticketFormField = formFieldTitle
	m.formScrollOffset = 999 // start way off-screen so scroll-adjustment fires

	// Take a snapshot of the model state BEFORE the first render.
	beforeFormScrollOffset := m.formScrollOffset

	// First render — pure; should not mutate.
	first := m.renderTicketForm()

	// Verify the model state is unchanged after first render.
	if m.formScrollOffset != beforeFormScrollOffset {
		t.Errorf("View() mutated m.formScrollOffset: %d → %d.\n"+
			"AGENTS.md §5.3: View() must be pure. Move scroll logic to Update().",
			beforeFormScrollOffset, m.formScrollOffset)
	}

	// Second render — must produce identical output (state didn't change).
	second := m.renderTicketForm()

	if first != second {
		t.Errorf("Two consecutive View() calls produced different output.\n"+
			"This means View() mutated model state during the first render.\n"+
			"First render (first 100 chars): %q\n"+
			"Second render (first 100 chars): %q",
			truncate(first, 100), truncate(second, 100))
	}
}

// truncate is defined in util.go (package-level function).
// TestComputeFormScrollOffset_KeepsActiveFieldVisible verifies the pure
// helper's contract: given a viewport size and an active field, the
// returned offset keeps the active field in view.
//
// CORRECT-7 self-check:
//   C-onformance: literal offset values
//   O-rdering: N/A
//   R-ange: 5+ cases (top/bottom/in-view/no-scroll/unknown)
//   R-eference: no external deps
//   E-xistence: missing fields case
//   C-ardinality: 1 helper, multiple scenarios
//   T-ime: no time concerns
func TestComputeFormScrollOffset_KeepsActiveFieldVisible(t *testing.T) {
	fieldStarts := map[int]int{5: 50}
	fieldEnds := map[int]int{5: 60}
	tests := []struct {
		name           string
		activeField    int
		currentOffset  int
		viewportHeight int
		totalLines     int
		wantMin        int
		wantMax        int
	}{
		{"field below viewport scrolls to bottom", 5, 0, 20, 100, 41, 50},
		{"field above viewport scrolls to top", 5, 80, 20, 100, 50, 50},
		{"field in view keeps current offset", 5, 45, 20, 100, 45, 45},
		{"no scroll needed (content fits viewport)", 5, 0, 100, 50, 0, 0},
		{"unknown field clamps current offset", 99, 200, 20, 100, 80, 80},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeFormScrollOffset(
				tt.activeField, tt.currentOffset,
				fieldStarts, fieldEnds,
				tt.viewportHeight, tt.totalLines,
			)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("got %d; want in [%d, %d]", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}
