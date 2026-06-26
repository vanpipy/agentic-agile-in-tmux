package ui

import (
	"strings"
	"testing"

	"github.com/pi/awp/internal/board"
	"github.com/pi/awp/internal/config"
	"github.com/pi/awp/internal/project"
)

// makeTestModel returns a Model wired with the bare minimum to
// exercise render* helpers (config + theme + colors). Uses a
// real GlobalTicketStore (required by NewModel) but with empty
// state. Does not start any TUI loop or PTY.
func makeTestModel(t *testing.T) *Model {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.UI.Theme = "default"
	t.Setenv("AWP_CONFIG_DIR", t.TempDir())
	reg := &project.ProjectRegistry{
		Projects: make(map[string]*project.Project),
	}
	store := project.NewGlobalTicketStore(reg)
	m := NewModel(cfg, store, reg, "", nil)
	m.width = 80
	m.height = 24
	return m
}

// TestRenderPriorityBadge covers the priority level boundaries:
// 0 (no priority), 1 (highest, !!), 2 (high, !), 3+ (no badge).
func TestRenderPriorityBadge(t *testing.T) {
	m := makeTestModel(t)

	tests := []struct {
		name     string
		priority int
		wantSub  string // empty means no badge
	}{
		{"no priority", 0, ""},
		{"priority 1 = !!", 1, "!!"},
		{"priority 2 = !", 2, "!"},
		{"priority 3+ no badge", 3, ""},
		{"priority 5 no badge", 5, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ticket := &board.Ticket{Priority: tt.priority}
			got := m.renderPriorityBadge(ticket)
			if tt.wantSub == "" {
				if got != "" {
					t.Errorf("Priority %d: got %q, want empty", tt.priority, got)
				}
				return
			}
			if !strings.Contains(got, tt.wantSub) {
				t.Errorf("Priority %d: got %q, want substring %q", tt.priority, got, tt.wantSub)
			}
		})
	}
}

// TestRenderSessionBadge covers the AgentStatus switch. The five
// states have specific render rules; other states return "".
func TestRenderSessionBadge(t *testing.T) {
	m := makeTestModel(t)

	tests := []struct {
		name        string
		status      board.AgentStatus
		hasPane     bool
		wantNonEmpty bool
	}{
		{"waiting always renders", board.AgentWaiting, false, true},
		{"idle without pane is empty", board.AgentIdle, false, false},
		{"idle with pane renders", board.AgentIdle, true, true},
		{"completed renders", board.AgentCompleted, false, true},
		{"error renders", board.AgentError, false, true},
		{"working is empty (no glyph)", board.AgentWorking, false, false},
		{"none is empty", board.AgentNone, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ticket := &board.Ticket{AgentStatus: tt.status}
			got := m.renderSessionBadge(ticket, tt.hasPane)
			if tt.wantNonEmpty && got == "" {
				t.Errorf("status=%v hasPane=%v: got empty, want non-empty",
					tt.status, tt.hasPane)
			}
			if !tt.wantNonEmpty && got != "" {
				t.Errorf("status=%v hasPane=%v: got %q, want empty",
					tt.status, tt.hasPane, got)
			}
		})
	}
}

// TestRenderDepBadge covers the dep-counting logic. The badge is
// empty when there are no blockers and no blocks; otherwise it
// renders in 3 patterns (both, blocked-only, blocks-only).
func TestRenderDepBadge(t *testing.T) {
	m := makeTestModel(t)

	// Without a wired global store, GetBlockedBy/GetBlocks return
	// nil/empty. We can only verify the "no deps" case here.
	t.Run("no deps returns empty", func(t *testing.T) {
		ticket := &board.Ticket{ID: "x"}
		if got := m.renderDepBadge(ticket); got != "" {
			t.Errorf("renderDepBadge(no deps) = %q, want empty", got)
		}
	})
}
