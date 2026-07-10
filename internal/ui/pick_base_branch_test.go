// pick_base_branch_test.go — TDD tests for the "pick original branch
// when creating a task" feature.
//
// Ticket: awp - pick original branch
//
// Description: "the default is main or master which cannot change with
// worktree or main repo". Users need a way to pick the base branch
// (the branch to fork off of) at ticket creation time, instead of
// awp auto-picking main/master via GetDefaultBranch().
//
// These tests pin the contract for the call sites that previously
// hardcoded `mgr.GetDefaultBranch()`:
//
//   - Model.setupWorktree
//   - Model.setupMainRepoBranch
//   - Model.saveTicketForm (form persistence of the picker's choice)
//
// All three must honor ticket.BaseBranch when set; fall back to
// GetDefaultBranch() when empty (backward compat).
package ui

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pi/awp/internal/board"
	"github.com/pi/awp/internal/config"
	"github.com/pi/awp/internal/project"
)

// makeTestModelWithRepo wires a Model around a real git repo so
// GetDefaultBranch() / ListLocalBranches() work end-to-end against
// actual git output. The test repo has TWO branches: "main" and
// "develop", so we can verify base-branch selection.
//
// Pattern: register the project BEFORE constructing NewModel so that
// GlobalTicketStore.ticketStores is populated at NewModel time
// (NewModel iterates store.Projects() to build worktreeMgrs).
func makeTestModelWithRepo(t *testing.T) (*Model, string) {
	t.Helper()
	repoDir := t.TempDir()
	initRealGitRepoWithTwoBranches(t, repoDir)

	t.Setenv("AWP_CONFIG_DIR", t.TempDir())
	cfg := config.DefaultConfig()
	reg, err := project.LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}

	// Register the project before creating the store/model so the
	// GlobalTicketStore's projects map is populated.
	proj := project.NewProject("test-repo", repoDir)
	proj.RepoPath = repoDir
	if err := reg.Add(proj); err != nil {
		t.Fatalf("reg.Add: %v", err)
	}

	store, err := project.LoadGlobalTicketStore(reg)
	if err != nil {
		t.Fatalf("LoadGlobalTicketStore: %v", err)
	}
	m := NewModel(cfg, store, reg, "", nil)
	m.width = 120
	m.height = 40

	return m, repoDir
}

// initRealGitRepoWithTwoBranches creates a git repo with both "main"
// and "develop" branches so the picker has multiple options.
func initRealGitRepoWithTwoBranches(t *testing.T, dir string) {
	t.Helper()
	cmds := [][]string{
		{"git", "init", "-q"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "config", "commit.gpgsign", "false"},
		{"git", "checkout", "-q", "-b", "main"},
		{"git", "commit", "--allow-empty", "-q", "-m", "init"},
		{"git", "checkout", "-q", "-b", "develop"},
		// Different commit so develop's tip differs from main's tip.
		{"git", "commit", "--allow-empty", "-q", "--allow-empty-message", "-m", "develop-commit"},
	}
	for _, args := range cmds {
		c := exec.Command(args[0], args[1:]...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", args, out, err)
		}
	}
	// Switch back to main so HEAD is stable.
	c := exec.Command("git", "checkout", "-q", "main")
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git checkout main: %v\n%s", err, out)
	}
}

// TestSetupWorktree_UsesTicketBaseBranch pins the new contract:
// setupWorktree must create the worktree forking off ticket.BaseBranch
// when set, NOT GetDefaultBranch(). Before the fix, mgr.GetDefaultBranch()
// was hardcoded; users had no way to base a ticket off "develop".
//
// We set ticket.BaseBranch = "develop", call setupWorktree, and
// assert the new branch's tip equals develop's tip (i.e., forked
// from develop, not main).
func TestSetupWorktree_UsesTicketBaseBranch(t *testing.T) {
	m, repoDir := makeTestModelWithRepo(t)

	projects := m.globalStore.Projects()
	if len(projects) != 1 {
		t.Fatalf("setup: want 1 project, got %d", len(projects))
	}
	proj := projects[0]

	ticket := board.NewTicket("Test ticket", proj.ID)
	ticket.UseWorktree = true
	// ★ The whole point: set BaseBranch to "develop" so the new branch
	// is forked off develop, NOT main (the default).
	ticket.BaseBranch = "develop"
	if err := m.globalStore.Add(ticket); err != nil {
		t.Fatalf("gts.Add: %v", err)
	}

	if err := m.setupWorktree(ticket); err != nil {
		t.Fatalf("setupWorktree: %v", err)
	}
	defer os.RemoveAll(ticket.WorktreePath)

	// CORE ASSERTION 1: ticket.BaseBranch round-tripped to the saved ticket.
	if ticket.BaseBranch != "develop" {
		t.Errorf("ticket.BaseBranch = %q, want %q", ticket.BaseBranch, "develop")
	}

	// CORE ASSERTION 2: the worktree was created (path non-empty).
	if ticket.WorktreePath == "" {
		t.Fatal("setupWorktree did not set WorktreePath")
	}

	// CORE ASSERTION 3: the new branch's tip == develop's tip (i.e.,
	// forked from develop, not main).
	newBranchTip := gitRevParse(t, repoDir, ticket.BranchName)
	developTip := gitRevParse(t, repoDir, "develop")
	mainTip := gitRevParse(t, repoDir, "main")
	if newBranchTip == mainTip {
		t.Errorf("new branch %q tip == main's tip (%s); ticket.BaseBranch=%q was ignored",
			ticket.BranchName, newBranchTip, ticket.BaseBranch)
	}
	if newBranchTip != developTip {
		t.Errorf("new branch %q tip = %s, want develop's tip %s (BaseBranch=%q)",
			ticket.BranchName, newBranchTip, developTip, ticket.BaseBranch)
	}
}

