package config

import (
	"strings"
	"testing"
)

func TestValidate_ValidDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	result := cfg.Validate()

	if result.HasErrors() {
		t.Errorf("default config should be valid, got errors:\n%s", result.FormatErrors())
	}
}

func TestValidate_InvalidBranchNaming(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Defaults.BranchNaming = "invalid"

	result := cfg.Validate()

	if !result.HasErrors() {
		t.Error("expected validation error for invalid branch_naming")
	}

	found := false
	for _, e := range result.Errors {
		if e.Section == "defaults" && e.Field == "branch_naming" {
			found = true
			if !strings.Contains(e.Message, "template, ai, prompt") {
				t.Errorf("error message should list valid values; got %q", e.Message)
			}
		}
	}
	if !found {
		t.Error("expected error for defaults.branch_naming")
	}
}

func TestValidate_NegativeSlugMaxLength(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Defaults.SlugMaxLength = -1

	result := cfg.Validate()

	if !result.HasErrors() {
		t.Error("expected validation error for negative slug_max_length")
	}

	found := false
	for _, e := range result.Errors {
		if e.Section == "defaults" && e.Field == "slug_max_length" {
			found = true
		}
	}
	if !found {
		t.Error("expected error for defaults.slug_max_length")
	}
}

func TestValidate_BranchTemplateMissingPlaceholders(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Defaults.BranchTemplate = "feature-branch"

	result := cfg.Validate()

	// This should be a warning, not an error
	if result.HasErrors() {
		t.Errorf("missing placeholders should be a warning, not error:\n%s", result.FormatErrors())
	}

	if !result.HasWarnings() {
		t.Error("expected warning for branch_template without placeholders")
	}

	found := false
	for _, w := range result.Warnings {
		if w.Section == "defaults" && w.Field == "branch_template" {
			found = true
		}
	}
	if !found {
		t.Error("expected warning for defaults.branch_template")
	}
}

func TestValidate_InvalidPiTemplatePrompt(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Pi.InitPrompt = "{{.Invalid syntax"

	result := cfg.Validate()

	if !result.HasErrors() {
		t.Error("expected validation error for invalid template syntax")
	}

	found := false
	for _, e := range result.Errors {
		if e.Section == "pi" && e.Field == "init_prompt" {
			found = true
		}
	}
	if !found {
		t.Error("expected error for pi.init_prompt")
	}
}

func TestValidate_UnknownThemeIsWarning(t *testing.T) {
	cfg := DefaultConfig()
	cfg.UI.Theme = "nonexistent-theme"

	result := cfg.Validate()

	// Theme validation is a warning, not an error (fallback to default)
	if result.HasErrors() {
		t.Errorf("unknown theme should be a warning, not error:\n%s", result.FormatErrors())
	}

	if !result.HasWarnings() {
		t.Error("expected warning for unknown theme")
	}

	found := false
	for _, w := range result.Warnings {
		if w.Section == "ui" && w.Field == "theme" {
			found = true
		}
	}
	if !found {
		t.Error("expected warning for ui.theme")
	}
}

func TestValidationResult_FormatErrors(t *testing.T) {
	r := &ValidationResult{}
	r.AddError("defaults", "branch_naming", "must be valid", "invalid")
	r.AddError("pi", "init_prompt", "is required", nil)

	output := r.FormatErrors()

	if !strings.Contains(output, "defaults") {
		t.Error("formatted errors should contain section name")
	}
	if !strings.Contains(output, "branch_naming") {
		t.Error("formatted errors should contain field name")
	}
	if !strings.Contains(output, "must be valid") {
		t.Error("formatted errors should contain message")
	}
	if !strings.Contains(output, "invalid") {
		t.Error("formatted errors should contain value")
	}
	if !strings.Contains(output, "pi") {
		t.Error("formatted errors should contain pi section")
	}
}

func TestValidationResult_FormatWarnings(t *testing.T) {
	r := &ValidationResult{}
	r.AddWarning("ui", "theme", "not found in catalog", "custom-theme")

	output := r.FormatWarnings()

	if !strings.Contains(output, "ui") {
		t.Error("formatted warnings should contain section name")
	}
	if !strings.Contains(output, "theme") {
		t.Error("formatted warnings should contain field name")
	}
	if !strings.Contains(output, "not found in catalog") {
		t.Error("formatted warnings should contain message")
	}
}

func TestValidationResult_HasErrors(t *testing.T) {
	r := &ValidationResult{}

	if r.HasErrors() {
		t.Error("empty result should not have errors")
	}

	r.AddError("test", "field", "message", nil)

	if !r.HasErrors() {
		t.Error("result with error should have errors")
	}
}

func TestValidationResult_HasWarnings(t *testing.T) {
	r := &ValidationResult{}

	if r.HasWarnings() {
		t.Error("empty result should not have warnings")
	}

	r.AddWarning("test", "field", "message", nil)

	if !r.HasWarnings() {
		t.Error("result with warning should have warnings")
	}
}

func TestValidate_MultipleErrors(t *testing.T) {
	cfg := &Config{
		Defaults: BoardSettings{
			BranchNaming:   "invalid",
			SlugMaxLength:  -1,
			BranchTemplate: "no-placeholders",
		},
		Pi: PiConfig{
			InitPrompt: "{{.Broken",
		},
	}

	result := cfg.Validate()

	// Should have at least 3 errors (branch_naming, slug_max_length, init_prompt)
	if len(result.Errors) < 3 {
		t.Errorf("expected at least 3 errors, got %d:\n%s", len(result.Errors), result.FormatErrors())
	}

	// Should have at least one warning (branch_template)
	if len(result.Warnings) < 1 {
		t.Error("expected at least 1 warning for branch_template")
	}
}

func TestValidate_EmptyBranchNamingIsValid(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Defaults.BranchNaming = ""

	result := cfg.Validate()

	for _, e := range result.Errors {
		if e.Field == "branch_naming" {
			t.Error("empty branch_naming should be valid (uses default)")
		}
	}
}

func TestValidate_ValidTemplatePrompt(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Pi.InitPrompt = "Working on: {{.Title}}\nDescription: {{.Description}}"

	result := cfg.Validate()

	for _, e := range result.Errors {
		if e.Section == "pi" && e.Field == "init_prompt" {
			t.Errorf("valid template should not produce error: %s", e.Message)
		}
	}
}