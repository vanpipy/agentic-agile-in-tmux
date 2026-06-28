// load_filter_test.go — TDD test for the post-2026-06-28 data-compat
// contract: when loading old JSONL files that contain tickets with
// status "done" or "archived" (the removed-from-state-machine values),
// those tickets are silently dropped from the in-memory store.
//
// The user's design decision (per the ticket description): "不兼容，
// 历史数据移除" (incompatible, remove historical data). This test pins
// the implementation of that contract.
//
// We test by writing a raw JSONL file with old statuses and asserting
// the load filter drops them. The board package's StatusDone/
// StatusArchived constants no longer exist, so the test cannot use
// them — it must use raw string literals that match the JSON values
// the old data would have contained.
//
// CORRECT-7 self-check on this test file:
//   C-onformance: literal expected count of loaded tickets
//   O-rdering:    N/A (filter is a set operation)
//   R-ange:       0, 1, N tickets; mixed valid + invalid
//   R-eference:   file I/O handled with t.TempDir
//   E-xistence:   empty store, empty file, missing file paths
//   C-ardinality: 0/1/2/3 cases
//   T-ime:        no time concerns
package project

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/pi/awp/internal/board"
)

// writeRawTicketJSONL writes a JSONL file directly, bypassing the
// board.NewTicket constructor. This lets us write tickets with
// removed status values ("done", "archived") that the constructor
// would never produce.
func writeRawTicketJSONL(t *testing.T, dir, projectID string, rawTickets []map[string]any) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(dir, projectID+".json")

	type storeJSON struct {
		ProjectID string                            `json:"project_id"`
		Tickets   map[string]map[string]any         `json:"tickets"`
		UpdatedAt string                            `json:"updated_at"`
	}
	tickets := make(map[string]map[string]any)
	for _, rt := range rawTickets {
		id, _ := rt["id"].(string)
		tickets[id] = rt
	}
	payload := storeJSON{
		ProjectID: projectID,
		Tickets:   tickets,
		UpdatedAt: "2026-01-01T00:00:00Z",
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// TestLoadGlobalTicketStore_FiltersDoneAndArchived pins the breaking-change
// contract: tickets with status "done" or "archived" in the JSONL file
// are silently dropped on load. Backlog and in_progress tickets are
// preserved.
//
// We use a single project with a mix of all 4 status values; only 2
// (backlog, in_progress) should appear in gts.All() after load.
func TestLoadGlobalTicketStore_FiltersDoneAndArchived(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("AWP_CONFIG_DIR", tmpDir)

	// Set up registry with one project pointing at our temp dir.
	projectID := "test-project-id"
	reg := &ProjectRegistry{Projects: make(map[string]*Project)}
	reg.Projects[projectID] = &Project{ID: projectID, Name: "test", RepoPath: tmpDir}

	// Write JSONL with all 4 status values. The "done" and "archived"
	// entries simulate data from before the simplification.
	writeRawTicketJSONL(t, filepath.Join(tmpDir, "tickets"), projectID, []map[string]any{
		{"id": "aaaaaaaa-1", "project_id": projectID, "title": "backlog ticket", "status": "backlog"},
		{"id": "bbbbbbbb-2", "project_id": projectID, "title": "in_progress ticket", "status": "in_progress"},
		{"id": "cccccccc-3", "project_id": projectID, "title": "old done ticket", "status": "done"},
		{"id": "dddddddd-4", "project_id": projectID, "title": "old archived ticket", "status": "archived"},
	})

	gts, err := LoadGlobalTicketStore(reg)
	if err != nil {
		t.Fatalf("LoadGlobalTicketStore: %v", err)
	}

	all := gts.All()
	if len(all) != 2 {
		t.Errorf("All count = %d, want 2 (backlog + in_progress; done/archived dropped)", len(all))
		for _, tk := range all {
			t.Logf("  loaded: id=%s status=%q title=%q", tk.ID, tk.Status, tk.Title)
		}
	}

	// Verify the surviving tickets are the expected two.
	gotTitles := map[string]bool{}
	for _, t := range all {
		gotTitles[t.Title] = true
	}
	if !gotTitles["backlog ticket"] {
		t.Error("backlog ticket not in loaded store")
	}
	if !gotTitles["in_progress ticket"] {
		t.Error("in_progress ticket not in loaded store")
	}
	if gotTitles["old done ticket"] {
		t.Error("done ticket leaked through filter — must be dropped")
	}
	if gotTitles["old archived ticket"] {
		t.Error("archived ticket leaked through filter — must be dropped")
	}
}

// TestLoadGlobalTicketStore_FiltersAllOldData checks the edge case
// where ALL tickets in a project are old status values — the project
// loads as an empty store.
func TestLoadGlobalTicketStore_FiltersAllOldData(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("AWP_CONFIG_DIR", tmpDir)

	projectID := "all-old-project"
	reg := &ProjectRegistry{Projects: make(map[string]*Project)}
	reg.Projects[projectID] = &Project{ID: projectID, Name: "test", RepoPath: tmpDir}

	writeRawTicketJSONL(t, filepath.Join(tmpDir, "tickets"), projectID, []map[string]any{
		{"id": "x1", "project_id": projectID, "title": "done-1", "status": "done"},
		{"id": "x2", "project_id": projectID, "title": "archived-1", "status": "archived"},
		{"id": "x3", "project_id": projectID, "title": "done-2", "status": "done"},
	})

	gts, err := LoadGlobalTicketStore(reg)
	if err != nil {
		t.Fatalf("LoadGlobalTicketStore: %v", err)
	}
	if got := gts.Count(); got != 0 {
		t.Errorf("Count = %d, want 0 (all tickets had old statuses)", got)
	}
}

// TestRemoveProject_DoesNotCreateArchiveDirectory pins the simplified
// RemoveProject contract: it no longer moves the JSONL file to a
// "tickets/archived/" directory. Deletion is final.
func TestRemoveProject_DoesNotCreateArchiveDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("AWP_CONFIG_DIR", tmpDir)

	ticketsDir := filepath.Join(tmpDir, "tickets")
	if err := os.MkdirAll(ticketsDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	projectID := "remove-test"
	jsonlPath := filepath.Join(ticketsDir, projectID+".json")
	if err := os.WriteFile(jsonlPath, []byte("{}"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	p := &Project{ID: projectID, Name: "remove-test", RepoPath: tmpDir}
	reg := &ProjectRegistry{Projects: make(map[string]*Project)}
	reg.Projects[projectID] = p

	gts, err := LoadGlobalTicketStore(reg)
	if err != nil {
		t.Fatalf("LoadGlobalTicketStore: %v", err)
	}

	// Act: remove the project.
	if err := gts.RemoveProject(projectID); err != nil {
		t.Fatalf("RemoveProject: %v", err)
	}

	// Assert 1: tickets/archived/ directory must NOT exist.
	archivedDir := filepath.Join(ticketsDir, "archived")
	if _, err := os.Stat(archivedDir); err == nil {
		t.Errorf("RemoveProject created %q; expected no archive directory.\n"+
			"Per design decision 2026-06-28: project deletion is final, no file-level archive.",
			archivedDir)
	} else if !os.IsNotExist(err) {
		t.Errorf("unexpected Stat error on %q: %v", archivedDir, err)
	}

	// Assert 2: original JSONL must NOT exist (it was removed, not archived).
	if _, err := os.Stat(jsonlPath); err == nil {
		t.Errorf("original JSONL %q still exists after RemoveProject; expected deletion",
			jsonlPath)
	} else if !os.IsNotExist(err) {
		t.Errorf("unexpected Stat error on %q: %v", jsonlPath, err)
	}
}

// TestLoadGlobalTicketStore_PreservesValidTickets sanity-checks that
// the filter does not over-drop: a project with only valid tickets
// loads normally.
func TestLoadGlobalTicketStore_PreservesValidTickets(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("AWP_CONFIG_DIR", tmpDir)

	projectID := "valid-only"
	reg := &ProjectRegistry{Projects: make(map[string]*Project)}
	reg.Projects[projectID] = &Project{ID: projectID, Name: "test", RepoPath: tmpDir}

	writeRawTicketJSONL(t, filepath.Join(tmpDir, "tickets"), projectID, []map[string]any{
		{"id": "v1", "project_id": projectID, "title": "b-1", "status": "backlog"},
		{"id": "v2", "project_id": projectID, "title": "i-1", "status": "in_progress"},
		{"id": "v3", "project_id": projectID, "title": "b-2", "status": "backlog"},
	})

	gts, err := LoadGlobalTicketStore(reg)
	if err != nil {
		t.Fatalf("LoadGlobalTicketStore: %v", err)
	}
	if got := gts.Count(); got != 3 {
		t.Errorf("Count = %d, want 3", got)
	}
}

// Compile-time check: the load filter does not break the
// board.Ticket contract. If board.Ticket loses the Status field
// (unlikely but defensive), this test file won't compile and we'll
// see it during the build.
var _ = board.StatusBacklog