// TestSetupWorktree_FallsBackToDefaultWhenBaseBranchEmpty pins the
// backward-compat behavior: if ticket.BaseBranch is empty (legacy
// tickets or tickets created before this feature), setupWorktree
// must keep using GetDefaultBranch().
func TestSetupWorktree_FallsBackToDefaultWhenBaseBranchEmpty(t *testing.T) {
	m, repoDir := makeTestModelWithRepo(t)

	projects := m.globalStore.Projects()
	proj := projects[0]
	ticket := board.NewTicket("Legacy ticket", proj.ID)
	ticket.UseWorktree = true
	ticket.BaseBranch = "" // legacy: empty
	if err := m.globalStore.Add(ticket); err != nil {
		t.Fatalf("gts.Add: %v", err)
	}

	if err := m.setupWorktree(ticket); err != nil {
		t.Fatalf("setupWorktree: %v", err)
	}
	defer os.RemoveAll(ticket.WorktreePath)

	defaultBranch, err := m.worktreeMgrs[proj.ID].GetDefaultBranch()
	if err != nil {
		t.Fatalf("GetDefaultBranch: %v", err)
	}
	if ticket.BaseBranch != defaultBranch {
		t.Errorf("ticket.BaseBranch = %q, want default %q (fallback)",
			ticket.BaseBranch, defaultBranch)
	}

	newBranchTip := gitRevParse(t, repoDir, ticket.BranchName)
	defaultTip := gitRevParse(t, repoDir, defaultBranch)
	if newBranchTip != defaultTip {
		t.Errorf("new branch %q tip = %s, want %s's tip %s (fallback)",
			ticket.BranchName, newBranchTip, defaultBranch, defaultTip)
	}
}

// TestSetupMainRepoBranch_UsesTicketBaseBranch pins the parallel
// contract for the non-worktree (main-repo) branch setup path.
// setupMainRepoBranch must ALSO honor ticket.BaseBranch.
//
// Note: setupMainRepoBranch only sets ticket fields (BranchName,
// BaseBranch, WorktreePath). The actual branch creation happens later
// in prepareSpawn via mgr.SetupBranch(). This test verifies the field
// contract; the end-to-end branch creation is covered by the
// prepareSpawn test below.
func TestSetupMainRepoBranch_UsesTicketBaseBranch(t *testing.T) {
	m, _ := makeTestModelWithRepo(t)

	projects := m.globalStore.Projects()
	proj := projects[0]
	ticket := board.NewTicket("Main-repo ticket", proj.ID)
	ticket.UseWorktree = false
	ticket.BaseBranch = "develop"
	if err := m.globalStore.Add(ticket); err != nil {
		t.Fatalf("gts.Add: %v", err)
	}

	if err := m.setupMainRepoBranch(ticket); err != nil {
		t.Fatalf("setupMainRepoBranch: %v", err)
	}

	// CORE ASSERTION 1: BaseBranch round-tripped.
	if ticket.BaseBranch != "develop" {
		t.Errorf("ticket.BaseBranch = %q, want %q", ticket.BaseBranch, "develop")
	}
	// CORE ASSERTION 2: WorktreePath points at main repo (not a worktree).
	if ticket.WorktreePath != proj.RepoPath {
		t.Errorf("ticket.WorktreePath = %q, want main repo %q",
			ticket.WorktreePath, proj.RepoPath)
	}
	// CORE ASSERTION 3: BranchName was generated.
	if ticket.BranchName == "" {
		t.Error("ticket.BranchName should be set after setupMainRepoBranch")
	}
}

