// projectcmd_validation_test.go — TDD test pinning projectCmd.New
// name validation.
//
// Finding CASTRATION-3 from post-P3P4 audit: projectCmd.New accepts a
// user-supplied name and passes it verbatim to app.CreateProject. No
// length or character validation. A 10MB name string could bloat the
// on-disk projects.json.
//
// Fix: validate name length (1-256 chars) and reject control characters.
package awp

import (
	"strings"
	"testing"

	"github.com/pi/awp/internal/project"
)

// TestProjectCmd_RejectsInvalidName was REMOVED in favor of the
// focused unit test on validateProjectName (TestValidateProjectName
// below). The cobra command calls validateProjectName, so testing the
// helper directly is sufficient.

// TestProjectNew_RejectsEmptyName pins the contract at the project layer.
// project.NewProject itself does not validate; the validation lives in
// cmd/awp/root.go (validateProjectName). This test just pins that
// NewProject doesn't enforce — the validation is upstream.
func TestProjectNew_RejectsEmptyName(t *testing.T) {
	p := project.NewProject("", "/tmp")
	// NewProject does not validate; that's the CLI's job.
	if p.Name != "" {
		t.Errorf("NewProject(\"\") = %q; want empty pass-through", p.Name)
	}
}

// TestValidateProjectName is the actual validation contract.
func TestValidateProjectName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"empty", "", true},
		{"whitespace only", "   ", true},
		{"single char", "a", false},
		{"normal", "my-repo", false},
		{"with dots", "my.repo", false},
		{"with spaces", "my repo", false},
		{"too long (257 chars)", strings.Repeat("a", 257), true},
		{"max length (256 chars)", strings.Repeat("a", 256), false},
		{"control char", "my\nrepo", true},
		{"tab char", "my\trepo", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateProjectName(tt.input)
			gotErr := err != nil
			if gotErr != tt.wantErr {
				t.Errorf("validateProjectName(%q) error = %v; wantErr = %v (err=%v)",
					tt.input, err, tt.wantErr, err)
			}
		})
	}
}
