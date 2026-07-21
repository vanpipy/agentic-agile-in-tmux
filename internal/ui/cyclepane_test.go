// cyclepane_test.go — TDD tests for the CyclePane sub-Model.
//
// Per 18.9, the cyclepane is a viewer: a sub-`tea.Model` that
// drains cycle events and renders them, with no FSM of its own
// and no write-back to the parent. These tests cover the
// sub-Model surface only.
//
// The wiring into the parent Model's Update/View (ModeCycle
// focus handoff, esc returns to kanban without killing cycle,
// the cyclepane is rendered instead of the kanban) is P6.3.
//
// CORRECT-7 self-check:
//   C: literal buffer + view content equality
//   O: events append in arrival order (slice semantics)
//   R: 0/1/N events; scroll within [0, max(0, len-height)]
//   R: t.TempDir only; no external deps
//   E: empty pane renders a hint, not blank
//   C: 1 pane per cycle (18.14 single-cycle rule)
//   T: no time concerns in the sub-Model itself
package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pi/awp/internal/wiking"
)

// newCyclePaneForTest is a minimal fixture. The pane has no
// dependency on the parent Model in its sub-Model surface —
// it's constructed with the data it needs (stem, optional seed
// events). Wiring to the parent happens in P6.3.
func newCyclePaneForTest(stem string) *CyclePane {
	return NewCyclePane(stem, 80, 20)
}

// TestCyclePane_AppendEventOnUpdate — the cyclepane's Update
// appends a wiking.Event to its internal buffer when it
// receives a cycleEventMsg. This is the load-bearing data
// flow: parent polls cycle events, dispatches as cycleEventMsg
// to the pane, pane stores for View() to render.
func TestCyclePane_AppendEventOnUpdate(t *testing.T) {
	p := newCyclePaneForTest("test-stem")
	if got := len(p.events); got != 0 {
		t.Fatalf("precondition: events should be empty, got %d", got)
	}

	round1 := 1
	ev := wiking.Event{Type: "round_started", Round: &round1}
	newP, _ := p.Update(cycleEventMsg{ev: ev})
	p = newP.(*CyclePane)
	if got := len(p.events); got != 1 {
		t.Fatalf("events after one update = %d, want 1", got)
	}
	if p.events[0].Type != "round_started" {
		t.Errorf("events[0].Type = %q, want %q", p.events[0].Type, "round_started")
	}

	// Second event appends, doesn't replace.
	score := 92
	ev2 := wiking.Event{Type: "score_parsed", Score: &score}
	newP, _ = p.Update(cycleEventMsg{ev: ev2})
	p = newP.(*CyclePane)
	if got := len(p.events); got != 2 {
		t.Fatalf("events after two updates = %d, want 2", got)
	}
	if p.events[1].Type != "score_parsed" {
		t.Errorf("events[1].Type = %q, want %q", p.events[1].Type, "score_parsed")
	}
}

// TestCyclePane_NonCycleMsgsIgnored — non-cycle messages
// (KeyMsg, WindowSizeMsg) are passed through without mutating
// the events buffer. The pane has no FSM; it's a pure
// renderer + buffer. KeyMsg handling for j/k scroll is in a
// separate test.
func TestCyclePane_NonCycleMsgsIgnored(t *testing.T) {
	p := newCyclePaneForTest("test-stem")
	newP, _ := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	p = newP.(*CyclePane)
	if got := len(p.events); got != 0 {
		t.Errorf("events should still be empty after a KeyMsg, got %d", got)
	}
}

// TestCyclePane_ViewShowsStemAndEvents — View() includes the
// cycle stem and the buffered event types. The exact render
// format is not pinned; only that the user-visible text
// mentions the stem and the events that have arrived.
func TestCyclePane_ViewShowsStemAndEvents(t *testing.T) {
	p := newCyclePaneForTest("my-article")
	round1 := 1
	score := 92
	p.Update(cycleEventMsg{ev: wiking.Event{Type: "round_started", Round: &round1}})
	p.Update(cycleEventMsg{ev: wiking.Event{Type: "score_above_threshold", Score: &score}})

	view := p.View()
	if !strings.Contains(view, "my-article") {
		t.Errorf("view should contain stem %q; got: %.300q", "my-article", view)
	}
	if !strings.Contains(view, "round_started") && !strings.Contains(view, "round") {
		t.Errorf("view should mention the round_started event; got: %.300q", view)
	}
	if !strings.Contains(view, "92") {
		t.Errorf("view should mention score 92; got: %.300q", view)
	}
}

