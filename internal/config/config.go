package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// defaultInitPrompt is the template used when Pi.InitPrompt is empty.
// References Go-template variables that the spawn flow substitutes
// at runtime ({{.Title}}, {{.Description}}, {{.BranchName}}, {{.BaseBranch}}).
const defaultInitPrompt = `You have been spawned by awp, a TUI kanban for managing development tasks with the pi coding agent.

## Your Assignment

You are now working on a specific ticket. This ticket represents a discrete unit of work that needs to be completed.

**Ticket Title:** {{.Title}}

**Ticket Description:**
{{.Description}}

## Technical Context

- **Git Branch:** {{.BranchName}}
- **Base Branch:** {{.BaseBranch}}
- **Working Directory:** This session is scoped to an isolated git worktree for this ticket

## Expectations

1. Focus exclusively on completing the work described in this ticket
2. The ticket description above is your primary specification - implement what it describes
3. If the description is unclear or incomplete, ask clarifying questions before proceeding
4. Make commits as appropriate for the work being done
5. When the work is complete, summarize what was accomplished

Begin by analyzing the ticket requirements and proposing your approach.`

// Config holds the user-customizable settings for awp.
//
// As of the 2026-06-22 simplification pass, the schema was reduced
// from 22 fields to 12 (see commit 981599b for multi-agent removal;
// subsequent pass for dead-field removal). Only fields that real
// users actually tweak are exposed here; everything else is
// hardcoded at the call site or in the project.ProjectSettings.
type Config struct {
	Pi       PiConfig          `json:"pi"`
	UI       UIConfig          `json:"ui"`
	Defaults BoardSettings     `json:"defaults"`
	Cleanup  CleanupSettings   `json:"cleanup"`
	Behavior BehaviorSettings  `json:"behavior"`
}

// PiConfig configures the pi coding agent. Command, Args, and
// InitPrompt are user-customizable for users with non-default
// setups (custom binary path, wrapper script, team-specific
// init prompts). Env is not exposed — zero callers in the
// codebase, and env scrubbing happens via buildCleanEnv.
type PiConfig struct {
	Command    string   `json:"command"`
	Args       []string `json:"args"`
	InitPrompt string   `json:"init_prompt"`
}

// BoardSettings holds per-user defaults for branch naming and
// slug length. Project-level ProjectSettings can override these.
type BoardSettings struct {
	BranchPrefix   string `json:"branch_prefix"`              // e.g., "task/"
	BranchNaming   string `json:"branch_naming"`              // "template" | "ai" | "prompt"
	BranchTemplate string `json:"branch_template"`            // e.g., "{prefix}{slug}"
	SlugMaxLength  int    `json:"slug_max_length"`            // default: 40
}

// UIConfig holds TUI display preferences.
type UIConfig struct {
	Theme           string       `json:"theme"`                    // e.g., "catppuccin-mocha"
	CustomColors    *ThemeColors `json:"custom_colors,omitempty"`  // brand colors override theme
	SidebarVisible  bool         `json:"sidebar_visible"`          // show project sidebar
}

// BehaviorSettings holds miscellaneous app-behavior toggles.
type BehaviorSettings struct {
	ConfirmQuitWithAgents bool `json:"confirm_quit_with_agents"` // prompt before quit if sessions running
}

// CleanupSettings controls worktree/branch cleanup behavior when
// deleting tickets. Defaults are sensible (delete worktree, keep
// branch so users can review) but users with specific workflows
// (e.g. want auto-branch-delete) can override.
type CleanupSettings struct {
	DeleteWorktree       bool `json:"delete_worktree"`        // Remove git worktree on ticket delete
	DeleteBranch         bool `json:"delete_branch"`          // Delete git branch after worktree removal
	ForceWorktreeRemoval bool `json:"force_worktree_removal"` // Force removal even with uncommitted changes
}

