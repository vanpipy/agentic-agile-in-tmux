package ui

import (
	"strings"
	"testing"
)

// TestTruncate covers the three branches of truncate:
//   1. String fits in max → returned unchanged
//   2. String overflows, max < 4 → hard cut (no ellipsis)
//   3. String overflows, max >= 4 → cut + " ..." suffix (4 bytes)
func TestTruncate(t *testing.T) {
	tests := []struct {
		name string
		s    string
		max  int
		want string
	}{
		// Branch 1: len(s) <= max → returned as-is
		{"empty string", "", 10, ""},
		{"empty string max=0", "", 0, ""},
		{"string equal to max", "hello", 5, "hello"},
		{"string shorter than max", "hi", 10, "hi"},

		// Branch 2: overflow + max < 4 → hard cut, no ellipsis
		{"max is zero", "hello world", 0, ""},
		{"max is one with overflow", "hello", 1, "h"},
		{"max is two with overflow", "hello", 2, "he"},
		{"max is three with overflow", "hello", 3, "hel"},

		// Branch 3: overflow + max >= 4 → s[:max-4] + " ..."
		{"max=4 overflow by 1", "hello", 4, " ..."},          // s[:0] + " ..."
		{"max=5 overflow by 1", "abcdef", 5, "a ..."},        // s[:1] + " ..."
		{"max=8 overflow", "hello world", 8, "hell ..."},     // s[:4] + " ..."
		{"max=10 long overflow", "the quick brown fox", 10, "the qu ..."}, // s[:6] + " ..." (10 chars total)

		// Edge: exactly at boundary
		{"len equals max", "12345", 5, "12345"},
		{"len one over max", "123456", 5, "1 ..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.s, tt.max)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q; want %q",
					tt.s, tt.max, got, tt.want)
			}
		})
	}
}

// TestTruncate_EllipsisExactlyFourBytes documents the contract that
// the ellipsis suffix is always exactly 4 bytes (" ..."), regardless
// of input — important for layout calculations in the status bar.
func TestTruncate_EllipsisExactlyFourBytes(t *testing.T) {
	const ellipsis = " ..."

	for _, s := range []string{
		"abcdefghij",
		"the quick brown fox jumps",
		"a very long string that should be truncated",
	} {
		got := truncate(s, 8) // any max >= 4 triggers ellipsis branch
		if !strings.HasSuffix(got, ellipsis) {
			t.Errorf("truncate(%q, 8) = %q; want suffix %q", s, got, ellipsis)
		}
		if len(got) != 8 {
			t.Errorf("truncate(%q, 8) length = %d; want 8", s, len(got))
		}
	}
}

// TestTruncate_HardCutNoEllipsis documents that small max values
// (< 4) intentionally produce a hard cut without ellipsis, since
// adding " ..." to a 3-char field would be longer than max.
func TestTruncate_HardCutNoEllipsis(t *testing.T) {
	for _, max := range []int{0, 1, 2, 3} {
		got := truncate("hello world", max)
		if strings.Contains(got, " ") && max < 4 {
			// The hard-cut branch returns s[:max] directly; " " can
			// only appear if input has spaces (which it does), so
			// we instead verify the length and lack of " ..."
			if strings.Contains(got, " ...") {
				t.Errorf("truncate(_, %d) should not contain ellipsis, got %q",
					max, got)
			}
		}
		if len(got) > max {
			t.Errorf("truncate(_, %d) length = %d; want <= %d", max, len(got), max)
		}
	}
}