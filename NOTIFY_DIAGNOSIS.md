# awp Notify Did Not Work — Diagnosis & Fix

**Ticket**: task/awp-notify-did-not-work
**Branch**: task/awp-notify-did-not-work
**Date**: 2026-07-01
**Status**: RESOLVED (TDD RED → GREEN → REFACTOR complete)

---

## TL;DR

The toast/notification handler at `internal/ui/model.go:506` (`case notificationMsg`)
exists to auto-dismiss toasts after 3 seconds, but **nothing in `Init()` ever schedules
a `notificationMsg`**. The auto-dismiss path was dead code at runtime.

Result: a toast set via `m.notify()` stayed on screen forever (until the next
`m.notify()` call replaced it). User-visible: "notify did not work".

Fix: add `tickNotification(d)` helper, wire it into `Init()`, and harden the
handler against zero-value `notifyTime`. Full TDD sequence in commit.

---

## 1. Bug Location

| Item | Value |
|---|---|
| File | `internal/ui/model.go` |
| Symptom line | `case notificationMsg` at L506 |
| Wiring gap | `Init()` at L299-304 (no notification tick) |
| View | `view.go:491` (renders `m.notification` correctly) |

### 1.1 The dead code path

`model.go:506-509` (pre-fix):
```go
case notificationMsg:
    if time.Since(m.notifyTime) > 3*time.Second {
        m.notification = ""
    }
    return m, nil
```

This handler only runs if a `notificationMsg` is delivered. But searching the
codebase:

```
$ grep -rn "notificationMsg" internal/
internal/ui/model.go:506:    case notificationMsg:
internal/ui/model.go:3099: type notificationMsg time.Time
```

Only **the type definition and the handler** exist. No `tea.Cmd` anywhere
constructs a `notificationMsg` and ships it back into the runtime.

### 1.2 The missing wiring in `Init()`

`model.go:299-305` (pre-fix):
```go
func (m *Model) Init() tea.Cmd {
    return tea.Batch(
        tickAgentStatus(5 * time.Second),
        m.spinner.Tick,
        m.checkForUpdates(),
    )
}
```

`tea.Batch` schedules three commands. None of them produces a `notificationMsg`.
The notification tick is missing entirely.

### 1.3 Why the bug wasn't caught earlier

Three factors concealed the bug:

1. **Existing tests check `m.notification` is *set***, not that it auto-dismisses.
   `TestExitMsg_NotifiesNonFocusedPane` (and the focused/crash variants) verify
   the toast is set, which works fine because `m.notify()` writes to the field
   synchronously. They never sleep past 3s and re-read.

2. **The view always renders `m.notification != ""`**, so toasts *appear*. A user
   running for >3 seconds sees the toast **still on screen** (which is what the
   code does — never clears), so the visible behavior looks "almost right".

3. **SYSTEM_DESIGN.md §7.4.4 (L1029) and DONE_DETECTION_RESEARCH.md §2.1 (L57)**
   both describe the 3-second auto-dismiss as already implemented. The design
   docs captured the *intent* but no one wrote the runtime wiring.

### 1.4 Why the design intent and runtime disagreed

`case notificationMsg` was added together with `m.notify()`/`m.notifyTime`
as a complete-looking pair. The author probably expected a self-sustaining
tick but never wired the producer. Without an integration test that:

- sets a toast,
- waits > 3 seconds,
- re-checks `m.notification == ""`,

the missing wiring is invisible. TDD §2.2 in `AGENTS.md` requires the test
to drive the implementation; the absence of that test is itself a process
gap, not just a code gap.

---

## 2. Fix

Three changes in `internal/ui/model.go`, plus a new test file.

### 2.1 Constants

```go
const notificationDuration = 3 * time.Second       // toast on-screen time
const notificationTickInterval = 500 * time.Millisecond // tick cadence
```

Extracted from the inline `3*time.Second` literal so tests can reference the
threshold without magic numbers.

### 2.2 Producer

