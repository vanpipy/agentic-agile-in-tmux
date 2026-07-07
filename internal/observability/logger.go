// Package observability — debug + file logging for awp.
//
// Phase 5: --debug flag enables slog at Debug level. Default level
// is Warn (changed 2026-07-07 from Info to match the new file
// logging policy: "always save, quiet by default").
//
// 2026-07-07 (SYSTEM_DESIGN.md §3.4): added file output to a daily
// rotating JSON log under logDir, and graceful fallback to
// stderr-only if the directory is unwritable. The package never
// blocks awp startup — observability failures degrade to stderr
// with a one-time warning, not a panic.
//
// Usage:
//
//	import "github.com/pi/awp/internal/observability"
//
//	if err := observability.Init(debug, logDir); err != nil {
//	    // log dir issue, observability already printed a warning
//	}
//	observability.Debug("spawning pi", "ticket", t.ID, "model", "opus")
//	observability.Warn("ticket load failed", "err", err)
//
// The package uses Go 1.21+ stdlib log/slog. No third-party deps.
package observability

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultLogFilePerm is the file mode for log files.
	DefaultLogFilePerm = 0640
	// DefaultLogDirPerm is the file mode for the log directory.
	DefaultLogDirPerm = 0750
	// LogRetentionDays is how long log files are kept on disk.
	LogRetentionDays = 7
	// CrashLogTailLines is how many lines of the normal log to
	// include in a crash file (last N). 0 disables.
	CrashLogTailLines = 100
	// crashLogBasePrefix is the filename prefix for crash files.
	crashLogBasePrefix = "awp-crash-"
	// dailyLogPrefix is the filename prefix for daily log files.
	dailyLogPrefix = "awp-"
)

var (
	mu            sync.RWMutex
	debugEnabled  = false
	logger        *slog.Logger
	enabled       = false
	// logDir is the current log directory, or "" if file logging
	// is disabled (e.g. fallback to stderr-only).
	logDir string
	// fileWriter is kept open between writes; rotated by date.
	// nil when file logging is not active.
	fileWriter     io.WriteCloser
	currentLogFile string
	currentLogDate string
)

// Init configures the global logger.
//
// debug:   if true, level is Debug; else Warn.
// logDir:  directory for daily JSON log files. Empty string disables
//          file logging (stderr only). If non-empty but unwritable,
//          Init returns an error AND a stderr warning is printed,
//          and subsequent calls still produce stderr output.
//
// Idempotent: safe to call multiple times. Each call re-applies
// settings and (re)opens the file writer.
func Init(enableDebug bool, dir string) error {
	mu.Lock()
	defer mu.Unlock()

	debugEnabled = enableDebug
	logDir = dir
	level := slog.LevelWarn
	if enableDebug {
		level = slog.LevelDebug
	}

	// Build the text (stderr) handler — always present.
	stderrHandler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level:     level,
		AddSource: enableDebug,
	})

	var handlers []slog.Handler
	handlers = append(handlers, stderrHandler)

	// Try to set up file output.
	if dir != "" {
		if err := os.MkdirAll(dir, DefaultLogDirPerm); err != nil {
			// Fallback: stderr only. Print warning ONCE per Init call.
			fmt.Fprintf(os.Stderr, "awp: observability: log dir %s unwritable (%v); falling back to stderr-only\n", dir, err)
		} else {
			if err := openDailyLogLocked(dir); err != nil {
				fmt.Fprintf(os.Stderr, "awp: observability: open log file failed (%v); falling back to stderr-only\n", err)
			} else {
				fileHandler := slog.NewJSONHandler(fileWriter, &slog.HandlerOptions{
					Level:     level,
					AddSource: enableDebug,
				})
				handlers = append(handlers, fileHandler)

				// Best-effort retention cleanup. Failure is silent
				// (we already have a working log dir).
				_ = cleanupOldLogsLocked(dir)
			}
		}
	}

	logger = slog.New(&multiHandler{handlers: handlers})
	enabled = true
	if enableDebug {
		logger.Debug("awp debug mode enabled")
	}
	return nil
}

