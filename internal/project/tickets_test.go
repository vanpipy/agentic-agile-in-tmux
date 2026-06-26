package project

import (
	"path/filepath"
	"time"
	"testing"

	"github.com/pi/awp/internal/board"
)

// newTestProject creates a project with a real temp directory
func newTestProject(t *testing.T, name string) (*Project, string) {
	t.Helper()
	dir := t.TempDir()
	p := NewProject(name, dir)
	return p, dir
}

func newTestGTS(t *testing.T) *GlobalTicketStore {
	t.Helper()
	// Redirect AWP_CONFIG_DIR to a temp dir for test isolation
	t.Setenv("AWP_CONFIG_DIR", t.TempDir())
	reg := &ProjectRegistry{
		Projects: make(map[string]*Project),
	}
	gts := NewGlobalTicketStore(reg)
	return gts
}

func TestNewProject(t *testing.T) {
	dir := t.TempDir()
	p := NewProject("myapp", dir)
	if p.Name != "myapp" {
		t.Errorf("Name = %q, want %q", p.Name, "myapp")
	}
	if p.RepoPath != dir {
		t.Errorf("RepoPath = %q, want %q", p.RepoPath, dir)
	}
	if p.ID == "" {
		t.Error("ID should be auto-generated")
	}
	if p.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}
}

