// truncate.go — single-line, CJK-aware text truncation for the kanban card.
//
// The 3-line card layout (badges / title / description) depends on each
// line being exactly one visible row. lipgloss.Width measures the visible
// cell count (each CJK glyph = 2 cells, ASCII = 1). We use it to find
// the longest prefix of the input that, when suffixed with '…' (1 cell),
// fits within `width`.
//
// Newline and carriage-return characters in the input are replaced with
// spaces before truncation. The 3-line card layout is enforced by the
// caller (one line per row), so embedded newlines would break the
// alignment. The function itself never returns a string with a
// newline.
//
// If the (newline-stripped) input already fits, it is returned
// unchanged. If even '…' alone doesn't fit (width <= 1), '…' is returned
// for width=1 or "" for width=0.
package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ellipsis is the truncation marker. 1 visible cell.
const ellipsis = "…"

// TruncateLine returns the longest prefix of s whose visible width
// (per lipgloss.Width) plus the '…' suffix fits within width. If s
// (after newline normalization) already fits, it is returned
// unchanged. If even '…' alone doesn't fit (width <= 1), '…' is
// returned for width=1 or "" for width=0.
//
// Embedded '\n' and '\r' are replaced with spaces before truncation.
// TruncateLine never wraps, never introduces newlines, and never
// returns a string whose visible width exceeds the given width.
func TruncateLine(s string, width int) string {
	if s == "" {
		return ""
	}
	if width <= 0 {
		return ""
	}
	// Normalize newlines to spaces. This is what makes the function
	// safe to call on arbitrary user input (e.g., ticket descriptions
	// typed in a text input).
	s = strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(s)
	// Fast path: input (after normalization) fits as-is.
	if lipgloss.Width(s) <= width {
		return s
	}
	if width == 1 {
		// Only '…' fits.
		return ellipsis
	}
	// Walk rune-by-rune from the start. For each prefix length, find
	// the largest prefix that, when combined with '…', fits within
	// width. The first such prefix is the answer.
	runes := []rune(s)
	// Binary-search-style: try greedy from the end, walking down if
	// the largest candidate doesn't fit. This is O(n) which is fine
	// for ticket titles/descriptions (tens to low hundreds of chars).
	for i := len(runes); i > 0; i-- {
		candidate := string(runes[:i]) + ellipsis
		if lipgloss.Width(candidate) <= width {
			return candidate
		}
	}
	// No prefix + '…' fits (e.g., width == 2 and first rune is CJK = 2 cells,
	// or width == 2 and first rune is ASCII = 1 cell + '…' = 2 cells fits,
	// so this only triggers when width < 2 which is handled above).
	// Defensive fallback: return '…' (1 cell, always fits in positive width).
	return ellipsis
}
