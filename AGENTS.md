# AGENTS.md

> Guide for AI coding agents: design principles, golden rules, and technical reference.
> Not a project plan or status snapshot.

---

## 1. Project

`awp` is a **pi-only** TUI multi-session kanban: run N pi sessions in parallel, watch and control them from one interface. No multi-agent abstraction.

**Philosophy**: depth over breadth. Every pi capability gets a UI; other agents are out of scope.

The codebase started as a fork of an early multi-agent kanban template and has since grown into an independent project (own config dir at `~/.config/awp/`).

---

## 2. Design Principles

- **Protocol over bytes** — use pi's `--mode rpc` JSONL, never screen-scrape.
- **Copy over invention** — the fork template is shipped; don't rewrite it.
- **Consumer, not modifier** — pi is a black box (only `--mode rpc` + extensions, never fork).
- **Explicit over implicit** — state derives from events, not guesses.
- **Opt-in over mandatory** — interception is off by default.
- **Bounded scope** — one ticket = one session = one worktree; parallelism isn't required.
- **Plan-then-build** — `SYSTEM_DESIGN.md` is the source of truth; update it before code.
- **TDD non-negotiable** — RED → GREEN → REFACTOR; no tests, no commit.
- **Completeness over simplicity** — never ship a "simplified version" (it becomes permanent). Copy fully, don't cherry-pick.

### 2.1 Anti-Shortcuts (Mandatory)

If these rationalizations appear in your head, stop immediately:

| Excuse | Why forbidden |
|--------|---------------|
| "Simplified version now, extend later" | Simplified = permanent. Re-architecting costs 10×. |
| "Just copy `terminal.Pane`, UI is too long" | UI is the product. Missing UI = missing core. |
| "90% unused, skip it" | You don't know which 10% blocks which user. |
| "Make build pass, adapt later" | Build passing ≠ working. Stubs and unsafe-casts are tech-debt bombs. |
| "Main flow first, edges later" | Edges are the real scenarios. |
| "Template has 3040 lines, we need 500" | You don't know why the other 2540 exist. |
| "Template uses A/B/C, we don't, so delete" | It took N months to prove A/B/C. You've spent 0. |

**Iron rules**:

1. System design must be complete. No "omit for now" — fill the gap.
2. Copy = full copy. Original 3040 + 1934 lines = that many lines, no "trimmed version".
3. Stubs/simplifications/unsafe-casts are implementation debt — mark with `DEBT:` in the commit message.
4. Unsure whether to simplify → ask. Don't unilaterally decide "this is fine".
5. Already shortcutting → stop immediately + `git reset --hard HEAD` + replan.

**Litmus test**: if a user asks "did you implement X?", can you answer 100% yes? "Yes, simplified" = fail.

---

## 3. Practices

### Code
- Names express intent; `pkg.Func` reads self-evidently.
- Wrap errors with context: `fmt.Errorf("ctx: %w", err)`. Preserve sentinels.
- Public APIs have godoc.
- Single responsibility; split when you can.
- Strong types over `interface{}` (avoid `any`).
- No `panic` for recoverable errors.
- `Update()` never blocks — async via `tea.Cmd`.

### Test
- RED → GREEN → REFACTOR, no skipping.
- Naming: `TestXxx_Scenario_Expected` (e.g. `TestDecide_BlacklistedCommand_Suspend`).
- One assertion per test, clear failure messages.
- Integration tests use `//go:build integration`.
- Race-detect critical modules: `go test -race ./internal/pi ./internal/agent`.
- Coverage target: core logic 80%+.

### Commit
- Conventional Commits: `feat:` `fix:` `refactor:` `docs:` `test:` `chore:`.
- Breaking: `feat!: ...` (bang before colon).
- **No AI co-authorship** — repo history stays human.
- Commit message explains **why**, not what (diff speaks for itself).

### Design Deviation
- Any deviation from `SYSTEM_DESIGN.md` updates the doc first.
- Write a markdown note explaining "why A, not B" (attach to PR/issue).
- Unsure → stop and ask. Don't guess.

---

## 4. Golden Rules (no exceptions)

1. **PTY, not tmux** — pi runs in a PTY; proven in the fork.
2. **Use pi's RPC protocol** — `--mode rpc` JSONL is the de facto standard.
3. **Interception via pi extension** — `ui.confirm` is the official mechanism; never SIGSTOP.
4. **Pi only** — no multi-agent abstraction, no `Agent` interface.
5. **Never modify pi source** — pi is a black box.
6. **`Update()` never blocks** — all I/O via `tea.Cmd` in goroutines.
7. **Doc before code** — `SYSTEM_DESIGN.md` is the source of truth.
8. **No unfaithful copies** — match the template's line counts; stubs marked DEBT.
9. **CORRECT self-check on every unit test** before commit:
   - **C**onformance — output = literal expected (not "non-empty").
   - **O**rdering — don't depend on map iteration order.
   - **R**ange — 0/1/max/overflow/negative boundaries.
   - **R**eference — external deps (file/network/time) handled.
   - **E**xistence — empty/nil/missing paths.
   - **C**ardinality — 0/1/N cases.
   - **T**ime — timeout/race/expiry.

   SKIP requires TODO + linked issue. SKIP doesn't count as coverage.

