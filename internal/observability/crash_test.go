package observability

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestWriteCrashFile_ContainsPanicValueAndStack: the crash file
// must contain the panic value and a full runtime.Stack dump.
//
// Red: WriteCrashFile does not exist yet.
func TestWriteCrashFile_ContainsPanicValueAndStack(t *testing.T) {
	logDir := t.TempDir()

	stack := make([]byte, 1<<14)
	n := runtime.Stack(stack, true)
	stack = stack[:n]

	panicVal := "synthetic-test-panic: division by cucumber"
	path, err := WriteCrashFile(logDir, panicVal, stack)
	if err != nil {
		t.Fatalf("WriteCrashFile: %v", err)
	}
	if path == "" {
		t.Fatal("expected non-empty path")
	}

	// File must be under logDir
	if !strings.HasPrefix(path, logDir) {
		t.Errorf("crash file %s not under logDir %s", path, logDir)
	}

	// Filename: awp-crash-YYYY-MM-DD-HHMMSS-<pid>.log
	base := filepath.Base(path)
	if !strings.HasPrefix(base, "awp-crash-") || !strings.HasSuffix(base, ".log") {
		t.Errorf("crash file name should be awp-crash-*.log, got %s", base)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read crash file: %v", err)
	}
	out := string(data)

	if !strings.Contains(out, panicVal) {
		t.Errorf("crash file missing panic value %q. Got: %s", panicVal, out)
	}
	// runtime.Stack output contains "goroutine " and the panic line
	if !strings.Contains(out, "goroutine ") {
		t.Errorf("crash file missing goroutine dump. Got: %s", out)
	}
	if !strings.Contains(out, "synthetic-test-panic") {
		t.Errorf("crash file missing panic string. Got: %s", out)
	}
}

// TestWriteCrashFile_IncludesLastLogLines: if a normal daily log
// exists in logDir, the crash file tails it so post-mortem has
// context.
//
// Red: WriteCrashFile does not exist yet.
func TestWriteCrashFile_IncludesLastLogLines(t *testing.T) {
	logDir := t.TempDir()
	today := time.Now().Format("2006-01-02")
	normalLog := filepath.Join(logDir, "awp-"+today+".log")

	// 200 lines of "event-N" — last 100 should be included
	var b strings.Builder
	for i := 0; i < 200; i++ {
		b.WriteString("event-")
		b.WriteString(itoa(i))
		b.WriteString("\n")
	}
	if err := os.WriteFile(normalLog, []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}

	stack := []byte("goroutine 1 [running]:\nmain.foo()\n")
	path, err := WriteCrashFile(logDir, "test-panic", stack)
	if err != nil {
		t.Fatalf("WriteCrashFile: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)

	// First 100 events should be excluded (we only tail last 100)
	if strings.Contains(out, "event-0\n") {
		t.Errorf("expected first 100 events to be excluded, but found event-0")
	}
	// Last events should be included
	if !strings.Contains(out, "event-199") {
		t.Errorf("expected last event (199) in crash file. Got tail of: %s", tail(out, 500))
	}
	if !strings.Contains(out, "event-100") {
		t.Errorf("expected event-100 (boundary) in crash file. Got tail of: %s", tail(out, 500))
	}
}

// TestWriteCrashFile_BadLogDirReturnsError: WriteCrashFile must
// return an error (not panic) when logDir is not writable.
func TestWriteCrashFile_BadLogDirReturnsError(t *testing.T) {
	badDir := filepath.Join(os.DevNull, "cannot-create-here")

	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("WriteCrashFile panicked on bad dir: %v", rec)
		}
	}()

	_, err := WriteCrashFile(badDir, "x", []byte("y"))
	if err == nil {
		t.Error("expected error for unwritable logDir")
	}
}

// itoa is a tiny int->string for the test (avoid pulling in strconv
// when we only need base-10 with 3 digits).
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [4]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
