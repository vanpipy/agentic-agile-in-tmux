package pi

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ErrSessionNotFound is returned by SessionStore methods when the
// requested session ID (full or prefix) doesn't match any on-disk
// session. Callers should compare via errors.Is.
var ErrSessionNotFound = errors.New("session not found")

// SessionInfo describes a single pi session discovered on disk.
//
// Fields are read from the session file's header (first line) and a
// sample of entries. We don't load the full message history here —
// that's what Read() is for.
type SessionInfo struct {
	ID            string    // pi UUID v7
	Path          string    // full .jsonl path
	CWD           string    // working directory when session was created
	Timestamp     time.Time // session start time
	ParentID      string    // empty unless this is a forked session
	Branch        string    // inferred from session name (best effort)
	ModelProvider string    // from model_change entry
	ModelID       string    // from model_change entry
	ThinkingLevel string    // from thinking_level_change entry
	MessageCount  int       // number of message entries
	ToolCount     int       // number of tool_execution entries (Phase 3)
	FirstPrompt   string    // truncated to 80 chars
	LastAssistant string    // truncated to 80 chars
	LastActivity  time.Time // timestamp of last entry (Phase 3)
}

// SessionStore scans and reads pi session files on disk.
//
// Default location: ~/.pi/agent/sessions/{encoded-cwd}/*.jsonl
type SessionStore struct {
	agentDir   string // defaults to ~/.pi/agent
	lastSkipped int   // count of files skipped during the last List call

	// Lazy index for FindByID. Built on first call; subsequent lookups
	// are O(1). The index is stale-tolerant: if files are added/removed
	// after the index is built, FindByID may return stale results. For
	// one-shot CLI usage this is fine; the TUI uses List(cwd) instead.
	indexOnce sync.Once
	index     map[string]string // session ID (stem of .jsonl) → full path
}

// NewSessionStore creates a SessionStore. agentDir is the path to
// pi's agent directory (contains the `sessions/` subdir). If empty,
// defaults to ~/.pi/agent — but if home detection fails, returns an
// error rather than silently producing a relative path that could
// clobber the user's CWD.
func NewSessionStore(agentDir string) (*SessionStore, error) {
	if agentDir == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return nil, fmt.Errorf("cannot determine home directory for pi agent path (set $HOME or pass agentDir explicitly): %w", err)
		}
		agentDir = filepath.Join(home, ".pi", "agent")
	}
	return &SessionStore{agentDir: agentDir}, nil
}

// sessionsDir returns the path to the sessions/ subdir.
func (s *SessionStore) sessionsDir() string {
	return filepath.Join(s.agentDir, "sessions")
}

// List returns all sessions for the given cwd (or all sessions if cwd is "").
// Results are sorted by Timestamp DESC (newest first).
//
// Per the design, we only read the first line of each .jsonl file for
// performance. Use Read() to load the full content.
//
// Files that fail to parse are silently skipped (e.g. truncated,
// corrupt, or in-progress writes). The number of skipped files is
// available via ListSkipped() to surface diagnostics to the user.
func (s *SessionStore) List(cwd string) ([]SessionInfo, error) {
	sessionsDir := s.sessionsDir()
	if _, err := os.Stat(sessionsDir); os.IsNotExist(err) {
		// No sessions dir yet — return empty list, not an error
		return []SessionInfo{}, nil
	}

	target := sessionsDir
	if cwd != "" {
		cwdKey := encodeCwdKey(cwd)
		target = filepath.Join(sessionsDir, cwdKey)
	}

	entries, err := os.ReadDir(target)
	if err != nil {
		if os.IsNotExist(err) {
			return []SessionInfo{}, nil
		}
		return nil, fmt.Errorf("read sessions dir: %w", err)
	}

	var sessions []SessionInfo
	skipped := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(target, e.Name())
		info, err := parseSessionInfo(path)
		if err != nil {
			// Phase 3 audit: previously silent. Now we count skipped
			// files so ListSkipped() can surface diagnostics. We
			// don't log per-file to avoid noise; user can run
			// `awp doctor` (Phase 5) for detailed diagnostics.
			skipped++
			continue
		}
		sessions = append(sessions, info)
	}
	s.lastSkipped = skipped

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].Timestamp.After(sessions[j].Timestamp)
	})

	return sessions, nil
}

