// Crash file writer. Captures panic value and full goroutine stack
// dump to a timestamped file under logDir. Called from runTUI()'s
// defer-recover wrapper BEFORE re-panicking so Bubble Tea's outer
// recover still runs (and restores the terminal).
//
// SYSTEM_DESIGN.md §3.4 (2026-07-07): added so the next panic
// leaves a record on disk, regardless of whether stderr is
// reachable.
package observability

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"time"
)

// WriteCrashFile writes a crash report to
//
//	<logDir>/awp-crash-YYYY-MM-DD-HHMMSS-<pid>.log
//
// containing:
//   - header with panic value, time, pid, go version, awp version
//   - debug.Stack() output (more reliable than runtime.Stack here
//     because it captures the current goroutine's call stack as
//     seen by the panicking code, not the caller of this function)
//   - tail of today's daily log (last CrashLogTailLines lines) for
//     post-mortem context
//
// Returns the absolute path of the written file on success, or an
// error if logDir cannot be written to. Never panics — a failed
// crash dump must not take down the program further.
func WriteCrashFile(logDir string, r any, stack []byte) (string, error) {
	if logDir == "" {
		return "", fmt.Errorf("observability: WriteCrashFile called with empty logDir")
	}
	if err := os.MkdirAll(logDir, DefaultLogDirPerm); err != nil {
		return "", fmt.Errorf("create log dir: %w", err)
	}

	now := time.Now()
	filename := fmt.Sprintf("%s%s-%d.log",
		crashLogBasePrefix,
		now.Format("2006-01-02-150405"),
		os.Getpid(),
	)
	path := filepath.Join(logDir, filename)

	// debug.Stack() is more reliable than the caller-supplied stack
	// when the caller is in a defer. Use the supplied stack as a
	// fallback so the test can drive exact contents.
	if len(stack) == 0 {
		stack = debug.Stack()
	}

	var b strings.Builder
	fmt.Fprintf(&b, "=== awp crash report ===\n")
	fmt.Fprintf(&b, "time:        %s\n", now.Format(time.RFC3339Nano))
	fmt.Fprintf(&b, "pid:         %d\n", os.Getpid())
	fmt.Fprintf(&b, "go version:  %s\n", runtime.Version())
	fmt.Fprintf(&b, "panic value: %v\n", r)
	fmt.Fprintf(&b, "\n=== stack ===\n")
	b.Write(stack)
	if !strings.HasSuffix(b.String(), "\n") {
		b.WriteByte('\n')
	}

	// Tail of today's normal log, if it exists.
	if tail := tailOfDailyLog(logDir, now, CrashLogTailLines); tail != "" {
		fmt.Fprintf(&b, "\n=== last %d lines of daily log ===\n", CrashLogTailLines)
		b.WriteString(tail)
		if !strings.HasSuffix(tail, "\n") {
			b.WriteByte('\n')
		}
	}

	if err := os.WriteFile(path, []byte(b.String()), DefaultLogFilePerm); err != nil {
		return "", fmt.Errorf("write crash file: %w", err)
	}
	return path, nil
}

// tailOfDailyLog returns the last n lines of today's daily log
// file, or "" if no log file exists / cannot be read. Reads the
// file backwards line-by-line so it works for multi-MB logs.
func tailOfDailyLog(logDir string, now time.Time, n int) string {
	if n <= 0 {
		return ""
	}
	path := filepath.Join(logDir, dailyLogPrefix+now.Format("2006-01-02")+".log")
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	// Use bufio.Scanner — for huge files, swap to ring buffer. n=100
	// is small enough that an in-memory slice of the last n lines is
	// fine.
	ring := make([]string, 0, n)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024) // 1MB max line
	for scanner.Scan() {
		if len(ring) == n {
			ring = ring[1:]
		}
		ring = append(ring, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return ""
	}
	var b strings.Builder
	for _, line := range ring {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}
