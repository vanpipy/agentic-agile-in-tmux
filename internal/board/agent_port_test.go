// agent_port_test.go — TDD test pinning the removal of AgentPort field.
//
// M7 (medium-priority): Ticket.AgentPort is a legacy multi-agent residue
// field with ZERO readers anywhere in the codebase. Keep it alive = keep
// dead weight that future agents must understand as "why is this here?"
//
// This test pins the contract: AgentPort must not appear in:
//   - The Ticket struct definition
//   - Any other source file (no readers/writers)
package board

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBoardGo_NoAgentPortField pins the contract that AgentPort is removed.
//
// We check for "AgentPort" outside of comments (anything between // or
// inside /* */ blocks is excluded). This allows the comment in board.go
// to reference the removed field by name as documentation.
//
// CORRECT-7 self-check:
//   C-onformance: no AgentPort outside comments
//   O-rdering: N/A
//   R-ange: N/A
//   R-eference: source-file scan only
//   E-xistence: substring must NOT exist in code
//   C-ardinality: 0 occurrences in code
//   T-ime: no time concerns
func TestBoardGo_NoAgentPortField(t *testing.T) {
	src := readBoardGoSource(t)
	codeOnly := stripComments(src)
	if strings.Contains(codeOnly, "AgentPort") {
		line := agentPortLineNumber(codeOnly)
		t.Errorf("board.go:%d still uses Ticket.AgentPort outside of a comment.\n"+
			"M7: this field is multi-agent residue with ZERO readers.\n"+
			"Remove it. (References inside comments are fine for documentation.)",
			line)
	}
}

// stripComments removes // line comments and /* */ block comments from
// Go source code. Returns the code body without comments.
func stripComments(src string) string {
	var result strings.Builder
	i := 0
	for i < len(src) {
		// Line comment
		if i+1 < len(src) && src[i] == '/' && src[i+1] == '/' {
			// Skip until end of line
			for i < len(src) && src[i] != '\n' {
				i++
			}
			continue
		}
		// Block comment
		if i+1 < len(src) && src[i] == '/' && src[i+1] == '*' {
			i += 2
			for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
				i++
			}
			i += 2
			continue
		}
		// String literal — preserve (don't strip // inside strings)
		if src[i] == '"' {
			result.WriteByte(src[i])
			i++
			for i < len(src) && src[i] != '"' {
				if src[i] == '\\' && i+1 < len(src) {
					result.WriteByte(src[i])
					i++
				}
				result.WriteByte(src[i])
				i++
			}
			if i < len(src) {
				result.WriteByte(src[i])
				i++
			}
			continue
		}
		result.WriteByte(src[i])
		i++
	}
	return result.String()
}

// TestNoAgentPortAnywhere extends the check to ALL .go files in the repo.
// Excludes comments (allows historical reference in documentation).
func TestNoAgentPortAnywhere(t *testing.T) {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}

	// Walk all .go files except this test file (which contains the
	// literal "AgentPort" intentionally in test names).
	const selfPath = "internal/board/agent_port_test.go"

	violations := []string{}
	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.Contains(path, "/.git/") || strings.Contains(path, "/vendor/") {
			return nil
		}
		relPath, _ := filepath.Rel(dir, path)
		if relPath == selfPath {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(stripComments(string(data)), "AgentPort") {
			violations = append(violations, relPath)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	if len(violations) > 0 {
		t.Errorf("AgentPort still used in code (outside comments) in %d file(s):\n  %s\n"+
			"These are dead multi-agent residue fields. Remove them.",
			len(violations), strings.Join(violations, "\n  "))
	}
}

// --- helpers ---

func agentPortLineNumber(src string) int {
	idx := strings.Index(src, "AgentPort")
	if idx < 0 {
		return 0
	}
	return strings.Count(src[:idx], "\n") + 1
}

// readBoardGoSource is defined in transition_test.go (shared).