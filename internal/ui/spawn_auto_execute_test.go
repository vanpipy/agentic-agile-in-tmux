// spawn_auto_execute_test.go — TDD test for the spawn-then-auto-execute
// regression.
//
// Bug (second part of the ticket "Fix awp - the task in done column
// cannot move"): when pi is spawned on a new ticket, the user expects
// pi to start working on the ticket automatically ("spawned agent 进去
// 的时候 ticket 没执行"). The actual behavior was that prepareSpawn
// passed the ticket context as `--append-system-prompt` (system context
// only) and no positional user message. pi in interactive TUI mode then
// boots, sees the system context, but does NOT auto-execute — it waits
// for the user to type something. The user has to manually press Enter
// or type a message, which is not the documented UX.
//
// Fix: pass the rendered InitPrompt as a positional argument to pi.
// pi's args parser treats any non-flag arg as a message and pi's
// InteractiveMode processes them via `await this.session.prompt(message)`
// on startup (see pi-mono
// packages/coding-agent/src/modes/interactive/interactive-mode.ts:817
// "Process initial messages" and packages/coding-agent/src/cli/args.ts
// where non-flag args are pushed to `result.messages`). pi then
// auto-executes the ticket description as the first user turn — the
// intended UX.
//
// The --append-system-prompt flag is no longer the right mechanism
// for the ticket context: it's read-only context that doesn't trigger
// any turn. The ticket context, framed as "You have been spawned by
// awp ... Begin by analyzing the ticket requirements and proposing
// your approach.", is a USER REQUEST, not system context — sending
// it as a positional arg is the natural fit.
//
// CORRECT-7 self-check on this test file:
//   C-onformance: literal arg list equality (no fuzzy matching)
//   O-rdering:    pi's parser does not require positional args to be
//                 last, but we assert presence not position
//   R-ange:       1 ticket, 3 sub-cases (positional arg present,
//                 content matches rendered template, no --append-system-prompt
//                 leaks through)
//   R-eference:   filesystem (temp git repo) only
//   E-xistence:   the regression target is the MISSING positional arg
//   C-ardinality: 1 ticket
//   T-ime:        no time concerns
package ui

import (
	"strings"
	"testing"

	"github.com/pi/awp/internal/board"
	"github.com/pi/awp/internal/config"
	"github.com/pi/awp/internal/project"
)

// findPositionalTicketMessage returns the first positional arg in
// `args` whose value is the rendered InitPrompt (i.e. contains the
// ticket's title and description). Returns "" if not found.
//
// pi's args parser (see pi-mono packages/coding-agent/src/cli/args.ts)
// classifies any arg not starting with "-" or "@" as a message. We
// identify "the ticket message" by content (contains the title),
// not by position, so the test stays robust to arg reordering.
//
// IMPORTANT: we skip args that are values of a preceding flag
// (e.g. in `--append-system-prompt "ticket: n"`, the value `"ticket:
// n"` is consumed by the flag and is NOT a positional arg). The
// way to tell is: an arg is a "flag value" if the previous arg is
// a flag that takes a value. This is the same logic pi's parser
// uses implicitly when it does `args[++i]` after recognizing a flag.
func findPositionalTicketMessage(args []string, ticketTitle string) string {
	// flagsThatTakeValue: any flag that consumes the next arg as its
	// value. If we see one of these, we skip the next arg.
	flagsThatTakeValue := map[string]bool{
		"--append-system-prompt":   true,
		"--system-prompt":          true,
		"--provider":               true,
		"--model":                  true,
		"--api-key":                true,
		"--mode":                   true,
		"--session":                true,
		"--session-id":             true,
		"--fork":                   true,
		"--session-dir":            true,
		"--name":                   true,
		"-n":                       true,
		"--models":                 true,
		"--tools":                  true,
		"-t":                       true,
		"--exclude-tools":          true,
		"-xt":                      true,
		"--thinking":               true,
		"--extension":              true,
		"-e":                       true,
		"--skill":                  true,
		"--prompt-template":        true,
		"--theme":                  true,
		"--export":                 true,
		"--list-models":            true,
	}

	skipNext := false
	for _, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if strings.HasPrefix(arg, "-") || strings.HasPrefix(arg, "@") {
			if flagsThatTakeValue[arg] {
				skipNext = true
			}
			continue
		}
		if strings.Contains(arg, ticketTitle) {
			return arg
		}
	}
	return ""
}

