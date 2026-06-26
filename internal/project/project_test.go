package project

import (
	"path/filepath"
	"testing"
)

// TestProject_GetWorktreeDir covers the fallback behavior:
// explicit WorktreeDir wins; otherwise default to {RepoPath}-worktrees.
func TestProject_GetWorktreeDir(t *testing.T) {
	t.Run("explicit dir returned as-is", func(t *testing.T) {
		p := &Project{RepoPath: "/tmp/repo", WorktreeDir: "/custom/dir"}
		if got := p.GetWorktreeDir(); got != "/custom/dir" {
			t.Errorf("GetWorktreeDir() = %q; want %q", got, "/custom/dir")
		}
	})

	t.Run("empty falls back to {RepoPath}-worktrees", func(t *testing.T) {
		p := &Project{RepoPath: "/tmp/repo", WorktreeDir: ""}
		want := filepath.Clean("/tmp/repo") + "-worktrees"
		if got := p.GetWorktreeDir(); got != want {
			t.Errorf("GetWorktreeDir() = %q; want %q", got, want)
		}
	})
}

// TestProject_GetBranchPrefix covers project-level override +
// default fallback (which uses a config-level value via the model,
// but here we test the Project method's own fallback "task/").
func TestProject_GetBranchPrefix(t *testing.T) {
	t.Run("project override wins", func(t *testing.T) {
		p := &Project{Settings: ProjectSettings{BranchPrefix: "feature/"}}
		if got := p.GetBranchPrefix(); got != "feature/" {
			t.Errorf("GetBranchPrefix() = %q; want %q", got, "feature/")
		}
	})

	t.Run("empty falls back to default", func(t *testing.T) {
		p := &Project{Settings: ProjectSettings{}}
		if got := p.GetBranchPrefix(); got != "task/" {
			t.Errorf("GetBranchPrefix() = %q; want %q", got, "task/")
		}
	})
}

// TestProject_GetBranchTemplate covers the same override pattern.
func TestProject_GetBranchTemplate(t *testing.T) {
	t.Run("project override wins", func(t *testing.T) {
		p := &Project{Settings: ProjectSettings{BranchTemplate: "{prefix}/{slug}"}}
		if got := p.GetBranchTemplate(); got != "{prefix}/{slug}" {
			t.Errorf("GetBranchTemplate() = %q; want %q", got, "{prefix}/{slug}")
		}
	})

	t.Run("empty falls back to default", func(t *testing.T) {
		p := &Project{Settings: ProjectSettings{}}
		if got := p.GetBranchTemplate(); got != "{prefix}{slug}" {
			t.Errorf("GetBranchTemplate() = %q; want %q", got, "{prefix}{slug}")
		}
	})
}

// TestProject_GetSlugMaxLength covers the > 0 override pattern.
// 0 or negative = use default 40.
func TestProject_GetSlugMaxLength(t *testing.T) {
	tests := []struct {
		name     string
		setting  int
		expected int
	}{
		{"positive override", 30, 30},
		{"explicit zero falls back", 0, 40},
		{"negative falls back", -1, 40},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Project{Settings: ProjectSettings{SlugMaxLength: tt.setting}}
			if got := p.GetSlugMaxLength(); got != tt.expected {
				t.Errorf("GetSlugMaxLength(setting=%d) = %d; want %d",
					tt.setting, got, tt.expected)
			}
		})
	}
}

