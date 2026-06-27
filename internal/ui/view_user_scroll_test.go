// view_user_scroll_test.go — TDD test pinning user-scroll preservation
// across View() renders.
//
// Finding DEATH-3 from post-P3P4 audit: After View() was made "pure"
// by extracting scroll logic to computeFormScrollOffset, the user's
// manual wheel scroll was silently overridden on every render.
//
// Fix: remove the auto-scroll from View() entirely. View() now reads
// m.formScrollOffset directly via clampScrollOffset. No auto-scroll
// on field navigation (Tab/shift+tab) — users must scroll manually
// on small terminals. Trade-off documented in the code.
//
// This test pins the new contract.
package ui

import (
	"testing"

	"github.com/pi/awp/internal/config"
	"github.com/pi/awp/internal/project"
)

// TestView_DoesNotOverrideUserWheelScroll pins the contract that View()
// never modifies m.formScrollOffset. If the user wheel-scrolls to offset
// 7, the next View() render must still show offset 7.
//
// CORRECT-7 self-check:
//   C-onformance: m.formScrollOffset unchanged after View() call
//   O-rdering: N/A
//   R-ange: 1 case
//   R-eference: no external deps
//   E-xistence: N/A
//   C-ardinality: 1 case
//   T-ime: no time concerns
func TestView_DoesNotOverrideUserWheelScroll(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("AWP_CONFIG_DIR", cfgDir)
	cfg := config.DefaultConfig()
	reg, _ := project.LoadRegistry()
	store := project.NewGlobalTicketStore(reg)

	m := NewModel(cfg, store, reg, "", nil)
	m.width = 100
	m.height = 5
	m.mode = ModeCreateTicket
	m.ticketFormField = formFieldTitle
	m.formScrollOffset = 7 // simulate user wheel scroll

	// Render. The auto-scroll logic must NOT override m.formScrollOffset.
	_ = m.renderTicketForm()

	if m.formScrollOffset != 7 {
		t.Errorf("View() mutated m.formScrollOffset: 7 → %d.\n"+
			"DEATH-3: View() must be PURE — auto-scroll belongs in\n"+
			"field-navigation handlers (nextFormField, prevFormField), not in render.",
			m.formScrollOffset)
	}
}
