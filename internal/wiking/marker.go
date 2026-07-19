// Package wiking implements the postman protocol for the 2-cycle
// (wiking ↔ coding) iteration. See SYSTEM_DESIGN.md §18 for the
// design contract this package implements.
//
// marker.go — §18.5 marker protocol. Sentinel line is the last
// non-blank line of an article/feedback file:
//   - wiking-end:       `--- end ---`
//   - coding-end:       `--- end with N ---` with N ∈ [0, 100]
//
// Strict-parser policy: anything that is neither a valid marker nor
// a missing file is ErrMalformedMarker. The postman keeps polling
// without phase transition (§18.5, §18.8).
package wiking

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// ErrNotYetWritten is returned when the marker file is missing or
// contains only blank lines. Distinct from ErrMalformedMarker.
var ErrNotYetWritten = errors.New("wiking: marker file not yet written")

// ErrMalformedMarker is returned when the marker's last line is
// non-marker text or matches neither regex. Per §18.5 strict-parser
// policy, the caller keeps polling without transitioning phase.
var ErrMalformedMarker = errors.New("wiking: malformed marker line")

// endMarkerRe matches the wiking-end sentinel `--- end ---` exactly
// (after TrimSpace). Anchor both sides so a partial match inside a
// paragraph can't sneak through.
var endMarkerRe = regexp.MustCompile(`^--- end ---$`)

// scoreMarkerRe matches `--- end with N ---` where N is one or more
// decimal digits. Range check (0 ≤ N ≤ 100) is enforced in code below.
var scoreMarkerRe = regexp.MustCompile(`^--- end with (\d+) ---$`)

const scoreMax = 100

// LastLine returns the last non-blank line of path, with trailing
// whitespace (including \r from PTY ONLCR) trimmed. Returns
// ErrNotYetWritten if the file is missing, unreadable, or contains
// only blank lines.
//
// Operates on byte streams; assumes a line-based marker protocol. The
// only "blank" considered is the trimmed empty string — Markdown
// headings, paragraphs, code blocks all count as content.
func LastLine(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrNotYetWritten
		}
		return "", fmt.Errorf("wiking: open %s: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Default Scan buffer (64 KiB) is fine for article-sized files.
	// If articles grow past this we can set Scanner.Buffer later;
	// for now the stdlib default is the contract.
	var last string
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), " \t\r")
		if line != "" {
			last = line
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("wiking: scan %s: %w", path, err)
	}
	if last == "" {
		return "", ErrNotYetWritten
	}
	return last, nil
}

// CheckEnd reports whether path's last line is the wiking-end marker
// `--- end ---`. Returns:
//   - (true, nil)                  marker present
//   - (false, ErrNotYetWritten)    file missing or only blank lines
//   - (false, ErrMalformedMarker)  last line is non-marker text
//   - (false, err)                 I/O failure
func CheckEnd(path string) (bool, error) {
	line, err := LastLine(path)
	if err != nil {
		return false, err
	}
	if endMarkerRe.MatchString(strings.TrimSpace(line)) {
		return true, nil
	}
	return false, ErrMalformedMarker
}

// CheckScore parses the score N from a coding-end marker
// `--- end with N ---`. Returns:
//   - (N, nil)                     marker present and N ∈ [0, 100]
//   - (0, ErrNotYetWritten)        file missing or only blank lines
//   - (0, ErrMalformedMarker)      last line is non-marker text, or
//                                  N out of [0, 100], or N non-numeric
//   - (0, err)                     I/O failure
//
// 200 is malformed (out of range); the regex is greedy on digits so
// 200 matches the regex pattern but fails the range check below.
func CheckScore(path string) (int, error) {
	line, err := LastLine(path)
	if err != nil {
		return 0, err
	}
	trimmed := strings.TrimSpace(line)
	m := scoreMarkerRe.FindStringSubmatch(trimmed)
	if m == nil {
		return 0, ErrMalformedMarker
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		// Regex guarantees digits-only; this is defense in depth.
		return 0, ErrMalformedMarker
	}
	if n < 0 || n > scoreMax {
		return 0, ErrMalformedMarker
	}
	return n, nil
}
