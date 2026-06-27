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
	"go/ast"
	"go/parser"
	"go/token"
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
	if hasIdentifier(src, "AgentPort") {
		t.Errorf("board.go still uses identifier 'AgentPort' outside of a comment.\n"+
			"M7: this field is multi-agent residue with ZERO readers.\n"+
			"Remove it. (References inside comments are fine for documentation.)")
	}
}

// hasIdentifier reports whether src contains the Go identifier name
// outside of comments. Uses go/parser AST walk so it correctly handles
// string literals, raw strings, and multi-line comments.
func hasIdentifier(src, name string) bool {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		// Fall back to substring search on parse error — better than
		// false-positive test failure.
		return strings.Contains(src, name)
	}
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == name {
			found = true
			return false
		}
		return true
	})
	return found
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
		if hasIdentifier(string(data), "AgentPort") {
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

// readBoardGoSource is defined in transition_test.go (shared).