// ListSkipped returns the number of files skipped during the most
// recent List call due to parse errors. Use this to surface
// diagnostics to the user (e.g. "12 sessions loaded, 1 skipped
// (corrupt)").
func (s *SessionStore) ListSkipped() int {
	return s.lastSkipped
}

// SessionContent represents the full content of a session file.
type SessionContent struct {
	Header  SessionHeader     `json:"header"`
	Entries []json.RawMessage `json:"entries"`
	Info    SessionInfo       `json:"info"`
}

// SessionHeader mirrors pi-mono's session header (first line of .jsonl).
type SessionHeader struct {
	Type          string `json:"type"`
	Version       int    `json:"version,omitempty"` // optional in v1
	ID            string `json:"id"`
	Timestamp     string `json:"timestamp"`
	CWD           string `json:"cwd"`
	ParentSession string `json:"parentSession,omitempty"`
}

// Read loads the full content of a session file at the given path.
func (s *SessionStore) Read(path string) (*SessionContent, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open session file: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024) // 4MB max line

	var header SessionHeader
	var entries []json.RawMessage
	first := true

	for scanner.Scan() {
		line := scanner.Bytes()
		if first {
			if err := json.Unmarshal(line, &header); err != nil {
				return nil, fmt.Errorf("parse header: %w", err)
			}
			first = false
			continue
		}
		entries = append(entries, json.RawMessage(append([]byte{}, line...)))
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan session file: %w", err)
	}

	info, err := parseSessionInfo(path)
	if err != nil {
		return nil, fmt.Errorf("parse session info: %w", err)
	}

	return &SessionContent{
		Header:  header,
		Entries: entries,
		Info:    info,
	}, nil
}

// parseSessionInfo reads the first line + up to maxScanLines for
// summary. Fast (O(1) file open + bounded scan). For sessions
// longer than maxScanLines, MessageCount and ToolCount are
// undercounted (intentional — accuracy beyond summary is not
// needed for the picker UX).
//
// Note: previous version scanned the entire file (O(n)). That was
// a Phase 3 foot-finding: callers like `awp session list` and
// FindByID both invoke parseSessionInfo on every session, so a
// 100K-line session = 100K lines of I/O per list. Bounded scan
// keeps picker snappy.
const maxScanLines = 200

