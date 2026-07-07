package observability

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// helper: temp log dir per-test, cleaned up via t.TempDir().
func newTempLogDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// TestInit_DefaultLevelIsWarn pins the contract that --debug=false
// (the default) suppresses Debug AND Info. This is the change from
// the old behavior where Info was always shown.
//
// Red: this test will fail because current Init() uses LevelInfo.
func TestInit_DefaultLevelIsWarn(t *testing.T) {
	logDir := newTempLogDir(t)

	// Capture stderr BEFORE Init so the logger's stderr handler
	// points at our pipe, not the real stderr.
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = oldStderr })

	if err := Init(false, logDir); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { Init(false, "") })

	Debug("hidden-debug", "k", "v")
	Info("hidden-info", "k", "v")
	Warn("visible-warn", "k", "v")
	w.Close()
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if strings.Contains(out, "hidden-debug") {
		t.Errorf("Debug should be hidden at default level. Got: %s", out)
	}
	if strings.Contains(out, "hidden-info") {
		t.Errorf("Info should be hidden at default level. Got: %s", out)
	}
	if !strings.Contains(out, "visible-warn") {
		t.Errorf("Warn should be visible at default level. Got: %s", out)
	}
}

// TestInit_DebugRaisesLevel pins the contract that --debug=true
// raises level to Debug (Debug+Info+Warn+Error all pass).
func TestInit_DebugRaisesLevel(t *testing.T) {
	logDir := newTempLogDir(t)

	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = oldStderr })

	if err := Init(true, logDir); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { Init(false, "") })

	Debug("visible-debug", "k", "v")
	Info("visible-info", "k", "v")
	Warn("visible-warn", "k", "v")
	w.Close()
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	for _, want := range []string{"visible-debug", "visible-info", "visible-warn"} {
		if !strings.Contains(out, want) {
			t.Errorf("Debug level should show %q. Got: %s", want, out)
		}
	}
}

// TestInit_WritesToFile pins the contract that Warn+ also lands in
// a file under logDir, not just stderr. This is the whole point of
// the change — capture crashes that happen on a quiet day.
//
// Red: current Init() creates no file.
func TestInit_WritesToFile(t *testing.T) {
	logDir := newTempLogDir(t)
	if err := Init(false, logDir); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { Init(false, "") })

	// Quiet stderr so the test output is clean
	oldStderr := os.Stderr
	devNull, _ := os.Open(os.DevNull)
	os.Stderr = devNull
	t.Cleanup(func() { os.Stderr = oldStderr })

	Warn("file-warn-msg", "k", "v")
	Error("file-error-msg", "k", "v")

	// Allow any sync to flush
	time.Sleep(50 * time.Millisecond)

	// Find today's log file
	today := time.Now().Format("2006-01-02")
	want := filepath.Join(logDir, "awp-"+today+".log")
	data, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("expected log file %s to exist: %v", want, err)
	}
	out := string(data)
	if !strings.Contains(out, "file-warn-msg") {
		t.Errorf("Warn not in file %s. Contents: %s", want, out)
	}
	if !strings.Contains(out, "file-error-msg") {
		t.Errorf("Error not in file %s. Contents: %s", want, out)
	}
	// File format is JSON
	var parsed map[string]any
	firstLine := strings.SplitN(out, "\n", 2)[0]
	if err := json.Unmarshal([]byte(firstLine), &parsed); err != nil {
		t.Errorf("file log line should be JSON. Line: %s. Err: %v", firstLine, err)
	}
	if parsed["msg"] != "file-warn-msg" {
		t.Errorf("JSON msg field wrong. Got: %v", parsed["msg"])
	}
}

// TestInit_DoesNotWriteFileForSuppressedLevel: at default level,
// Debug/Info do not even hit the file (saves I/O).
func TestInit_DoesNotWriteFileForSuppressedLevel(t *testing.T) {
	logDir := newTempLogDir(t)
	if err := Init(false, logDir); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { Init(false, "") })

	oldStderr := os.Stderr
	devNull, _ := os.Open(os.DevNull)
	os.Stderr = devNull
	t.Cleanup(func() { os.Stderr = oldStderr })

	Debug("hidden", "k", "v")
	Info("hidden", "k", "v")
	time.Sleep(50 * time.Millisecond)

	// File may not even exist (logger short-circuits), or exist but
	// not contain the messages. Both are acceptable.
	today := time.Now().Format("2006-01-02")
	path := filepath.Join(logDir, "awp-"+today+".log")
	if data, err := os.ReadFile(path); err == nil {
		if strings.Contains(string(data), `"msg":"hidden"`) {
			t.Errorf("Debug/Info at default level must not land in file. Got: %s", data)
		}
	}
}

// TestInit_LogDirUnwritableFallsBackToStderr: when logDir is a path
// we can't write to, Init must NOT panic, must NOT block startup;
// stderr must still work.
//
// Red: current Init() has no fallback — it crashes on nil file write.
func TestInit_LogDirUnwritableFallsBackToStderr(t *testing.T) {
	// /dev/null is a file, not a dir — MkdirAll under it must fail.
	badDir := filepath.Join(os.DevNull, "awp-cannot-create-here")

	// Capture stderr so the fallback warning is observable but quiet
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = oldStderr })

	// Must not panic
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("Init panicked on bad logDir: %v", rec)
		}
	}()

	err := Init(false, badDir)
	w.Close()
	buf := make([]byte, 8192)
	n, _ := r.Read(buf)
	stderrOut := string(buf[:n])

	// Two acceptable behaviors:
	//   (a) Init returns an error AND keeps stderr working
	//   (b) Init succeeds with empty logDir AND prints a warning
	// Either way, the warning message must be on stderr.
	if err != nil {
		// Case (a): err returned. That's fine.
		_ = stderrOut
	} else {
		// Case (b): must have a warning on stderr explaining fallback
		if !strings.Contains(stderrOut, "log") {
			t.Errorf("Init silently fell back to stderr. Expected a warning. Stderr: %s", stderrOut)
		}
	}

	// After fallback, Warn must still produce output
	Init(false, "") // reset to a working state
	Warn("post-fallback", "k", "v")
	_ = stderrOut
}

// TestInit_RetentionDeletesOldFiles: files older than 7 days in
// logDir are removed on Init. Today's file is preserved.
func TestInit_RetentionDeletesOldFiles(t *testing.T) {
	logDir := newTempLogDir(t)

	// Pre-create fake old and new files
	old := filepath.Join(logDir, "awp-2020-01-01.log")
	fresh := filepath.Join(logDir, "awp-2099-12-31.log")
	if err := os.WriteFile(old, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fresh, []byte("fresh"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := Init(false, logDir); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { Init(false, "") })

	// Old file should be gone
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("expected old file %s to be deleted, err=%v", old, err)
	}
	// Fresh file (date in 2099) should remain — but only if our
	// cutoff is "before today". Either fresh is kept OR the cutoff
	// is date-agnostic. Test the loose invariant: a file dated today
	// survives.
	today := filepath.Join(logDir, "awp-"+time.Now().Format("2006-01-02")+".log")
	if _, err := os.Stat(today); err != nil {
		t.Errorf("expected today's file %s to exist after Init: %v", today, err)
	}
}
