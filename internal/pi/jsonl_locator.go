// jsonl_locator.go — Locate the JSONL session file for a given cwd.
//
// Ticket task/awp PR 2 (step 5): the poll loop needs to map a pane's
// workdir back to its JSONL session file so it can scan for
// stopReason. This file is the smallest possible helper layer for
// that — bigger decisions (per-session UUID, cwd-header verification,
// etc.) are deferred to future tickets per DONE_DETECTION_RESEARCH.md
// §5 R5.

package pi

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// SessionDirForCWD returns the absolute path to pi's session
// directory for the given workdir (e.g., `/home/u/.../sessions/--home-u---`).
// The path always resolves under `~/.pi/agent/sessions/<encoded-cwd>/`,
// matching pi-mono's convention (verified in DONE_DETECTION_RESEARCH.md
// §2.1).
//
// Returns ("", nil) cleanly if HOME cannot be determined — callers
// can then defer/ignore rather than crashing.
func SessionDirForCWD(cwd string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", errors.New("cannot determine home directory for pi session lookup (set $HOME)")
	}
	return filepath.Join(home, ".pi", "agent", "sessions", encodeCwdKey(cwd)), nil
}

// LatestSessionJSONL returns the path of the newest .jsonl file in
// the session directory for `cwd`. "Newest" is by mtime DESC.
//
// Returns ("", nil) if:
//   - the session directory doesn't exist yet (pi hasn't written here)
//   - the directory exists but has no .jsonl files yet
//
// Ambiguity when the user runs `pi` manually in the same worktree
// is documented in DONE_DETECTION_RESEARCH.md §5 R5 as accepted
// for v1. The "right" fix is to pin a session UUID at spawn time
// (--session); that's a separate ticket.
func LatestSessionJSONL(cwd string) (string, error) {
	dir, err := SessionDirForCWD(cwd)
	if err != nil {
		return "", err
	}
	return latestJSONLInDir(dir)
}

// latestJSONLInDir is the testable core of LatestSessionJSONL. The
// only difference from LatestSessionJSONL is that the directory is
// passed explicitly instead of being computed from cwd + $HOME.
// Keeping them separate lets unit tests avoid touching HOME.
func latestJSONLInDir(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	var newestPath string
	var newestMtime int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		mtime := info.ModTime().UnixNano()
		if newestPath == "" || mtime > newestMtime {
			newestPath = filepath.Join(dir, e.Name())
			newestMtime = mtime
		}
	}
	return newestPath, nil
}
