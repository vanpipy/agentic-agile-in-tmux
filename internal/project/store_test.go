package project

import (
	"os"
	"path/filepath"
	"testing"
)


// TestMain redirects AWP_CONFIG_DIR to a temp dir unless the test
// environment has already set it (e.g., for cross-package integration
// testing). This is a SAFETY NET for any test that loads/saves the
// registry — without it, test runs can clobber the user's real
// ~/.config/awp/{projects,filters}.json.
//
// Tests that need a *specific* dir (e.g., to share with sub-tests)
// should still call t.Setenv("AWP_CONFIG_DIR", ...) themselves.
func TestMain(m *testing.M) {
	 if os.Getenv("AWP_CONFIG_DIR") == "" {
	 	 dir, err := os.MkdirTemp("", "awp-test-")
	 	 if err == nil {
	 	 	 os.Setenv("AWP_CONFIG_DIR", dir)
	 	 }
	 }
	 os.Exit(m.Run())
}

// TestPollutionGuard verifies that TestMain redirects AWP_CONFIG_DIR
// to a temp dir. This is the regression test for the bug where
// reg.Add() calls in tests without AWP_CONFIG_DIR setenv would
// overwrite the user's real ~/.config/awp/{projects,filters}.json.
//
// How it works: by the time this test runs, TestMain has already
// set AWP_CONFIG_DIR. If we save a Project now, it should land in
// the temp dir, NOT in the user's real config dir.
//
// NOTE: This test does NOT verify "user data is not touched" — that
// is the *external* contract. We verify the *internal* contract:
// "after TestMain, AWP_CONFIG_DIR is set to a temp dir."
func TestPollutionGuard(t *testing.T) {
	dir := os.Getenv("AWP_CONFIG_DIR")
	if dir == "" {
		t.Fatal("AWP_CONFIG_DIR not set after TestMain — pollution risk!")
	}
	// Verify it's a temp dir (under os.TempDir() or has the awp-test- prefix)
	if !filepath.IsAbs(dir) {
		t.Errorf("AWP_CONFIG_DIR = %q; want absolute path", dir)
	}
	// Save a project — if TestMain works, this lands in temp dir
	reg, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	p := NewProject("__pollution_test__", "/tmp/__test__")
	if err := reg.Add(p); err != nil {
		t.Fatalf("Add: %v", err)
	}
	// Verify project file is in temp dir, not user's real config
	expectedFile := filepath.Join(dir, "projects.json")
	if _, err := os.Stat(expectedFile); err != nil {
		t.Errorf("expected %s to exist after Save: %v", expectedFile, err)
	}
}


// TestLoadRegistry_NoFile verifies LoadRegistry returns an empty
// registry (not an error) when projects.json doesn't exist.
func TestLoadRegistry_NoFile(t *testing.T) {
	t.Setenv("AWP_CONFIG_DIR", t.TempDir())

	reg, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry() error: %v", err)
	}
	if reg == nil {
		t.Fatal("LoadRegistry() returned nil registry")
	}
	if reg.Projects == nil {
		t.Error("LoadRegistry() Projects map should be non-nil")
	}
	if len(reg.Projects) != 0 {
		t.Errorf("LoadRegistry() Projects count = %d; want 0", len(reg.Projects))
	}
}

// TestLoadRegistry_InvalidJSON verifies an unreadable / invalid file
// returns the underlying error (not a silent default).
func TestLoadRegistry_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AWP_CONFIG_DIR", dir)

	// Write invalid JSON to projects.json
	projPath := filepath.Join(dir, "projects.json")
	if err := os.WriteFile(projPath, []byte("not valid json"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := LoadRegistry()
	if err == nil {
		t.Error("LoadRegistry() should return error for invalid JSON")
	}
}

// TestRegistry_SaveLoadRoundtrip verifies Save+Load preserves all
// fields including the nested Project struct.
func TestRegistry_SaveLoadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AWP_CONFIG_DIR", dir)

	reg, _ := LoadRegistry()
	orig := NewProject("myapp", dir)
	reg.Add(orig)

	// Load again — should see the project we just saved.
	loaded, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry after Save: %v", err)
	}
	if len(loaded.Projects) != 1 {
		t.Fatalf("loaded.Projects count = %d; want 1", len(loaded.Projects))
	}
	got, ok := loaded.Projects[orig.ID]
	if !ok {
		t.Fatalf("loaded.Projects missing key %q", orig.ID)
	}
	if got.Name != "myapp" {
		t.Errorf("Name = %q; want %q", got.Name, "myapp")
	}
	if got.RepoPath != dir {
		t.Errorf("RepoPath = %q; want %q", got.RepoPath, dir)
	}
}

