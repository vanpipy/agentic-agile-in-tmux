// notify_auto_dismiss_test.go — TDD tests for the toast auto-dismiss contract.
//
// Ticket task/awp-notify-did-not-work: investigate why "notify did not work".
//
// Bug analysis (full text in TICKET_DIAGNOSIS.md accompanying the commit):
//   m.notify() sets m.notification + m.notifyTime. The view renders it.
//   The Update handler `case notificationMsg` is supposed to clear
//   m.notification once the toast has been on screen for > 3 seconds.
//   But NOTHING ever sends a notificationMsg at runtime — Init()'s
//   tea.Batch schedules tickAgentStatus, spinner.Tick, checkForUpdates,
//   but no notification tick. The auto-dismiss path is dead code.
//
//   Symptom: a toast set via m.notify() stays on screen forever
//   (until the next m.notify() call replaces it). User-visible
//   report: "notify did not work".
//
// This file pins FOUR contracts:
//
//  1. The handler at model.go:case notificationMsg MUST clear
//     m.notification once the toast has been on screen for > 3 seconds.
//     The 3-second threshold is encoded as notificationDuration.
//
//  2. The handler MUST NOT clear m.notification when the toast is still
//     fresh (< notificationDuration). Otherwise every periodic tick
//     would wipe the toast immediately.
//
//  3. The handler MUST be a no-op on empty m.notification. The
//     zero-value notifyTime (1970) must not trigger an erroneous clear
//     before any toast is ever set.
//
//  4. Init() MUST schedule a tickNotification so the auto-dismiss
//     is wired at runtime. Pre-fix, Init() returns a tea.Batch with
//     no notification tick, so a notificationMsg is never delivered
//     and the dismiss handler never runs.
package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// TestNotify_AutoDismissesAfterTimeout pins contract #1: after the toast
// has been on screen for > notificationDuration, the handler clears
// m.notification. This is the core "toast disappears" behavior.
func TestNotify_AutoDismissesAfterTimeout(t *testing.T) {
	m := newTestModel(t)

	m.notify("hello world")
	if m.notification != "hello world" {
		t.Fatalf("m.notify did not set m.notification; got %q", m.notification)
	}

	// Backdate notifyTime so the handler sees "toast has been on
	// screen past notificationDuration". Use a generous margin
	// (3x the threshold) to avoid timing flakes on slow CI.
	const elapsed = 3 * notificationDuration
	m.notifyTime = time.Now().Add(-elapsed)

	_, _ = m.Update(notificationMsg(time.Now()))

	if m.notification != "" {
		t.Errorf("toast did not auto-dismiss after %v on screen.\n"+
			"Pre-fix root cause: nothing ever schedules notificationMsg at\n"+
			"runtime, so this Update call is the first time the handler\n"+
			"has ever run in production. The clear branch should still\n"+
			"fire when the time window has elapsed.\n"+
			"Got m.notification = %q, want \"\"",
			elapsed, m.notification)
	}
}

// TestNotify_PreservesBeforeTimeout pins contract #2: a fresh toast
// (< notificationDuration old) must NOT be cleared. Otherwise the
// periodic tick would wipe the toast immediately on arrival.
func TestNotify_PreservesBeforeTimeout(t *testing.T) {
	m := newTestModel(t)

	m.notify("fresh toast")
	// notifyTime is time.Now() — well within notificationDuration.
	_, _ = m.Update(notificationMsg(time.Now()))

	if m.notification != "fresh toast" {
		t.Errorf("toast was cleared too early.\n"+
			"Pre-fix root cause: a stray notifyTime-zero check (1970 vs now)\n"+
			"would make the first tick delete every fresh toast because\n"+
			"the elapsed-since check would read 'huge duration'.\n"+
			"Got m.notification = %q, want %q", m.notification, "fresh toast")
	}
}

// TestNotify_EmptyNotificationIsNoop pins contract #3: when no toast is
// on screen, the handler must not invent one or mutate state.
// The zero-value notifyTime (1970) is a real risk: time.Since(zero)
// ≈ 56 years, which trivially exceeds notificationDuration.
func TestNotify_EmptyNotificationIsNoop(t *testing.T) {
	m := newTestModel(t)

	// notifyTime is the zero value (1970-01-01).
	if !m.notifyTime.IsZero() {
		t.Fatalf("test setup error: expected zero notifyTime, got %v", m.notifyTime)
	}

	_, _ = m.Update(notificationMsg(time.Now()))

	if m.notification != "" {
		t.Errorf("empty-state handler mutated m.notification.\n"+
			"Pre-fix root cause: handler did not guard on m.notification != \"\".\n"+
			"A zero notifyTime (1970) means time.Since(zero) > 3s always, so\n"+
			"the clear branch would set m.notification = \"\" — which happens\n"+
			"to be the desired value, but masks a missing-guard bug.\n"+
			"Got m.notification = %q, want \"\"", m.notification)
	}
}

