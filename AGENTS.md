# AGENTS.md

> Operational playbook for AI coding agents working on `awp`.
> Rules that change behavior live here; API details live in source; design lives in `SYSTEM_DESIGN.md`; project-wide standards live in `.specify/memory/constitution.md`.

---

## 1. Project

`awp` is a **pi-only** TUI multi-session kanban: run N [pi](https://github.com/earendil-works/pi-coding-agent) sessions in parallel, watch and control them from one interface. No multi-agent abstraction.

**Philosophy**: depth over breadth. Every pi capability gets a UI; other agents are out of scope.

The codebase started as a fork of an early multi-agent kanban template and has since grown independent (config at `~/.config/awp/`).

---

## 2. Iron Rules (no exceptions)

These are non-negotiable. Violating one is grounds to revert the commit.

### 2.1 Anti-Shortcuts

If these rationalizations appear in your head, **stop immediately**:

| Excuse | Why forbidden |
|--------|---------------|
| "Simplified version now, extend later" | Simplified = permanent. Re-architecting costs 10×. |
| "Just copy `terminal.Pane`, UI is too long" | UI is the product. Missing UI = missing core. |
| "90% unused, skip it" | You don't know which 10% blocks which user. |
| "Make build pass, adapt later" | Build passing ≠ working. Stubs and unsafe-casts are tech-debt bombs. |
| "Main flow first, edges later" | Edges are the real scenarios. |
| "Template has N lines, we need M" | You don't know why the other N−M exist. |
| "Template uses A/B/C, we don't, so delete" | It took N months to prove A/B/C. You've spent 0. |

**Rules**:

1. System design must be complete. No "omit for now" — fill the gap.
2. Copy = full copy. Original line count = that many lines, no "trimmed version".
3. Stubs/simplifications/unsafe-casts are implementation debt — mark with `DEBT:` in the commit message.
4. Unsure whether to simplify → ask. Don't unilaterally decide "this is fine".
5. Already shortcutting → stop immediately + `git reset --hard HEAD` + replan.

**Litmus test**: if a user asks "did you implement X?", can you answer 100% yes? "Yes, simplified" = fail.

### 2.2 TDD + CORRECT

- **TDD is mandatory**: RED → GREEN → REFACTOR. No tests, no commit. (Full standards: `.specify/memory/constitution.md` §3.)
- **CORRECT self-check** before every commit, on every unit test:

  | Letter | Check | What it means |
  |--------|-------|---------------|
  | **C** | Conformance | output = literal expected (not "non-empty") |
  | **O** | Ordering | don't depend on map iteration order |
  | **R** | Range | 0/1/max/overflow/negative boundaries |
  | **R** | Reference | external deps (file/network/time) handled |
  | **E** | Existence | empty/nil/missing paths |
  | **C** | Cardinality | 0/1/N cases |
  | **T** | Time | timeout/race/expiry |

  `SKIP` requires a linked TODO + issue. `SKIP` doesn't count as coverage.

---

## 3. Golden Rules

Action-oriented, no overlap with §2 or with `constitution.md`.

1. **Pi only** — no multi-agent abstraction, no `Agent` interface, no "adapter" for other agents.
2. **Use pi's RPC protocol** — `--mode rpc` JSONL on stdin/stdout. Never screen-scrape.
3. **Never modify pi source** — pi is a black box. Use extensions only.
4. **`Update()` never blocks** — all I/O via `tea.Cmd` in goroutines. Blocking kills the TUI.
5. **PTY, not tmux** — proven in the fork; tmux adds a layer that breaks RPC semantics.
6. **Doc before code** — `SYSTEM_DESIGN.md` is the source of truth. Update it first, then code.
7. **Interception via extension** — `ui.confirm` is the official mechanism. Never `SIGSTOP`.
8. **No unfaithful copies** — match the template's behavior; stubs are debt and must be marked `DEBT:`.

---

## 4. Workflow

### 4.1 Plan

- Read `SYSTEM_DESIGN.md` before touching code. If the change isn't covered, update the design first.
- Don't decide architecture in the PR. Discuss in an issue or in design doc revisions.

### 4.2 Build

- TDD: write the failing test first.
- Tests live next to code (`foo.go` + `foo_test.go`) for unit; `test/<pkg>/` with `//go:build integration` for integration; `e2e/` with `//go:build e2e` for end-to-end (needs real binaries).
- Race-detect anything concurrent: `go test -race ./internal/pi ./internal/terminal ./internal/ui`.

### 4.3 Commit

- Conventional Commits: `feat:` `fix:` `refactor:` `docs:` `test:` `chore:`.
- Breaking: `feat!: ...` (bang before colon).
- Commit message explains **why**, not what (the diff speaks for itself).
- **No AI co-authorship** — repo history stays human.

### 4.4 Escalation

1. **Design question** → `SYSTEM_DESIGN.md`.
2. **Pi protocol question** → `pi-mono/packages/coding-agent/src/modes/rpc/rpc-types.ts` (source over blog).
3. **Architecture pattern** → existing `internal/{ui,terminal}/` code (do what they do unless there's a reason).
4. **Project standard** → `.specify/memory/constitution.md`.
5. **Still no answer** → write an issue-style markdown, **stop and ask**. Don't guess.
6. **Tempted to "simplify"** → re-read §2.1, confirm you're not violating completeness.

---

## 5. Technical Anchors

These are the rules that don't fit in §2/§3 and aren't already in `SYSTEM_DESIGN.md`. For details, follow the source links.

### 5.1 pi RPC (event names only — schema in source)

- RPC mode: `pi --mode rpc` (JSONL on stdin/stdout).
- Sessions on disk: `~/.pi/agent/sessions/{encoded-cwd}/*.jsonl`.
- Fixed tool names: `bash` `read` `edit` `write` `find` `grep` `ls`.
- Extension hooks (use exactly): `tool_call` `tool_result` `user_bash` `input` `confirm` `select` `agent_start` `agent_end`.
- Event stream: `agent_start`, `turn`, `message`, `tool_execution`, `compaction`, `auto_retry`, `queue_update`.
- **Schema source of truth**: `pi-mono/packages/coding-agent/src/modes/rpc/rpc-types.ts`. Never guess types from blog posts.

### 5.2 Source Layout

```
cmd/awp/                      # cobra CLI
internal/ui/                  # single Model + 16-mode state machine (model.go, view.go)
internal/terminal/            # PTY + vt10x emulator + scrollback
internal/board/               # Ticket / Column
internal/config/              # themes (20) + config loading
internal/{pi,app,doctor,git,project,observability,buildinfo,update}/   # support packages
test/<pkg>/                   # integration (//go:build integration)
e2e/                          # end-to-end (//go:build e2e, needs real binaries)
```

- All imports use `github.com/pi/awp/internal/...`.
- The UI model is intentionally one big `model.go` (single state machine). Don't split prematurely.

### 5.3 Bubble Tea (TEA pattern)

- `Update(msg) (tea.Model, tea.Cmd)` — pure function, mutates model state only; returns `tea.Cmd` for async.
- `View() string` — pure render of model state, no side effects.
- `tea.Cmd` is a closure; runtime executes it in a goroutine, results return as `tea.Msg`.
- Self-loop pattern: each `OutputMsg` triggers the next `readOutput()` until EOF.
- Throttle renders (e.g. `tea.Tick(50ms, ...)`); never redraw per byte.

### 5.4 Terminal Package Rules

PTY management + terminal emulation. For internals, read `internal/terminal/pane.go` (well-commented).

**Rules**:

- Never write to a PTY without checking liveness (closed fd → panic).
- Always handle resize (`pty.Setsize`); skipping it corrupts the display.
- Don't render per output byte — throttle.
- Always close PTY file descriptors (defer after `pty.Start`).
- Don't assume the emulator handles every escape sequence; some need manual parsing.
- Render state must use a `dirty` flag; returning a cached view freezes the UI (see `handleOutput` regression test).

**Concurrency invariants** (see top-of-file doc-comment in `pane.go` for the full diagram):

- `p.mu` is the ONLY shared lock. Never introduce a second mutex for fields already protected by `p.mu` — it'll deadlock with itself.
- The alt-screen callback fires synchronously from inside `vt.Write()` (called by `handleOutputLocked` with `p.mu` held). The callback MUST NOT take `p.mu`. It sends to `altScreenActiveCh` non-blockingly; `altScreenConsumer` applies the update under `p.mu`.
- `p.altScreenActiveCh` is initialized **once** in `New()` and never re-assigned. The callback and consumer share the same channel reference.
- `inputDrain` does NOT hold `p.mu`. It reads from `vt.Read()` independently to prevent the x/vt internal pipe from blocking `vt.Write`.
- `Stop()` releases `p.mu` BEFORE closing channels. Closing channels while holding `p.mu` deadlocks the consumer.
- `stopOnce` (sync.Once) makes `Stop()` idempotent. Don't replace with a bool check.

### 5.5 Ticket State Machine

Ticket status transitions are validated by `Ticket.CanTransitionTo(target TicketStatus) error` in `internal/board/board.go`. The state machine:

```
              ┌─────────────────────────────┐
              ▼                             │
backlog ⇄ in_progress ──► done ─────────────┤
                │           │               │
                └───────────┴──► archived ◄──┘
                              (terminal)
```

- **`backlog` ⇄ `in_progress`**: allowed in both directions. `in_progress → backlog` with `AgentStatus == AgentWorking` is BLOCKED (would orphan the running pi).
- **`in_progress` → `done`**: allowed; marks `CompletedAt`.
- **`done` → `backlog` or `done` → `in_progress`**: allowed (user reopens/restarts).
- **Any → `archived`**: allowed.
- **`archived` → ***: FORBIDDEN (archived is terminal). `archived → archived` is a no-op.

UI surfaces rejections via the existing notification toast:
```go
if err := m.globalStore.Move(ticket.ID, target); err != nil {
    m.notify("Move rejected: " + err.Error())
    return m, nil
}
```

---

## 6. Pre-Commit

```bash
go build -o awp .               # 0 errors
go vet ./...                    # 0 warnings
go test ./...                   # all pass
go test -race ./internal/pi ./internal/terminal ./internal/ui
```

If any check fails, fix it before commit. Don't `--no-verify` past these.