// DefaultConfig returns the default configuration. Every field
// here is overridable via config.json, but the defaults are chosen
// so that awp works out-of-the-box without any config file.
func DefaultConfig() *Config {
	return &Config{
		Pi: PiConfig{
			Command:    "pi",
			Args:       []string{},
			InitPrompt: defaultInitPrompt,
		},
		Defaults: BoardSettings{
			BranchPrefix:   "task/",
			BranchNaming:   "template",
			BranchTemplate: "{prefix}{slug}",
			SlugMaxLength:  40,
		},
		UI: UIConfig{
			Theme:          "catppuccin-mocha",
			SidebarVisible: true,
		},
		Behavior: BehaviorSettings{
			ConfirmQuitWithAgents: true,
		},
		Cleanup: CleanupSettings{
			DeleteWorktree:       true,
			DeleteBranch:         false,
			ForceWorktreeRemoval: false,
		},
	}
}

// ConfigDir returns the configuration directory path.
// Priority: AWP_CONFIG_DIR > XDG_CONFIG_HOME/awp > ~/.config/awp
func ConfigDir() (string, error) {
	// Explicit override (testing, CI, multiple instances)
	if dir := os.Getenv("AWP_CONFIG_DIR"); dir != "" {
		return dir, nil
	}

	// XDG standard
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "awp"), nil
	}

	// Default fallback
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "awp"), nil
}

// ConfigPath returns the default config file path
func ConfigPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Load reads configuration from file or returns defaults.
//
// The config file is optional: if missing or empty, returns
// DefaultConfig() with no error. This is the zero-config behavior.
func Load(path string) (*Config, error) {
	if path == "" {
		var err error
		path, err = ConfigPath()
		if err != nil {
			return DefaultConfig(), nil
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return nil, err
	}

	// Empty file = use defaults
	if len(data) == 0 {
		return DefaultConfig(), nil
	}

	cfg := DefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// InitPrompt returns the effective initial prompt for pi. Returns
// Pi.InitPrompt if non-empty, else the built-in default.
func (c *Config) InitPrompt() string {
	if c.Pi.InitPrompt != "" {
		return c.Pi.InitPrompt
	}
	return defaultInitPrompt
}

func (c *Config) GetTheme() Theme {
	return GetTheme(c.UI.Theme, c.UI.CustomColors)
}

// Save writes configuration to file using the atomic tmp+rename pattern.
// On crash mid-write, the destination file is left intact (rename is atomic
// on POSIX). This matches the pattern in internal/project/tickets.go:Save()
// and internal/project/store.go:Save() — see Cluster B.1 of the 2026-06-27
// audit for context.
func (c *Config) Save(path string) error {
	if path == "" {
		var err error
		path, err = ConfigPath()
		if err != nil {
			return err
		}
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		// Clean up the orphaned tmp file so repeated failed saves don't
		// accumulate debris in the user's config dir.
		os.Remove(tmpPath)
		return err
	}
	return nil
}

// LoadWithValidation loads config and returns structured validation result
func LoadWithValidation(path string) (*Config, *ValidationResult, error) {
	if path == "" {
		var err error
		path, err = ConfigPath()
		if err != nil {
			return DefaultConfig(), nil, nil
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := DefaultConfig()
			return cfg, cfg.Validate(), nil
		}
		return nil, nil, err
	}

	if len(data) == 0 {
		cfg := DefaultConfig()
		return cfg, cfg.Validate(), nil
	}

	cfg := DefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		result := &ValidationResult{}
		if jsonErr := formatJSONError(err); jsonErr != "" {
			result.AddError("json", "", jsonErr, nil)
		} else {
			result.AddError("json", "", err.Error(), nil)
		}
		return nil, result, err
	}

	result := cfg.Validate()

	return cfg, result, nil
}

// formatJSONError attempts to provide better JSON error context
func formatJSONError(err error) string {
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return fmt.Sprintf("invalid JSON at byte %d: %s", syntaxErr.Offset, syntaxErr.Error())
	}

	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return fmt.Sprintf("field %q expects %s but got %s", typeErr.Field, typeErr.Type, typeErr.Value)
	}

	return ""
}