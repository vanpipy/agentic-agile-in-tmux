// session_constructor_test.go — TDD test pinning NewSessionStore's error contract.
//
// Finding CASTRATION-1 from post-P3P4 audit: NewSessionStore silently
// swallowed `os.UserHomeDir()` failure. If home detection fails, agentDir
// becomes ".pi/agent" (relative to CWD), and subsequent writes could
// clobber the user's working directory.
//
// Fix: NewSessionStore returns (*SessionStore, error). Callers that
// supply a non-empty agentDir get it as-is. Callers that supply ""
// get a path under $HOME — error if home detection fails.
package pi

import (
	"strings"
	"testing"
)

// TestNewSessionStore_EmptyPathReturnsError pins the contract that
// empty agentDir + failed home detection returns an error.
//
// CORRECT-7 self-check:
//   C-onformance: error must be non-nil
//   O-rdering: N/A
//   R-ange: 1 case (HOME unset / cannot be detected)
//   R-eference: HOME env var
//   E-xistence: error mentions "home directory"
//   C-ardinality: 1 case
//   T-ime: no time concerns
func TestNewSessionStore_EmptyPathReturnsError(t *testing.T) {
	// Point HOME at an empty path so os.UserHomeDir fails.
	// (On Linux, HOME="" causes os.UserHomeDir to return "" without error
	// — which means filepath.Join("", ".pi", "agent") = ".pi/agent".
	// We need a more aggressive failure mode: set HOME to a nonexistent
	// path AND mock the lookup. Since we can't mock os.UserHomeDir easily,
	// we test the explicit path: agentDir="" with HOME pointing to
	// something os.Stat fails on.)
	t.Setenv("HOME", "")
	// On macOS, UserHomeDir falls back to /etc/passwd lookup. On Linux
	// it just reads $HOME/USER. Setting HOME="" makes the latter return
	// empty string; combined with no $USER, we get empty home.
	t.Setenv("USER", "")

	store, err := NewSessionStore("")
	if err == nil {
		t.Fatalf("NewSessionStore(\"\") with empty HOME/USER returned nil error; "+
			"agentDir would become \".pi/agent\" (relative to CWD), risking clobber. "+
			"store=%+v", store)
	}
	if !strings.Contains(err.Error(), "home") {
		t.Errorf("error = %q; want it to mention 'home' for debuggability", err.Error())
	}
}

// TestNewSessionStore_ExplicitPathAlwaysSucceeds pins the contract:
// callers that supply an explicit agentDir don't depend on home detection.
func TestNewSessionStore_ExplicitPathAlwaysSucceeds(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewSessionStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSessionStore(%q) error: %v", tmpDir, err)
	}
	if store == nil {
		t.Fatal("store is nil")
	}
	if store.agentDir != tmpDir {
		t.Errorf("agentDir = %q; want %q", store.agentDir, tmpDir)
	}
}
