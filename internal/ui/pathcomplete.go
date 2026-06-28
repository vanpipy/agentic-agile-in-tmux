package ui

// Path completion for the Add Project form. The bubbles textinput
// suggestion system is the consumer: SetSuggestions + ShowSuggestions
// enable TAB / arrow-key cycling, and the suggestion list is filtered
// by HasPrefix(suggestion, value). See pathcomplete_test.go for the
// contract this satisfies.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// completePath returns directory candidates matching the given input.
// Handles ~/ expansion (and re-prefixes suggestions with ~/ for
// display). Returns nil if input is empty, parent doesn't exist, or
// no matches.
//
// Behaviour:
//   - Input "~/pro"         → list home subdirs starting with "pro"
//   - Input "/usr/local/bi" → list /usr/local subdirs starting with "bi"
//   - Input "/tmp" (exists) → ["/tmp/"] + all of /tmp's subdirs
//                             (self first so first TAB confirms;
//                              rest available for arrow-key browsing)
//   - Input "/tmp/"         → same as above (trailing / normalized)
//   - Input "/tmp/."        → list /tmp's hidden subdirs (base=".")
//                             (bash-style: explicit "." enables dotglob)
//   - Input "x" (no slash)  → list CWD subdirs starting with "x"
//
// Hidden entries are excluded UNLESS the base starts with ".". This
// matches bash's default (no dotglob) while honoring the user's
// explicit intent to see hidden files.
//
// Trailing slash on input is normalized by filepath.Abs. Matching
// results are always full paths with a trailing "/" to mark them as
// directories (lets the user keep tabbing deeper without re-typing /).
func completePath(input string) []string {
	if input == "" {
		return nil
	}

	// Detect explicit intent to see hidden entries. Must check BEFORE
	// filepath.Abs because Clean("/foo/.") drops the trailing dot,
	// losing the signal.
	wantsHidden := input == "." || strings.HasSuffix(input, "/.")

	usedHomePrefix := strings.HasPrefix(input, "~/")
	expanded := expandHome(input)
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return nil
	}

	home, _ := os.UserHomeDir()

	// Existing-dir shortcut: typing a real directory returns it as
	// the first candidate (with trailing /), then its subdirs as
	// additional candidates. This makes "type tmp, TAB" confirm tmp/
	// in one keystroke, and "type tmp, TAB, Down, TAB" drill into
	// a subdir.
	if !wantsHidden {
		if info, err := os.Stat(abs); err == nil && info.IsDir() {
			self := abs + "/"
			if usedHomePrefix && home != "" && strings.HasPrefix(self, home) {
				self = "~" + self[len(home):]
			}
			matches := []string{self}
			if subEntries, err := os.ReadDir(abs); err == nil {
				for _, e := range subEntries {
					if !e.IsDir() {
						continue
					}
					name := e.Name()
					full := filepath.Join(abs, name) + "/"
					if usedHomePrefix && home != "" && strings.HasPrefix(full, home) {
						full = "~" + full[len(home):]
					}
					matches = append(matches, full)
				}
				// Keep self at index 0, sort the rest alphabetically.
				if len(matches) > 2 {
					head := matches[:1]
					tail := matches[1:]
					sort.Strings(tail)
					matches = append(head, tail...)
				}
			}
			return matches
		}
	}

	// Non-existing path: list parent's entries that start with base.
	// For wantsHidden, re-anchor so parent = abs (the cleaned path
	// we computed) and base = "." (the literal last segment the
	// user typed). This handles "X/." which Abs cleans to "X".
	parent, base := filepath.Dir(abs), filepath.Base(abs)
	if wantsHidden {
		parent = abs
		base = "."
	}

	entries, err := os.ReadDir(parent)
	if err != nil {
		return nil
	}

	matches := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if !wantsHidden && strings.HasPrefix(name, ".") {
			continue
		}
		if !strings.HasPrefix(name, base) {
			continue
		}

		full := filepath.Join(parent, name) + "/"
		if usedHomePrefix && home != "" && strings.HasPrefix(full, home) {
			full = "~" + full[len(home):]
		}
		matches = append(matches, full)
	}

	if len(matches) == 0 {
		return nil
	}
	sort.Strings(matches)
	return matches
}

// expandHome replaces a leading "~/..." with the home directory.
// Returns input unchanged if it doesn't start with ~.
func expandHome(input string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return input
	}
	if input == "~" {
		return home
	}
	if strings.HasPrefix(input, "~/") {
		return filepath.Join(home, input[2:])
	}
	return input
}