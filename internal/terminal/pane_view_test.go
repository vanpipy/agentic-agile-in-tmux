package terminal

import (
	"strings"
	"testing"
)

// TestPane_View_NoRawErrorWhenNotReady verifies Pane.View() doesn't
// leak the raw "Terminal not initialized" string to the UI layer.
//
// User feedback: '第一次进入显示 "Ternimal not initialized"'.
//
// The Model's renderAgentView now checks pane.IsReady() and shows
// a loading state. But as defense-in-depth, Pane.View() should
// also not return a user-facing error string when vt is nil.
func TestPane_View_NoRawErrorWhenNotReady(t *testing.T) {
	pane := New("test", 120, 40, 10000)
	pane.SetWorkdir(t.TempDir())
	// Don't call pane.Start — vt is nil

	view := pane.View()
	if strings.Contains(view, "Terminal not initialized") {
		t.Errorf("Pane.View() leaks 'Terminal not initialized' when vt is nil: %q", view)
	}
}

// TestPane_IsReady verifies IsReady returns correct state.
func TestPane_IsReady(t *testing.T) {
	pane := New("test", 120, 40, 10000)
	pane.SetWorkdir(t.TempDir())
	
	if pane.IsReady() {
		t.Error("IsReady() should be false before Start")
	}
	
	cmd := pane.Start("", nil...)
	if cmd != nil { cmd() }
	
	if !pane.IsReady() {
		t.Error("IsReady() should be true after Start with empty command (render-only)")
	}
}