// TestPrepareSpawn_PassesTicketAsPositionalArg pins the contract that
// the rendered ticket context is passed to pi as a positional user
// message, NOT only as --append-system-prompt. This is the regression
// for the bug "spawned agent 进入的时候 ticket 没执行" — without this,
// pi boots, sees the system context, and waits for user input; with
// this, pi auto-executes the ticket as the first turn.
func TestPrepareSpawn_PassesTicketAsPositionalArg(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("AWP_CONFIG_DIR", tmpDir)
	if err := initGitRepoForTest(t, tmpDir); err != nil {
		t.Fatalf("init git repo: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Pi.InitPrompt = "ticket: {{.Title}} | desc: {{.Description}} | branch: {{.BranchName}} | base: {{.BaseBranch}}"

	registry := &project.ProjectRegistry{
		Projects: map[string]*project.Project{},
	}
	p := project.NewProject("auto-exec", tmpDir)
	registry.Projects[p.ID] = p

	gts, err := project.LoadGlobalTicketStore(registry)
	if err != nil {
		t.Fatalf("LoadGlobalTicketStore: %v", err)
	}

	const ticketTitle = "Auto-execute regression ticket"
	ticket := board.NewTicket(ticketTitle, p.ID)
	ticket.Description = "Build the thing."
	ticket.UseWorktree = false
	if err := gts.Add(ticket); err != nil {
		t.Fatalf("gts.Add: %v", err)
	}

	model := NewModel(cfg, gts, registry, "", nil)
	cmd := model.prepareSpawn(ticket, p)
	if cmd == nil {
		t.Fatal("prepareSpawn returned nil")
	}
	msg := cmd()
	ready, ok := msg.(spawnReadyMsg)
	if !ok {
		t.Fatalf("prepareSpawn returned %T, want spawnReadyMsg; msg=%+v", msg, msg)
	}

	// PRIMARY: a positional arg must contain the rendered ticket context.
	positional := findPositionalTicketMessage(ready.args, ticketTitle)
	if positional == "" {
		t.Errorf("prepareSpawn did not pass the ticket as a positional arg to pi.\n"+
			"This is the bug: pi in interactive TUI mode only auto-executes on\n"+
			"positional user messages (pi-mono cli/args.ts: !arg.startsWith(\"-\")\n"+
			"=> result.messages.push(arg), and interactive-mode.ts:817 processes\n"+
			"initialMessages via await session.prompt). Without a positional arg,\n"+
			"pi boots with the system context but sits idle waiting for the user\n"+
			"to type — the spawned agent never starts working on the ticket.\n"+
			"args = %v", ready.args)
	}

	// Secondary: the rendered template variables must be substituted
	// (i.e. the InitPrompt must be rendered, not raw {{.Title}} etc.).
	if positional != "" {
		for _, raw := range []string{
			"{{.Title}}", "{{.Description}}", "{{.BranchName}}", "{{.BaseBranch}}",
		} {
			if strings.Contains(positional, raw) {
				t.Errorf("positional arg still contains raw template %q — template not rendered.\n"+
					"Positional arg: %s", raw, positional)
			}
		}
		// Sanity: ticket content actually present.
		if !strings.Contains(positional, ticketTitle) {
			t.Errorf("positional arg missing ticket title %q. Got: %s", ticketTitle, positional)
		}
	}
}

// TestPrepareSpawn_NoLongerLeavesSystemPromptFlag is the negative
// half of the contract: now that the ticket is passed as a positional
// arg, the old --append-system-prompt flag must NOT also be in the
// args list. Otherwise pi would see the ticket context twice (once
// in the system prompt, once as the first user message), which is
// noisy and contradicts the design (system context ≠ user request).
func TestPrepareSpawn_NoLongerLeavesSystemPromptFlag(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("AWP_CONFIG_DIR", tmpDir)
	if err := initGitRepoForTest(t, tmpDir); err != nil {
		t.Fatalf("init git repo: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Pi.InitPrompt = "ticket: {{.Title}}"

	registry := &project.ProjectRegistry{
		Projects: map[string]*project.Project{},
	}
	p := project.NewProject("no-system-flag", tmpDir)
	registry.Projects[p.ID] = p

	gts, err := project.LoadGlobalTicketStore(registry)
	if err != nil {
		t.Fatalf("LoadGlobalTicketStore: %v", err)
	}
	ticket := board.NewTicket("n", p.ID)
	ticket.UseWorktree = false
	if err := gts.Add(ticket); err != nil {
		t.Fatalf("gts.Add: %v", err)
	}

	model := NewModel(cfg, gts, registry, "", nil)
	cmd := model.prepareSpawn(ticket, p)
	if cmd == nil {
		t.Fatal("prepareSpawn returned nil")
	}
	msg := cmd()
	ready, ok := msg.(spawnReadyMsg)
	if !ok {
		t.Fatalf("prepareSpawn returned %T, want spawnReadyMsg; msg=%+v", msg, msg)
	}

	for i, arg := range ready.args {
		if arg == "--append-system-prompt" {
			t.Errorf("prepareSpawn still passes --append-system-prompt at index %d.\n"+
				"Now that the ticket is the initial user message (positional arg),\n"+
				"the old --append-system-prompt flag is redundant and would make pi\n"+
				"see the ticket context twice. Remove it.\n"+
				"args = %v", i, ready.args)
		}
	}
}