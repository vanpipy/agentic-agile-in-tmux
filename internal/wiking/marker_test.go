// marker_test.go — RED tests for the marker protocol.
//
// Per SYSTEM_DESIGN.md §18.5: strict-parser policy, with sentinel
// regexes `^--- end ---$` (wiking) and `^--- end with (\d+) ---$`
// (coding). ErrNotYetWritten and ErrMalformedMarker are distinct
// signals (§18.5, §18.8): the former means "still waiting", the
// latter means "keep polling without transitioning".

package wiking

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// LastLine — missing-file and empty-file paths.

func TestLastLine_MissingFile(t *testing.T) {
	_, err := LastLine(filepath.Join(t.TempDir(), "absent.md"))
	if !errors.Is(err, ErrNotYetWritten) {
		t.Fatalf("missing file: got %v, want ErrNotYetWritten", err)
	}
}

func TestLastLine_EmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.md")
	os.WriteFile(path, []byte(""), 0o644)
	_, err := LastLine(path)
	if !errors.Is(err, ErrNotYetWritten) {
		t.Fatalf("empty file: got %v, want ErrNotYetWritten", err)
	}
}

func TestLastLine_OnlyBlankLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blanks.md")
	os.WriteFile(path, []byte("\n\n   \n\t\n"), 0o644)
	_, err := LastLine(path)
	if !errors.Is(err, ErrNotYetWritten) {
		t.Fatalf("blanks only: got %v, want ErrNotYetWritten", err)
	}
}

func TestLastLine_ReturnsLastNonBlank(t *testing.T) {
	path := filepath.Join(t.TempDir(), "article.md")
	os.WriteFile(path, []byte("# Heading\n\nbody para\n"), 0o644)
	got, err := LastLine(path)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got != "body para" {
		t.Fatalf("got %q, want %q", got, "body para")
	}
}

// CheckEnd — wiking-end marker `--- end ---`.

func TestCheckEnd_Valid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "article.md")
	os.WriteFile(path, []byte("# Heading\n\nbody\n--- end ---\n"), 0o644)
	got, err := CheckEnd(path)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if !got {
		t.Fatal("expected marker detected")
	}
}

func TestCheckEnd_TrailingWhitespaceTolerated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "article.md")
	os.WriteFile(path, []byte("--- end ---   \n"), 0o644)
	got, err := CheckEnd(path)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if !got {
		t.Fatal("expected marker with trailing whitespace")
	}
}

func TestCheckEnd_MissingFile(t *testing.T) {
	_, err := CheckEnd(filepath.Join(t.TempDir(), "absent.md"))
	if !errors.Is(err, ErrNotYetWritten) {
		t.Fatalf("got %v, want ErrNotYetWritten", err)
	}
}

func TestCheckEnd_NonMarkerLastLineIsMalformed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "article.md")
	os.WriteFile(path, []byte("# Heading\nstill writing\n"), 0o644)
	_, err := CheckEnd(path)
	if !errors.Is(err, ErrMalformedMarker) {
		t.Fatalf("got %v, want ErrMalformedMarker", err)
	}
}

func TestCheckEnd_OnlyLastLineCounts(t *testing.T) {
	// Earlier marker-shaped lines in the body must not be mistaken for
	// the terminator. The postman reads only the last line (§18.5).
	path := filepath.Join(t.TempDir(), "article.md")
	body := strings.Repeat("--- end ---\n", 5) + "still writing\n"
	os.WriteFile(path, []byte(body), 0o644)

	_, err := CheckEnd(path)
	if !errors.Is(err, ErrMalformedMarker) {
		t.Fatalf("expected ErrMalformedMarker (last line not marker), got %v", err)
	}
}

// CheckScore — coding-end marker `--- end with N ---`.

func TestCheckScore_ValidScores(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  int
	}{
		{"zero", "analysis\n--- end with 0 ---", 0},
		{"mid", "analysis\n--- end with 50 ---", 50},
		{"hundred", "--- end with 100 ---", 100},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "feedback.md")
			os.WriteFile(path, []byte(tc.input+"\n"), 0o644)
			got, err := CheckScore(path)
			if err != nil {
				t.Fatalf("unexpected: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestCheckScore_Malformed(t *testing.T) {
	// Anything outside the exact `--- end with N ---` shape, or with N
	// outside [0, 100], is ErrMalformedMarker per §18.5 strict-parser.
	cases := []string{
		"--- end with abc ---",  // non-numeric
		"--- end with 200 ---",  // out of range high
		"--- end with 101 ---",  // out of range
		"--- end with -1 ---",   // negative literal
		"--- end with 1.5 ---",  // float
		"---END WITH 50 ---",    // wrong case
		"--- end ---",           // score marker expected, end-only given
		"--- end with",          // incomplete
		"just text",             // arbitrary content
	}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "fb.md")
			os.WriteFile(path, []byte(input+"\n"), 0o644)
			_, err := CheckScore(path)
			if !errors.Is(err, ErrMalformedMarker) {
				t.Fatalf("input %q: got %v, want ErrMalformedMarker", input, err)
			}
		})
	}
}

func TestCheckScore_MissingFile(t *testing.T) {
	_, err := CheckScore(filepath.Join(t.TempDir(), "absent.md"))
	if !errors.Is(err, ErrNotYetWritten) {
		t.Fatalf("got %v, want ErrNotYetWritten", err)
	}
}

// CRLF tolerance — wiking/coding agents may run in a PTY where the kernel
// ONLCR translates LF → CRLF (§6.2.1). The marker parser must accept
// `\r\n` line endings.
func TestCheckEnd_TrimsCarriageReturn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "article.md")
	os.WriteFile(path, []byte("--- end ---\r\n"), 0o644)
	got, err := CheckEnd(path)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if !got {
		t.Fatal("expected marker across CRLF line ending")
	}
}
