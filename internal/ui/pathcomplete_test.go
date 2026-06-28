package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCompletePath_Empty: no input → no suggestions.
// (Bubbles textinput filters by HasPrefix(value, suggestion). With no
// value, every suggestion would match — which is what we want for the
// "open the form and see ~/ subdirs" case, not this one. We return
// nil to let the caller handle the empty-input case differently.)
func TestCompletePath_Empty(t *testing.T) {
	if got := completePath(""); got != nil {
		t.Errorf("empty input: got %v, want nil", got)
	}
}

// TestCompletePath_NonexistentParent: parent dir doesn't exist → nil.
// e.g. user types "totally/made/up/path" — no way to enumerate matches.
func TestCompletePath_NonexistentParent(t *testing.T) {
	if got := completePath("/nonexistent_xyz_123_zzz/abc"); got != nil {
		t.Errorf("nonexistent parent: got %v, want nil", got)
	}
}

// TestCompletePath_BasicPrefix: type a prefix, get the matching subdir.
// Uses t.TempDir() for isolation.
func TestCompletePath_BasicPrefix(t *testing.T) {
	tmp := t.TempDir()
	for _, name := range []string{"alpha", "beta", "gamma", "delta"} {
		if err := os.Mkdir(filepath.Join(tmp, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	matches := completePath(tmp + "/a")
	if len(matches) != 1 {
		t.Fatalf("prefix %q/a: got %d matches, want 1: %v", tmp, len(matches), matches)
	}
	if !strings.HasSuffix(matches[0], "/alpha/") {
		t.Errorf("match: got %q, want suffix /alpha/", matches[0])
	}
}

// TestCompletePath_ExactMatch: type the full name of an existing
// (empty) dir → returns [self+"/"]. Subdirs would be added if any
// existed; this test pins the empty-dir edge case so we don't get []
// for what should be a one-keystroke TAB-to-confirm.
func TestCompletePath_ExactMatch(t *testing.T) {
	tmp := t.TempDir()
	if err := os.Mkdir(filepath.Join(tmp, "only"), 0o755); err != nil {
		t.Fatal(err)
	}

	matches := completePath(tmp + "/only")
	if len(matches) != 1 {
		t.Fatalf("exact match (empty dir): got %d, want 1: %v", len(matches), matches)
	}
	if !strings.HasSuffix(matches[0], "/only/") {
		t.Errorf("match: got %q, want suffix /only/", matches[0])
	}
}

// TestCompletePath_FiltersFiles: only directories, not files.
func TestCompletePath_FiltersFiles(t *testing.T) {
	tmp := t.TempDir()
	// Create one dir and one file with the same prefix
	if err := os.Mkdir(filepath.Join(tmp, "a_dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "a_file"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	matches := completePath(tmp + "/a")
	if len(matches) != 1 {
		t.Fatalf("got %d matches, want 1 (only dir): %v", len(matches), matches)
	}
	if !strings.HasSuffix(matches[0], "/a_dir/") {
		t.Errorf("got %q, want /a_dir/", matches[0])
	}
}

// TestCompletePath_SkipsHidden: dotfiles excluded UNLESS base is ".".
// Mimics bash's default dotglob=off behavior, and bash's opt-in
// when the user explicitly types a leading dot.
func TestCompletePath_SkipsHidden(t *testing.T) {
	tmp := t.TempDir()
	if err := os.Mkdir(filepath.Join(tmp, ".hidden"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(tmp, "visible"), 0o755); err != nil {
		t.Fatal(err)
	}

	// "tmp/." → only hidden matches (user explicitly opted in).
	matches := completePath(tmp + "/.")
	if len(matches) != 1 || !strings.HasSuffix(matches[0], "/.hidden/") {
		t.Errorf("expected 1 hidden match for %q/., got %v", tmp, matches)
	}

	// "tmp/v" → only visible (non-dot prefix excludes dotfiles).
	matches = completePath(tmp + "/v")
	if len(matches) != 1 || !strings.HasSuffix(matches[0], "/visible/") {
		t.Errorf("expected 1 match for /v: %v", matches)
	}

	// "tmp" alone (existing dir) → both, since HasPrefix bubbles
	// filter will narrow as user types more. We exclude hidden here
	// only if the user's base is empty AND the entry is hidden; we
	// currently return all subdirs (incl. hidden) so the user can
	// see and tab to them after typing the leading dot.
	matches = completePath(tmp)
	// Expect [tmp/, tmp/.hidden/, tmp/visible/]
	if len(matches) != 3 {
		t.Errorf("existing dir: got %d, want 3 (self + 2 subdirs incl hidden): %v",
			len(matches), matches)
	}
}

// TestCompletePath_TrailingSlashOnDirs: dir matches get a trailing /.
// Typing the full name of an existing (empty) dir returns it with
// a trailing slash so the user can immediately drill deeper.
func TestCompletePath_TrailingSlashOnDirs(t *testing.T) {
	tmp := t.TempDir()
	if err := os.Mkdir(filepath.Join(tmp, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	matches := completePath(tmp + "/sub")
	if len(matches) != 1 {
		t.Fatalf("got %d matches, want 1: %v", len(matches), matches)
	}
	if !strings.HasSuffix(matches[0], "/") {
		t.Errorf("dir match should have trailing /, got %q", matches[0])
	}
}

// TestCompletePath_ExistingDir: typing an existing dir returns
// [self+"/", subdir1, subdir2, ...]. Self is first so a single TAB
// confirms; the subdirs are available for arrow-key browsing
// (and the bubbles HasPrefix filter will narrow them as the user
// types more characters).
func TestCompletePath_ExistingDir(t *testing.T) {
	tmp := t.TempDir()
	for _, name := range []string{"x_a", "x_b", "y_c"} {
		if err := os.Mkdir(filepath.Join(tmp, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	matches := completePath(tmp)
	if len(matches) != 4 {
		t.Fatalf("existing dir: got %d, want 4 (self + 3 subdirs): %v", len(matches), matches)
	}
	// First match must be the dir itself (so TAB confirms).
	if !strings.HasSuffix(matches[0], "/") || strings.Count(matches[0], "/") != strings.Count(tmp, "/")+1 {
		t.Errorf("first match should be self with trailing /, got %q (tmp=%q)", matches[0], tmp)
	}
}

// TestCompletePath_TildeExpansion: ~/foo should re-prefix to ~/.
func TestCompletePath_TildeExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home dir")
	}

	// Create a known dir in $HOME
	target := filepath.Join(home, ".awp_pathcomplete_test_target")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(target) })

	matches := completePath("~/.awp_pathcomplete_test_target")
	if len(matches) != 1 {
		t.Fatalf("got %d matches, want 1: %v", len(matches), matches)
	}
	if !strings.HasPrefix(matches[0], "~/") {
		t.Errorf("expected ~/ prefix, got %q", matches[0])
	}
	if !strings.HasSuffix(matches[0], "/.awp_pathcomplete_test_target/") {
		t.Errorf("expected target dir suffix, got %q", matches[0])
	}
}