// TestPrepareSpawn_MainRepoForkedFromTicketBaseBranch verifies the
// end-to-end contract: when UseWorktree=false and BaseBranch="develop",
// prepareSpawn creates the branch off develop via mgr.SetupBranch.
//
// setupMainRepoBranch only sets fields; prepareSpawn is what actually
// forks the branch.
func TestPrepareSpawn_MainRepoForkedFromTicketBaseBranch(t *testing.T) {
	m, repoDir := makeTestModelWithRepo(t)

	projects := m.globalStore.Projects()
	proj := projects[0]
	ticket := board.NewTicket("Spawn-test", proj.ID)
	ticket.UseWorktree = false
	ticket.BaseBranch = "develop"
	ticket.Priority = 3
	if err := m.globalStore.Add(ticket); err != nil {
		t.Fatalf("gts.Add: %v", err)
	}

	// Run setupMainRepoBranch then prepareSpawn (mirrors the
	// spawn flow for the main-repo case).
	if err := m.setupMainRepoBranch(ticket); err != nil {
		t.Fatalf("setupMainRepoBranch: %v", err)
	}

	cmd := m.prepareSpawn(ticket, proj)
	if cmd == nil {
		t.Fatal("prepareSpawn returned nil cmd")
	}
	msg := cmd()
	defer func() {
		// Cleanup: delete the branch and reset HEAD to main.
		_ = exec.Command("git", "-C", repoDir, "branch", "-D", ticket.BranchName).Run()
		_ = exec.Command("git", "-C", repoDir, "checkout", "-q", "main").Run()
	}()

	ready, ok := msg.(spawnReadyMsg)
	if !ok {
		t.Fatalf("prepareSpawn returned %T, want spawnReadyMsg; msg=%+v", msg, msg)
	}

	// CORE ASSERTION: ready.baseBranch reflects the ticket's pick.
	if ready.baseBranch != "develop" {
		t.Errorf("spawnReadyMsg.baseBranch = %q, want %q",
			ready.baseBranch, "develop")
	}

	// CORE ASSERTION: the branch was actually created off develop.
	newBranchTip := gitRevParse(t, repoDir, ticket.BranchName)
	developTip := gitRevParse(t, repoDir, "develop")
	if newBranchTip != developTip {
		t.Errorf("new branch %q tip = %s, want develop's tip %s",
			ticket.BranchName, newBranchTip, developTip)
	}
}

// TestSaveTicketForm_PersistsBaseBranch pins the form-layer contract:
// when the user picks a base branch in the form, saveTicketForm must
// persist it to ticket.BaseBranch. Before the fix, the form had no
// base branch field at all and BaseBranch was set later by
// setupWorktree (overwriting whatever the form had).
func TestSaveTicketForm_PersistsBaseBranch(t *testing.T) {
	m, _ := makeTestModelWithRepo(t)

	m.createNewTicket()
	projects := m.globalStore.Projects()
	if len(projects) != 1 {
		t.Fatalf("setup: want 1 project, got %d", len(projects))
	}
	m.selectedProject = projects[0]
	// Re-run the picker load now that selectedProject is set.
	m.loadBaseBranches()

	m.titleInput.SetValue("Pick-base-branch-ticket")
	// Pick "develop" as the base branch.
	m.ticketBaseBranch = "develop"
	idx := indexOf(m.baseBranchCandidates, "develop")
	if idx >= 0 {
		m.baseBranchListIndex = idx
	}

	_, _ = m.saveTicketForm(false)

	var created *board.Ticket
	for _, tk := range m.globalStore.All() {
		if tk.Title == "Pick-base-branch-ticket" {
			created = tk
			break
		}
	}
	if created == nil {
		t.Fatal("ticket not created")
	}

	if created.BaseBranch != "develop" {
		t.Errorf("ticket.BaseBranch = %q, want %q (form pick)",
			created.BaseBranch, "develop")
	}
}

