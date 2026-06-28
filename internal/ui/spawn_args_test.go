package ui

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/pi/awp/internal/board"
	"github.com/pi/awp/internal/config"
	"github.com/pi/awp/internal/project"
)

// TestPrepareSpawn_DoesNotPassPiInit guards the fix for the user
// report "first spawn 闪退": awp used to pass "--init <template>"
// to pi, which pi 0.80 rejects with "Unknown option: --init"
// and immediately exits. The fix changed --init → --append-system-prompt.
//
// This test exercises prepareSpawn() directly (the function that
// builds the spawn args) without spawning pi, asserting that
// --init is never in the produced args list.
//
// If a future refactor reintroduces --init (e.g., by copying the
// old openkanban-era code), this test fails immediately.
//
// Approach: use ticket.UseWorktree=false so we skip git worktree
// creation (the most fragile part of prepareSpawn). The remaining
// flow — building args from cfg.Pi.* — is what we're testing.
func TestPrepareSpawn_DoesNotPassPiInit(t *testing.T) {
	tmpDir := t.TempDir()
	if err := initGitRepoForTest(t, tmpDir); err != nil {
		t.Fatalf("init git repo: %v", err)
	}

	cfg := config.DefaultConfig()
	// Make InitPrompt non-empty so the prompt-injection branch runs.
	cfg.Pi.InitPrompt = "you are working on ticket TICKET-X"

	registry := &project.ProjectRegistry{
		Projects: map[string]*project.Project{},
	}
	p := project.NewProject("test-proj", tmpDir)
	registry.Projects[p.ID] = p

	gts, err := project.LoadGlobalTicketStore(registry)
	if err != nil {
		t.Fatalf("LoadGlobalTicketStore: %v", err)
	}
	ticket := board.NewTicket("hello", p.ID)
	ticket.UseWorktree = false // skip worktree branch
	if err := gts.Add(ticket); err != nil {
		t.Fatalf("gts.Add: %v", err)
	}

	model := NewModel(cfg, gts, registry, "", nil)

	cmd := model.prepareSpawn(ticket, p)
	if cmd == nil {
		t.Fatal("prepareSpawn returned nil cmd")
	}
	msg := cmd()

	// Could be spawnReadyMsg or spawnErrorMsg depending on flow.
	// We only care when it's spawnReadyMsg (the success path that
	// is exercised in production). spawnErrorMsg means a different
	// failure occurred that should be addressed separately.
	ready, ok := msg.(spawnReadyMsg)
	if !ok {
		t.Fatalf("prepareSpawn returned %T, want spawnReadyMsg; msg=%+v", msg, msg)
	}

	// Primary assertion: --init must NOT be in args.
	for i, arg := range ready.args {
		if arg == "--init" {
			t.Errorf("awp still passes --init to pi (at index %d). "+
				"pi 0.80 rejects --init → spawn flashes and exits. "+
				"Use --append-system-prompt instead.",
				i)
		}
		if strings.HasPrefix(arg, "--init=") {
			t.Errorf("awp passes --init=... form to pi: %q", arg)
		}
	}

	// Secondary assertion: when InitPrompt is set, the rendered prompt
	// must be passed as a positional arg (NOT as --append-system-prompt).
	// See TestPrepareSpawn_PassesTicketAsPositionalArg in
	// spawn_auto_execute_test.go for the contract that drives this.
	foundPositional := false
	for _, arg := range ready.args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if strings.Contains(arg, "TICKET-X") {
			foundPositional = true
			break
		}
	}
	if !foundPositional {
		t.Errorf("awp should pass the rendered InitPrompt as a positional arg "+
			"when InitPrompt is set; args = %v", ready.args)
	}

	// Tertiary assertion: --append-system-prompt must NOT be in args
	// (the ticket is now the initial user message; the old flag would
	// make pi see the ticket context twice).
	for _, arg := range ready.args {
		if arg == "--append-system-prompt" {
			t.Errorf("awp should NOT pass --append-system-prompt; the ticket "+
				"is now passed as a positional arg (the initial user message). "+
				"args = %v", ready.args)
		}
	}

	_ = p.ID
}

// initGitRepoForTest initializes a temp dir as a git repo so
// GetDefaultBranch works inside prepareSpawn.
func initGitRepoForTest(t *testing.T, dir string) error {
	t.Helper()
	cmds := [][]string{
		{"git", "init", "-q"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "config", "commit.gpgsign", "false"},
		{"git", "checkout", "-q", "-b", "main"},
		// Need a commit so the default branch resolves.
		{"git", "commit", "--allow-empty", "-q", "-m", "init"},
	}
	for _, args := range cmds {
		c := exec.Command(args[0], args[1:]...)
		c.Dir = dir
		if err := c.Run(); err != nil {
			return err
		}
	}
	return nil
}
