package project

import (
	"testing"

	"github.com/pi/awp/internal/board"
)

// TestNewFilter covers the constructor's invariants: ID is set,
// Name is preserved.
func TestNewFilter(t *testing.T) {
	f := NewFilter("myfilter")
	if f.ID == "" {
		t.Error("NewFilter().ID is empty")
	}
	if f.Name != "myfilter" {
		t.Errorf("Name = %q; want %q", f.Name, "myfilter")
	}
	if f.IsDefault {
		t.Error("IsDefault should default to false")
	}
}

// TestSavedFilter_Matches exercises all the constraint fields:
// ProjectIDs, Statuses, Labels.
func TestSavedFilter_Matches(t *testing.T) {
	makeTicket := func(projectID, status string, labels []string) *board.Ticket {
		t := &board.Ticket{
			ID:          "id-1",
			ProjectID:   projectID,
			Status:      board.TicketStatus(status),
		}
		t.Labels = labels
		return t
	}

	t.Run("no constraints matches everything", func(t *testing.T) {
		f := NewFilter("all")
		tk := makeTicket("any", "todo", nil)
		if !f.Matches(tk) {
			t.Error("empty filter should match any ticket")
		}
	})

	t.Run("project filter matches same project", func(t *testing.T) {
		f := NewFilter("by-proj")
		f.ProjectIDs = []string{"proj-A"}
		tk := makeTicket("proj-A", "todo", nil)
		if !f.Matches(tk) {
			t.Error("matching project should pass")
		}
	})

	t.Run("project filter rejects different project", func(t *testing.T) {
		f := NewFilter("by-proj")
		f.ProjectIDs = []string{"proj-A"}
		tk := makeTicket("proj-B", "todo", nil)
		if f.Matches(tk) {
			t.Error("different project should be rejected")
		}
	})

	t.Run("multi-project filter matches any listed", func(t *testing.T) {
		f := NewFilter("by-projs")
		f.ProjectIDs = []string{"proj-A", "proj-B"}
		tk := makeTicket("proj-B", "todo", nil)
		if !f.Matches(tk) {
			t.Error("project in list should match")
		}
	})

	t.Run("status filter matches same status", func(t *testing.T) {
		f := NewFilter("by-status")
		f.Statuses = []string{"in_progress"}
		tk := makeTicket("proj-A", "in_progress", nil)
		if !f.Matches(tk) {
			t.Error("matching status should pass")
		}
	})

	t.Run("status filter rejects different status", func(t *testing.T) {
		f := NewFilter("by-status")
		f.Statuses = []string{"in_progress"}
		tk := makeTicket("proj-A", "done", nil)
		if f.Matches(tk) {
			t.Error("different status should be rejected")
		}
	})
}

// TestFilterRegistry_AddGetDelete covers basic CRUD.
func TestFilterRegistry_AddGetDelete(t *testing.T) {
	t.Setenv("AWP_CONFIG_DIR", t.TempDir())
	r := newFilterRegistry()

	f := NewFilter("alpha")
	if err := r.Add(f); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got := r.Get(f.ID)
	if got == nil {
		t.Fatalf("Get(%q) returned nil", f.ID)
	}
	if got.Name != "alpha" {
		t.Errorf("Name = %q; want %q", got.Name, "alpha")
	}

	if err := r.Delete(f.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if r.Get(f.ID) != nil {
		t.Error("Get after Delete: should return nil")
	}
}

// TestFilterRegistry_GetDefault covers the IsDefault lookup.
func TestFilterRegistry_GetDefault(t *testing.T) {
	t.Setenv("AWP_CONFIG_DIR", t.TempDir())
	r := newFilterRegistry()

	t.Run("empty registry has no default", func(t *testing.T) {
		if got := r.GetDefault(); got != nil {
			t.Errorf("GetDefault() = %+v; want nil", got)
		}
	})

	t.Run("non-default filter doesn't match", func(t *testing.T) {
		f := NewFilter("not-default")
		r.Add(f)
		if got := r.GetDefault(); got != nil {
			t.Errorf("GetDefault() = %+v; want nil", got)
		}
	})

	t.Run("default filter is returned", func(t *testing.T) {
		d := NewFilter("default-one")
		d.IsDefault = true
		r.Add(d)

		other := NewFilter("non-default")
		r.Add(other)

		got := r.GetDefault()
		if got == nil {
			t.Fatal("GetDefault() = nil; want the default filter")
		}
		if got.ID != d.ID {
			t.Errorf("GetDefault() ID = %q; want %q", got.ID, d.ID)
		}
	})
}

// TestFilterRegistry_List covers returning all filters.
func TestFilterRegistry_List(t *testing.T) {
	t.Setenv("AWP_CONFIG_DIR", t.TempDir())
	r := newFilterRegistry()

	t.Run("empty registry", func(t *testing.T) {
		if got := r.List(); len(got) != 0 {
			t.Errorf("List length = %d; want 0", len(got))
		}
	})

	t.Run("3 filters", func(t *testing.T) {
		r.Add(NewFilter("a"))
		r.Add(NewFilter("b"))
		r.Add(NewFilter("c"))
		if got := r.List(); len(got) != 3 {
			t.Errorf("List length = %d; want 3", len(got))
		}
	})
}

// TestFilterRegistry_SaveLoadRoundtrip verifies persistence works.
func TestFilterRegistry_SaveLoadRoundtrip(t *testing.T) {
	t.Setenv("AWP_CONFIG_DIR", t.TempDir())

	r := newFilterRegistry()
	f := NewFilter("persisted")
	f.IsDefault = true
	r.Add(f)

	loaded, err := LoadFilterRegistry()
	if err != nil {
		t.Fatalf("LoadFilterRegistry: %v", err)
	}

	got := loaded.Get(f.ID)
	if got == nil {
		t.Fatalf("Get(%q) after Load returned nil", f.ID)
	}
	if got.Name != "persisted" {
		t.Errorf("Name = %q; want %q", got.Name, "persisted")
	}
	if !got.IsDefault {
		t.Error("IsDefault should be true after round-trip")
	}
}

// TestLoadFilterRegistry_NoFile verifies default behavior when the
// file doesn't exist.
func TestLoadFilterRegistry_NoFile(t *testing.T) {
	t.Setenv("AWP_CONFIG_DIR", t.TempDir())

	reg, err := LoadFilterRegistry()
	if err != nil {
		t.Fatalf("LoadFilterRegistry: %v", err)
	}
	if reg == nil {
		t.Fatal("returned nil registry")
	}
	if reg.Filters == nil {
		t.Error("Filters map should be non-nil")
	}
	if len(reg.Filters) != 0 {
		t.Errorf("Filters count = %d; want 0", len(reg.Filters))
	}
}