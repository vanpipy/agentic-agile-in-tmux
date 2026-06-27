// start_render_only_test.go — TDD test pinning the Start() panic contract.
//
// After P1 hardening (delete the Start() footgun): Start() is render-only
// only. Calling it with a non-empty command must panic so that
// contributors who reach for the deprecated path get a clear error
// pointing them to StartCmd().
package terminal

import (
	"strings"
	"testing"
)

// TestStart_PTYModePanics pins the contract: Start() panics if called
// with a non-empty command. This is the runtime guard that prevents the
// data race between Start's IIFE and installCallbacks' consumer goroutines.
//
// CORRECT-7 self-check:
//   C-onformance: panic message must mention "StartCmd"
//   O-rdering: N/A
//   R-ange: 1 case
//   R-eference: no external deps
//   E-xistence: N/A
//   C-ardinality: 1 case
//   T-ime: no time concerns
func TestStart_PTYModePanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("Start(\"echo\", ...) did not panic; expected runtime guard for PTY mode")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic value type = %T; want string", r)
		}
		if !strings.Contains(msg, "StartCmd") {
			t.Errorf("panic message = %q; want it to mention 'StartCmd' so contributors find the migration target", msg)
		}
	}()

	p := New("test", 80, 24, 100)
	_ = p.Start("echo", "hello")
}

// TestStart_RenderOnlyDoesNotPanic pins the contract: render-only mode
// (command=="") still works as before. This guards against a regression
// where the panic guard is too broad and breaks the render-only tests.
func TestStart_RenderOnlyDoesNotPanic(t *testing.T) {
	p := New("test", 80, 24, 100)
	cmd := p.Start("", nil...)
	if cmd == nil {
		// nil cmd is expected for render-only (no PTY to read from)
		_ = cmd
	}
	// If we got here without panicking, the render-only path is safe.
}