package buildinfo

import (
	"strings"
	"testing"
)

func TestString(t *testing.T) {
	s := String()
	// Should contain version, commit, build date, go version
	for _, want := range []string{"awp", "commit:", "built:", "go:"} {
		if !strings.Contains(s, want) {
			t.Errorf("String() missing %q. Got: %s", want, s)
		}
	}
}

func TestShort(t *testing.T) {
	s := Short()
	// Should be a single line
	if strings.Count(s, "\n") != 0 {
		t.Errorf("Short() should be one line, got: %s", s)
	}
	if !strings.HasPrefix(s, "awp ") {
		t.Errorf("Short() should start with 'awp ', got: %s", s)
	}
}

func TestDefaults(t *testing.T) {
	// Default values should be sensible
	if Version == "" {
		t.Error("Version should not be empty")
	}
	if Commit == "" {
		t.Error("Commit should not be empty")
	}
	if BuildDate == "" {
		t.Error("BuildDate should not be empty")
	}
}