// TestCyclePane_ViewEmptyShowsHint — an empty cyclepane
// (no events yet) renders a hint, not a blank screen. The
// user has visual confirmation that the pane is alive even
// before the cycle emits its first event.
func TestCyclePane_ViewEmptyShowsHint(t *testing.T) {
	p := newCyclePaneForTest("my-article")
	view := p.View()
	if view == "" {
		t.Error("empty pane view should not be blank")
	}
	// Should hint that the cycle is running but no events yet.
	low := strings.ToLower(view)
	if !strings.Contains(low, "waiting") && !strings.Contains(low, "no events") &&
		!strings.Contains(low, "running") && !strings.Contains(low, "cycle") {
		t.Errorf("empty pane view should hint at the running cycle; got: %.300q", view)
	}
}

// TestCyclePane_JKScrolls — pressing 'j' advances scroll down
// by one (so older events at the top become visible); 'k'
// advances scroll up. Scroll is clamped to [0, maxScroll]. The
// pane's "viewport" is height lines; with more events than
// fit, j reveals the older entries (positive scroll).
//
// Why this scroll direction: the events arrive newest-last,
// and the natural render order is "newest at the bottom"
// (most recent at the bottom of the buffer). Scrolling
// DOWN (j, positive scroll) reveals older events at the top.
func TestCyclePane_JKScrolls(t *testing.T) {
	p := NewCyclePane("my-article", 80, 3) // height=3, so 3 events fit
	// Push 6 events.
	for i := 1; i <= 6; i++ {
		score := i
		newP, _ := p.Update(cycleEventMsg{ev: wiking.Event{
			Type:  "score_parsed",
			Score: &score,
		}})
		p = newP.(*CyclePane)
	}
	if got := len(p.events); got != 6 {
		t.Fatalf("setup: events = %d, want 6", got)
	}
	if p.scroll != 0 {
		t.Errorf("initial scroll = %d, want 0", p.scroll)
	}

	// Press j — scroll down (reveal older events).
	newP, _ := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	p = newP.(*CyclePane)
	if p.scroll != 1 {
		t.Errorf("scroll after j = %d, want 1", p.scroll)
	}

	// Press j two more times.
	newP, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	p = newP.(*CyclePane)
	newP, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	p = newP.(*CyclePane)
	if p.scroll != 3 {
		t.Errorf("scroll after 3 j presses = %d, want 3", p.scroll)
	}

	// Press k — scroll up.
	newP, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	p = newP.(*CyclePane)
	if p.scroll != 2 {
		t.Errorf("scroll after k = %d, want 2", p.scroll)
	}

	// Press k many times — should clamp to 0.
	for i := 0; i < 10; i++ {
		newP, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
		p = newP.(*CyclePane)
	}
	if p.scroll != 0 {
		t.Errorf("scroll after many k presses = %d, want 0 (clamped)", p.scroll)
	}

	// Press j many times — should clamp to maxScroll = len-height = 3.
	for i := 0; i < 20; i++ {
		newP, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
		p = newP.(*CyclePane)
	}
	if p.scroll != 3 {
		t.Errorf("scroll after many j presses = %d, want 3 (clamped to len-height)", p.scroll)
	}
}

// TestCyclePane_AppendEventIgnoresOtherKeys — only j and k
// scroll. Other keys (a, x, space) should not mutate scroll.
func TestCyclePane_AppendEventIgnoresOtherKeys(t *testing.T) {
	p := newCyclePaneForTest("test")
	for _, k := range []rune{'a', 'x', ' ', '/', 'q'} {
		newP, _ := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{k}})
		p = newP.(*CyclePane)
	}
	if p.scroll != 0 {
		t.Errorf("scroll = %d after non-scroll keys, want 0", p.scroll)
	}
}