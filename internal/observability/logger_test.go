package observability

import (
	"os"
	"strings"
	"testing"
)

func TestIsDebug_Default(t *testing.T) {
	// Reset state
	mu.Lock()
	debug = false
	enabled = false
	mu.Unlock()
	if IsDebug() {
		t.Error("default IsDebug should be false")
	}
	if IsEnabled() {
		t.Error("default IsEnabled should be false")
	}
}

func TestInit_Debug(t *testing.T) {
	Init(true)
	defer Init(false) // reset
	if !IsDebug() {
		t.Error("after Init(true), IsDebug should be true")
	}
	if !IsEnabled() {
		t.Error("after Init(true), IsEnabled should be true")
	}
}

func TestInit_Info(t *testing.T) {
	Init(false)
	defer func() {
		Init(false)
	}()
	if IsDebug() {
		t.Error("after Init(false), IsDebug should be false")
	}
}

func TestInit_Idempotent(t *testing.T) {
	Init(true)
	Init(false)
	Init(true)
	defer Init(false)
	if !IsDebug() {
		t.Error("idempotent Init should keep last call's state")
	}
}

func TestDebug_SkipsWhenNotEnabled(t *testing.T) {
	Init(false)
	// Capture stderr
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	defer func() {
		os.Stderr = oldStderr
	}()
	Debug("should not appear")
	w.Close()
	buf := make([]byte, 1024)
	n, _ := r.Read(buf)
	if strings.Contains(string(buf[:n]), "should not appear") {
		t.Error("Debug should not write when debug is off")
	}
}

func TestDebug_WritesWhenEnabled(t *testing.T) {
	// Set up pipe FIRST so the logger writes into it
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	defer func() {
		os.Stderr = oldStderr
	}()

	Init(true)  // creates logger pointing at current os.Stderr (the pipe)
	defer Init(false)

	Debug("test message", "key", "value")
	w.Close()
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "test message") {
		t.Errorf("Debug should write when enabled. Got: %s", out)
	}
	if !strings.Contains(out, "key=value") {
		t.Errorf("Debug should include args. Got: %s", out)
	}
}

func TestInfo_AlwaysWrites(t *testing.T) {
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	defer func() {
		os.Stderr = oldStderr
	}()
	Init(false) // Info level

	Info("info message", "foo", "bar")
	w.Close()
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	if !strings.Contains(string(buf[:n]), "info message") {
		t.Errorf("Info should always write. Got: %s", string(buf[:n]))
	}
}

func TestWarn_Error_Levels(t *testing.T) {
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	defer func() {
		os.Stderr = oldStderr
	}()
	Init(false)

	Warn("warn message")
	Error("error message")
	w.Close()
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "warn message") {
		t.Error("Warn should write")
	}
	if !strings.Contains(out, "error message") {
		t.Error("Error should write")
	}
}

// (intentionally no test-only imports needed beyond os/strings/testing)
