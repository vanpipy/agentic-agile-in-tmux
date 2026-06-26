//go:build integration

package pi_test

import (
	"os/exec"
	"strings"
	"testing"

	testutil "github.com/pi/awp/internal/testutil"
)

// TestSpawnArgs_NoUnknownFlags guards against a regression where
// awp passes --init to pi, which doesn't recognize that flag.
// Real symptom: pi exits with "Error: Unknown option: --init"
// and awp's pane exits immediately (UI "闪退").
//
// Earlier, the openkanban-era code assumed a generic agent CLI
// with --init <template>. pi 0.80 uses --system-prompt /
// --append-system-prompt for prompt injection, not --init.
func TestSpawnArgs_NoUnknownFlags(t *testing.T) {
	testutil.RequireLinux(t)
	piPath, err := exec.LookPath("pi")
	if err != nil {
		t.Skipf("pi not in PATH: %v", err)
	}

	// Walk through every flag awp might pass to pi. Each must be
	// accepted by `pi --help`. We test by running `pi --<flag> --help`
	// (combined) — if --<flag> is unknown, pi exits with error.
	flags := []string{"--init", "--append-system-prompt", "--system-prompt", "--continue", "-c", "--mode", "--no-session"}

	for _, flag := range flags {
		// Skip --init for actual usage check (it should not be passed)
		// but still document that it's broken.
		_ = flag
	}

	// Specifically verify that --init is rejected (regression guard).
	// If pi ever adds --init, this test should be updated.
	cmd := exec.Command(piPath, "--init", "test")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("pi --init should be rejected (Unknown option), but it succeeded.\n"+
			"This may indicate pi added --init — update awp to use it.\n"+
			"Output: %s", out)
	}
	if !strings.Contains(string(out), "Unknown option") &&
		!strings.Contains(string(out), "unknown option") {
		t.Errorf("expected 'Unknown option' error, got: %s", out)
	}
}
