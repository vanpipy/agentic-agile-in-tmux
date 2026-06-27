# awp — agentic-with-pi

TUI kanban for running multiple [pi](https://github.com/earendil-works/pi-coding-agent) sessions in parallel. One git worktree per ticket, one view to watch and control them all.

Pi only. No multi-agent abstraction, no screen scraping. Built on pi's `--mode rpc` JSONL protocol.

## Install

```bash
go install github.com/pi/awp@latest
```

Requires `pi` on `$PATH`.

## First Run

```bash
awp doctor                              # 7-point self-check
cd ~/your-project && awp project new myproject
awp                                     # launch TUI
```

TUI keys: `j/k` move, `h/l` switch column, `s` start pi, `Enter` view events, `P` pick session, `q` quit, `?` help.

Interception popup: `Y` approve, `N` deny, `A` always allow, `Esc` cancel.

## Commands

```bash
awp                              # launch TUI
awp project new [name]           # register current dir
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

## Interception (optional, off by default)

With this on, every pi tool call needs your approval.

Edit `~/.config/awp/interception.json`:

```json
{
  "enabled": true,
  "block_patterns": ["rm -rf /*", "sudo *", "* /etc/passwd"],
  "allow_patterns": ["ls *", "cat *", "pwd"]
}
```

Simple glob, not regex. `allow_patterns` wins on overlap. Legacy `blacklist`/`whitelist` fields still work.

## Architecture

- **Bubble Tea** TUI (single Model, 8-mode state machine).
- **creack/pty** for the pi subprocess — no tmux.
- **pi `--mode rpc`** for JSONL events — no screen scraping.
- **cobra** for CLI, **Lip Gloss** for theming (20 themes).

Source layout:

```
cmd/awp/                 # cobra CLI
internal/{pi,agent,ui,app,config,doctor,git,project,board,terminal,observability,buildinfo}/
test/{sub}/              # integration (//go:build integration)
e2e/                     # end-to-end (//go:build e2e, needs real binaries)
```

Full design: `SYSTEM_DESIGN.md`. Spec audit: `CROSS_VALIDATION.md`.

## Development

```bash
make build                       # local debug
make release VERSION=0.2.0       # versioned release
go test ./...                    # unit tests
go test -race ./...              # race detector
go test -tags integration ./test/...
go test -tags e2e ./e2e/...
go test -cover ./...             # coverage
go vet ./...                     # static checks
cd internal/pi/extension && bun test  # TypeScript extension
```

Read `AGENTS.md` (design rules) and `SYSTEM_DESIGN.md` before changing code. Update the design doc first. TDD: red → green → refactor.

## Status

All 6 phases complete. Audit 94/100 (0 critical, 0 major), 357 tests, 68% coverage, `go vet` clean, 16M binary.

Optional follow-ups: 5 deferred UI modes, `auto_approve_after_seconds`, real integration tests.

## Contributing

Read `AGENTS.md`. Pre-commit:

```bash
go build -o awp . && go vet ./... && go test ./... && go test -race ./internal/...
```

Conventional Commits. No AI co-authorship.

## License

MIT
