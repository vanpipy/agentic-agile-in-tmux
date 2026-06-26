//go:build e2e

package e2e_test

import (
	"strings"
	"testing"

	testutil "github.com/pi/awp/internal/testutil"
)

func TestCLI_Version(t *testing.T) {
	bin := testutil.AwpBin(t)
	out, _, code := testutil.RunAwp(t, bin, "version")
	if code != 0 {
		t.Errorf("version exit code = %d, want 0", code)
	}
	if !strings.Contains(out, "awp") {
		t.Errorf("version output = %q, want to contain 'awp'", out)
	}
}

func TestCLI_Help(t *testing.T) {
	bin := testutil.AwpBin(t)
	out, _, code := testutil.RunAwp(t, bin, "--help")
	if code != 0 {
		t.Errorf("--help exit code = %d, want 0", code)
	}
	for _, want := range []string{"Usage:", "Available Commands:"} {
		if !strings.Contains(out, want) {
			t.Errorf("--help output missing %q", want)
		}
	}
}

func TestCLI_Doctor(t *testing.T) {
	bin := testutil.AwpBin(t)
	_, _, code := testutil.RunAwp(t, bin, "doctor")
	// exit 0 = all pass, 1 = some fail
	// We don't know what passes in this env, so just verify the output
	if code != 0 && code != 1 {
		t.Errorf("doctor exit code = %d, want 0 or 1", code)
	}
}

func TestCLI_Doctor_Verbose(t *testing.T) {
	bin := testutil.AwpBin(t)
	out, _, _ := testutil.RunAwp(t, bin, "doctor", "--verbose")
	// Should show all checks
	if !strings.Contains(out, "✓") && !strings.Contains(out, "✗") {
		t.Errorf("doctor verbose output = %q, want checkmarks", out)
	}
}

func TestCLI_Theme_List(t *testing.T) {
	bin := testutil.AwpBin(t)
	out, _, code := testutil.RunAwp(t, bin, "theme", "list")
	if code != 0 {
		t.Errorf("theme list exit code = %d, want 0", code)
	}
	if !strings.Contains(out, "Available themes") {
		t.Errorf("theme list output = %q, want 'Available themes'", out)
	}
	if !strings.Contains(out, "dracula") {
		t.Errorf("theme list output = %q, want to contain 'dracula'", out)
	}
}

func TestCLI_Theme_UnknownSetErrors(t *testing.T) {
	bin := testutil.AwpBin(t)
	_, stderr, code := testutil.RunAwp(t, bin, "theme", "set", "nonexistent-theme-xyz")
	if code == 0 {
		t.Error("theme set unknown should error")
	}
	if !strings.Contains(stderr, "unknown theme") {
		t.Errorf("stderr = %q, want 'unknown theme'", stderr)
	}
}

func TestCLI_Project_Help(t *testing.T) {
	bin := testutil.AwpBin(t)
	out, _, code := testutil.RunAwp(t, bin, "project", "--help")
	if code != 0 {
		t.Errorf("project --help exit code = %d, want 0", code)
	}
	for _, want := range []string{"new", "list", "delete"} {
		if !strings.Contains(out, want) {
			t.Errorf("project --help output missing %q", want)
		}
	}
}

func TestCLI_Session_Help(t *testing.T) {
	bin := testutil.AwpBin(t)
	out, _, code := testutil.RunAwp(t, bin, "session", "--help")
	if code != 0 {
		t.Errorf("session --help exit code = %d, want 0", code)
	}
	for _, want := range []string{"list", "show", "resume", "fork"} {
		if !strings.Contains(out, want) {
			t.Errorf("session --help output missing %q", want)
		}
	}
}

func TestCLI_UnknownCommand(t *testing.T) {
	bin := testutil.AwpBin(t)
	_, _, code := testutil.RunAwp(t, bin, "nonexistent-command-xyz")
	if code == 0 {
		t.Error("unknown command should error")
	}
}
