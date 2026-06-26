// Package observability — debug logging for awp v2.
//
// Phase 5: --debug flag enables slog at Debug level. Default level
// is Info. All output goes to stderr (stdout is reserved for the TUI
// or future structured output).
//
// Usage:
//
//	import "github.com/pi/awp/internal/observability"
//
//	observability.Init(true) // enable debug
//	observability.Debug("spawning pi", "ticket", t.ID, "model", "opus")
//
// The package uses Go 1.21+ stdlib log/slog. No third-party deps.
package observability

import (
	"log/slog"
	"os"
	"sync"
)

var (
	mu      sync.RWMutex
	debug   = false
	logger  *slog.Logger
	enabled = false
)

// Init configures the global debug state. Call once at startup
// from main(), after parsing the --debug flag.
//
//	idempotent: safe to call multiple times.
func Init(enableDebug bool) {
	mu.Lock()
	defer mu.Unlock()
	debug = enableDebug
	level := slog.LevelInfo
	if enableDebug {
		level = slog.LevelDebug
	}
	logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level:     level,
		AddSource: enableDebug, // include file:line in debug
	}))
	enabled = true
	if enableDebug {
		logger.Debug("awp debug mode enabled")
	}
}

// IsDebug returns whether debug mode is on.
func IsDebug() bool {
	mu.RLock()
	defer mu.RUnlock()
	return debug
}

// IsEnabled returns whether the logger has been initialized.
func IsEnabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return enabled
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

// Debug logs a debug-level message. Use for verbose tracing.
func Debug(msg string, args ...any) {
	if !IsDebug() {
		return
	}
	getLogger().Debug(msg, args...)
}

// Info logs an info-level message. Always shown unless level is raised.
func Info(msg string, args ...any) {
	getLogger().Info(msg, args...)
}

// Warn logs a warning.
func Warn(msg string, args ...any) {
	getLogger().Warn(msg, args...)
}

// Error logs an error.
func Error(msg string, args ...any) {
	getLogger().Error(msg, args...)
}
