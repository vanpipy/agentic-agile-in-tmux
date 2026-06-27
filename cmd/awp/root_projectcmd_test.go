// root_projectcmd_test.go — regression test pinning projectCmd.New's basename
// extraction to use filepath.Base instead of hand-written loops.
//
// P3 cleanup: the previous code hand-rolled a character loop to extract
// the basename of the current working directory, which only handled Unix
// '/' separator (not Windows '\'). Replaced with filepath.Base which is
// portable and clearer.
//
// This test pins the source-level contract: projectCmd.New uses
// filepath.Base for basename extraction.
package awp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRootGo_ProjectCmdUsesFilepathBase pins the contract that the
// projectCmd.New handler uses filepath.Base (not a hand-written loop)
// to extract the basename of CWD when no name argument is given.
//
// CORRECT-7 self-check:
//   C-onformance: literal substring "filepath.Base" must appear in projectNewCmd body
//   O-rdering: N/A
//   R-ange: N/A
//   R-eference: source-file scan only
//   E-xistence: substring must exist
//   C-ardinality: 1 case
//   T-ime: no time concerns
func TestRootGo_ProjectCmdUsesFilepathBase(t *testing.T) {
	src := readRootGoSource(t)

	// Find the projectNewCmd function body.
	start := strings.Index(src, "var projectNewCmd = &cobra.Command{")
	if start < 0 {
		t.Fatal("projectNewCmd not found in root.go")
	}
	// Find the closing of the function (matching closing brace). Approximate
	// by finding the next "var " declaration after projectNewCmd.
	end := strings.Index(src[start+1:], "\nvar ")
	if end < 0 {
		end = len(src)
	} else {
		end += start + 1
	}
	body := src[start:end]

	if !strings.Contains(body, "filepath.Base(") {
		t.Errorf("projectNewCmd does not use filepath.Base for basename extraction.\n"+
			"P3 cleanup: hand-written loops are fragile (Unix-only '/').\n"+
			"Use filepath.Base(abs) which handles both '/' and '\\\\'.")
	}
}

// --- helpers ---

func readRootGoSource(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			path := filepath.Join(dir, "cmd", "awp", "root.go")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			return string(data)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}