// IsDebug returns whether debug mode is on.
func IsDebug() bool {
	mu.RLock()
	defer mu.RUnlock()
	return debugEnabled
}

// IsEnabled returns whether the logger has been initialized.
func IsEnabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return enabled
}

// LogDir returns the current log directory, or "" if file logging
// is disabled / fell back to stderr.
func LogDir() string {
	mu.RLock()
	defer mu.RUnlock()
	return logDir
}

// getLogger returns the global logger, or a no-op if not initialized.
func getLogger() *slog.Logger {
	mu.RLock()
	defer mu.RUnlock()
	if logger == nil {
		return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}))
	}
	return logger
}

// Debug logs a debug-level message. Suppressed at default level.
func Debug(msg string, args ...any) {
	if !IsDebug() {
		return
	}
	getLogger().Debug(msg, args...)
}

// Info logs an info-level message. Suppressed at default level
// (Warn+ only); visible with --debug.
func Info(msg string, args ...any) {
	getLogger().Info(msg, args...)
}

// Warn logs a warning. Always written (default level).
func Warn(msg string, args ...any) {
	getLogger().Warn(msg, args...)
}

// Error logs an error. Always written (default level).
func Error(msg string, args ...any) {
	getLogger().Error(msg, args...)
}

// --- file rotation helpers (must be called with mu held) ---

// openDailyLogLocked opens today's log file. If the date OR the
// directory has changed since the last open, the old writer is
// closed and a new one is opened. Caller must hold mu.
func openDailyLogLocked(dir string) error {
	today := time.Now().Format("2006-01-02")
	wantPath := filepath.Join(dir, dailyLogPrefix+today+".log")
	if currentLogFile != "" && currentLogFile == wantPath && fileWriter != nil {
		// Same dir, same day, same file — nothing to do.
		return nil
	}
	if fileWriter != nil {
		_ = fileWriter.Close()
		fileWriter = nil
	}
	f, err := os.OpenFile(wantPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, DefaultLogFilePerm)
	if err != nil {
		return err
	}
	fileWriter = f
	currentLogFile = wantPath
	currentLogDate = today
	return nil
}

// cleanupOldLogsLocked removes log files older than LogRetentionDays.
// Daily log files (awp-YYYY-MM-DD.log) are dated by filename; crash
// files (awp-crash-*) are dated by mtime (no date in the name).
// Caller must hold mu.
func cleanupOldLogsLocked(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	cutoff := time.Now().AddDate(0, 0, -LogRetentionDays)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		var fileDate time.Time
		switch {
		case strings.HasPrefix(name, dailyLogPrefix) && strings.HasSuffix(name, ".log"):
			// awp-YYYY-MM-DD.log
			dateStr := strings.TrimSuffix(strings.TrimPrefix(name, dailyLogPrefix), ".log")
			t, err := time.Parse("2006-01-02", dateStr)
			if err != nil {
				continue
			}
			fileDate = t
		case strings.HasPrefix(name, crashLogBasePrefix):
			// awp-crash-...: no date in name, use mtime
			info, err := e.Info()
			if err != nil {
				continue
			}
			fileDate = info.ModTime()
		default:
			continue
		}
		if fileDate.Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
	return nil
}

// --- multi-handler ---

// multiHandler fans out records to all underlying handlers. A
// handler that returns an error does not block the others. This is
// the slog.NewMultiHandler pattern from the stdlib examples
// (go.googlesource.com/go/+/refs/heads/master/src/log/slog/example_custom_handler_test.go).
type multiHandler struct {
	handlers []slog.Handler
}

func (m *multiHandler) Enabled(ctx context.Context, l slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, l) {
			return true
		}
	}
	return false
}

func (m *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	var firstErr error
	for _, h := range m.handlers {
		if h.Enabled(ctx, r.Level) {
			if err := h.Handle(ctx, r); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func (m *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		out[i] = h.WithAttrs(attrs)
	}
	return &multiHandler{handlers: out}
}

func (m *multiHandler) WithGroup(name string) slog.Handler {
	out := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		out[i] = h.WithGroup(name)
	}
	return &multiHandler{handlers: out}
}
