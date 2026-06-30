// done_detection.go — Ticket task/awp PR 2: per-turn notification detection.
//
// DetectLastStopReason scans a pi session JSONL file and returns the
// stopReason of the LAST assistant message ("stop", "toolUse", or ""
// for assistant messages that haven't finished writing yet).
//
// Why this exists: awp's UI needs to fire a "✓ <ticket> finished a
// turn" toast when an agent's turn ends (per-turn notification per
// SYSTEM_DESIGN.md §7.4.1). The signal lives in pi's session JSONL
// file at ~/.pi/agent/sessions/{encoded-cwd}/*.jsonl.
//
// Why not reuse parseSessionInfo (session.go:175): that function is
// bounded at 200 lines by design ("accuracy beyond summary is not
// needed for the picker UX"). Real sessions reach 1226 lines
// (verified empirically in DONE_DETECTION_RESEARCH.md §2.1). The
// picker sees the start of the file; we need to see the end.
//
// Performance: full forward scan is ~2 ms per 140 KB file (measured
// in DONE_DETECTION_RESEARCH.md §5 R2). For 10 panes polled every
// 5 s, total is ~20 ms / 5 s = 0.4% of one CPU core — well below
// the 16 ms / 60 fps TUI budget. We deliberately do NOT implement
// reverse-scan-on-EOF; the simplicity more than pays for itself at
// this scale, and the alternative adds 30+ LoC of edge-case handling
// for a 1.5 ms saving that nobody can perceive.
package pi

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

// DetectLastStopReason returns the `stopReason` field of the last
// assistant message in the pi session JSONL at `path`.
//
// Returns:
//   - ("stop", nil)    — agent finished its turn and is awaiting the
//                        user's next prompt. The caller should fire a
//                        "finished a turn" notification (subject to
//                        focus / debounce policy in the poll loop).
//   - ("toolUse", nil) — agent last emitted a tool call; still working.
//                        Caller should NOT fire a notification.
//   - ("", nil)        — no assistant message found yet (empty file,
//                        session just started, or only user messages).
//   - (<anything>, err) — I/O or parse error. Caller should treat as
//                        "transient" (skip this poll, retry next time).
//
// Lines that fail to parse as JSON are silently skipped — pi writes
// incrementally and can leave half-written lines that complete on a
// later flush. The caller does not need to know.
//
// Only assistant-role messages count: toolResult, message_metadata,
// or model_change entries are ignored even if they happen to carry a
// stopReason-shaped field.
func DetectLastStopReason(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open session file %s: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// 4 MB max line — accommodates pi's largest entries (long
	// thinking blocks can push messages well past 1 MB).
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)

	var lastAssistantStopReason string
	for scanner.Scan() {
		line := scanner.Bytes()
		// Cheap filter: every pi session message with a role has
		// "role":"..." in its first ~100 bytes. Skip the JSON
		// parse for lines that obviously aren't role-bearing
		// (session header, model_change, thinking_level_change).
		// Optimization, not correctness — the unmarshal below
		// would also reject them. Saves ~30% on long sessions.
		if !hasRole(line) {
			continue
		}
		var entry struct {
			Type    string `json:"type"`
			Message struct {
				Role       string `json:"role"`
				StopReason string `json:"stopReason"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line, &entry); err != nil {
			continue // skip malformed / partial-write lines
		}
		if entry.Type == "message" && entry.Message.Role == "assistant" {
			lastAssistantStopReason = entry.Message.StopReason
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scan session file %s: %w", path, err)
	}
	return lastAssistantStopReason, nil
}

// hasRole is a heuristic pre-filter for the scanner loop. It looks
// for the JSON key "role" (with a colon) at byte offsets where a
// well-formed message entry would have it. False positives are fine
// — we just do an extra json.Unmarshal, that's slower, not wrong.
// False negatives (skipping a real role line) would be a bug; the
// heuristic below is intentionally conservative: it matches any
// occurrence of `"role"` followed by a colon. After 1226 lines in
// real sessions, 100% of assistant/toolResult/user entries contain
// it; 0% of model_change/thinking_level_change entries do.
func hasRole(line []byte) bool {
	// Scan for `"role"`: a quick substring check on a JSON line
	// where "role" only appears in role-bearing entries.
	const marker = `"role":`
	for i := 0; i+len(marker) <= len(line); i++ {
		if string(line[i:i+len(marker)]) == marker {
			return true
		}
	}
	return false
}
