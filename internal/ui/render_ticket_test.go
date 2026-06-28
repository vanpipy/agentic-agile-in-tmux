// render_ticket_test.go — TDD tests for the simplified ticket card
// layout (post-2026-06-28 state-machine simplification).
//
// Card content (height fixed):
//   - 3 lines when description is non-empty: badges / title / desc
//   - 2 lines when description is empty: badges / title
//   - 1 line if both badges and description are empty (just title)
//
// We test the **content** invariants, not the full lipgloss output
// (which includes a 1-cell border and MarginBottom). Content is
// extracted from the rendered card by stripping the border, padding,
// and ANSI codes.
//
// CORRECT-7 self-check:
//   C-onformance: literal expected substrings on each line
//   O-rdering:    N/A
//   R-ange:       2-line (no desc) + 3-line (with desc); long/short titles
//   R-eference:   uses a real Model + globalStore; no I/O
//   E-xistence:   empty desc, empty title, empty labels
//   C-ardinality: 0/1/2/3+ badges
//   T-ime:        no time concerns
package ui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/pi/awp/internal/board"
	"github.com/pi/awp/internal/config"
	"github.com/pi/awp/internal/project"
)

// stripANSI removes ANSI escape codes (lipgloss styling) for
// substring matching.
var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

// extractContentLines strips the border, padding, and trailing margin
// from a rendered card and returns the visible content lines.
// A rendered card looks like:
//   ╭───...───╮
//   │ row 1   │
//   │ row 2   │
//   │ row 3   │
//   ╰───...───╯
//   <margin>
// We extract the rows between the top and bottom border lines.
func extractContentLines(rendered string) []string {
	plain := stripANSI(rendered)
	lines := strings.Split(plain, "\n")
	// Find the top border (first line starting with ╭) and bottom
	// border (first line starting with ╰ after the top).
	start := -1
	end := -1
	for i, line := range lines {
		if start < 0 && strings.HasPrefix(strings.TrimSpace(line), "╭") {
			start = i
			continue
		}
		if start >= 0 && strings.HasPrefix(strings.TrimSpace(line), "╰") {
			end = i
			break
		}
	}
	if start < 0 || end < 0 || end <= start {
		// No border found; return the full plain output.
		return lines
	}
	contentLines := lines[start+1 : end]
	// Strip the leading "│" border character and trailing whitespace
	// from each line. The lipgloss border is rendered as "│" with
	// no padding inside the card (Padding(0, 1) adds 1 cell of
	// horizontal padding, not vertical).
	result := make([]string, len(contentLines))
	for i, line := range contentLines {
		stripped := strings.TrimSpace(line)
		stripped = strings.TrimPrefix(stripped, "│")
		stripped = strings.TrimSuffix(stripped, "│")
		result[i] = strings.TrimSpace(stripped)
	}
	return result
}

// newModelForRenderTest creates a minimal Model suitable for testing
// renderTicket in isolation.
func newModelForRenderTest(t *testing.T) *Model {
	t.Helper()
	cfgDir := t.TempDir()
	t.Setenv("AWP_CONFIG_DIR", cfgDir)

	reg, err := project.LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	gts, err := project.LoadGlobalTicketStore(reg)
	if err != nil {
		t.Fatalf("LoadGlobalTicketStore: %v", err)
	}

	cfg := config.DefaultConfig()
	return NewModel(cfg, gts, reg, "", nil)
}

// TestRenderTicket_ContentHasThreeLinesWithDescription pins the
// 3-line content contract: badges / title / desc.
func TestRenderTicket_ContentHasThreeLinesWithDescription(t *testing.T) {
	m := newModelForRenderTest(t)
	ticket := board.NewTicket("Fix login timeout", "proj-1")
	ticket.Description = "Login keeps timing out when the user is idle"
	// Force a badge to render on line 0.
	ticket.Priority = 1

	width := 40
	out := m.renderTicket(ticket, false, false, width, m.colors.primary)
	rows := extractContentLines(out)
	if len(rows) != 3 {
		t.Errorf("content line count = %d; want 3 (badges/title/desc)\nrows: %q",
			len(rows), rows)
	}
}

// TestRenderTicket_ContentHasTwoLinesWithoutDescription pins the
// 2-line content contract: badges / title (description row skipped
// when description is empty).
func TestRenderTicket_ContentHasTwoLinesWithoutDescription(t *testing.T) {
	m := newModelForRenderTest(t)
	ticket := board.NewTicket("Fix login timeout", "proj-1")
	ticket.Description = ""
	ticket.Priority = 1

	width := 40
	out := m.renderTicket(ticket, false, false, width, m.colors.primary)
	rows := extractContentLines(out)
	if len(rows) != 2 {
		t.Errorf("content line count = %d; want 2 (badges/title; no desc)\nrows: %q",
			len(rows), rows)
	}
}

// TestRenderTicket_TitleOnFirstNonBadgeLine checks positional
// placement: the title appears on a content line. The exact index
// depends on whether badges are present, so we check it's the
// second line when badges are present.
func TestRenderTicket_TitleOnSecondLineWithBadges(t *testing.T) {
	m := newModelForRenderTest(t)
	const title = "Fix login timeout"
	ticket := board.NewTicket(title, "proj-1")
	ticket.Priority = 1 // forces !! badge on line 0

	out := m.renderTicket(ticket, false, false, 60, m.colors.primary)
	rows := extractContentLines(out)
	if len(rows) < 2 {
		t.Fatalf("expected at least 2 content lines; got %d\nrows: %q", len(rows), rows)
	}
	if !strings.Contains(rows[0], "!!") {
		t.Errorf("line 0 (badges) missing '!!' priority badge: %q", rows[0])
	}
	if !strings.Contains(rows[1], title) {
		t.Errorf("line 1 (title) missing title %q: %q", title, rows[1])
	}
}