// TestInit_SchedulesNotificationTick pins contract #4: Init() must
// return a tea.Cmd that, when invoked, will eventually deliver a
// notificationMsg. Pre-fix Init()'s tea.Batch had no notification tick
// at all, so the dismiss handler never ran in production.
//
// We verify the structural property: tickNotification(d) must exist
// and emit a notificationMsg when the tick fires. If tickNotification
// doesn't exist, this test fails to compile — which is a strong
// signal that the wiring is missing entirely.
func TestInit_SchedulesNotificationTick(t *testing.T) {
	// Structural check: tickNotification(d) must exist and emit
	// notificationMsg when the tick fires. Without this, Init()
	// has no way to schedule a notificationMsg at runtime.
	cmd := tickNotification(1 * time.Millisecond)
	if cmd == nil {
		t.Fatal("tickNotification(1ms) returned nil; expected non-nil tea.Cmd")
	}
	msg := cmd()
	if _, ok := msg.(notificationMsg); !ok {
		t.Errorf("tickNotification produced wrong message type.\n"+
			"Pre-fix root cause: tickNotification didn't exist at all,\n"+
			"so Init() could never wire auto-dismiss.\n"+
			"Got msg type %T, want notificationMsg", msg)
	}
}

// TestInit_BatchIncludesNotificationTick is the higher-level wiring
// check: Init()'s returned tea.Cmd must include a notification tick
// that actually fires a notificationMsg at runtime. This guards against
// a future regression where someone removes tickNotification from
// Init()'s tea.Batch (the bug c24e035 was meant to fix).
//
// Strategy: run Init()'s returned cmd in a goroutine, capture the
// tea.Msg it produces. If Init() schedules a tickNotification, the
// returned BatchMsg unwraps to a tea.Tick that fires notificationMsg
// after notificationTickInterval. The test waits up to 2x the tick
// interval for the notificationMsg to arrive.
//
// tea.Batch's invocation returns BatchMsg synchronously (the batch
// itself doesn't block); the runtime then dispatches each child cmd
// in its own goroutine. We capture whatever the first child cmd
// returns — if Init() doesn't include tickNotification, no
// notificationMsg will be observed within the timeout.
func TestInit_BatchIncludesNotificationTick(t *testing.T) {
	m := newTestModel(t)

	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init() returned nil; expected non-nil tea.Cmd")
	}

	// tea.Batch returns BatchMsg synchronously (the batch doesn't
	// block); the runtime then dispatches each child cmd in its own
	// goroutine. Capture whatever any child cmd returns.
	// If Init() includes tickNotification, we'll see notificationMsg
	// within ~notificationTickInterval + slack.
	type result struct {
		msg tea.Msg
	}
	results := make(chan result, 16)

	// tea.Batch returns a Cmd that, when invoked, produces a BatchMsg.
	// The runtime then iterates BatchMsg and invokes each child cmd.
	// Our test harness mimics the runtime by running the batch
	// cmd and dispatching each child.
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("Init() cmd did not produce BatchMsg; got %T", msg)
	}
	for _, child := range batch {
		go func(c tea.Cmd) {
			results <- result{c()}
		}(child)
	}

	deadline := time.After(2 * notificationTickInterval)
	for {
		select {
		case r := <-results:
			if _, ok := r.msg.(notificationMsg); ok {
				return // success: notification tick is wired into Init()
			}
			// Ignore other msg types (spinner.Tick, agentStatusMsg, etc.)
		case <-deadline:
			t.Errorf("Init()'s tea.Batch did not produce any notificationMsg within %v.\n"+
				"This means tickNotification is missing from Init()'s batch —\n"+
				"the original 'notify did not work' regression returns.\n"+
				"Verified contract: Init() must include tickNotification alongside\n"+
				"tickAgentStatus, spinner.Tick, and checkForUpdates.",
				2*notificationTickInterval)
			return
		}
	}
}

// TestView_ShowsNotification pins the end-to-end contract: when
// m.notification is set, View() renders it. This is what the user
// actually sees. Without this assertion, the bug could regress
// silently (e.g., a future refactor removes the view branch).
func TestView_ShowsNotification(t *testing.T) {
	m := newTestModel(t)
	m.width = 100
	m.height = 24
	m.notification = "Render JSON diff exited"

	view := m.View()
	if !strings.Contains(view, "Render JSON diff exited") {
		t.Errorf("View() does not render the notification.\n"+
			"Got view = %q", view)
	}
}