---

## 5. Technical Reference

### 5.1 pi (the integrated agent)

- `pi --mode rpc`: JSONL on stdin/stdout.
- Sessions: `~/.pi/agent/sessions/{encoded-cwd}/*.jsonl`.
- Fixed tool names: `bash` `read` `edit` `write` `find` `grep` `ls`.
- Extension hooks (use exactly): `tool_call` `tool_result` `user_bash` `input` `confirm` `select` `agent_start` `agent_end`.
- Event stream: `agent_start`, `turn`, `message`, `tool_execution`, `compaction`, `auto_retry`, `queue_update`.
- Source of truth: `pi-mono/packages/coding-agent/src/modes/rpc/rpc-types.ts`.

### 5.2 Architecture Template (forked from early multi-agent kanban)

- **UI** lives entirely in `internal/ui/model.go` (~3040 lines) + `view.go` (~1934 lines); no sub-package.
- **PTY terminal** in `internal/terminal/` (~1,250 lines: pty + vt10x + scrollback + selection).
- **Data model** in `internal/board/` (Ticket/Column, ~160 lines).
- **Themes** in `internal/config/theme.go` (8 presets).
- **Pattern**: single Model + 13-mode state machine. `Update()` switches on `msg` at the top; `View()` branches on mode, calling `renderXxx()`.
- **Boot**: `tea.NewProgram(model, tea.WithAltScreen())`.
- **Source paths**: `internal/{ui,terminal,board,config}/`.

**Copy rules** (already internalized):

- All imports use `github.com/pi/awp/internal/...`.
- Line-count delta ≤ 100 (allows for 3-5 awp-specific modes + Model fields).
- Delta > 100 → stop and audit what was missed.

### 5.3 Bubble Tea (TEA)

- `Update(msg) (tea.Model, tea.Cmd)` — pure function, mutates model state only; returns `tea.Cmd` for async.
- `View() string` — pure render of model state, no side effects.
- `tea.Cmd` is a closure; runtime executes it in a goroutine, results return as `tea.Msg`.
- Self-loop pattern: each `OutputMsg` triggers the next `readOutput()` until EOF.
- Render throttling: `tea.Tick(50ms, ...)` to avoid per-byte redraws.
- Sub-components: wrap as independent `tea.Model` (e.g. `ui/eventpane/model.go`).

### 5.4 Go

- Strong types over `interface{}`.
- Errors must be wrapped with context.
- `goroutine` + `channel` for concurrency; avoid mutex.
- Standard library first; justify third-party deps.
- `go vet ./...` must report 0 warnings.

### 5.5 Terminal Package (PTY + vt10x)

PTY management and terminal emulation for agent processes.

#### Core Components

- **Pane** — manages single PTY + virtual terminal.
- **ScrollbackBuffer** — ring buffer for history (default 10k lines).
- **SelectionState** — text-selection state machine.

#### PTY Handling

Uses `creack/pty`:

```go
pty.Start(cmd)      // spawn with PTY
pty.Setsize(f, ws)  // resize
```

#### Terminal Emulation

Uses `vt10x` for escape-sequence parsing:
- Cursor management, cell-based rendering, color/attribute handling.

#### Message Types (BubbleTea integration)

- `OutputMsg` — new terminal output.
- `ExitMsg` — process terminated.
- `RenderTickMsg` — throttled render trigger.

#### Rendering

- Throttled at 50ms intervals.
- `dirty` flag tracks re-render need.
- Cached view string until dirty.

#### Key Translation

`translateKey()` converts BubbleTea `KeyMsg` to PTY bytes:
- Arrow keys → escape sequences.
- `Ctrl+C` → 0x03.
- `Enter` → `\r`.

#### Environment

`buildCleanEnv()`:
- Sets `TERM=xterm-256color`.
- Strips agent-related env vars.
- Preserves `PATH`, `HOME`, `USER`.

#### Escape Sequence Detection

Byte-scanning for mode switches:
- Mouse mode: `\x1b[?1000h`.
- Alt screen: `\x1b[?1049h`.

#### Anti-Patterns

- Don't write to PTY without checking liveness.
- Don't skip resize handling — causes display corruption.
- Don't render per output — use throttling.
- Don't leak PTY file descriptors — always close.
- Don't assume vt10x handles all sequences — some need manual parsing.

---

## 6. Escalation

1. **Design** → `SYSTEM_DESIGN.md`.
2. **Protocol** → `pi-mono` source (source over blog).
3. **Architecture** → `internal/{ui,terminal}/` (do what they do unless there's a reason).
4. **Spec** → `.specify/memory/constitution.md` (TDD / godoc / vet).
5. **Still no answer** → write an issue-style markdown, **stop and ask**. Don't guess.
6. **Tempted to "simplify"** → re-read §2.1, confirm you're not violating completeness.

---

## 7. Pre-Commit Checks

```bash
go build -o awp .
go vet ./...                              # 0 warnings
go test ./...
go test -race ./internal/pi ./internal/agent
```

All 4 must pass + commit message must explain why.