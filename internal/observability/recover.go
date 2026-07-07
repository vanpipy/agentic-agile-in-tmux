// Panic recovery helpers for goroutines that are NOT covered by
// runTUI()'s defer-recover (which only catches panics in the main
// goroutine, i.e. inside prog.Run()).
//
// SYSTEM_DESIGN.md §3.4 (2026-07-07 follow-up): after the second
// panic-on-spawn, we discovered that Bubble Tea's
// recoverFromGoPanic catches cmd goroutine panics and prints them
// to stdout (lost during alt-screen teardown), and raw `go func()`
// goroutines have NO recover at all. These helpers give every
// goroutine a chance to dump a crash file before the panic
// propagates.
//
// Pattern:
//
//	// For a tea.Cmd body (goroutine launched by Bubble Tea):
//	defer observability.RecoverPanic("prepareSpawn")
//
//	// For a raw goroutine:
//	go observability.SafeGo("pty-read-loop", func() {
//	    // ... body that may panic
//	})
//
// In both cases, the panic is caught, a crash file is written, the
// panic value is logged to the daily log, and then the panic is
// re-raised so upstream recovers (Bubble Tea's recoverFromGoPanic,
// for example) still run.
//
// Implementation note: RecoverPanic must be the DIRECT operand of
// `defer`. In Go, `defer f()` where f is a function call evaluates
// f when the defer fires and discards the return value — so a
// helper that returns a closure (e.g. `defer RecoverPanic(name)`)
// would never invoke the closure. RecoverPanic itself calls
// recover(), so it must be the deferred function directly.
package observability

import (
	"fmt"
	"os"
	"runtime/debug"
)

// RecoverPanic is intended for use with `defer`. When the
// enclosing function panics, it catches the panic, writes a crash
// file, logs to the daily log, and re-panics so upstream recovery
// (e.g. Bubble Tea's recoverFromGoPanic) still runs.
//
// name is a short identifier included in the log line and the
// crash file context. Pass the function name (e.g.
// "prepareSpawn", "pty-read-loop").
//
// Usage:
//
//	defer observability.RecoverPanic("prepareSpawn")
//
// RecoverPanic itself calls recover(), so it must be the direct
// operand of `defer` — not the result of calling it.
func RecoverPanic(name string) {
	r := recover()
	if r == nil {
		return
	}
	stack := debug.Stack()
	path, writeErr := WriteCrashFile(LogDir(), r, stack)
	// Log to the daily log so it shows up in the tail of the
	// next crash file too.
	Error("panic recovered",
		"goroutine", name,
		"panic", fmt.Sprintf("%v", r),
		"crash_log", path,
	)
	if writeErr != nil {
		fmt.Fprintf(os.Stderr, "awp: panic in %s: %v (crash log write failed: %v)\n", name, r, writeErr)
	} else {
		fmt.Fprintf(os.Stderr, "awp: panic in %s: %v (crash log: %s)\n", name, r, path)
	}
	// Re-panic so upstream recovers (Bubble Tea's
	// recoverFromGoPanic, the event loop's outer recover) still
	// run. They restore the terminal and emit ErrProgramPanic
	// so the process exits cleanly.
	panic(r)
}

// SafeGo launches fn in a new goroutine with a recover that
// writes a crash file on panic. Use this for raw `go func()`
// sites that are NOT covered by runTUI()'s defer-recover.
//
//	name: short identifier (e.g. "pty-read-loop", "pi-event-loop")
//	fn:   the function to run
//
// On panic: writes a crash file, logs to the daily log, prints to
// stderr, and the goroutine exits cleanly. Other goroutines and
// the main program are unaffected. Unlike RecoverPanic, SafeGo
// does NOT re-panic — there's no upstream recover to defer to in
// a raw goroutine, and a goroutine crash should not take down
// siblings.
func SafeGo(name string, fn func()) {
	go func() {
		defer func() {
			r := recover()
			if r == nil {
				return
			}
			stack := debug.Stack()
			path, writeErr := WriteCrashFile(LogDir(), r, stack)
			Error("panic in SafeGo goroutine",
				"goroutine", name,
				"panic", fmt.Sprintf("%v", r),
				"crash_log", path,
			)
			if writeErr != nil {
				fmt.Fprintf(os.Stderr, "awp: panic in %s: %v (crash log write failed: %v)\n", name, r, writeErr)
			} else {
				fmt.Fprintf(os.Stderr, "awp: panic in %s: %v (crash log: %s)\n", name, r, path)
			}
		}()
		fn()
	}()
}