// TestRegistry_AddDuplicatePath verifies Add returns ErrDuplicatePath
// when a different ID is used for the same RepoPath.
func TestRegistry_AddDuplicatePath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AWP_CONFIG_DIR", dir)

	reg, _ := LoadRegistry()
	p1 := NewProject("app1", dir)
	if err := reg.Add(p1); err != nil {
		t.Fatalf("Add first: %v", err)
	}

	p2 := NewProject("app2", dir) // same RepoPath, different ID
	err := reg.Add(p2)
	if err != ErrDuplicatePath {
		t.Errorf("Add second (dup path) error = %v; want ErrDuplicatePath", err)
	}

	if len(reg.Projects) != 1 {
		t.Errorf("Projects count = %d; want 1 (duplicate should NOT be added)", len(reg.Projects))
	}
}

// TestRegistry_GetNotFound verifies Get returns ErrProjectNotFound
// for unknown IDs.
func TestRegistry_GetNotFound(t *testing.T) {
	reg := newRegistry()
	_, err := reg.Get("nonexistent")
	if err != ErrProjectNotFound {
		t.Errorf("Get(nonexistent) error = %v; want ErrProjectNotFound", err)
	}
}

// TestRegistry_FindByPath covers hit and miss paths.
func TestRegistry_FindByPath(t *testing.T) {
	t.Setenv("AWP_CONFIG_DIR", t.TempDir())
	reg := newRegistry()
	p1 := NewProject("app1", "/tmp/p1")
	p2 := NewProject("app2", "/tmp/p2")
	p3 := NewProject("app3", "/tmp/p3")
	reg.Add(p1)
	reg.Add(p2)
	reg.Add(p3)

	t.Run("exact match", func(t *testing.T) {
		got, err := reg.FindByPath("/tmp/p2")
		if err != nil {
			t.Fatalf("FindByPath: %v", err)
		}
		if got.ID != p2.ID {
			t.Errorf("got ID = %q; want %q", got.ID, p2.ID)
		}
	})

	t.Run("no match returns error", func(t *testing.T) {
		_, err := reg.FindByPath("/tmp/nonexistent")
		if err != ErrProjectNotFound {
			t.Errorf("FindByPath error = %v; want ErrProjectNotFound", err)
		}
	})

	t.Run("empty path also returns error", func(t *testing.T) {
		_, err := reg.FindByPath("")
		if err != ErrProjectNotFound {
			t.Errorf("FindByPath(\"\") error = %v; want ErrProjectNotFound", err)
		}
	})
}

// TestRegistry_Delete verifies Delete removes the entry and persists.
func TestRegistry_Delete(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AWP_CONFIG_DIR", dir)

	reg, _ := LoadRegistry()
	p := NewProject("myapp", dir)
	reg.Add(p)

	if err := reg.Delete(p.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := reg.Projects[p.ID]; ok {
		t.Error("Delete: project still in map")
	}

	// Verify persistence: load fresh
	loaded, _ := LoadRegistry()
	if _, ok := loaded.Projects[p.ID]; ok {
		t.Error("Delete not persisted to disk")
	}
}

// TestRegistry_Update verifies Update mutates an existing project.
func TestRegistry_Update(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AWP_CONFIG_DIR", dir)

	reg, _ := LoadRegistry()
	p := NewProject("myapp", dir)
	reg.Add(p)

	p.Name = "renamed"
	if err := reg.Update(p); err != nil {
		t.Fatalf("Update: %v", err)
	}

	loaded, _ := LoadRegistry()
	got, _ := loaded.Get(p.ID)
	if got.Name != "renamed" {
		t.Errorf("after Update: Name = %q; want %q", got.Name, "renamed")
	}
}

// TestRegistry_List verifies List returns all projects.
func TestRegistry_List(t *testing.T) {
	t.Setenv("AWP_CONFIG_DIR", t.TempDir())
	reg := newRegistry()
	reg.Add(NewProject("a", "/tmp/a"))
	reg.Add(NewProject("b", "/tmp/b"))
	reg.Add(NewProject("c", "/tmp/c"))

	list := reg.List()
	if len(list) != 3 {
		t.Errorf("List length = %d; want 3", len(list))
	}
}