func TestTicketStore_AddGetDelete(t *testing.T) {
	p, _ := newTestProject(t, "test")
	store := NewTicketStore(p.ID, p.RepoPath)

	tk := board.NewTicket("Test ticket", p.ID)
	tk.Title = "First"

	if err := store.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	store.Add(tk)

	if store.Count() != 1 {
		t.Errorf("Count = %d, want 1", store.Count())
	}

	got, err := store.Get(tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title != "First" {
		t.Errorf("Get.Title = %q, want %q", got.Title, "First")
	}
}

func TestTicketStore_Move(t *testing.T) {
	p, _ := newTestProject(t, "test")
	store := NewTicketStore(p.ID, p.RepoPath)
	tk := board.NewTicket("test", p.ID)
	store.Add(tk)
	_ = store.Save()

	if err := store.Move(tk.ID, board.StatusInProgress); err != nil {
		t.Fatalf("Move: %v", err)
	}
	got, _ := store.Get(tk.ID)
	if got.Status != board.StatusInProgress {
		t.Errorf("Status = %v, want InProgress", got.Status)
	}
}

func TestTicketStore_GetByStatus(t *testing.T) {
	p, _ := newTestProject(t, "test")
	store := NewTicketStore(p.ID, p.RepoPath)

	tk1 := board.NewTicket("backlog", p.ID)
	tk2 := board.NewTicket("inprogress", p.ID)
	store.Add(tk1)
	store.Add(tk2)
	_ = store.Move(tk2.ID, board.StatusInProgress)
	_ = store.Save()

	backlog := store.GetByStatus(board.StatusBacklog)
	if len(backlog) != 1 {
		t.Errorf("Backlog count = %d, want 1", len(backlog))
	}
	inprogress := store.GetByStatus(board.StatusInProgress)
	if len(inprogress) != 1 {
		t.Errorf("InProgress count = %d, want 1", len(inprogress))
	}
}

func TestTicketStore_Delete(t *testing.T) {
	p, _ := newTestProject(t, "test")
	store := NewTicketStore(p.ID, p.RepoPath)
	tk := board.NewTicket("to delete", p.ID)
	store.Add(tk)
	_ = store.Save()

	if err := store.Delete(tk.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if store.Count() != 0 {
		t.Errorf("Count after delete = %d, want 0", store.Count())
	}
	// Delete non-existent should error
	if err := store.Delete(tk.ID); err == nil {
		t.Error("Delete of non-existent should error")
	}
}

func TestTicketStore_LoadFromDisk(t *testing.T) {
	dir := t.TempDir()
	p := NewProject("test", dir)
	store := NewTicketStore(p.ID, dir)
	tk := board.NewTicket("persist me", p.ID)
	store.Add(tk)
	if err := store.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Reload
	loaded, err := LoadTicketStore(p)
	if err != nil {
		t.Fatalf("LoadTicketStore: %v", err)
	}
	if loaded.Count() != 1 {
		t.Errorf("Loaded count = %d, want 1", loaded.Count())
	}
	got, _ := loaded.Get(tk.ID)
	if got == nil {
		t.Fatal("loaded ticket is nil")
	}
	if got.Title != "persist me" {
		t.Errorf("Loaded title = %q, want %q", got.Title, "persist me")
	}
}

func TestTicketStore_All(t *testing.T) {
	p, _ := newTestProject(t, "test")
	store := NewTicketStore(p.ID, p.RepoPath)
	for i := 0; i < 5; i++ {
		tk := board.NewTicket("t", p.ID)
		store.Add(tk)
	}
	all := store.All()
	if len(all) != 5 {
		t.Errorf("All count = %d, want 5", len(all))
	}
}

func TestGlobalTicketStore_ProjectLookup(t *testing.T) {
	gts := newTestGTS(t)
	p, _ := newTestProject(t, "test")
	gts.AddProject(p)

	if got := gts.GetProject(p.ID); got != p {
		t.Error("GetProject didn't return the same project")
	}
	if !gts.HasProjects() {
		t.Error("HasProjects should be true")
	}
	if len(gts.Projects()) != 1 {
		t.Errorf("Projects count = %d, want 1", len(gts.Projects()))
	}
}

func TestGlobalTicketStore_RemoveProject(t *testing.T) {
	gts := newTestGTS(t)
	p, _ := newTestProject(t, "test")
	gts.AddProject(p)
	gts.registry.Projects[p.ID] = p // also add to registry

	if err := gts.RemoveProject(p.ID); err != nil {
		t.Fatalf("RemoveProject: %v", err)
	}
	if gts.HasProjects() {
		t.Error("HasProjects should be false after remove")
	}
	// Removing non-existent errors
	if err := gts.RemoveProject("nonexistent"); err == nil {
		t.Error("Remove non-existent should error")
	}
}

func TestGlobalTicketStore_AllAcrossProjects(t *testing.T) {
	gts := newTestGTS(t)
	p1, _ := newTestProject(t, "p1")
	p2, _ := newTestProject(t, "p2")
	gts.AddProject(p1)
	gts.AddProject(p2)

	tk1 := board.NewTicket("t1", p1.ID)
	tk2 := board.NewTicket("t2", p2.ID)
	_ = gts.Add(tk1)
	_ = gts.Add(tk2)

	all := gts.All()
	if len(all) != 2 {
		t.Errorf("All count = %d, want 2", len(all))
	}
}

func TestGlobalTicketStore_GetStoreForTicket(t *testing.T) {
	gts := newTestGTS(t)
	p, _ := newTestProject(t, "test")
	gts.AddProject(p)

	tk := board.NewTicket("test", p.ID)
	_ = gts.Add(tk)

	if got := gts.GetStoreForTicket(tk); got == nil {
		t.Error("GetStoreForTicket should return non-nil for known project")
	}
	if got := gts.GetProjectForTicket(tk); got != p {
		t.Error("GetProjectForTicket didn't return expected project")
	}

	// Ticket from unknown project
	unknown := board.NewTicket("test", "nonexistent")
	if gts.GetStoreForTicket(unknown) != nil {
		t.Error("GetStoreForTicket for unknown project should return nil")
	}
	if gts.GetProjectForTicket(unknown) != nil {
		t.Error("GetProjectForTicket for unknown project should return nil")
	}
}

func TestGlobalTicketStore_AddUnknownProject(t *testing.T) {
	gts := newTestGTS(t)
	// Don't add project first
	tk := board.NewTicket("test", "unknown")
	err := gts.Add(tk)
	if err == nil {
		t.Error("Add to unknown project should error")
	}
}

func TestGlobalTicketStore_Blockers(t *testing.T) {
	gts := newTestGTS(t)
	p, _ := newTestProject(t, "test")
	gts.AddProject(p)

	tk1 := board.NewTicket("first", p.ID)
	tk2 := board.NewTicket("second", p.ID)
	tk2.BlockedBy = []board.TicketID{tk1.ID}
	_ = gts.Add(tk1)
	_ = gts.Add(tk2)

	blockers := gts.GetBlockedBy(tk2.ID)
	if len(blockers) != 1 || blockers[0].ID != tk1.ID {
		t.Errorf("GetBlockedBy returned wrong tickets: %v", blockers)
	}
	// tk1 blocks tk2
	blocks := gts.GetBlocks(tk1.ID)
	if len(blocks) != 1 || blocks[0].ID != tk2.ID {
		t.Errorf("GetBlocks returned wrong tickets: %v", blocks)
	}

	// Remove blocker references for tk1
	gts.RemoveBlockerReferences(tk1.ID)
	// Re-fetch tk2 since RemoveBlockerReferences mutates
	tk2updated, _ := gts.Get(tk2.ID)
	if len(tk2updated.BlockedBy) != 0 {
		t.Errorf("BlockedBy not cleared: %v", tk2updated.BlockedBy)
	}
}

func TestProject_WorktreeDir(t *testing.T) {
	p := &Project{
		Name:        "test",
		RepoPath:    "/repo",
		WorktreeDir: "/worktrees/test",
	}
	got := p.WorktreeDir
	if got != filepath.Join("/worktrees", "test") {
		t.Errorf("GetWorktreeDir = %q, want %q", got, "/worktrees/test")
	}
}

func TestProject_Touch(t *testing.T) {
	p := &Project{UpdatedAt: time.Time{}}
	p.Touch()
	if p.UpdatedAt.IsZero() {
		t.Error("Touch should update LastUsedAt")
	}
}

func TestProject_GetDefaults(t *testing.T) {
	p := NewProject("test", "/repo")
	if p.GetBranchPrefix() == "" {
		t.Error("GetBranchPrefix empty")
	}
	if p.GetBranchTemplate() == "" {
		t.Error("GetBranchTemplate empty")
	}
	if p.GetSlugMaxLength() <= 0 {
		t.Error("GetSlugMaxLength should be > 0")
	}
}
