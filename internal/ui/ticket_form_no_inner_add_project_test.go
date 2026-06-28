// ticket_form_no_inner_add_project_test.go — TDD tests for the
// "single entry point for Add Project" UX change.
//
// Requirement: in the ticket creation form, the project field MUST
// only let the user PICK from existing projects. Adding a new
// project is a top-level (board view / sidebar) operation only.
// Keeping a single entry point prevents the user from accidentally
// creating orphan projects mid-flow and surfaces a cleaner mental
// model (sidebar is the "project inventory", ticket form is the
// "ticket creator").
//
// Tests verify that, from within the ticket form, the user can:
//   - navigate the project list with j/k (clamped to len-1, no
//     overshoot to a synthetic "+ Add project" entry)
//   - press Enter to confirm selection (no fallback to add-project)
//   - click anywhere in the project list area (no synthetic "+"
//     row to click on)
//
// And that the sidebar's "+ Add project" still works (regression
// for the keep-this-working side).
package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// makeTestModelForTicketForm returns a Model wired for testing the
// ticket creation form, with two fake projects already registered
// so the project field has something to navigate.
func makeTestModelForTicketForm(t *testing.T) *Model {
	t.Helper()
	m := makeTestModelForCreateProject(t)
	// Register two projects so the project field has a real list.
	repoA := setupFakeGitRepo(t)
	repoB := setupFakeGitRepo(t)
	m.openCreateProjectForTest(repoA)
	m.openCreateProjectForTest(repoB)
	// Enter ticket form.
	m.mode = ModeCreateTicket
	m.ticketFormField = formFieldProject
	m.projectListIndex = 0
	m.selectedProject = nil
	return m
}

// openCreateProjectForTest drives the global ModeCreateProject flow
// without touching the UI loop — equivalent to user pressing 'a'
// in sidebar then submitting a valid path.
func (m *Model) openCreateProjectForTest(repoPath string) {
	m.mode = ModeCreateProject
	m.addProjectPath.SetValue(repoPath)
	m.createProjectFromPath()
	// After successful create, mode is reset to Normal by
	// createProjectFromPath. Caller may re-enter as needed.
}

// TestTicketForm_NavOnProjectField_StaysWithinList is the core
// "no overshoot to a + Add project entry" test.
//
// Before the fix: maxIndex = len(projects), so j/k could navigate
// to index == len(projects), which then opened the inner add-project
// form on Enter. After the fix: maxIndex = len(projects)-1 (when
// at least one project exists), so nav stays within real projects.
func TestTicketForm_NavOnProjectField_StaysWithinList(t *testing.T) {
	m := makeTestModelForTicketForm(t)
	if got := len(m.globalStore.Projects()); got != 2 {
		t.Fatalf("setup: want 2 projects, got %d", got)
	}

	// Press 'j' (down) from index 0 → 1, then 'j' again → should
	// wrap to 0 (NOT to a synthetic "+" index == len(projects) == 2).
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if m.projectListIndex != 1 {
		t.Fatalf("after 1×j: projectListIndex = %d, want 1", m.projectListIndex)
	}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if m.projectListIndex != 0 {
		t.Errorf("after 2×j: projectListIndex = %d, want 0 (wrap, not overshoot to '+')", m.projectListIndex)
	}

	// Press 'k' from 0 → wrap to 1 (last real project), NOT to -1
	// or to a synthetic "+" entry.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if m.projectListIndex != 1 {
		t.Errorf("after k from 0: projectListIndex = %d, want 1 (wrap, not overshoot)", m.projectListIndex)
	}
}

// TestTicketForm_EnterOnProjectField_DoesNotOpenAddProject is the
// canonical "no add-project from inside ticket form" test.
//
// Before the fix: pressing Enter when projectListIndex == len(projects)
// opened the nested showAddProjectForm. After the fix: the inner
// form is unreachable (field removed entirely), so Enter on the
// project field either picks a real project or no-ops.
func TestTicketForm_EnterOnProjectField_DoesNotOpenAddProject(t *testing.T) {
	m := makeTestModelForTicketForm(t)
	// Even if we artificially set projectListIndex past the end
	// (simulating a stale state from before the fix), Enter must
	// NOT open any add-project surface (inner form was removed;
	// ModeCreateProject is only reachable from sidebar).
	m.projectListIndex = 99

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if m.mode == ModeCreateProject {
		t.Error("Enter on project field jumped to top-level ModeCreateProject (inner form is forbidden)")
	}
}

// TestTicketForm_EnterOnProjectField_SelectsHighlightedProject
// verifies the happy path still works: Enter picks the highlighted
// project (this is the user-facing value of the change — pick
// instead of "navigate then enter to add").
func TestTicketForm_EnterOnProjectField_SelectsHighlightedProject(t *testing.T) {
	m := makeTestModelForTicketForm(t)
	m.projectListIndex = 1 // pick the second project

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	projects := m.globalStore.Projects()
	if m.selectedProject == nil {
		t.Fatal("Enter did not select a project")
	}
	if m.selectedProject.ID != projects[1].ID {
		t.Errorf("selected project ID = %q, want %q", m.selectedProject.ID, projects[1].ID)
	}
}

// TestTicketForm_ProjectSelectorView_NoAddProjectEntry verifies
// the visual contract: the project list inside the ticket form
// renders ONLY real projects — no synthetic "+ Add project" row.
//
// Before the fix: view.go appended a "+ Add project" line at the
// end of the list, even inside the ticket form.
func TestTicketForm_ProjectSelectorView_NoAddProjectEntry(t *testing.T) {
	m := makeTestModelForTicketForm(t)
	m.ticketFormField = formFieldProject // ensure the field renders the list

	view := m.renderProjectSelector()
	plain := ansi.Strip(view)

	if contains([]byte(plain), "+ Add project") {
		t.Errorf("renderProjectSelector still shows '+ Add project' inside ticket form:\n%s", plain)
	}
}

// TestSidebar_AddProject_StillWorks is the regression test: the
// top-level "add project" path (sidebar 'a' / Enter on the +
// row) must keep working — this is the SOLE entry point now.
func TestSidebar_AddProject_StillWorks(t *testing.T) {
	m := makeTestModelForCreateProject(t)
	if got := len(m.globalStore.Projects()); got != 0 {
		t.Fatalf("setup: want 0 projects, got %d", got)
	}

	// openAddProjectForm is the single entry point for new projects.
	_, _ = m.openAddProjectForm()
	if m.mode != ModeCreateProject {
		t.Errorf("openAddProjectForm: mode = %q, want %q", m.mode, ModeCreateProject)
	}
	if !m.addProjectPath.Focused() {
		t.Error("openAddProjectForm: addProjectPath not focused")
	}

	// And the form still creates the project on submit.
	repoDir := setupFakeGitRepo(t)
	m.addProjectPath.SetValue(repoDir)
	m.createProjectFromPath()
	if got := len(m.globalStore.Projects()); got != 1 {
		t.Errorf("after createProjectFromPath: %d projects; want 1", got)
	}
}