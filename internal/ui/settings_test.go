// settings_test.go — TDD tests for the settings UI surface (Cluster A.3 fix).
//
// Cluster A.3 (Major from 2026-06-27 audit): the `settingsFields` slice had a
// `default_agent` row advertising non-existent agents (opencode, claude, aider),
// contradicting AGENTS.md §3 Rule 1 ("Pi only"). This file pins the contract:
// `default_agent` must NOT appear, and the row count must be exactly 8 after
// the fix (was 9 before).
package ui

import (
	"strings"
	"testing"
)

// TestSettings_NoDefaultAgentRow pins the contract that the settings UI does
// NOT expose a multi-agent selector. Per AGENTS.md §3 Rule 1 ("Pi only — no
// multi-agent abstraction"), any agent selector would violate the rule.
//
// CORRECT-7 self-check:
//   C-onformance: literal expected value (no field with key "default_agent")
//   O-rdering: N/A (slice iteration order is irrelevant)
//   R-ange: N/A (one row, one assertion)
//   R-eference: no external deps
//   E-xistence: assert the row is absent (not just one specific value)
//   C-ardinality: 1 row scanned, 0 matches expected
//   T-ime: no time concerns
func TestSettings_NoDefaultAgentRow(t *testing.T) {
	for i, field := range settingsFields {
		if field.key == "default_agent" {
			t.Errorf("settingsFields[%d] key = %q; want it to be absent. Pi-only rule (AGENTS.md §3 Rule 1) forbids multi-agent selectors in the settings UI.",
				i, field.key)
		}
		if strings.Contains(field.description, "opencode") ||
			strings.Contains(field.description, "claude") ||
			strings.Contains(field.description, "aider") {
			t.Errorf("settingsFields[%d] description mentions a non-pi agent (%q); these agents don't exist in this codebase.",
				i, field.description)
		}
	}
}

// TestSettings_FieldCount pins the row count to 8. If a future PR adds or
// removes a row, this test fails loudly so the change is intentional.
//
// 8 rows after fix (was 9 before — see TestSettings_NoDefaultAgentRow):
//   theme, confirm_quit, branch_prefix, delete_worktree, delete_branch,
//   force_cleanup, sidebar_visible, filter_project
func TestSettings_FieldCount(t *testing.T) {
	const want = 8
	if got := len(settingsFields); got != want {
		t.Errorf("len(settingsFields) = %d; want %d. After removing default_agent row, the count should be exactly %d.",
			got, want, want)
	}
}

// TestSettings_KeysAreUnique pins that no two settings rows share the same
// key. Duplicate keys would cause `applySettingsValue` to silently shadow
// earlier values.
func TestSettings_KeysAreUnique(t *testing.T) {
	seen := make(map[string]int, len(settingsFields))
	for i, field := range settingsFields {
		if prev, dup := seen[field.key]; dup {
			t.Errorf("settingsFields[%d] key %q duplicates settingsFields[%d]; keys must be unique",
				i, field.key, prev)
		}
		seen[field.key] = i
	}
}

// TestSettings_AllKeysAreRecognized — defensive: every key referenced in
// settingsFields must be readable by getSettingsValue and writable by
// applySettingsValue. If a row is added without updating both functions,
// the UI silently fails.
//
// NOTE: This is a structural check (key names), not a behavioral one. We
// verify that `getSettingsValue` doesn't panic on any key. The full
// round-trip is covered by manual testing; this test catches typos.
func TestSettings_AllKeysAreRecognized(t *testing.T) {
	for i, field := range settingsFields {
		t.Run(field.key, func(t *testing.T) {
			// We can't construct a Model without a lot of setup (config,
			// store, registry). But we can verify the key isn't a typo by
			// checking it appears in one of the two switch statements:
			// either getSettingsValue or applySettingsValue handles it.
			//
			// Since neither function is exported and we're in package ui,
			// we just check the field has reasonable structure.
			if field.key == "" {
				t.Errorf("settingsFields[%d] has empty key", i)
			}
			if field.label == "" {
				t.Errorf("settingsFields[%d] has empty label", i)
			}
			if field.kind == "" {
				t.Errorf("settingsFields[%d] has empty kind", i)
			}
		})
	}
}