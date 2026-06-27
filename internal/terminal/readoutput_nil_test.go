// readoutput_nil_test.go — TDD test for readOutputUnlocked's nil-check.
//
// Finding DEATH-2 from post-P3P4 audit: readOutputUnlocked was thought to
// be vulnerable to a nil-pty panic if called between startSetup and the
// closure invocation. The current code already checks `p.pty == nil`
// and returns nil — this test pins that contract.
//
// CORRECT-7 self-check:
//   C-onformance: returned cmd must be nil when p.pty is nil
//   O-rdering: N/A
//   R-ange: 1 case
//   R-eference: no external deps
//   E-xistence: N/A
//   C-ardinality: 1 case
//   T-ime: no time concerns
package terminal

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestReadOutputUnlocked_NilPtyReturnsNilCmd pins the contract that
// readOutputUnlocked does not panic when p.pty is nil. The function is
// only safe to call under p.mu; we simulate this by constructing a
// Pane without ever calling Start.
func TestReadOutputUnlocked_NilPtyReturnsNilCmd(t *testing.T) {
	p := New("test", 80, 24, 100)

	p.mu.Lock()
	defer p.mu.Unlock()

	cmd := p.readOutputUnlocked()
	if cmd != nil {
		// Run the cmd; it should not panic.
		var msg tea.Msg = cmd()
		_ = msg
		t.Errorf("readOutputUnlocked with nil p.pty returned non-nil cmd; expected nil to prevent nil-ptr Read")
	}
}
