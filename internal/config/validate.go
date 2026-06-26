package config

import (
	"fmt"
	"strings"
	"text/template"
)

// ValidationError represents a single config validation issue
type ValidationError struct {
	Section string // "defaults", "pi", "ui", etc.
	Field   string // "init_prompt", "branch_naming", etc.
	Message string // Human-readable error
	Value   any    // The invalid value (for display)
}

// ValidationResult holds all validation errors and warnings
type ValidationResult struct {
	Errors   []ValidationError
	Warnings []ValidationError
}

// HasErrors returns true if there are any validation errors
func (r *ValidationResult) HasErrors() bool {
	return len(r.Errors) > 0
}

// HasWarnings returns true if there are any validation warnings
func (r *ValidationResult) HasWarnings() bool {
	return len(r.Warnings) > 0
}

// AddError adds a validation error
func (r *ValidationResult) AddError(section, field, message string, value any) {
	r.Errors = append(r.Errors, ValidationError{
		Section: section,
		Field:   field,
		Message: message,
		Value:   value,
	})
}

// AddWarning adds a validation warning
func (r *ValidationResult) AddWarning(section, field, message string, value any) {
	r.Warnings = append(r.Warnings, ValidationError{
		Section: section,
		Field:   field,
		Message: message,
		Value:   value,
	})
}

// FormatErrors returns a formatted string of all errors for CLI output
func (r *ValidationResult) FormatErrors() string {
	var sb strings.Builder
	for _, e := range r.Errors {
		if e.Field != "" {
			sb.WriteString(fmt.Sprintf("  [%s] %s\n", e.Section, e.Field))
		} else {
			sb.WriteString(fmt.Sprintf("  [%s]\n", e.Section))
		}
		sb.WriteString(fmt.Sprintf("    %s\n", e.Message))
		if e.Value != nil {
			sb.WriteString(fmt.Sprintf("    got: %v\n", e.Value))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// FormatWarnings returns a formatted string of all warnings for CLI output
func (r *ValidationResult) FormatWarnings() string {
	var sb strings.Builder
	for _, w := range r.Warnings {
		if w.Field != "" {
			sb.WriteString(fmt.Sprintf("  [%s] %s\n", w.Section, w.Field))
		} else {
			sb.WriteString(fmt.Sprintf("  [%s]\n", w.Section))
		}
		sb.WriteString(fmt.Sprintf("    %s\n", w.Message))
		if w.Value != nil {
			sb.WriteString(fmt.Sprintf("    got: %v\n", w.Value))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// Validate performs full config validation and returns all errors and warnings
func (c *Config) Validate() *ValidationResult {
	result := &ValidationResult{}
	c.validateDefaults(result)
	c.validateUI(result)
	return result
}

// validateDefaults validates the defaults section.
func (c *Config) validateDefaults(r *ValidationResult) {
	// BranchNaming must be a valid enum value
	validNaming := map[string]bool{"template": true, "ai": true, "prompt": true, "": true}
	if !validNaming[c.Defaults.BranchNaming] {
		r.AddError("defaults", "branch_naming",
			fmt.Sprintf("must be one of: template, ai, prompt (got %q)", c.Defaults.BranchNaming),
			c.Defaults.BranchNaming)
	}

	// SlugMaxLength must be positive if set
	if c.Defaults.SlugMaxLength < 0 {
		r.AddError("defaults", "slug_max_length",
			"must be a positive number",
			c.Defaults.SlugMaxLength)
	}

	// BranchTemplate should contain placeholders (warning only)
	if c.Defaults.BranchTemplate != "" {
		if !strings.Contains(c.Defaults.BranchTemplate, "{slug}") &&
			!strings.Contains(c.Defaults.BranchTemplate, "{prefix}") {
			r.AddWarning("defaults", "branch_template",
				"should contain {slug} or {prefix} placeholder",
				c.Defaults.BranchTemplate)
		}
	}

	// InitPrompt template syntax (if user provided one)
	if c.Pi.InitPrompt != "" {
		if err := validateTemplate(c.Pi.InitPrompt); err != nil {
			r.AddError("pi", "init_prompt",
				fmt.Sprintf("invalid Go template syntax: %v", err),
				nil)
		}
	}
}

// validateUI validates the UI section.
func (c *Config) validateUI(r *ValidationResult) {
	if c.UI.Theme != "" && !IsValidTheme(c.UI.Theme) {
		r.AddWarning("ui", "theme",
			fmt.Sprintf("unknown theme %q, falling back to catppuccin-mocha. Available: %v",
				c.UI.Theme, ThemeNames()),
			c.UI.Theme)
	}
}

// validateTemplate checks if a string is a valid Go template
func validateTemplate(tmpl string) error {
	_, err := template.New("check").Parse(tmpl)
	return err
}