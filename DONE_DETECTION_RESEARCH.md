# Research: Per-Task "Done" Notifications in awp

**Ticket:** task/awp — "Scan the repository, try to find a way to detect the work has done or not"
**Date:** 2026-06-28 (revised after clarifying user goal: per-task stop notifications)
**Status:** Investigation complete; approach identified and stress-tested against the code;
no code changes shipped.

---

## 1. Problem Statement (revised)

**Actual user need:** when a task stops, the user wants a TUI-internal notification. The user
gave the canonical scenario: 10 tasks A–J running, the user is currently working in pane B;
pane A finishes its work; the user should see a notification in the TUI (without having to
flip back to pane A).

**"Stop" definition (per user):**

1. **Process exit** — pi's process dies (user quits, agent crashes, network drops). Already
   partially handled by `m.notify("Agent exited")` at `internal/ui/model.go:456`.
2. **Per-turn completion** — the assistant just emitted its last token for this turn and is
   now waiting for the user's next prompt (pi's `stopReason == "stop"`). **NEW** — no
   detection today.

**Focus policy (per user):** the currently focused pane is **silent** — when the user is
sitting in pane B and pane B finishes, do not notify (they can see the pane go idle).

This is an **edge-triggered** event, not a **level-triggered** state. The codebase has
existing infra (`m.notify()`, toast rendering, the 5-second `pollAgentStatusesAsync` loop);
we just need to wire the JSONL tail into it.

---

## 2. What Already Exists

### 2.1 `m.notify()` and the toast system

`internal/ui/model.go:2727-2730`:
```go
func (m *Model) notify(msg string) {
    m.notification = msg
    m.notifyTime = time.Now()
}
```

The toast:
- renders in **both** ModeNormal (sidebar) and ModeAgentView (pane) — `view.go:471` and
  `view.go:1428` both check `m.notification`.
- auto-clears after 3 seconds — `model.go:483-486`:
  ```go
  case notificationMsg:
      if time.Since(m.notifyTime) > 3*time.Second {
          m.notification = ""
      }
  ```
- already styled with ✓ (success) and ✗ (error) icons via prefix detection — `view.go:471-484`.

**Limitation:** single-string field. New notifications **overwrite** the old one. With 10
panes finishing close together, only the latest survives. See §5 R6.

### 2.2 The 5-second poll loop

`internal/ui/model.go:2805` defines `pollAgentStatusesAsync`. It runs every 5 s via
`tickAgentStatus` (initialized in `Init()` at `model.go:292`, re-armed in handlers at
`model.go:324, 467`). It already returns a `tea.Msg` (`agentStatusResultMsg`) which is
processed in `model.go:472-479`:

```go
case agentStatusResultMsg:
    for ticketID, status := range msg {
        if ticket, _ := m.globalStore.Get(ticketID); ticket != nil {
            ticket.AgentStatus = status
        }
    }
```

The `paneInfo` struct collected at `model.go:2809-2828` already includes `worktreePath` and
`branchName`. So we know which JSONL to read for each pane — without any new fields.

### 2.3 PTY exit (`terminal.ExitMsg`)

`internal/terminal/pane.go:294` defines `ExitMsg{PaneID, Err}`. Two emit sites:
- `pane.go:389` — `pty.Start` failed.
- `pane.go:642` — `pty.Read` returned error (i.e., process closed its PTY = exited).

The main UI handler at `internal/ui/model.go:448-460`:
```go
case terminal.ExitMsg:
    ticketID := board.TicketID(msg.PaneID)
    delete(m.panes, ticketID)
    if ticket, _ := m.globalStore.Get(ticketID); ticket != nil {
        ticket.AgentStatus = board.AgentNone
        m.saveTicket(ticket)
    }
    if m.focusedPane == ticketID {
        m.mode = ModeNormal
        m.focusedPane = ""
        m.notify("Agent exited")
    }
    return m, nil
```

**Gap vs. user spec:** the `m.notify` call is **inside** the `if m.focusedPane == ticketID`
guard. So today, **non-focused exits are silent**. The user wants those to notify too (with
the ticket title). Two-line fix in production code; see §6 step 4.

A second handler at `model.go:370-378` covers the spawning phase — already notifies on failure.

### 2.4 Pi session JSONL files

