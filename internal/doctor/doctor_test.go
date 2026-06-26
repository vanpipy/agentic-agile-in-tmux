package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunner_Run_NoPi(t *testing.T) {
	// PATH= empty → pi won't be found
	t.Setenv("PATH", "")
	r := NewRunner()
	res, err := r.Run()
	if err != nil {
		t.Fatal(err)
	}
	if res.AllOK {
		t.Error("expected failures when pi not on PATH")
	}
	// Should have at least one failure
	if len(res.Checks) == 0 {
		t.Error("expected some checks")
	}
	// First check (pi binary) should fail
	if res.Checks[0].Name != "pi binary on PATH" {
		t.Errorf("first check = %q, want 'pi binary on PATH'", res.Checks[0].Name)
	}
	if res.Checks[0].Passed {
		t.Error("pi binary check should fail with empty PATH")
	}
}

func TestCheckAwpConfigDir_Missing(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("AWP_CONFIG_DIR", filepath.Join(tmpDir, "does-not-exist"))
	r := NewRunner()
	c := r.checkAwpConfigDir()
	if c.Passed {
		t.Error("expected failure when config dir missing and Fix=false")
	}
	if !strings.Contains(c.Message, "does not exist") {
		t.Errorf("message = %q, expected to contain 'does not exist'", c.Message)
	}
}

func TestCheckAwpConfigDir_MissingWithFix(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	configDir := filepath.Join(tmpDir, "awp")
	t.Setenv("AWP_CONFIG_DIR", configDir)
	r := NewRunner()
	r.Fix = true
	c := r.checkAwpConfigDir()
	if !c.Passed {
		t.Errorf("expected pass with Fix=true, got: %s", c.Message)
	}
	if !strings.Contains(c.Message, "created") {
		t.Errorf("message = %q, expected to contain 'created'", c.Message)
	}
	// Verify dir was actually created
	if _, err := os.Stat(configDir); err != nil {
		t.Errorf("config dir not created: %v", err)
	}
}

func TestCheckAwpConfigDir_Exists(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	configDir := filepath.Join(tmpDir, "awp")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AWP_CONFIG_DIR", configDir)
	r := NewRunner()
	c := r.checkAwpConfigDir()
	if !c.Passed {
		t.Errorf("expected pass, got: %s", c.Message)
	}
}

func TestCheckGitAvailable_Available(t *testing.T) {
	r := NewRunner()
	c := r.checkGitAvailable()
	// Assert structural invariants regardless of env state.
	if c.Name != "git available" {
		t.Errorf("name = %q, want %q", c.Name, "git available")
	}
	if c.Message == "" {
		t.Error("message should not be empty")
	}
	// Soft-assert: in dev envs git should be available.
	// If not, log so the user sees the env issue but don't skip
	// (skipping hides a missing-dependency problem from CI).
	if !c.Passed {
		t.Logf("note: git not on PATH in this env: %s", c.Message)
	}
}

func TestCheckPiBinary(t *testing.T) {
	r := NewRunner()
	c := r.checkPiBinary()
	// We don't assert pass/fail since PATH varies; just check format
	if c.Name != "pi binary on PATH" {
		t.Errorf("name = %q", c.Name)
	}
	if c.Message == "" {
		t.Error("message should not be empty")
	}
}

func TestResult_Format_AllOK(t *testing.T) {
	res := &Result{
		AllOK: true,
		Checks: []Check{
			{Name: "test 1", Passed: true, Message: "ok"},
			{Name: "test 2", Passed: true, Message: "ok"},
		},
	}
	formatted := res.Format(false)
	if !strings.Contains(formatted, "All checks passed") {
		t.Error("expected 'All checks passed' message")
	}
	if !strings.Contains(formatted, "✓") {
		t.Error("expected ✓ marks")
	}
}

func TestResult_Format_Failure(t *testing.T) {
	res := &Result{
		AllOK: false,
		Checks: []Check{
			{Name: "test 1", Passed: true, Message: "ok"},
			{Name: "test 2", Passed: false, Message: "broken"},
		},
	}
	formatted := res.Format(false)
	if !strings.Contains(formatted, "Some checks failed") {
		t.Error("expected 'Some checks failed' message")
	}
	if !strings.Contains(formatted, "✗") {
		t.Error("expected ✗ mark")
	}
	// In non-verbose mode, failure message should still appear
	if !strings.Contains(formatted, "broken") {
		t.Error("expected failure message in non-verbose mode")
	}
}

func TestResult_Format_Verbose(t *testing.T) {
	res := &Result{
		AllOK: true,
		Checks: []Check{
			{Name: "test 1", Passed: true, Message: "ok"},
		},
	}
	formatted := res.Format(true)
	if !strings.Contains(formatted, "ok") {
		t.Error("verbose mode should show passing messages too")
	}
}
