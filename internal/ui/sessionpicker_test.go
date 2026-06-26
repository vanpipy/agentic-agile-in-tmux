package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pi/awp/internal/pi"
	"github.com/pi/awp/internal/project"
)

// _ ensures all imports are referenced (forces the linter to keep
// them; removing imports from the list above is the cleaner fix).
var _ = fmt.Sprintf

// TestMatchProjectByCWD covers exact match, prefix match, and the
// fallback-to-first-project rules.
func TestMatchProjectByCWD(t *testing.T) {
	// Three projects with distinct repo paths.
	projA := &project.Project{ID: "A", RepoPath: "/tmp/repo-a"}
	projB := &project.Project{ID: "B", RepoPath: "/tmp/repo-b"}
	projC := &project.Project{ID: "C", RepoPath: "/tmp/repo-c"}

	projects := []*project.Project{projA, projB, projC}

	tests := []struct {
		name     string
		cwd      string
		wantID   string
	}{
		{"exact match first project", "/tmp/repo-a", "A"},
		{"exact match middle project", "/tmp/repo-b", "B"},
		{"exact match last project", "/tmp/repo-c", "C"},
		{"subdir of first project", "/tmp/repo-a/sub", "A"},
		{"deep subdir", "/tmp/repo-b/src/pkg/foo", "B"},
		{"no match falls back to first", "/tmp/no-match", "A"},
		{"empty cwd falls back to first", "", "A"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchProjectByCWD(projects, tt.cwd)
			if got != tt.wantID {
				t.Errorf("matchProjectByCWD(%q) = %q; want %q", tt.cwd, got, tt.wantID)
			}
		})
	}
}

func TestMatchProjectByCWD_EmptyProjects(t *testing.T) {
	if got := matchProjectByCWD(nil, "/tmp/anything"); got != "" {
		t.Errorf("matchProjectByCWD(nil) = %q; want empty", got)
	}
	if got := matchProjectByCWD([]*project.Project{}, "/tmp/anything"); got != "" {
		t.Errorf("matchProjectByCWD([]) = %q; want empty", got)
	}
}

// TestIsFilterChar documents which runes are allowed in the picker
// filter input. Anything else is rejected (keypress ignored).
func TestIsFilterChar(t *testing.T) {
	allowed := []rune{
		'a', 'z', 'm', // lowercase
		'A', 'Z', 'M', // uppercase
		'0', '5', '9', // digits
		'.', '/', '-', '_', '@', // safe symbols
	}
	for _, r := range allowed {
		if !isFilterChar(r) {
			t.Errorf("isFilterChar(%q) = false; want true", r)
		}
	}

	rejected := []rune{
		' ', '!', '?', ';', ':', ',',
		'(', ')', '[', ']', '{', '}',
		'<', '>', '=', '+', '*', '&', '^', '%', '$', '#',
		'!', '"', '\'', '`', '~', '\\', '|',
	}
	seen := map[rune]bool{}
	for _, r := range rejected {
		if seen[r] {
			continue
		}
		seen[r] = true
		if isFilterChar(r) {
			t.Errorf("isFilterChar(%q) = true; want false", r)
		}
	}
}

// TestTruncateID covers the cases for picker UI session ID display.
func TestTruncateID(t *testing.T) {
	tests := []struct {
		name string
		s    string
		n    int
		want string
	}{
		{"empty string", "", 5, ""},
		{"empty string max=0", "", 0, ""},
		{"short string fits", "abc", 5, "abc"},
		{"exact length", "abcde", 5, "abcde"},
		{"longer than max", "abcdefghij", 5, "abcd\u2026"}, // 4 chars + ellipsis (1 char) = 5 total
		{"n=2 truncates to 1 char + ellipsis", "abcdef", 2, "a\u2026"},
		{"n=1 hard cut (no ellipsis)", "abcdef", 1, "a"},
		{"n=0 hard cut", "abcdef", 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateID(tt.s, tt.n)
			if got != tt.want {
				t.Errorf("truncateID(%q, %d) = %q; want %q",
					tt.s, tt.n, got, tt.want)
			}
		})
	}
}

// TestFilteredPickerSessions exercises the client-side filter that
// narrows m.pickerSessions by CWD substring match (case-insensitive).
func TestFilteredPickerSessions(t *testing.T) {
	m := &Model{
		pickerSessions: []pi.SessionInfo{
			{ID: "1", CWD: "/home/user/projectA/src"},
			{ID: "2", CWD: "/home/user/projectB"},
			{ID: "3", CWD: "/home/user/projectA/tests"},
			{ID: "4", CWD: "/work/other"},
		},
	}

	tests := []struct {
		name       string
		filter     string
		wantCount  int
		wantIDs    []string // in order
	}{
		{"empty filter returns all", "", 4, []string{"1", "2", "3", "4"}},
		{"match first", "projecta", 2, []string{"1", "3"}},
		{"case insensitive match", "PROJECTA", 2, []string{"1", "3"}},
		{"match src only", "src", 1, []string{"1"}},
		{"match tests only", "tests", 1, []string{"3"}},
		{"match work", "work", 1, []string{"4"}},
		{"no match", "nonexistent", 0, []string{}},
		{"partial match", "project", 3, []string{"1", "2", "3"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.pickerFilter = tt.filter
			got := m.filteredPickerSessions()
			if len(got) != tt.wantCount {
				t.Errorf("filter=%q: got %d sessions; want %d",
					tt.filter, len(got), tt.wantCount)
			}
			for i, want := range tt.wantIDs {
				if i >= len(got) {
					break
				}
				if got[i].ID != want {
					t.Errorf("filter=%q: session[%d].ID = %q; want %q",
						tt.filter, i, got[i].ID, want)
				}
			}
		})
	}
}

func TestFilteredPickerSessions_EmptyPickerSessions(t *testing.T) {
	m := &Model{pickerSessions: nil, pickerFilter: "anything"}
	got := m.filteredPickerSessions()
	if got != nil && len(got) != 0 {
		t.Errorf("got %v; want empty slice", got)
	}
}

// TestMatchProjectByCWD_NormalizesRelativePath verifies that
// matchProjectByCWD normalizes relative paths via filepath.Abs
// before comparing — same behavior whether input is absolute or
// relative. This guards against edge cases where the picker is
// launched from a non-root cwd.
func TestMatchProjectByCWD_NormalizesRelativePath(t *testing.T) {
	// Create a real temp dir and a project inside it.
	base := t.TempDir() // auto-cleanup
	repoDir := base + "/repo-x"
	if err := mkdirAll(repoDir); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Chdir(base) // chdir into parent of repoDir

	proj := &project.Project{ID: "X", RepoPath: repoDir}
	projects := []*project.Project{proj}

	// Relative path that, when resolved, equals project path
	got := matchProjectByCWD(projects, "repo-x")
	if got != "X" {
		t.Errorf("relative-path match failed: got %q, want %q", got, "X")
	}

	// Same project, but absolute path — also matches
	got = matchProjectByCWD(projects, repoDir)
	if got != "X" {
		t.Errorf("absolute-path match failed: got %q, want %q", got, "X")
	}
}

// mkdirAll is a tiny wrapper that keeps the test code short.
func mkdirAll(dir string) error {
	return os.MkdirAll(dir, 0755)
}

// mustAbs is a test helper that fails the test if filepath.Abs fails.
func mustAbs(t *testing.T, p string) string {
	t.Helper()
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatalf("filepath.Abs(%q): %v", p, err)
	}
	return abs
}

// Sanity: ensure strings package is used (for test helpers above)
var _ = strings.Contains