Pi writes every conversation to a JSONL file at:
```
~/.pi/agent/sessions/{encoded-cwd}/{timestamp}_{uuid}.jsonl
```

Verified against current pi-mono (`packages/coding-agent/src/core/session-manager.ts:439-443`)
— awp's `encodeCwdKey` (`internal/pi/session.go:469-483`) is functionally equivalent.
Empirically tested against six path shapes: outputs match 100% for any path without `~` or
`file://` (worktree paths never have those).

Each `message` entry has a `stopReason`:
- `"toolUse"` — assistant wants to run a tool (still working).
- `"stop"` — assistant finished the turn (waiting for next user prompt).

Real-world file sizes:
```
$ wc -l ~/.pi/agent/sessions/--home-leroy-Project-sages--/*.jsonl | sort -rn | head -3
   1226 .../2026-05-25T14-28-55-872Z_...jsonl
    653 .../2026-06-27T07-39-27-404Z_...jsonl
    503 .../2026-06-14T09-44-39-631Z_...jsonl
```
Up to ~140 KB / 1226 messages per session. Files are appended to during the session
(verified: mtime moves every few hundred ms).

The pattern is always:
```
user → assistant(toolUse) → toolResult → assistant(toolUse) → toolResult → … → assistant(stop) → user
```

So **the latest assistant message's `stopReason` is the authoritative "turn done" signal**.

---

## 3. The Plan (one screen)

```
[ Model.pollAgentStatusesAsync, every 5s ]
        │
        ├── existing: pane.Running() → AgentNone (unchanged)
        │
        └── NEW: for each running pane:
                       1. look up JSONL path via encodeCwdKey(workdir) + sessionID-or-latest
                       2. stat mtime; skip if unchanged since last poll
                       3. else: read new bytes from lastOffset, find last assistant stopReason
                       4. edge detect: was "toolUse" (or empty), now "stop"?
                          if yes AND pane != focusedPane:
                              emit paneTurnDoneMsg{ticketID, title}
                       5. update cache: (path, mtime, offset, lastStopReason)
```

```
[ Model.Update(paneTurnDoneMsg) ]
        │
        └── if msg.paneID != m.focusedPane:
                m.notify("✓ <ticket title> done")
            # focused pane: silent (user can see pane state directly)
```

Plus a one-line fix in the existing `terminal.ExitMsg` handler to notify for non-focused panes
too (currently silent).

### What is **not** part of this plan

- No changes to `Ticket` struct (no new fields).
- No changes to `view.go` ✓ rendering branch — `AgentCompleted` stays dead, on purpose.
- No changes to `piStateToAgentStatus` reducer.
- No new disk I/O (toasts are ephemeral; `saveTicket` still fires only on user events).
- No new dependencies, no architecture changes.

---

## 4. Approaches Considered

### Approach A — JSONL tail → edge event (recommended)

