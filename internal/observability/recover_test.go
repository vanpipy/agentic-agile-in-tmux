package observability

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRecoverPanic_WritesCrashFile: defer RecoverPanic("name")
// catches the panic, writes a crash file, and re-panics so
// upstream recovers still run.
func TestRecoverPanic_WritesCrashFile(t *testing.T) {
	logDir := t.TempDir()
	if err := Init(false, logDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { Init(false, "") })

	// Suppress the stderr print from RecoverPanic and the re-panic
	// for clean test output.
	oldStderr := os.Stderr
	devNull, _ := os.Open(os.DevNull)
	os.Stderr = devNull
	t.Cleanup(func() { os.Stderr = oldStderr })

	defer func() {
		if rec := recover(); rec == nil {
			t.Fatal("expected re-panic, got none")
		} else if rec != "synthetic-recover-test" {
			t.Errorf("re-panic value wrong: got %v, want %v", rec, "synthetic-recover-test")
		}
		// Crash file should exist
		entries, _ := os.ReadDir(logDir)
		var found bool
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "awp-crash-") {
				found = true
				data, _ := os.ReadFile(filepath.Join(logDir, e.Name()))
				out := string(data)
				if !strings.Contains(out, "synthetic-recover-test") {
					t.Errorf("crash file missing panic value. Got: %s", out)
				}
				if !strings.Contains(out, "goroutine ") {
					t.Errorf("crash file missing stack. Got: %s", out)
				}
			}
		}
		if !found {
			t.Error("no crash file written")
		}
	}()

	func() {
		defer RecoverPanic("test-fn")
		panic("synthetic-recover-test")
	}()
}

// TestRecoverPanic_NoPanicIsNoOp: when no panic occurs, RecoverPanic
// does nothing.
func TestRecoverPanic_NoPanicIsNoOp(t *testing.T) {
	logDir := t.TempDir()
	if err := Init(false, logDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { Init(false, "") })

	func() {
		defer RecoverPanic("no-panic-test")
		// no panic
	}()

	// No crash file should be written
	entries, _ := os.ReadDir(logDir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "awp-crash-") {
			t.Errorf("unexpected crash file %s when no panic occurred", e.Name())
		}
	}
}

// TestSafeGo_PanicInGoroutineWritesCrashFile: a panic inside a
// SafeGo-launched goroutine is caught and the crash file is
// written, without killing the whole process.
func TestSafeGo_PanicInGoroutineWritesCrashFile(t *testing.T) {
	logDir := t.TempDir()
	if err := Init(false, logDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { Init(false, "") })

	oldStderr := os.Stderr
	devNull, _ := os.Open(os.DevNull)
	os.Stderr = devNull
	t.Cleanup(func() { os.Stderr = oldStderr })

	SafeGo("test-goroutine", func() {
		panic("safe-go-panic-value")
	})

	// Wait for the goroutine to finish (and write the crash file).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		entries, _ := os.ReadDir(logDir)
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "awp-crash-") {
				data, _ := os.ReadFile(filepath.Join(logDir, e.Name()))
				if strings.Contains(string(data), "safe-go-panic-value") {
					return // success
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for crash file from SafeGo")
}
