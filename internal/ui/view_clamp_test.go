// view_clamp_test.go — TDD test for the clampScrollOffset pure helper.
//
// After DEATH-3 (auto-scroll removal): View() uses clampScrollOffset
// directly on m.formScrollOffset. This helper pins the contract.
//
// CORRECT-7 self-check:
//   C-onformance: literal values
//   O-rdering: N/A
//   R-ange: 6 cases (in-range, below-min, above-max, content-fits, etc.)
//   R-eference: no external deps
//   E-xistence: edge cases (negative, zero)
//   C-ardinality: 1 helper, multiple scenarios
//   T-ime: no time concerns
package ui

import "testing"

func TestClampScrollOffset(t *testing.T) {
	tests := []struct {
		name           string
		offset         int
		totalLines     int
		viewportHeight int
		want           int
	}{
		{"in range", 5, 100, 10, 5},
		{"below min clamps to 0", -3, 100, 10, 0},
		{"above max clamps to total-viewport", 999, 100, 10, 90},
		{"content fits viewport (no scroll possible)", 50, 5, 10, 0},
		{"content fits viewport negative input", -10, 5, 10, 0},
		{"viewport larger than content", 0, 5, 100, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clampScrollOffset(tt.offset, tt.totalLines, tt.viewportHeight)
			if got != tt.want {
				t.Errorf("clampScrollOffset(%d, %d, %d) = %d; want %d",
					tt.offset, tt.totalLines, tt.viewportHeight, got, tt.want)
			}
		})
	}
}