func parseSessionInfo(path string) (SessionInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return SessionInfo{}, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4*1024), 4*1024*1024)
	if !scanner.Scan() {
		return SessionInfo{}, fmt.Errorf("empty file")
	}

	var header SessionHeader
	if err := json.Unmarshal(scanner.Bytes(), &header); err != nil {
		return SessionInfo{}, fmt.Errorf("parse header: %w", err)
	}

	if header.Type != "session" {
		return SessionInfo{}, fmt.Errorf("not a session file (type=%q)", header.Type)
	}

	info := SessionInfo{
		ID:   header.ID,
		Path: path,
		CWD:  header.CWD,
	}

	if t, err := time.Parse(time.RFC3339Nano, header.Timestamp); err == nil {
		info.Timestamp = t
	} else if t, err := time.Parse(time.RFC3339, header.Timestamp); err == nil {
		info.Timestamp = t
	}
	info.LastActivity = info.Timestamp

	info.ParentID = header.ParentSession

	scanned := 0
	for scanner.Scan() && scanned < maxScanLines {
		line := scanner.Bytes()
		scanned++
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var entry struct {
			Type     string          `json:"type"`
			Provider string          `json:"provider,omitempty"`
			ModelID  string          `json:"modelId,omitempty"`
			Level    string          `json:"level,omitempty"`
			Message  json.RawMessage `json:"message,omitempty"`
			Content  string          `json:"content,omitempty"`
			Role     string          `json:"role,omitempty"`
			Time     string          `json:"time,omitempty"`
		}
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}

		switch entry.Type {
		case "model_change":
			if info.ModelProvider == "" {
				info.ModelProvider = entry.Provider
				info.ModelID = entry.ModelID
			}
		case "thinking_level_change":
			if info.ThinkingLevel == "" {
				info.ThinkingLevel = entry.Level
			}
		case "message":
			info.MessageCount++
			var m struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			}
			if err := json.Unmarshal(entry.Message, &m); err == nil {
				if m.Role == "user" && info.FirstPrompt == "" {
					info.FirstPrompt = truncate(m.Content, 80)
				}
				if m.Role == "assistant" {
					info.LastAssistant = truncate(m.Content, 80)
				}
			}
		case "tool_execution_start":
			info.ToolCount++
		}

		// Update LastActivity with this entry's timestamp if present
		if entry.Time != "" {
			if t, err := time.Parse(time.RFC3339Nano, entry.Time); err == nil {
				info.LastActivity = t
			} else if t, err := time.Parse(time.RFC3339, entry.Time); err == nil {
				info.LastActivity = t
			}
		}
	}
	// If file has more entries than maxScanLines, mark as truncated.
	// The picker can show "(truncated)" to inform the user.
	if scanner.Scan() {
		// More lines exist after the bounded scan
		// (we don't count them — that's the point of the bound).
	}

	return info, nil
}

// FindByID locates a session file by ID across all encoded-cwd dirs.
// Returns (SessionInfo, true) on hit, (zero, false) on miss.
//
// First call builds an in-memory index (id → filepath); subsequent calls
// are O(1) lookup. Index is built lazily via sync.Once. Trade-offs:
//   - One-shot CLI calls: index is built once, used once — same cost as
//     the previous O(n) walk, plus a slightly higher peak memory.
//   - Long-running TUI: the TUI doesn't use FindByID (uses List(cwd)
//     instead), so the index never gets built.
//
// Prefix matching (≥8 chars) is preserved for backward compatibility.
//
// Phase 3 caller: `awp session show <id>`, `awp session resume <id>`.
func (s *SessionStore) FindByID(id string) (SessionInfo, bool) {
	s.indexOnce.Do(s.buildIndex)

	// Try exact match first.
	if path, ok := s.index[id]; ok {
		if info, err := parseSessionInfo(path); err == nil {
			return info, true
		}
	}

	// Fall back to prefix match (id >= 8 chars).
	if len(id) >= 8 {
		for stem, path := range s.index {
			if strings.HasPrefix(stem, id) {
				if info, err := parseSessionInfo(path); err == nil {
					return info, true
				}
			}
		}
	}

	return SessionInfo{}, false
}

// buildIndex populates s.index by walking every sessions/<encoded-cwd>/
// subdir. Called once via sync.Once on first FindByID.
func (s *SessionStore) buildIndex() {
	s.index = make(map[string]string)
	root := s.sessionsDir()
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			stem := strings.TrimSuffix(f.Name(), ".jsonl")
			s.index[stem] = filepath.Join(dir, f.Name())
		}
	}
}

// encodeCwdKey converts an absolute path to pi's session-dir naming
// convention (see pi-mono session-manager.ts:438).
// Strip leading separator, replace remaining / \ : with -, wrap with --.
// Example: /home/foo -> --home-foo--
func encodeCwdKey(cwd string) string {
	resolved, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		resolved = cwd
	}
	abs, err := filepath.Abs(resolved)
	if err != nil {
		abs = resolved
	}
	stripped := strings.TrimLeft(abs, "/\\")
	encoded := strings.NewReplacer("/", "-", "\\", "-", ":", "-").Replace(stripped)
	return "--" + encoded + "--"
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
