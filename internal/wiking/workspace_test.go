// workspace_test.go — RED tests for file layout, naming, sync, and
// resume-from-disk per SYSTEM_DESIGN.md §18.6 and §18.9.
//
// Workspace is filesystem-only: it knows paths, naming conventions,
// atomic sync, and how to enumerate article-N.md files. The cycle
// driver (cycle.go) interprets the disk state into "what to do next".

package wiking

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Path conventions.

func TestWorkspace_PathConventions(t *testing.T) {
	ws, err := NewWorkspace(WorkspaceConfig{
		WikiDir: "/tmp/wiki",
		RunID:   "run-1",
		AWPHome: "/tmp/awp",
	})
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		got  string
		want string
	}{
		{ws.WikingPath(1), "/tmp/wiki/article-1.md"},
		{ws.WikingPath(7), "/tmp/wiki/article-7.md"},
		{ws.FeedbackPath(2), "/tmp/wiki/article-2-feedback-2.md"},
		{ws.FeedbackPath(12), "/tmp/wiki/article-12-feedback-12.md"},
		{ws.CanonicalPath(), "/tmp/wiki/article.md"},
		{ws.RunDir(), "/tmp/awp/cycle/run-1"},
		{ws.EventsPath(), "/tmp/awp/cycle/run-1/events.jsonl"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("got %q want %q", c.got, c.want)
		}
	}
}

// Construction.

func TestNewWorkspace_CreatesRunDir(t *testing.T) {
	awp := t.TempDir()
	_, err := NewWorkspace(WorkspaceConfig{
		WikiDir: t.TempDir(),
		RunID:   "test-run",
		AWPHome: awp,
	})
	if err != nil {
		t.Fatal(err)
	}
	expected := filepath.Join(awp, "cycle", "test-run")
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("run dir not created: %v", err)
	}
}