// TestBaseBranchPicker_PopulatesFromProjectRepo pins the contract
// that loadBaseBranches populates the picker from the SELECTED
// PROJECT's git repo (not stale state or a different project's).
func TestBaseBranchPicker_PopulatesFromProjectRepo(t *testing.T) {
	m, _ := makeTestModelWithRepo(t)

	m.createNewTicket()
	projects := m.globalStore.Projects()
	m.selectedProject = projects[0]
	m.loadBaseBranches()

	if len(m.baseBranchCandidates) < 1 {
		t.Fatal("baseBranchCandidates empty after loadBaseBranches")
	}
	for _, b := range m.baseBranchCandidates {
		if b == "" {
			t.Error("baseBranchCandidates contains empty entry")
		}
	}

	if !containsStr(m.baseBranchCandidates, "main") {
		t.Errorf("baseBranchCandidates = %v, missing 'main'", m.baseBranchCandidates)
	}
	if !containsStr(m.baseBranchCandidates, "develop") {
		t.Errorf("baseBranchCandidates = %v, missing 'develop'", m.baseBranchCandidates)
	}
}

// TestBaseBranchPicker_DefaultsToProjectDefault pins the contract
// that loadBaseBranches sets ticketBaseBranch to the project's default
// branch (so users don't have to pick if they don't want to).
func TestBaseBranchPicker_DefaultsToProjectDefault(t *testing.T) {
	m, _ := makeTestModelWithRepo(t)

	m.createNewTicket()
	projects := m.globalStore.Projects()
	m.selectedProject = projects[0]
	m.loadBaseBranches()

	if m.ticketBaseBranch != "main" {
		t.Errorf("ticketBaseBranch = %q, want %q (project default)",
			m.ticketBaseBranch, "main")
	}
	wantIndex := indexOf(m.baseBranchCandidates, "main")
	if m.baseBranchListIndex != wantIndex {
		t.Errorf("baseBranchListIndex = %d, want %d (pointing at %q in %v)",
			m.baseBranchListIndex, wantIndex, "main", m.baseBranchCandidates)
	}
}

// TestBaseBranchSelector_RendersAllBranches pins the rendering
// contract: when the Base Branch field is focused, the selector
// shows ALL local branches with a cursor on the currently chosen
// one. Users must see "main" and "develop" in the rendered output
// when the field is focused.
func TestBaseBranchSelector_RendersAllBranches(t *testing.T) {
	m, _ := makeTestModelWithRepo(t)

	m.createNewTicket()
	m.selectedProject = m.globalStore.Projects()[0]
	m.loadBaseBranches()
	m.ticketFormField = formFieldBaseBranch

	view := m.renderBaseBranchSelector()
	plain := stripANSI(view)

	if !strings.Contains(plain, "main") {
		t.Errorf("renderBaseBranchSelector missing 'main':\n%s", plain)
	}
	if !strings.Contains(plain, "develop") {
		t.Errorf("renderBaseBranchSelector missing 'develop':\n%s", plain)
	}
}

// TestBaseBranchSelector_CompactWhenInactive pins the visual
// contract: when the Base Branch field is NOT focused, the selector
// shows ONLY the currently chosen branch (compact display, mirroring
// the priority/worktree fields).
func TestBaseBranchSelector_CompactWhenInactive(t *testing.T) {
	m, _ := makeTestModelWithRepo(t)

	m.createNewTicket()
	m.selectedProject = m.globalStore.Projects()[0]
	m.loadBaseBranches()
	m.ticketFormField = formFieldTitle // base-branch field is NOT focused
	m.ticketBaseBranch = "develop"

	view := m.renderBaseBranchSelector()
	plain := stripANSI(view)

	if !strings.Contains(plain, "develop") {
		t.Errorf("compact render missing 'develop':\n%s", plain)
	}
	// Compact display should NOT show the other branches (main).
	// (Other branches appear in the active-field render, so the
	// absence here is what proves the compact path works.)
	for _, line := range splitLinesForTest(plain) {
		trimmed := line
		// Skip lines that contain 'develop' (that's the current pick)
		if containsStr([]string{trimmed}, "develop") {
			continue
		}
		// Skip hint lines and decorative lines.
		if trimmed == "" || containsStr([]string{trimmed}, "navigate") {
			continue
		}
		if containsStr([]string{trimmed}, "main") {
			t.Errorf("compact render unexpectedly shows other branches:\n%s", plain)
		}
	}
}

