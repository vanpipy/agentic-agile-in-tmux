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
