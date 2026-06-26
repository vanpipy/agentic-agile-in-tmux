package awp

import "testing"

// TestRegisterSIGUSR1StackDumper_Present verifies that the
// registerSIGUSR1StackDumper function exists and is callable.
// The actual signal behavior is verified manually because
// signals are process-wide and can't be safely unhooked in tests.
//
// Companion check: see cmd/awp/root.go which only calls
// registerSIGUSR1StackDumper() when observability.IsDebug() is
// true, so production binaries don't accept SIGUSR1.
func TestRegisterSIGUSR1StackDumper_Present(t *testing.T) {
	// Just calling the function should not panic. It registers
	// a signal handler and spawns a goroutine; we rely on the
	// process exiting at test end to clean up.
	registerSIGUSR1StackDumper()
}
