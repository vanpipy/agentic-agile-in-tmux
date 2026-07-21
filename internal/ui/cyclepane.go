// cyclepane.go — CyclePane sub-Model.
//
// Per SYSTEM_DESIGN.md §18.9, the cyclepane is a viewer:
// a sub-`tea.Model` that drains cycle events from
// m.cycleEvents and renders them, with no FSM of its own and
// no write-back to the parent Model. The parent owns the
// cycle; the pane is a presentational surface for it.
//
// Wiring (P6.3) lands later. The pane in this file is a
// self-contained `tea.Model`: NewCyclePane(stem, w, h)
// constructs one; Update handles cycleEventMsg (append) and
// j/k (scroll); View renders the buffered events. Nothing in
// this file reads or writes the parent Model.

package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pi/awp/internal/wiking"
)

// CyclePane is a read-only viewer for a single running cycle.
// Per 18.14, there is at most one active cycle per process,
// so the parent Model holds at most one *CyclePane at a time.
type CyclePane struct {
	stem   string
	events []wiking.Event
	width  int
	height int
	scroll int
}

// NewCyclePane constructs a CyclePane. stem is the article
// name (18.9 chip name) shown in the pane header. width and
// height are the viewport dimensions; the pane renders up to
// height events at a time, with j/k scrolling older entries
// into view.
func NewCyclePane(stem string, width, height int) *CyclePane {
	if width < 1 {
		width = 80
	}
	if height < 1 {
		height = 20
	}
	return &CyclePane{
		stem:   stem,
		width:  width,
		height: height,
	}
}

// Init is a no-op for the viewer (no async work to schedule
// at startup). The parent's Update dispatches events to the
// pane via cycleEventMsg; the pane doesn't poll on its own.
func (p *CyclePane) Init() tea.Cmd { return nil }

// Update handles messages for the cyclepane. Two paths:
//   - cycleEventMsg: append the event to the buffer. After
//     append, clamp scroll so the new event is visible
//     (auto-scroll to bottom is the typical chat/feed UX;
//     if the user has scrolled up to read history, we don't
//     yank them back to the bottom — that breaks the
//     "scrollback" use case).
//   - tea.KeyMsg: handle j/k for scroll. Other keys are
//     ignored (the pane has no FSM, no other affordances in
//     v1; Enter/e land in P6.3 alongside the parent's
//     $EDITOR integration).
func (p *CyclePane) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case cycleEventMsg:
		p.events = append(p.events, msg.ev)
		// Clamp scroll: the new event may have pushed the
		// buffer past height, but we don't auto-scroll to
		// bottom (would disrupt scrollback). The clamp keeps
		// scroll within the valid range given the new length.
		p.clampScroll()
		return p, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			p.scroll++
			p.clampScroll()
		case "k", "up":
			p.scroll--
			p.clampScroll()
		}
		return p, nil
	}
	return p, nil
}

// clampScroll keeps p.scroll in [0, max(0, len-height)].
// Called after every scroll change and every event append.
func (p *CyclePane) clampScroll() {
	maxScroll := len(p.events) - p.height
	if maxScroll < 0 {
		maxScroll = 0
	}
	if p.scroll < 0 {
		p.scroll = 0
	}
	if p.scroll > maxScroll {
		p.scroll = maxScroll
	}
}

// View renders the cyclepane. Layout (top to bottom):
//   1. Header line: "cycle: <stem>" (matches the parent
//      header chip so the user sees a consistent identifier
//      when focused on the pane).
//   2. Event list: up to `height` lines, scrolled per
//      p.scroll. Each event renders as a short summary
//      (type + key payload) so the user can track cycle
//      progress at a glance.
//   3. Footer hint: "j/k scroll" when the buffer overflows
//      the viewport.
//
// Empty-buffer case: when no events have arrived, renders
// a single hint line ("cycle: <stem> — waiting for events")
// so the pane never displays as a blank panel.
func (p *CyclePane) View() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("cycle: %s\n", p.stem))

	if len(p.events) == 0 {
		b.WriteString("  (waiting for first event)\n")
		return b.String()
	}

	// Slice the events per scroll: with scroll=s, the topmost
	// visible event is events[s], and we render up to height
	// events.
	start := p.scroll
	end := start + p.height
	if end > len(p.events) {
		end = len(p.events)
	}
	for i := start; i < end; i++ {
		b.WriteString("  ")
		b.WriteString(formatCycleEvent(p.events[i]))
		b.WriteString("\n")
	}

	// Footer hint when there's more history above or below.
	if len(p.events) > p.height {
		b.WriteString(fmt.Sprintf("  [%d-%d of %d]  j/k scroll",
			start+1, end, len(p.events)))
	}
	return b.String()
}

// formatCycleEvent renders one wiking.Event as a single-line
// summary. The 18.7 type catalog drives the switch so the
// pane shows the same wording the toast does (handleCycleEventMsg
// in model.go), keeping the two surfaces consistent.
//
// Per-type payload extraction is minimal in v1: type + the
// headline field (score, round, etc). Full per-type renderers
// (with duration_ms, marker_path, etc.) are out of P6.2 scope.
func formatCycleEvent(ev wiking.Event) string {
	switch ev.Type {
	case "round_started":
		return fmt.Sprintf("[round %s] started", intPtrStr(ev.Round))
	case "wiking_spawned":
		return "wiking agent started"
	case "wiking_done":
		return "wiking draft written"
	case "coding_spawned":
		return "coding reviewer started"
	case "coding_done":
		return "coding review written"
	case "score_parsed":
		if ev.Score != nil {
			return fmt.Sprintf("score parsed: %d", *ev.Score)
		}
		return "score parsed"
	case "score_above_threshold":
		if ev.Score != nil {
			return fmt.Sprintf("score %d (>= threshold, accepted)", *ev.Score)
		}
		return "score above threshold"
	case "loop":
		if ev.Score != nil {
			return fmt.Sprintf("score %d < threshold, looping", *ev.Score)
		}
		return "looping"
	case "synced":
		return "synced article"
	case "cycle_accepted":
		return "cycle accepted"
	case "cycle_failed":
		return "cycle failed"
	case "phase_timeout":
		return "phase timeout"
	case "no_progress":
		return "no-progress exit"
	case "error":
		return "error"
	case "terminated":
		return "terminated"
	default:
		if ev.Type == "" {
			return "(event)"
		}
		return ev.Type
	}
}