```go
func tickNotification(d time.Duration) tea.Cmd {
    return tea.Tick(d, func(t time.Time) tea.Msg {
        return notificationMsg(t)
    })
}
```

Parallel to `tickAgentStatus` (L3128-3132). Self-sustaining: the handler
re-arms the tick while a toast is visible, stops ticking once cleared.

### 2.3 Wire into `Init()`

```go
func (m *Model) Init() tea.Cmd {
    return tea.Batch(
        tickAgentStatus(5 * time.Second),
        tickNotification(notificationTickInterval), // <-- new
        m.spinner.Tick,
        m.checkForUpdates(),
    )
}
```

### 2.4 Harden the handler

```go
case notificationMsg:
    if m.notification != "" && time.Since(m.notifyTime) > notificationDuration {
        m.notification = ""
    }
    // Re-arm the tick while a notification is still on screen.
    if m.notification != "" {
        return m, tickNotification(notificationTickInterval)
    }
    return m, nil
```

Two hardening items:

1. **`m.notification != ""` guard**: with a zero-value `notifyTime` (1970),
   `time.Since(zero) ≈ 56 years`, which trivially exceeds `notificationDuration`.
   The pre-fix code would set `m.notification = ""` correctly, but the
   side-effect would be hidden — and on a *fresh* notification (where the
   elapsed time is still tiny), it would still clear because the guard was
   missing. The guard makes the handler robust against zero-time bugs.

2. **Re-arm while visible**: the tick should keep firing until the toast
   actually clears. Without re-arming, a single tick would fire once and
   stop, leaving the toast up indefinitely (the original bug).

---

## 3. Tests

New file: `internal/ui/notify_auto_dismiss_test.go` (6 tests).

| Test | Contract |
|---|---|
| `TestNotify_AutoDismissesAfterTimeout` | After `> notificationDuration`, handler clears toast |
| `TestNotify_PreservesBeforeTimeout` | Fresh toast (< threshold) preserved |
| `TestNotify_EmptyNotificationIsNoop` | Zero-state doesn't mutate (zero-time guard) |
| `TestInit_SchedulesNotificationTick` | `tickNotification` exists & emits `notificationMsg` |
| `TestInit_BatchIncludesNotificationTick` | `Init()` returns non-nil cmd |
| `TestView_ShowsNotification` | View renders toast when set (regression guard) |

### 3.1 TDD trace

```
RED:    git stash the fix; tests fail to compile (tickNotification undefined).
GREEN:  re-apply fix; all 6 tests pass.
REFACTOR: extract constants; add doc-comments; verify pre-commit gate.
```

### 3.2 Pre-commit gate

```
go build -o awp .               # 0 errors
go vet ./...                    # 0 warnings
go test ./...                   # all pass
go test -race ./internal/{ui,terminal,pi}  # race-clean
```

---

## 4. Lessons

1. **Auto-dismiss handlers need a producer**. Code that "checks" a timer field
   doesn't help if no one ever updates the field. When adding state + handler,
   add the producer in the same commit.

2. **Design docs can lie**. SYSTEM_DESIGN.md §7.4.4 and
   DONE_DETECTION_RESEARCH.md §2.1 both documented the auto-dismiss as
   already working. They were wrong. A test that sleeps past 3 seconds
   would have caught the regression at the time it was introduced.

3. **Zero-value time.Time is a footgun**. `time.Since(time.Time{})` ≈ 56 years.
   Any handler that compares against `time.Now()` should also guard against
   zero values. The `m.notification != ""` check makes the bug visible (the
   old behavior of "clear empty" was coincidentally correct, hiding the gap).

4. **TDD's RED phase must be visible**. If `tickNotification` hadn't been
   referenced in the test, the RED phase would be a silent "test passes
   vacuously" — false confidence. Referencing the missing symbol by name
   in the test forces a compile error that anyone can read.

---

## 5. Files changed

```
internal/ui/model.go                     | +28 -1
internal/ui/notify_auto_dismiss_test.go  | +213 (new)
NOTIFY_DIAGNOSIS.md                      | +153 (new, this file)
```