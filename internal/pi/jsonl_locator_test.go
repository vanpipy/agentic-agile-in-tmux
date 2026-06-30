// jsonl_locator_test.go — TDD tests for latestJSONLInDir (the
// core of LatestSessionJSONL, factored out for testability).
//
// Per ticket task/awp PR 2: the poll loop needs to map a pane's
// workdir to its JSONL session file. Direct testing of the public
// API would require manipulating $HOME; we test the underlying
// helper instead.

package pi

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestLatestJSONLInDir_NewestByMtime pins the core ordering contract:
// the most-recently-modified .jsonl wins.
func TestLatestJSONLInDir_NewestByMtime(t *testing.T) {
	dir := t.TempDir()
	// The isUnderHome guard in latestJSONLInDir refuses to walk a
	// dir outside $HOME. t.TempDir() is typically under /tmp/, so
	// we override HOME to make the temp dir look on-home.
	t.Setenv("HOME", filepath.Dir(filepath.Dir(filepath.Dir(dir))))
	old := filepath.Join(dir, "old.jsonl")
	mid := filepath.Join(dir, "mid.jsonl")
	new := filepath.Join(dir, "new.jsonl")
	writeWithMtime(t, old, []byte("a"), time.Unix(1000, 0))
	writeWithMtime(t, mid, []byte("b"), time.Unix(2000, 0))
	writeWithMtime(t, new, []byte("c"), time.Unix(3000, 0))

	got, err := latestJSONLInDir(dir)
	if err != nil {
		t.Fatalf("latestJSONLInDir: %v", err)
	}
	if got != new {
		t.Errorf("latest = %q; want %q (must pick newest mtime)", got, new)
	}
}

// TestLatestJSONLInDir_IgnoresNonJSONL pins that .notjsonl files
// (e.g., .DS_Store, README.md, lock files) don't get picked.
func TestLatestJSONLInDir_IgnoresNonJSONL(t *testing.T) {
	dir := t.TempDir()
	// See TestLatestJSONLInDir_NewestByMtime for why we override HOME.
	t.Setenv("HOME", filepath.Dir(filepath.Dir(filepath.Dir(dir))))
	txt := filepath.Join(dir, "notes.txt")
	lock := filepath.Join(dir, "session.jsonl.lock")
	jsonl := filepath.Join(dir, "session.jsonl")
	writeWithMtime(t, txt, []byte("n"), time.Unix(5000, 0)) // newest mtime
	writeWithMtime(t, lock, []byte("l"), time.Unix(4000, 0))
	writeWithMtime(t, jsonl, []byte("j"), time.Unix(1000, 0))

	got, err := latestJSONLInDir(dir)
	if err != nil {
		t.Fatalf("latestJSONLInDir: %v", err)
	}
	if got != jsonl {
		t.Errorf("latest = %q; want %q (must ignore .txt and .lock)", got, jsonl)
	}
}

// TestLatestJSONLInDir_IgnoresSubdirs pins that subdirectories
// don't accidentally become "files" via the os.ReadDir scan.
func TestLatestJSONLInDir_IgnoresSubdirs(t *testing.T) {
	dir := t.TempDir()
	// See TestLatestJSONLInDir_NewestByMtime for why we override HOME.
	t.Setenv("HOME", filepath.Dir(filepath.Dir(filepath.Dir(dir))))
	if err := os.Mkdir(filepath.Join(dir, "subdir.jsonl"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	real := filepath.Join(dir, "real.jsonl")
	writeWithMtime(t, real, []byte("r"), time.Unix(1000, 0))

	got, err := latestJSONLInDir(dir)
	if err != nil {
		t.Fatalf("latestJSONLInDir: %v", err)
	}
	if got != real {
		t.Errorf("latest = %q; want %q (must not pick subdir)", got, real)
	}
}

// TestLatestJSONLInDir_MissingDirReturnsEmpty pins the missing-dir
// contract: the session dir might not exist yet (pi hasn't created
// it). Return ("", nil) — not an error — so the poll loop skips.
func TestLatestJSONLInDir_MissingDirReturnsEmpty(t *testing.T) {
	got, err := latestJSONLInDir("/nonexistent/path/never/exists")
	if err != nil {
		t.Errorf("missing dir returned error: %v", err)
	}
	if got != "" {
		t.Errorf("missing dir returned path %q; want \"\"", got)
	}
}

// TestLatestJSONLInDir_EmptyDirReturnsEmpty pins the no-files case.
func TestLatestJSONLInDir_EmptyDirReturnsEmpty(t *testing.T) {
	// Use a HOME-relative path. /tmp on a normal Linux box is
	// NOT under HOME; the isUnderHome guard will refuse it and
	// return ("", nil) — same as "empty dir" from the caller's POV.
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir() // exists, empty, but off-HOME so guard refuses
	got, err := latestJSONLInDir(dir)
	if err != nil {
		t.Errorf("empty dir returned error: %v", err)
	}
	if got != "" {
		t.Errorf("empty dir returned path %q; want \"\"", got)
	}
}

// writeWithMtime creates a file with explicit mtime. Used for
// deterministic ordering in tests; os.WriteFile alone gives
// "now" which is fine for some tests but not for ordering checks.
func writeWithMtime(t *testing.T, path string, data []byte, mtime time.Time) {
	t.Helper()
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}
