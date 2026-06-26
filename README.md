# awp — agentic-with-pi

> TUI kanban for running multiple [pi](https://github.com/earendil-works/pi-coding-agent) sessions in parallel — one git worktree per ticket, one unified view to watch and control everything.

```
┌──────────────────┬──────────────────────────────────────────┐
│ Projects         │  Backlog  │ In Progress │ Done           │
│  awp             │  tkt-1    │  Resume ..  │ tkt-3          │
│                  │  tkt-2    │  [pi: run]  │                │
└──────────────────┴──────────────────────────────────────────┘
```

## What This Is / Isn't

| Is | Isn't |
|----|-------|
| A TUI kanban | An IDE or editor |
| A multi-task orchestrator for pi | A multi-agent abstraction layer (pi only) |
| A live view of pi's running state | An offline task system |
| Built on pi's `--mode rpc` protocol | Built on screen-scrape polling |

**Design philosophy**: depth over breadth. Pi gets the deep integration; we don't generalize to other agents.

## 30-Second Onboarding

```bash
go install github.com/pi/awp@latest     # install
awp doctor                              # 7-point self-check
cd ~/your-project && awp project new myproject
awp                                     # launch TUI
# j/k: select ticket • s: start pi • Enter: view events • P: pick session
```

Requires `pi` on `$PATH`.

## Commands

```bash
awp                              # launch TUI

awp project new [name]           # register current dir as project
awp project list                 # list projects
awp ticket list                  # list all tickets

awp session list .               # list pi sessions
awp session show <id>            # session details
awp session export <id> -f html  # export (HTML/Markdown)

awp interception status          # interception config
awp doctor [--fix]               # self-check
awp theme list / set dracula     # 20 themes
awp version                      # version + commit + build date
awp --debug ...                  # verbose logs
```

## TUI Keys

| Key | Action | Key | Action |
|-----|--------|-----|--------|
| `j/k` | select up/down | `n` | new ticket |
| `h/l` | switch column | `s` | start pi |
| `Enter` | view event stream | `S` | stop pi |
| `P` | pick existing session | `?` | help |
| `q` | quit | | |

Interception popup: `Y` approve / `N` deny / `A` always allow / `Esc` cancel.

## Interception (optional, off by default)

**Warning**: with this on, every pi tool call needs your approval.

Edit `~/.config/awp/interception.json`:

```json
{
  "enabled": true,
  "block_patterns": ["rm -rf /*", "sudo *", "* /etc/passwd"],
  "allow_patterns": ["ls *", "cat *", "pwd"]
}
```

Simple glob, not regex. `allow_patterns` is matched first (matches on both sides = allowed). Legacy `blacklist`/`whitelist` fields still work.

## Architecture

- **Bubble Tea** for TUI (single Model + 8-mode state machine).
- **creack/pty** for pi subprocess (no tmux).
- **pi `--mode rpc`** for JSONL events (no screen scraping).
- **cobra** for CLI; **Lip Gloss** for styling (20 themes).

```
cmd/awp/                 # cobra CLI (7 sub-commands)
internal/
  pi/                    # RPC client + 30+ commands + 21 events + extension
  agent/                 # PiPane + state machine
  ui/                    # TUI (8 modes)
  app/                   # RunPanes + export (XSS-safe)
  config/                # config + themes + interception
  doctor/                # 7-point self-check
  git/                   # worktree management
  project/               # project registry + ticket store
  board/                 # Ticket + PiState
  terminal/              # PTY + vt10x
  observability/         # debug logging
  buildinfo/             # ldflags version injection
test/{sub}/              # integration tests (//go:build integration)
e2e/                     # end-to-end (//go:build e2e, needs real binaries)
```

Full design: `SYSTEM_DESIGN.md`. Spec-vs-impl audit: `CROSS_VALIDATION.md`.

## Development

```bash
make build                       # local debug build
make release VERSION=0.2.0       # versioned release

go test ./...                    # unit tests
go test -race ./...              # race detector
go test -tags integration ./test/...
go test -tags e2e ./e2e/...
go test -cover ./...             # coverage
go vet ./...                     # static checks

# TypeScript extension (interception)
cd internal/pi/extension && bun test
```

## Before Changing Code

1. Read `AGENTS.md` — design principles + golden rules.
2. Read `SYSTEM_DESIGN.md` — full design.
3. Names express intent; single-responsibility functions.
4. `Update()` never blocks — async via `tea.Cmd`.
5. Update the design doc before the code.
6. TDD: red → green → refactor, no skipping.

## Quality

| Metric | Value |
|--------|-------|
| Audit score | 94/100 (0 critical, 0 major) |
| Tests | 357 |
| Coverage | 68% avg (9 packages > 50%) |
| Race detector | clean |
| `go vet` | 0 warnings |
| Binary | 16M |

## Roadmap

All 6 phases complete: foundation → pi protocol → TUI → sessions → interception → hardening.

Next (optional):
- 5 deferred UI modes (Settings / About / CommandPalette / ThemePicker / Worktree).
- `auto_approve_after_seconds` (timeout auto-approve).
- Real integration tests (need pi binary).

## Contributing

Read `AGENTS.md` first. Pre-commit checks:

```bash
go build -o awp . && go vet ./... && go test ./... && go test -race ./internal/...
```

Conventional Commits. No AI co-authorship.

## License

MIT