func TestNewWorkspace_RequiresFields(t *testing.T) {
	awp := t.TempDir()
	wiki := t.TempDir()
	cases := []struct {
		name string
		cfg  WorkspaceConfig
	}{
		{"missing wiki", WorkspaceConfig{RunID: "x", AWPHome: awp}},
		{"missing runid", WorkspaceConfig{WikiDir: wiki, AWPHome: awp}},
		{"missing awphome", WorkspaceConfig{WikiDir: wiki, RunID: "x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewWorkspace(tc.cfg)
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

// SyncOnAccept.

func TestSyncOnAccept_HappyPath(t *testing.T) {
	wiki := t.TempDir()
	ws, err := NewWorkspace(WorkspaceConfig{WikiDir: wiki, RunID: "r", AWPHome: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}

	src := ws.WikingPath(5)
	if err := os.WriteFile(src, []byte("# accept me\n---\n--- end ---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ws.SyncOnAccept(5); err != nil {
		t.Fatalf("sync: %v", err)
	}

	canonical, err := os.ReadFile(ws.CanonicalPath())
	if err != nil {
		t.Fatal(err)
	}
	srcData, _ := os.ReadFile(src)
	if !bytes.Equal(canonical, srcData) {
		t.Fatalf("canonical differs from src:\ncanonical=%q\nsrc=%q", canonical, srcData)
	}
}

func TestSyncOnAccept_SourceMissingErrors(t *testing.T) {
	ws, _ := NewWorkspace(WorkspaceConfig{WikiDir: t.TempDir(), RunID: "r", AWPHome: t.TempDir()})
	if err := ws.SyncOnAccept(5); err == nil {
		t.Fatal("expected error for missing source")
	}
}

func TestSyncOnAccept_OverwritesCanonical(t *testing.T) {
	wiki := t.TempDir()
	ws, _ := NewWorkspace(WorkspaceConfig{WikiDir: wiki, RunID: "r", AWPHome: t.TempDir()})

	if err := os.WriteFile(ws.CanonicalPath(), []byte("OLD CONTENT\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := ws.WikingPath(3)
	if err := os.WriteFile(src, []byte("NEW CONTENT\n--- end ---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ws.SyncOnAccept(3); err != nil {
		t.Fatal(err)
	}

	canonical, _ := os.ReadFile(ws.CanonicalPath())
	if !strings.Contains(string(canonical), "NEW CONTENT") {
		t.Fatalf("canonical didn't update: %q", canonical)
	}
}

// ResumeRound.

func TestResumeRound_FreshDir(t *testing.T) {
	wiki := t.TempDir()
	ws, _ := NewWorkspace(WorkspaceConfig{WikiDir: wiki, RunID: "r", AWPHome: t.TempDir()})

	n, err := ws.ResumeRound()
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("fresh dir: got %d want 0", n)
	}
}

func TestResumeRound_FindsHighestValid(t *testing.T) {
	wiki := t.TempDir()
	ws, _ := NewWorkspace(WorkspaceConfig{WikiDir: wiki, RunID: "r", AWPHome: t.TempDir()})

	// Rounds 2 and 5 have valid markers; 3 has malformed (no end marker).
	// Resume should return 5 (max valid).
	if err := os.WriteFile(ws.WikingPath(2), []byte("body\n--- end ---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ws.WikingPath(3), []byte("body\nstill writing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ws.WikingPath(5), []byte("body\n--- end ---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	n, err := ws.ResumeRound()
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 {
		t.Fatalf("got %d want 5", n)
	}
}

func TestResumeRound_AcceptsMidRange(t *testing.T) {
	wiki := t.TempDir()
	ws, _ := NewWorkspace(WorkspaceConfig{WikiDir: wiki, RunID: "r", AWPHome: t.TempDir()})

	if err := os.WriteFile(ws.WikingPath(1), []byte("a\n--- end ---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ws.WikingPath(4), []byte("d\n--- end ---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	n, _ := ws.ResumeRound()
	if n != 4 {
		t.Fatalf("got %d want 4 (max valid)", n)
	}
}

func TestResumeRound_IgnoresNonArticleFiles(t *testing.T) {
	wiki := t.TempDir()
	ws, _ := NewWorkspace(WorkspaceConfig{WikiDir: wiki, RunID: "r", AWPHome: t.TempDir()})

	if err := os.WriteFile(ws.WikingPath(2), []byte("x\n--- end ---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Files that LOOK like articles but aren't `article-N.md`:
	canonical := []byte("canonical\n")
	feedback := []byte("feedback\n--- end with 87 ---\n")
	readme := []byte("readme\n")
	if err := os.WriteFile(ws.CanonicalPath(), canonical, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ws.FeedbackPath(2), feedback, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wiki, "README.md"), readme, 0o644); err != nil {
		t.Fatal(err)
	}

	n, _ := ws.ResumeRound()
	if n != 2 {
		t.Fatalf("got %d want 2", n)
	}
}

func TestResumeRound_NoMarkerFileIsSkipped(t *testing.T) {
	wiki := t.TempDir()
	ws, _ := NewWorkspace(WorkspaceConfig{WikiDir: wiki, RunID: "r", AWPHome: t.TempDir()})

	// Round 4 ends with --- end --- so passes.
	if err := os.WriteFile(ws.WikingPath(4), []byte("ok\n--- end ---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Round 5 has --- end --- and one round 6 has the same; both valid.
	if err := os.WriteFile(ws.WikingPath(6), []byte("ok\n--- end ---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	n, _ := ws.ResumeRound()
	if n != 6 {
		t.Fatalf("got %d want 6", n)
	}
}

func TestParseArticleN(t *testing.T) {
	cases := []struct {
		in   string
		want int
		ok   bool
	}{
		{"article-1.md", 1, true},
		{"article-42.md", 42, true},
		{"article-0.md", 0, true},
		{"article.md", 0, false},
		{"article-1.txt", 0, false},
		{"article-1x.md", 0, false},
		{"ARTICLE-1.md", 0, false},
		{"article--1.md", 0, false},
		{"article-01.md", 1, true}, // leading zero allowed
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			n, err := parseArticleN(tc.in)
			if tc.ok && err != nil {
				t.Fatalf("got err: %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatalf("expected err")
			}
			if tc.ok && n != tc.want {
				t.Fatalf("got %d want %d", n, tc.want)
			}
		})
	}
}

// TestSanitizeRunID — RunID is the path component under
// ~/.awp/cycle/<RunID>/, so any path-unsafe character in the
// input stem is a directory-escape vector. SanitizeRunID
// replaces '/', '\\', and other filesystem-special characters
// with '_'. Empty input returns "default" to match the UI's
// blank-stem fallback.
//
// Without sanitization: a stem like "../../../tmp/foo" would
// cause MkdirAll to create directories outside the cycle/
// subdir. Defense in depth — the user's $HOME is theirs, but
// ticket titles (the UI's typical stem source) can come from
// external sources, so the path traversal shouldn't exist in
// the first place.
func TestSanitizeRunID(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty_returns_default", "", "default"},
		{"plain_unchanged", "my-article", "my-article"},
		{"slash_replaced", "foo/bar", "foo_bar"},
		{"backslash_replaced", "foo\\bar", "foo_bar"},
		{"colon_replaced", "C:\\path", "C__path"},
		{"dotdot_neutralised", "../../../tmp/foo", ".._.._.._tmp_foo"},
		{"multiple_slashes", "a/b/c/d", "a_b_c_d"},
		{"semicolon_passes_through", "foo;rm -rf", "foo;rm -rf"},
		// Note: ';' is a shell metachar but legal in filenames on
		// every common filesystem. The sanitizer targets path
		// traversal (which needs '/' or '\'), not shell injection
		// (which would need a separate shlex pass). runID is a
		// path component, not a shell arg.
		{"quoted_replaced", `"foo"`, "_foo_"},
		{"glob_neutralised", "foo*?", "foo__"},
		{"null_byte_replaced", "foo\x00bar", "foo_bar"},
		{"unicode_passthrough", "café-über", "café-über"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeRunID(tc.in)
			if got != tc.want {
				t.Errorf("SanitizeRunID(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
