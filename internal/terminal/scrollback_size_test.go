package terminal

import (
	"testing"
)

// TestPane_New_AppliesScrollbackSize verifies that the scrollbackSize
// passed to New() is actually applied to x/vt's internal scrollback
// buffer (not just stored in the field as 'informational' info).
//
// Before the fix: p.scrollbackSize was stored but never sent to
// x/vt, so x/vt used its 10000-line default regardless of
// awp's configuration. Users who set awp's scrollback_size in
// config.json saw no effect.
func TestPane_New_AppliesScrollbackSize(t *testing.T) {
	tests := []struct {
		name     string
		size     int
		wantCap  int
	}{
		{name: "small 100", size: 100, wantCap: 100},
		{name: "medium 500", size: 500, wantCap: 500},
		{name: "zero uses default", size: 0, wantCap: 10000}, // x/vt default
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New("scroll", 80, 24, tt.size)
			cmd := p.Start("", nil...)
			if cmd != nil {
				cmd()
			}
			defer p.Stop()

			p.mu.Lock()
			defer p.mu.Unlock()
			if p.vt == nil {
				t.Fatal("p.vt is nil after Start")
			}
			got := p.vt.Scrollback().MaxLines()
			if got != tt.wantCap {
				t.Errorf("ScrollbackMaxLines = %d, want %d (user-set scrollbackSize=%d was not applied)",
					got, tt.wantCap, tt.size)
			}
		})
	}
}
