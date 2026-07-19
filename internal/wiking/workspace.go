// Package wiking — workspace.go implements §18.6 file layout and
// §18.9 resume-from-disk. Workspace is filesystem-only: it knows
// paths, naming conventions, atomic sync, and how to enumerate
// article-N.md files for the cycle driver to interpret.

package wiking

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// WorkspaceConfig builds a Workspace. All fields are required so the
// caller (config-aware layer) decides the roots; tests pass t.TempDir
// for both WikiDir and AWPHome.
type WorkspaceConfig struct {
	// WikiDir is the directory holding articles. Typically the root
	// of a git repo the user owns (the "wiki"). Not auto-created —
	// the caller is expected to manage its existence.
	WikiDir string
	// RunID is a unique identifier for this cycle run, used as the
	// subdirectory under <AWPHome>/cycle/. Cycle library generates
	// one via time-based + random hash.
	RunID string
	// AWPHome is the awp state root (typically ~/.awp). The run
	// subdir lives under <AWPHome>/cycle/<RunID>/ and is auto-created.
	AWPHome string
}

// Workspace knows where the cycle's files live. Created once per cycle.
type Workspace struct {
	wikiDir string
	runID   string
	awpHome string
}

// NewWorkspace validates config and creates the cycle run directory.
// WikiDir is NOT auto-created (it's a user-managed git repo). RunDir
// IS auto-created because it holds events.jsonl which the postman owns.
func NewWorkspace(cfg WorkspaceConfig) (*Workspace, error) {
	if cfg.WikiDir == "" {
		return nil, errors.New("wiking: WorkspaceConfig.WikiDir required")
	}
	if cfg.RunID == "" {
		return nil, errors.New("wiking: WorkspaceConfig.RunID required")
	}
	if cfg.AWPHome == "" {
		return nil, errors.New("wiking: WorkspaceConfig.AWPHome required")
	}
	runDir := filepath.Join(cfg.AWPHome, "cycle", cfg.RunID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return nil, fmt.Errorf("wiking: mkdir %s: %w", runDir, err)
	}
	return &Workspace{wikiDir: cfg.WikiDir, runID: cfg.RunID, awpHome: cfg.AWPHome}, nil
}

// WikiDir returns the configured wiki directory.
func (w *Workspace) WikiDir() string { return w.wikiDir }

// RunID returns the configured run identifier.
func (w *Workspace) RunID() string { return w.runID }

// AWPHome returns the configured awp state root.
func (w *Workspace) AWPHome() string { return w.awpHome }

// WikingPath returns the path the wiking-role pi writes its draft to.
// Convention per §18.6: <WikiDir>/article-<N>.md.
func (w *Workspace) WikingPath(n int) string {
	return filepath.Join(w.wikiDir, fmt.Sprintf("article-%d.md", n))
}

// FeedbackPath returns the path the coding-role pi writes feedback to.
// Convention per §18.6: <WikiDir>/article-<N>-feedback-<N>.md.
// The two Ns are the same (one round — one pair of files).
func (w *Workspace) FeedbackPath(n int) string {
	return filepath.Join(w.wikiDir, fmt.Sprintf("article-%d-feedback-%d.md", n, n))
}

// CanonicalPath returns the synced article (the "winner" of accepted rounds).
// Convention per §18.6: <WikiDir>/article.md.
func (w *Workspace) CanonicalPath() string {
	return filepath.Join(w.wikiDir, "article.md")
}

// RunDir returns the per-run subdirectory under <AWPHome>/cycle/<RunID>/.
// Holds events.jsonl and any other per-run metadata.
func (w *Workspace) RunDir() string {
	return filepath.Join(w.awpHome, "cycle", w.runID)
}

// EventsPath returns the events.jsonl path for this run.
// §18.6: ~/.awp/cycle/<run-id>/events.jsonl.
func (w *Workspace) EventsPath() string {
	return filepath.Join(w.RunDir(), "events.jsonl")
}

// SyncOnAccept atomically replaces article.md with article-<n>.md.
// Uses write-then-rename for crash-safety (a partial write to a
// renamed file is either fully present or fully absent — never half).
func (w *Workspace) SyncOnAccept(n int) error {
	src := w.WikingPath(n)
	dst := w.CanonicalPath()
	tmp := dst + ".tmp"

	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("wiking: read %s: %w", src, err)
	}
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("wiking: write tmp %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		// Best-effort cleanup before returning.
		_ = os.Remove(tmp)
		return fmt.Errorf("wiking: rename %s -> %s: %w", tmp, dst, err)
	}
	return nil
}

// ResumeRound scans WikiDir for `article-N.md` files with a valid
// wiking-end marker (§18.5) and returns the max N. Returns 0 if
// nothing valid exists (fresh cycle). Feedback files and the
// canonical article.md are ignored — the cycle driver inspects
// them separately to decide which phase to resume in.
func (w *Workspace) ResumeRound() (int, error) {
	entries, err := os.ReadDir(w.wikiDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil // wiki not initialized — treat as fresh
		}
		return 0, fmt.Errorf("wiking: read %s: %w", w.wikiDir, err)
	}
	maxN := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n, err := parseArticleN(e.Name())
		if err != nil {
			continue // not an article-N.md
		}
		ok, err := CheckEnd(w.WikingPath(n))
		if err != nil {
			// ErrNotYetWritten (vanished) or ErrMalformedMarker
			// (wiking wrote but didn't finalize). Either way, this
			// round is incomplete — skip and look for a smaller N.
			continue
		}
		if !ok {
			continue
		}
		if n > maxN {
			maxN = n
		}
	}
	return maxN, nil
}

// parseArticleN parses `article-N.md` to extract the integer N.
// Returns an error if the name doesn't match. The empty-N case
// (`article-.md`, `article.md`) returns an error too.
func parseArticleN(name string) (int, error) {
	const prefix = "article-"
	const suffix = ".md"
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
		return 0, fmt.Errorf("wiking: %q not %sN%s", name, prefix, suffix)
	}
	s := name[len(prefix) : len(name)-len(suffix)]
	if s == "" {
		return 0, fmt.Errorf("wiking: %q has no N", name)
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("wiking: %q has non-numeric N: %w", name, err)
	}
	if n < 0 {
		return 0, fmt.Errorf("wiking: %q has negative N", name)
	}
	return n, nil
}
