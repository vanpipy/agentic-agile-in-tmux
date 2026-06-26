package terminal

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

func TestSelectionState_Lifecycle(t *testing.T) {
	s := NewSelectionState()
	if s.IsActive() {
		t.Error("new state should not be active")
	}

	s.Start(Position{Col: 5, Row: 10})
	if !s.IsActive() {
		t.Error("after Start, state should be active")
	}

	s.Update(Position{Col: 7, Row: 12})
	s.Finish()
	// Note: IsActive() returns true when start/end are set.
	// Use Clear() to reset.

	start, end := s.Bounds()
	if start.Col != 5 || start.Row != 10 {
		t.Errorf("Start = (%d,%d), want (5,10)", start.Col, start.Row)
	}
	if end.Col != 7 || end.Row != 12 {
		t.Errorf("End = (%d,%d), want (7,12)", end.Col, end.Row)
	}
}

func TestSelectionState_Contains(t *testing.T) {
	s := NewSelectionState()
	s.Start(Position{Col: 5, Row: 10})
	s.Update(Position{Col: 7, Row: 12})
	s.Finish()

	tests := []struct {
		name string
		pos  Position
		want bool
	}{
		{"inside (middle)", Position{Col: 6, Row: 11}, true},
		{"outside (top-left)", Position{Col: 0, Row: 0}, false},
		{"boundary at start", Position{Col: 5, Row: 10}, true},
		{"boundary at end", Position{Col: 7, Row: 12}, true},
		{"just above top edge", Position{Col: 5, Row: 9}, false},
		{"just below bottom edge", Position{Col: 7, Row: 13}, false},
		{"just left of start col", Position{Col: 4, Row: 10}, false},
		{"just right of end col", Position{Col: 8, Row: 12}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := s.Contains(tt.pos); got != tt.want {
				t.Errorf("Contains(%v) = %v, want %v", tt.pos, got, tt.want)
			}
		})
	}
}

func TestSelectionState_Clear(t *testing.T) {
	s := NewSelectionState()
	s.Start(Position{Col: 5, Row: 10})
	s.Update(Position{Col: 7, Row: 12})
	s.Finish()
	s.Clear()
	start, _ := s.Bounds()
	if start.Col != 0 || start.Row != 0 {
		t.Errorf("after Clear, start should be 0,0, got (%d,%d)", start.Col, start.Row)
	}
}

func TestTrimTrailingSpaces(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"hello   ", "hello"},
		{"no trailing", "no trailing"},
		{"   ", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := trimTrailingSpaces([]rune(tt.in))
		if string(got) != tt.want {
			t.Errorf("trimTrailingSpaces(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSelectionState_ExtractText_NoSelection(t *testing.T) {
	s := NewSelectionState()
	got := s.ExtractText(nil, func(c, r int) *uv.Cell { return &uv.Cell{} }, 0, 0)
	if got != "" {
		t.Errorf("ExtractText with no selection = %q, want empty", got)
	}
}

func TestSelectionState_ExtractText_Scrollback(t *testing.T) {
	s := NewSelectionState()
	scrollback := [][]*uv.Cell{
		glyphs("hello world"),
	}
	// Row -1 is the most recent scrollback line
	s.Start(Position{Col: 0, Row: -1})
	s.Update(Position{Col: 4, Row: -1})
	s.Finish()

	got := s.ExtractText(scrollback, nil, 0, 1) // scrollbackLen=1
	if got != "hello" {
		t.Errorf("ExtractText = %q, want %q", got, "hello")
	}
}

func glyphs(s string) []*uv.Cell {
	out := make([]*uv.Cell, 0, len(s))
	for _, r := range s {
		out = append(out, &uv.Cell{Content: string(r)})
	}
	return out
}