The plan in §3. Single source of truth (pi's session JSONL), edge detection at the poll,
`m.notify()` for delivery. ~80-120 LoC including tests.

| Aspect | Value |
|---|---|
| LoC | ~80-120 (parser 40 + edge detector 25 + cache 15 + tests 30-50) |
| Latency | ≤ 5 s (existing poll interval) |
| I/O cost | see §5 R2 benchmark |
| Disk writes per detection | **0** (notifications are ephemeral) |
| Breaks any test | none — no test pins "Agent exited" notification behavior |
| Aligns with AGENTS.md §3 | yes — uses pi's session files, not screen-scrape |
| Aligns with AGENTS.md §5.4 | yes — `p.mu` discipline unchanged |

### Approach B — Post-mortem only

Only the `terminal.ExitMsg` handler. No JSONL tail, no per-turn detection. ~5 LoC change.

| Aspect | Value |
|---|---|
| Detects "per-turn completion" | **NO** |
| Detects "process exit" for non-focused pane | YES (after the §6 fix) |
| LoC | ~5 |

Use this as a quick first PR (the user's pain point is "agent exited silently while I was in
another pane" — Approach B alone solves that for the process-exit case). Approach A layers on
top to cover per-turn.

### Approach C — Switch to `--mode rpc`

Spawn pi with `--mode rpc`, consume JSONL events on stdin/stdout for the control plane.

**Not recommended.** Same reasons as before (v2 is interactive-TUI-based; RPC mode is for
automation, not interactive UX). The JSONL file **is** pi's persistent record of those RPC
events — reading it is functionally equivalent for our use case without the architectural
disruption.

### Approach D — PTY heuristics (rejected)

Watch for pi's "▌ idle cursor" returning after the agent's output ends.

**Forbidden by AGENTS.md §3** — "Never screen-scrape." Even if not forbidden, fragile to pi
UI changes.

### Approach E — Extension-based push

Extend `internal/pi/extension/awp-extension.ts` to write a status file when `agent_end` fires.
Awp polls the file.

**Equivalent in power to Approach A but worse engineering** — uses a side-channel when the
canonical record already exists. Reject.

---

## 5. Risks and Caveats

### R1. `parseSessionInfo` is not reusable for "last message"

`internal/pi/session.go:230` declares `const maxScanLines = 200`. Its doc-comment explicitly
says: *"For sessions longer than maxScanLines, MessageCount and ToolCount are undercounted
(intentional — accuracy beyond summary is not needed for the picker UX)."*

Real sessions reach 1226 lines. The last assistant message is at the **end** of the file, well
past the picker scan window. We must write a new function — either a full-file scan (O(n) per
poll, too costly), a tail scan (read last 4 KB, parse lines backwards), or an incremental scan
that maintains an offset (best). None of these reuse `parseSessionInfo`'s internal loop
without modification.

### R2. Cost with 10 concurrent panes (measured)

Benchmark on a Linux workstation, 10 panes with 1226 messages / ~140 KB each, Go 1.21,
`encoding/json`:

| Scenario | Wall time | Per-pane cost |
|---|---|---|
| Cold start, 10 panes sequential | 19.5 ms | ~2.0 ms |
| Cold start, 10 panes **parallel** (tea.Cmd goroutines) | 5.0 ms | — |
| Warm idle (mtime unchanged), 10 panes, 100 polls | 1.9 ms total | **1.9 µs/poll** |
| Warm active (mtime changes), 10 panes, 50 polls | 933 ms total | 1.86 ms per poll |
| Warm active, **30 panes**, 50 polls | 2.8 s total | 1.86 ms per poll |

Idle cost is essentially zero (stat-only skip). Active cost is ~2 ms per pane per poll.
**No disk writes** for notifications — unlike my earlier over-designed plan that included
per-change `saveTicket` calls. Total per 5 s cycle for 10 active panes: ~20 ms = 0.4% of one
CPU core. Not a bottleneck.

Two real concerns the benchmark did not simulate:

1. **mtime resolution.** On ext3, mtime is 1-second; on ext4 and most modern filesystems,
   nanosecond. **Fix:** also check `file size`; treat "size > lastOffset" as re-parse
   regardless. Pi calls `fsync` after each entry (verified by real session mtimes moving
   every few hundred ms), so the gap is narrow in practice.
2. **File truncation / rotation.** Pi does NOT currently rotate session files mid-session
   (append-only per session). **Fix:** treat `size < lastOffset` as cache invalidation →
   cold-start re-scan.

### R3. Toast overwrite race

`m.notification` is a single string. With 10 panes finishing close together, only the
latest toast survives the 3-second auto-clear.

This is acceptable for v1 (most users won't have all 10 finishing within 3 seconds), but if
we ever care about queueing, we'd need to extend the schema to `[]notification` or use a
dedicated notification log. Out of scope for this ticket — recorded as future work.

### R4. `stopReason` vocabulary may vary

Empirically observed values in real pi sessions: `"stop"` and `"toolUse"`. Different pi
versions and providers might emit other strings (`"end_turn"`, `"max_tokens"`,
`"stop_sequence"`).

**Mitigation in the mapper:** treat `"toolUse"` as "still working", anything else (including
`""`, `"stop"`, `"end_turn"`, `"unknown"`) as "done for this turn". Conservative: might
over-report "done" on unknown non-tool stopReasons. The user can always inspect the pane.
Document the known set in code with a comment.

### R5. Session ID ambiguity in multi-session workdirs

If the user opens their worktree in a separate terminal and runs `pi` there, that creates a
**second** session JSONL in the same encoded-cwd directory. Heuristic (a) — pick newest by
mtime — could attribute that file to awp's pane.

Two mitigations, pick one:

- **(b-preferred)** Spawn pi with `--session <awp-generated-uuid>`. Awp stores the UUID in
  `ticket.AgentSessionID` at spawn time (currently this field is set only on resume via
  `sessionpicker.go:182`). **Eliminates the ambiguity class entirely** and survives awp
  restarts.
- **(a-mitigated)** Match the JSONL header's `cwd` field to `pane.workdir` after symlink
  resolution. Cheap to check (first line of each file).

(b) is preferred because it also gives us a stable handle for future "delete the session"
work and survives awp restarts.

### R6. Process exit + per-turn double-notify

If pane A finishes its turn (`stopReason: stop`) and then the user quits pi within the same
5-second poll window, we'll fire **two** notifications: "A finished a turn" then
"A's process exited". Strictly speaking, both are correct events, but the second one is
redundant.

**Mitigation options:**
- (i) Accept the redundancy — both events are factually true, and the 3-second toast
  auto-clear means the user likely sees one.
- (ii) When firing `terminal.ExitMsg`, suppress if we already fired `paneTurnDoneMsg` for
  the same pane within the last `teaTick interval`.
- (iii) Distinguish wording: "A finished a turn" vs "A exited".

Recommend (iii) — clearest UX. Both fire, but with different prefixes so the user can tell
them apart.

### R7. Focused-pane focus shift misses notification

If user is in pane A and pane A finishes (silent — focused), then user switches to pane B,
the toast for A's completion is lost forever. Strict interpretation of the user's "focused
pane silent" rule.

Acceptable for v1 (matches the user's stated rule). If they later want a "missed
notifications" indicator on focus change, that's a separate ticket.

---

## 6. Recommendation and TDD Steps

**Ship in two PRs:**

- **PR 1 (Approach B):** Fix the existing `terminal.ExitMsg` handler so non-focused panes
  notify too. ~5 LoC, zero risk, immediately addresses the user's pain point for
  process-exit events. Tests pin the new behavior.

- **PR 2 (Approach A):** Layer JSONL tail for per-turn completion. ~80-120 LoC, the meat of
  this design.

### PR 1 — concrete steps

1. **RED** — `internal/ui/exit_notification_test.go`:
   - Construct a Model with two panes (A, B); focus = B.
   - Send `terminal.ExitMsg{PaneID: "A"}`.
   - Assert: `m.notification` contains "A" (ticket title); not the generic "Agent exited".
2. **GREEN** — modify `model.go:448-460`:
   - Look up the ticket title via `m.globalStore.Get(ticketID)`.
   - If `m.focusedPane == ticketID`: keep existing "Agent exited" toast + clear focus.
   - If `m.focusedPane != ticketID`: notify with title, do NOT clear focus.
3. **REFACTOR** — extract a helper `notifyExit(ticketID, ticketTitle)` for both branches.

### PR 2 — concrete steps

1. **RED** — `internal/pi/turn_done_detection_test.go`:
   - Build a synthetic session JSONL with three assistant messages (last one
     `stopReason: "stop"`).
   - Call `DetectLastStopReason(path)` → assert `"stop"`.
   - Also test: empty file, no assistant messages, malformed JSON, and **end-with-toolUse**
     (proves we read the last one, not just any one).
2. **GREEN** — add `DetectLastStopReason(path string) (string, error)` to
   `internal/pi/session.go`:
   - Scan from EOF backwards in 4 KB chunks until we find a line with
     `type: "message"`, `role: "assistant"`. Reverse-scan avoids reading the whole 140 KB
     on each call.
3. **RED** — `internal/pi/turn_done_cache_test.go`:
   - Construct a cache, simulate two polls with different `lastStopReason` values.
   - Assert: edge event fires on `toolUse → stop`; does NOT fire on `stop → stop` (already
     notified); does NOT fire on `stop → toolUse` (no longer done).
4. **GREEN** — add a `TurnDoneCache` struct with `(path, mtime, offset, lastStopReason)`:
   - `Update(newStopReason) → (eventFired bool)`: returns true if transitioned from
     non-`stop` to `stop`.
5. **RED** — `internal/ui/poll_turn_done_test.go`:
   - Mock a Model with one pane whose workdir has a fake JSONL.
   - Drive the poll: assert no notification if focused; assert notification if not focused.
6. **GREEN** — extend `pollAgentStatusesAsync` (`model.go:2805`):
   - For each pane, find JSONL, call `TurnDoneCache.Update`.
   - On edge → `stop`: emit `paneTurnDoneMsg{ticketID, paneID, title}`.
7. **GREEN** — add handler for `paneTurnDoneMsg` in `Model.Update`:
   ```go
   case paneTurnDoneMsg:
       if msg.paneID != string(m.focusedPane) {
           m.notify("✓ " + msg.title + " finished a turn")
       }
   ```
8. **REFACTOR** — extract shared JSONL helpers; document `stopReason` vocabulary in code.

---

## 7. Cross-References

| File | What it does today | Change needed |
|---|---|---|
| `internal/ui/model.go:128-129` | `notification`, `notifyTime` fields | unchanged (still used) |
| `internal/ui/model.go:2727` | `notify(msg)` setter | unchanged |
| `internal/ui/model.go:448-460` | `ExitMsg` handler — notifies only if focused | **notify for non-focused panes too** (PR 1) |
| `internal/ui/model.go:370-378` | `ExitMsg` handler for spawning | unchanged (already notifies) |
| `internal/ui/model.go:483-486` | 3-second auto-clear | unchanged |
| `internal/ui/model.go:2805` | `pollAgentStatusesAsync` | **extend to emit turn-done edges** (PR 2) |
| `internal/ui/view.go:471` | toast rendering with ✓/✗ icons | unchanged (already styled) |
| `internal/ui/view.go:1428` | toast rendering in pane view | unchanged |
| `internal/pi/session.go:175` | `parseSessionInfo` (bounded 200-line) | NOT reusable for "last message"; new fn needed (see §5 R1) |
| `internal/pi/session.go:469` | `encodeCwdKey` | verified equivalent to pi-mono's `getDefaultSessionDirPath` |
| `internal/board/board.go:67` | `AgentCompleted` AgentStatus | **unchanged on purpose** — stays dead for this ticket |
| `internal/ui/view.go:400/1777` | `AgentCompleted` render branches | **unchanged on purpose** — stays dead for this ticket |
| `internal/ui/model.go:2853` | `piStateToAgentStatus` | **unchanged on purpose** — not part of this design |
| `AGENTS.md §3 Rule 2` | "Use pi's RPC protocol — never screen-scrape" | satisfied by reading JSONL |
| `AGENTS.md §5.4` | pane concurrency invariants | unchanged by this work |

---

## 8. Out of Scope

- **Persistent "✓ done" badge.** This ticket is about events, not state. `AgentCompleted`
  remains dead. If we ever want persistent status, that's a separate ticket (and would need
  to address the `piStateToAgentStatus` reducer bug).
- **OS notifications.** User said TUI-internal only.
- **Queueing multiple toasts.** See §5 R3 — out of scope for v1.
- **"Missed notifications" on focus change.** See §5 R7 — out of scope; matches user's
  stated "focused silent" rule.
- **Predicting** whether the agent's work is correct (LLM-as-judge, tests, etc.).
- **Auto-deleting tickets when work is done.**
- **Real-time updates faster than 5 s.** Poll cadence is fine for human attention.
- **Bringing up the RPC event pipeline** (would require `--mode rpc`, much larger change).

---

## 9. Summary

**There IS a way, and it's much smaller than the first draft suggested.** The actual user
need is per-task stop notifications (process exit + per-turn completion), edge-triggered,
TUI-internal, focused-pane silent. The implementation has two parts:

1. **Fix the existing `ExitMsg` handler** so non-focused exits also notify (~5 LoC). This
   alone covers the user's most likely pain point: "I was in pane B and pane A died
   silently."
2. **Add JSONL tail** in `pollAgentStatusesAsync` to detect `stopReason: "stop"` transitions
   (~80-120 LoC). Edge detection: fire notification when `toolUse → stop`, not on every
   `stop` reading.

The fix is no architectural changes, no new dependencies, no screen scraping, no new
disk I/O. The toast infrastructure already exists (`m.notify`, ✓ styling, 3-second auto-clear,
cross-mode rendering). The 5-second poll loop already exists. The 200-line JSONL scan limit
is the only real engine block (R1) and is solved with a new reverse-scan function.

The previous draft's over-engineering (persistent status, saveTicket on every change, R3
reducer bypass) is **deliberately removed** — none of it serves the user's stated goal.