// TestHandleBaseBranchNav_UpdatesTicketBaseBranch pins the nav
// contract: pressing 'j' moves the cursor and updates the picked
// branch. The user always sees the current pick reflected in
// ticketBaseBranch.
func TestHandleBaseBranchNav_UpdatesTicketBaseBranch(t *testing.T) {
	m, _ := makeTestModelWithRepo(t)

	m.createNewTicket()
	m.selectedProject = m.globalStore.Projects()[0]
	m.loadBaseBranches()
	m.ticketFormField = formFieldBaseBranch

	// baseBranchCandidates is sorted ascending: ["develop", "main"]
	// Initial cursor on the project default branch.
	if m.ticketBaseBranch != "main" {
		t.Fatalf("setup: ticketBaseBranch = %q, want %q",
			m.ticketBaseBranch, "main")
	}
	initialIdx := m.baseBranchListIndex

	// Move cursor up: should wrap from main → develop (the other entry).
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if m.baseBranchListIndex == initialIdx {
		t.Error("pressing 'k' did not move the cursor")
	}
	if m.ticketBaseBranch != "develop" {
		t.Errorf("after 'k': ticketBaseBranch = %q, want %q",
			m.ticketBaseBranch, "develop")
	}

	// Move down: should go back to main.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if m.ticketBaseBranch != "main" {
		t.Errorf("after 'j': ticketBaseBranch = %q, want %q",
			m.ticketBaseBranch, "main")
	}
}

// TestNextFormField_VisitsBaseBranch pins the form-navigation contract:
// Tab from Branch lands on BaseBranch (not Labels). Regression guard
// for the field-shift: when we added BaseBranch at index 3, all the
// constants after it shifted by 1, but the navigation must still
// visit every field in order.
func TestNextFormField_VisitsBaseBranch(t *testing.T) {
	m, _ := makeTestModelWithRepo(t)

	m.createNewTicket()
	m.ticketFormField = formFieldBranch

	m.nextFormField(false) // isEdit=false → maxField=formFieldProject
	if m.ticketFormField != formFieldBaseBranch {
		t.Errorf("after 1×nextFormField from Branch: ticketFormField = %d, want %d (BaseBranch)",
			m.ticketFormField, formFieldBaseBranch)
	}

	m.nextFormField(false) // BaseBranch → Labels
	if m.ticketFormField != formFieldLabels {
		t.Errorf("after 2×nextFormField from Branch: ticketFormField = %d, want %d (Labels)",
			m.ticketFormField, formFieldLabels)
	}
}

// TestPrevFormField_VisitsBaseBranch pins the reverse-navigation
// contract: Shift-Tab from Labels lands on BaseBranch (not Branch).
func TestPrevFormField_VisitsBaseBranch(t *testing.T) {
	m, _ := makeTestModelWithRepo(t)

	m.createNewTicket()
	m.ticketFormField = formFieldLabels

	m.prevFormField(false)
	if m.ticketFormField != formFieldBaseBranch {
		t.Errorf("after prevFormField from Labels: ticketFormField = %d, want %d (BaseBranch)",
			m.ticketFormField, formFieldBaseBranch)
	}
}

// TestEditTicket_PopulatesBaseBranchFromExistingValue pins the
// edit-mode contract: when re-opening a ticket for editing, the
// base-branch picker must highlight the ticket's existing pick,
// not blindly reset to the project default.
func TestEditTicket_PopulatesBaseBranchFromExistingValue(t *testing.T) {
	m, _ := makeTestModelWithRepo(t)

	projects := m.globalStore.Projects()
	proj := projects[0]
	ticket := board.NewTicket("Edit-test", proj.ID)
	ticket.BaseBranch = "develop"
	if err := m.globalStore.Add(ticket); err != nil {
		t.Fatalf("gts.Add: %v", err)
	}

	// Select the ticket.
	m.refreshColumnTickets()
	for i, t2 := range m.columnTickets[0] {
		if t2.ID == ticket.ID {
			m.activeColumn = 0
			m.activeTicket = i
			break
		}
	}

	m.editTicket()
	if m.ticketBaseBranch != "develop" {
		t.Errorf("editTicket: ticketBaseBranch = %q, want %q (existing ticket pick)",
			m.ticketBaseBranch, "develop")
	}
	wantIdx := indexOf(m.baseBranchCandidates, "develop")
	if m.baseBranchListIndex != wantIdx {
		t.Errorf("editTicket: baseBranchListIndex = %d, want %d (pointing at 'develop')",
			m.baseBranchListIndex, wantIdx)
	}
}

// ─── helpers ────────────────────────────────────────────────────────

func gitRevParse(t *testing.T, dir, ref string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--verify", ref).Output()
	if err != nil {
		t.Fatalf("git rev-parse %s: %v", ref, err)
	}
	return trim(string(out))
}

func trim(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == ' ' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

func containsStr(s []string, v string) bool {
	return indexOf(s, v) >= 0
}

// splitLinesForTest splits a string by newlines. Used for
// line-by-line assertions on rendered output.
func splitLinesForTest(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		out = append(out, line)
	}
	return out
}