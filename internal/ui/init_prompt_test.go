package ui

import (
	"strings"
	"testing"

	"github.com/pi/awp/internal/board"
	"github.com/pi/awp/internal/config"
	"github.com/pi/awp/internal/project"
)

// TestPrepareSpawn_RendersInitPromptTemplate verifies the
// InitPrompt template ({{.Title}}, {{.Description}}, etc.)
// is rendered with actual ticket data before being passed to pi.
//
// User reported: pi received "**Ticket Title:** {{.Title}}" instead
// of the actual title. This is because the template wasn't rendered
// before being passed as --append-system-prompt.
func TestPrepareSpawn_RendersInitPromptTemplate(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("AWP_CONFIG_DIR", tmpDir)
	// initGitRepo is provided in spawn_args_test.go; reuse it
	if err := initGitRepoForTest(t, tmpDir); err != nil {
		t.Fatalf("init git repo: %v", err)
	}

	cfg := config.DefaultConfig()
	// DefaultConfig's InitPrompt uses {{.Title}}, {{.Description}},
	// {{.BranchName}}, {{.BaseBranch}} as Go template variables.

	registry := &project.ProjectRegistry{
		Projects: map[string]*project.Project{},
	}
	p := project.NewProject("test", tmpDir)
	registry.Projects[p.ID] = p
	gts, err := project.LoadGlobalTicketStore(registry)
	if err != nil {
		t.Fatalf("LoadGlobalTicketStore: %v", err)
	}

	ticket := board.NewTicket("My specific ticket title", p.ID)
	ticket.Description = "A detailed description of the task."
	ticket.UseWorktree = false
	ticket.BranchName = "task/my-branch"
	ticket.BaseBranch = "main"
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
		t.Fatalf("prepareSpawn returned %T, want spawnReadyMsg", msg)
	}

	// Find the --append-system-prompt arg and the value after it.
	var promptArg string
	for i, arg := range ready.args {
		if arg == "--append-system-prompt" && i+1 < len(ready.args) {
			promptArg = ready.args[i+1]
			break
		}
	}
	if promptArg == "" {
		t.Fatalf("--append-system-prompt not found in args: %v", ready.args)
	}

	// CRITICAL: none of the {{.X}} placeholders should remain.
	for _, placeholder := range []string{"{{.Title}}", "{{.Description}}", "{{.BranchName}}", "{{.BaseBranch}}"} {
		if strings.Contains(promptArg, placeholder) {
			t.Errorf("prompt still contains %q — template not rendered: %s",
				placeholder, promptArg)
		}
	}

	// Sanity: actual ticket data should be in the rendered prompt.
	if !strings.Contains(promptArg, "My specific ticket title") {
		t.Errorf("rendered prompt missing actual title. Got: %s", promptArg)
	}
	if !strings.Contains(promptArg, "A detailed description") {
		t.Errorf("rendered prompt missing actual description. Got: %s", promptArg)
	}
	if !strings.Contains(promptArg, "task/my-branch") {
		t.Errorf("rendered prompt missing branch name. Got: %s", promptArg)
	}
}

// TestPrepareSpawn_RendersEffectiveBranchForNewTicket pins the
// contract that the InitPrompt sent to pi reflects what awp will
// ACTUALLY do — including the branch name generated from the
// ticket title and the base branch resolved from git defaults.
//
// User reported: after spawning an agent, the ticket info passed
// to pi was limited / didn't work. The root cause is that
// prepareSpawn generates `branchName` and `baseBranch` locally
// (from the title slug and git default branch respectively) but
// only stores them in local variables — the ticket pointer passed
// to renderInitPrompt still has empty BranchName/BaseBranch. So
// for any ticket created without an explicit BranchName, the
// rendered InitPrompt has empty {{.BranchName}} and {{.BaseBranch}}
// placeholders, even though the actual worktree was created using
// the generated values.
//
// This test creates a ticket WITHOUT setting BranchName/BaseBranch
// (the realistic new-ticket case) and asserts that the generated
// values appear in the rendered prompt.
func TestPrepareSpawn_RendersEffectiveBranchForNewTicket(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("AWP_CONFIG_DIR", tmpDir)
	if err := initGitRepoForTest(t, tmpDir); err != nil {
		t.Fatalf("init git repo: %v", err)
	}

	cfg := config.DefaultConfig() // default InitPrompt uses {{.BranchName}}, {{.BaseBranch}}

	registry := &project.ProjectRegistry{
		Projects: map[string]*project.Project{},
	}
	p := project.NewProject("test", tmpDir)
	registry.Projects[p.ID] = p
	gts, err := project.LoadGlobalTicketStore(registry)
	if err != nil {
		t.Fatalf("LoadGlobalTicketStore: %v", err)
	}

	// New ticket — no explicit BranchName/BaseBranch set.
	// This is the realistic case that exposes the bug.
	ticket := board.NewTicket("Add user auth flow", p.ID)
	ticket.Description = "Implement OAuth login + logout."
	ticket.UseWorktree = false
	if err := gts.Add(ticket); err != nil {
		t.Fatalf("gts.Add: %v", err)
	}

	// Sanity: the ticket starts with empty branch info (the bug surface).
	if ticket.BranchName != "" {
		t.Fatalf("preconditions: ticket.BranchName should start empty, got %q", ticket.BranchName)
	}
	if ticket.BaseBranch != "" {
		t.Fatalf("preconditions: ticket.BaseBranch should start empty, got %q", ticket.BaseBranch)
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

	// Find --append-system-prompt value.
	var promptArg string
	for i, arg := range ready.args {
		if arg == "--append-system-prompt" && i+1 < len(ready.args) {
			promptArg = ready.args[i+1]
			break
		}
	}
	if promptArg == "" {
		t.Fatalf("--append-system-prompt not found in args: %v", ready.args)
	}

	// The effective branch and base should match what awp will actually
	// use to create the worktree (returned in spawnReadyMsg).
	wantBranch := ready.branchName
	wantBase := ready.baseBranch
	if wantBranch == "" {
		t.Fatal("preconditions: spawnReadyMsg.branchName should be non-empty after prepareSpawn")
	}
	if wantBase == "" {
		t.Fatal("preconditions: spawnReadyMsg.baseBranch should be non-empty after prepareSpawn")
	}

	// CORE ASSERTION: the rendered InitPrompt must contain the
	// effective branch name, not the (empty) ticket.BranchName.
	if !strings.Contains(promptArg, wantBranch) {
		t.Errorf("rendered prompt missing EFFECTIVE branch name %q. "+
			"This means {{.BranchName}} rendered as empty even though "+
			"awp created the worktree on %q. Pi gets a misleading prompt. "+
			"Got: %s",
			wantBranch, wantBranch, promptArg)
	}
	if !strings.Contains(promptArg, wantBase) {
		t.Errorf("rendered prompt missing EFFECTIVE base branch %q. "+
			"This means {{.BaseBranch}} rendered as empty even though "+
			"awp forked the worktree from %q. Pi gets a misleading prompt. "+
			"Got: %s",
			wantBase, wantBase, promptArg)
	}

	// Belt-and-suspenders: the placeholders must not survive.
	for _, placeholder := range []string{"{{.BranchName}}", "{{.BaseBranch}}"} {
		if strings.Contains(promptArg, placeholder) {
			t.Errorf("prompt still contains %q — template not rendered: %s",
				placeholder, promptArg)
		}
	}
}
