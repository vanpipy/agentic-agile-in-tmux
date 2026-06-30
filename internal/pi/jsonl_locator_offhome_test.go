// jsonl_locator_offhome_test.go — TDD test for the isUnderHome
// defense in latestJSONLInDir.
//
// Ticket task/awp audit follow-up (CASTRATION finding): the
// existing `buildIndex` (session.go:407) refuses to walk a session
// dir that resolves outside the user's HOME — a defense-in-depth
// guard against a maliciously-placed symlink in ~/.pi/agent/sessions/.
// The new `latestJSONLInDir` should apply the same guard for
// consistency. If the user replaces their sessions dir with a
// symlink to /tmp/ or some other writable dir, we should refuse
// to walk it (just like buildIndex).
//
// This test exercises that guard via the public LatestSessionJSONL
// API (HOME is overridden via t.Setenv so we don't touch the real
// $HOME in tests).
//
// Pre-fix expected: FAIL — LatestSessionJSONL returns a path inside
// the symlink target because latestJSONLInDir doesn't check.
//
// Post-fix expected: PASS — LatestSessionJSONL returns ("", nil)
// because the resolved path is off-home, matching buildIndex's
// behavior.

package pi

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLatestSessionJSONL_OffHomeSymlinkRefusedToWalk pins the
// CASTRATION-2 follow-up contract: if the resolved session dir
// escapes HOME, LatestSessionJSONL must return ("", nil), not a
// path inside the symlink target.
func TestLatestSessionJSONL_OffHomeSymlinkRefusedToWalk(t *testing.T) {
	// Set up a fake HOME so os.UserHomeDir() returns a controlled
	// path. We then place a symlink inside HOME that points OUTSIDE
	// HOME, mimicking a malicious ~/.pi/agent/sessions/ replacement.
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	cwd := "/tmp/proj-x"
	encoded := encodeCwdKey(cwd)
	piDir := filepath.Join(fakeHome, ".pi", "agent", "sessions", encoded)

	// Outside fakeHome: an attacker-controlled dir with a fake
	// session file. The symlink target is OFF-home, which is the
	// scenario this test pins.
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(piDir), 0755); err != nil {
		t.Fatalf("mkdir parent: %v", err)
	}
	if err := os.Symlink(outside, piDir); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	decoy := filepath.Join(outside, "session.jsonl")
	if err := os.WriteFile(decoy, []byte(`{"type":"session","id":"x","timestamp":"t","cwd":"/y"}`+"\n"), 0644); err != nil {
		t.Fatalf("write decoy: %v", err)
	}

	got, err := LatestSessionJSONL(cwd)
	if err != nil {
		t.Fatalf("LatestSessionJSONL: %v", err)
	}

	// Post-fix assertion: must NOT return the decoy path. Pre-fix
	// would return filepath.Join(outside, "session.jsonl") because
	// latestJSONLInDir doesn't check.
	if got == decoy {
		t.Errorf("CASTRATION-2 follow-up: LatestSessionJSONL walked a symlink "+
			"that resolved OUTSIDE HOME.\n"+
			"Pre-fix: returns %q (the decoy path inside the symlink target).\n"+
			"Post-fix: should return \"\" because the symlink target is off-home.\n"+
			"Compare with session.go:407 (buildIndex has the same defense).", got)
	}
	if got != "" {
		t.Errorf("LatestSessionJSONL with off-home symlink returned %q; want \"\"", got)
	}
}

// TestLatestSessionJSONL_OnHomePathStillWorks pins the regression
// guard: the new isUnderHome check must NOT break the normal
// (legitimate) case. A real session dir under HOME must still
// return its JSONL.
func TestLatestSessionJSONL_OnHomePathStillWorks(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	// Real on-home session dir (no symlink).
	cwd := "/tmp/regression"
	encoded := encodeCwdKey(cwd)
	sessionDir := filepath.Join(fakeHome, ".pi", "agent", "sessions", encoded)
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	real := filepath.Join(sessionDir, "session.jsonl")
	if err := os.WriteFile(real, []byte("{}"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := LatestSessionJSONL(cwd)
	if err != nil {
		t.Fatalf("LatestSessionJSONL: %v", err)
	}
	if got != real {
		t.Errorf("LatestSessionJSONL on legitimate on-home path = %q; want %q\n"+
			"(regression: the isUnderHome guard must not break normal operation)", got, real)
	}
}