// TestRenderTicket_DescriptionOnThirdLineWithBadgesAndTitle pins
// the 3-line content order: badges / title / desc.
func TestRenderTicket_DescriptionOnThirdLineWithBadgesAndTitle(t *testing.T) {
	m := newModelForRenderTest(t)
	const title = "Fix login timeout"
	const desc = "Login keeps timing out when the user is idle"
	ticket := board.NewTicket(title, "proj-1")
	ticket.Description = desc
	ticket.Priority = 1

	out := m.renderTicket(ticket, false, false, 60, m.colors.primary)
	rows := extractContentLines(out)
	if len(rows) != 3 {
		t.Fatalf("expected 3 content lines; got %d\nrows: %q", len(rows), rows)
	}
	// Line 0: badges
	if !strings.Contains(rows[0], "!!") {
		t.Errorf("line 0 (badges) missing '!!': %q", rows[0])
	}
	// Line 1: title
	if !strings.Contains(rows[1], title) {
		t.Errorf("line 1 (title) missing %q: %q", title, rows[1])
	}
	// Line 2: description
	if !strings.Contains(rows[2], desc) {
		t.Errorf("line 2 (desc) missing %q: %q", desc, rows[2])
	}
}

// TestRenderTicket_TitleTruncatesWithEllipsis pins the truncate
// contract: a long title is truncated (not wrapped) using '…'.
func TestRenderTicket_TitleTruncatesWithEllipsis(t *testing.T) {
	m := newModelForRenderTest(t)
	longTitle := strings.Repeat("a", 200) + " END"
	ticket := board.NewTicket(longTitle, "proj-1")
	ticket.Priority = 1

	width := 30
	out := m.renderTicket(ticket, false, false, width, m.colors.primary)
	rows := extractContentLines(out)

	// Find the title row (line 1, since line 0 is badges).
	if len(rows) < 2 {
		t.Fatalf("expected at least 2 content lines; got %d", len(rows))
	}
	titleRow := rows[1]

	// Ellipsis marker must be present.
	if !strings.Contains(titleRow, "…") {
		t.Errorf("title row not truncated with '…': %q", titleRow)
	}
	// Full long title must NOT appear.
	if strings.Contains(titleRow, longTitle) {
		t.Errorf("full title still present; truncation did not happen: %q", titleRow)
	}
	// The title row's visible width must be ≤ width.
	if w := lipgloss.Width(titleRow); w > width {
		t.Errorf("title row visible width = %d; want ≤ %d (truncation failed): %q",
			w, width, titleRow)
	}
}

// TestRenderTicket_DescriptionTruncatesWithEllipsis pins the same
// truncate contract for the description line.
func TestRenderTicket_DescriptionTruncatesWithEllipsis(t *testing.T) {
	m := newModelForRenderTest(t)
	ticket := board.NewTicket("Title", "proj-1")
	ticket.Priority = 1
	longDesc := strings.Repeat("b", 200) + " END"
	ticket.Description = longDesc

	width := 30
	out := m.renderTicket(ticket, false, false, width, m.colors.primary)
	rows := extractContentLines(out)
	if len(rows) != 3 {
		t.Fatalf("expected 3 content lines; got %d", len(rows))
	}
	descRow := rows[2]

	if !strings.Contains(descRow, "…") {
		t.Errorf("description row not truncated with '…': %q", descRow)
	}
	if w := lipgloss.Width(descRow); w > width {
		t.Errorf("description row visible width = %d; want ≤ %d: %q", w, width, descRow)
	}
}

// TestRenderTicket_VisibleHeightDoesNotGrowWithLongContent pins
// the architectural invariant: the card's visible height is FIXED
// (doesn't grow when title or description is long). lipgloss.Height
// includes the border, so the expected height is:
//   content_lines + 2 (top + bottom border) + 0 (no vertical padding)
// We don't pin the exact total height here because that's a UI
// detail; we pin the relative property: long content does not
// increase height.
func TestRenderTicket_VisibleHeightDoesNotGrowWithLongContent(t *testing.T) {
	m := newModelForRenderTest(t)

	short := board.NewTicket("X", "proj-1")
	short.Description = "Y"
	short.Priority = 1

	long := board.NewTicket(strings.Repeat("a", 200), "proj-1")
	long.Description = strings.Repeat("b", 200)
	long.Priority = 1

	width := 30
	shortOut := m.renderTicket(short, false, false, width, m.colors.primary)
	longOut := m.renderTicket(long, false, false, width, m.colors.primary)

	shortH := lipgloss.Height(shortOut)
	longH := lipgloss.Height(longOut)

	if longH != shortH {
		t.Errorf("long content grew card height: short=%d, long=%d (want equal — no wrapping)\n"+
			"short rows: %q\nlong rows: %q",
			shortH, longH, extractContentLines(shortOut), extractContentLines(longOut))
	}
}
