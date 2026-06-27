// branding_test.go — regression test for Cluster A.1/A.2 fixes.
//
// Pins the contract that user-visible branding reads "awp", not "OpenKanban".
// Whitelisted "openkanban" / "OpenKanban" references (all lowercase too) are
// functional保留 per memory `awp.openkanban_cleanup_done`:
//   - .openkanban directory path (data migration in tickets.go)
//   - OPENKANBAN_SESSION env var (pane.go:1540)
//   - comments documenting historical context (pane_test.go, test/...)
//
// This test scans source files for any "OpenKanban" reference (case-sensitive)
// and asserts the only matches are in the whitelisted paths. Adding a new
// "OpenKanban" reference anywhere else means stale branding leaked.
package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBranding_NoOpenKanbanUserVisibleString scans every .go file under the
// project root for the literal "OpenKanban" and fails if any match is outside
// the whitelist of functional保留 references.
//
// CORRECT-7 self-check:
//   C-onformance: literal substring "OpenKanban"
//   O-rdering: N/A (set membership)
//   R-ange: scan all files (one sweep)
//   R-eference: filesystem only (no network)
//   E-xistence: missing whitelist entry = test fails
//   C-ardinality: 0 violations expected
//   T-ime: scan completes in <100ms; no timeout concern
func TestBranding_NoOpenKanbanUserVisibleString(t *testing.T) {
	// Functional保留 whitelist per memory `awp.openkanban_cleanup_done`.
	// Each entry: file path (relative to project root) → list of allowed substrings.
	whitelist := map[string][]string{
		"internal/project/tickets.go": {
			"// (before this project was awp) used a `.openkanban` directory. Read",
			"oldPath := filepath.Join(project.RepoPath, \".openkanban\", \"tickets.json\")",
			"You can safely delete the old .openkanban directory after the first run.",
		},
		"internal/terminal/pane.go": {
			"// capturing scrollback per chunk to fix the openkanban",
			"env = append(env, \"OPENKANBAN_SESSION=\"+sessionName)",
		},
		"test/pi/spawn_args_test.go": {
			"// Earlier, the openkanban-era code assumed a generic agent CLI",
		},
		"test/terminal/spawn_chain_test.go": {
			"// TestSpawn_OpenkanbanStyle exercises the prepareSpawn",
			"func TestSpawn_OpenkanbanStyle(t *testing.T) {",
		},
		"internal/ui/spawn_args_test.go": {
			"// old openkanban-era code), this test fails immediately.",
		},
		"internal/terminal/pane_test.go": {
			"// This guards against the openkanban every-other-line truncation",
		},
		"e2e/spawn_real_pi_test.go": {
			"// TestSpawn_RealPi_NoPanic drives the full openkanban-style spawn",
		},
		// branding_test.go is itself the regression test; all "OpenKanban"
		// references here are deliberate (handled as special case above).
	}

	// Find project root by walking up from this test file.
	projectRoot := findProjectRoot(t)

	// branding_test.go is a self-referential test; all of its "OpenKanban"
	// references are intentional (the regression-test contract itself).
	// Special-case the entire file as "all lines allowed".
	const brandingTestFile = "internal/ui/branding_test.go"

	violations := []string{}

	err := filepath.Walk(projectRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// Only scan .go files
		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		// Skip vendor and .git
		if strings.Contains(path, "/.git/") || strings.Contains(path, "/vendor/") {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		// Look for "OpenKanban" (the user-visible branding, case-sensitive).
		if !strings.Contains(string(data), "OpenKanban") {
			return nil
		}

		relPath, _ := filepath.Rel(projectRoot, path)

		// branding_test.go is itself the test that enforces this rule;
		// every "OpenKanban" reference here is the regression contract.
		if relPath == brandingTestFile {
			return nil
		}

		// Check whitelist: if the file is in the whitelist, only allowed
		// substrings may contain "OpenKanban".
		allowed, isWhitelisted := whitelist[relPath]
		if !isWhitelisted {
			violations = append(violations, relPath+": contains 'OpenKanban' (not whitelisted)")
			return nil
		}

		// File is whitelisted; verify each line containing "OpenKanban" matches
		// one of the allowed substrings.
		for _, line := range strings.Split(string(data), "\n") {
			if !strings.Contains(line, "OpenKanban") {
				continue
			}
			ok := false
			for _, allowedSubstr := range allowed {
				if strings.Contains(line, allowedSubstr) {
					ok = true
					break
				}
			}
			if !ok {
				violations = append(violations, relPath+": non-whitelisted line contains 'OpenKanban': "+line)
			}
		}

		return nil
	})

	if err != nil {
		t.Fatalf("walk failed: %v", err)
	}

	if len(violations) > 0 {
		t.Errorf("Found %d OpenKanban references outside the whitelist. These are stale branding; the project was renamed to 'awp':\n  %s",
			len(violations), strings.Join(violations, "\n  "))
	}
}

// findProjectRoot walks up from this test file looking for go.mod.
// Mirrors internal/testutil/RepoRoot but inlined to avoid an import cycle
// (testutil imports nothing from ui, but ui importing testutil feels wrong).
func findProjectRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}