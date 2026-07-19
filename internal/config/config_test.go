package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	// Pi
	if cfg.Pi.InitPrompt == "" {
		t.Error("Pi.InitPrompt should not be empty (has built-in default)")
	}

	// Defaults (branch naming)
	if cfg.Defaults.BranchPrefix != "task/" {
		t.Errorf("Defaults.BranchPrefix = %q; want %q", cfg.Defaults.BranchPrefix, "task/")
	}
	if cfg.Defaults.BranchNaming != "template" {
		t.Errorf("Defaults.BranchNaming = %q; want %q", cfg.Defaults.BranchNaming, "template")
	}
	if cfg.Defaults.BranchTemplate != "{prefix}{slug}" {
		t.Errorf("Defaults.BranchTemplate = %q; want %q", cfg.Defaults.BranchTemplate, "{prefix}{slug}")
	}
	if cfg.Defaults.SlugMaxLength != 40 {
		t.Errorf("Defaults.SlugMaxLength = %d; want %d", cfg.Defaults.SlugMaxLength, 40)
	}

	// UI
	if cfg.UI.Theme != "catppuccin-mocha" {
		t.Errorf("UI.Theme = %q; want %q", cfg.UI.Theme, "catppuccin-mocha")
	}
	if !cfg.UI.SidebarVisible {
		t.Error("UI.SidebarVisible should be true by default")
	}

	// Postman (§18.10 defaults)
	if cfg.Cycle.Threshold != 90 {
		t.Errorf("Cycle.Threshold = %d; want 90", cfg.Cycle.Threshold)
	}
	if cfg.Cycle.IdleInterval.Seconds() != 30 {
		t.Errorf("Cycle.IdleInterval = %v; want 30s", cfg.Cycle.IdleInterval)
	}
	if cfg.Cycle.WikingInterval.Seconds() != 5 {
		t.Errorf("Cycle.WikingInterval = %v; want 5s", cfg.Cycle.WikingInterval)
	}
	if cfg.Cycle.CodingInterval.Seconds() != 10 {
		t.Errorf("Cycle.CodingInterval = %v; want 10s", cfg.Cycle.CodingInterval)
	}
	if cfg.Cycle.WikingTimeout.Minutes() != 30 {
		t.Errorf("Cycle.WikingTimeout = %v; want 30m", cfg.Cycle.WikingTimeout)
	}
	if cfg.Cycle.CodingTimeout.Minutes() != 60 {
		t.Errorf("Cycle.CodingTimeout = %v; want 60m", cfg.Cycle.CodingTimeout)
	}
	if cfg.Cycle.MaxNoProgress != 20 {
		t.Errorf("Cycle.MaxNoProgress = %d; want 20", cfg.Cycle.MaxNoProgress)
	}
	if cfg.Wiking.Prompt == "" {
		t.Error("Wiking.Prompt should have a default")
	}
	if cfg.Coding.Prompt == "" {
		t.Error("Coding.Prompt should have a default")
	}
}

func TestConfigCycle_RoundTrip(t *testing.T) {
	// JSON marshaling preserves all fields including the new ones.
	original := DefaultConfig()
	original.Cycle.Threshold = 75
	original.Cycle.IdleInterval = 7 * time.Second
	original.Wiking.Prompt = "custom wiking prompt"
	original.Wiking.CWD = "/custom/wiki"
	original.Wiking.AllowedTools = []string{"bash", "read", "write"}
	original.Coding.AllowedTools = []string{"bash", "read"}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var loaded Config
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if loaded.Cycle.Threshold != 75 {
		t.Errorf("threshold round-trip got %d want 75", loaded.Cycle.Threshold)
	}
	if loaded.Cycle.IdleInterval != 7*time.Second {
		t.Errorf("idle_interval round-trip got %v want 7s", loaded.Cycle.IdleInterval)
	}
	if loaded.Wiking.Prompt != "custom wiking prompt" {
		t.Errorf("wiking.prompt round-trip got %q", loaded.Wiking.Prompt)
	}
	if loaded.Wiking.CWD != "/custom/wiki" {
		t.Errorf("wiking.cwd round-trip got %q", loaded.Wiking.CWD)
	}
	if len(loaded.Wiking.AllowedTools) != 3 {
		t.Errorf("wiking.allowed_tools round-trip got %d items", len(loaded.Wiking.AllowedTools))
	}
}

func TestConfigValidate_CycleErrors(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Config)
		// sub is the section/sub-field prefix the error should mention.
		mention string
	}{
		{
			name:    "threshold too high",
			mut:     func(c *Config) { c.Cycle.Threshold = 200 },
			mention: "cycle.threshold",
		},
		{
			name:    "threshold negative",
			mut:     func(c *Config) { c.Cycle.Threshold = -1 },
			mention: "cycle.threshold",
		},
		{
			name:    "max_no_progress zero",
			mut:     func(c *Config) { c.Cycle.MaxNoProgress = 0 },
			mention: "cycle.max_no_progress",
		},
		{
			name:    "wiking_interval zero",
			mut:     func(c *Config) { c.Cycle.WikingInterval = 0 },
			mention: "cycle.wiking_interval",
		},
		{
			name:    "coding_timeout zero",
			mut:     func(c *Config) { c.Cycle.CodingTimeout = 0 },
			mention: "cycle.coding_timeout",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tc.mut(cfg)
			r := cfg.Validate()
			if !r.HasErrors() {
				t.Fatal("expected validation errors, got none")
			}
			found := false
			for _, e := range r.Errors {
				if e.Section+"."+e.Field == tc.mention {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("expected error mentioning %q, got: %s", tc.mention, r.FormatErrors())
			}
		})
	}
}

func TestConfigValidate_DefaultHasNoErrors(t *testing.T) {
	// The shipped default config must validate clean.
	cfg := DefaultConfig()
	r := cfg.Validate()
	if r.HasErrors() {
		t.Fatalf("default config should be error-free, got:\n%s", r.FormatErrors())
	}
	// Empty prompts warn — the shipped defaults supply prompts, so
	// only warnings about extended cleanup or theme errors might appear.
}

func TestConfigDir(t *testing.T) {
	t.Setenv("AWP_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	dir, err := ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir() error: %v", err)
	}
	if dir == "" {
		t.Error("ConfigDir() returned empty string")
	}
	if filepath.Base(dir) != "awp" {
		t.Errorf("ConfigDir() = %q; want to end with 'awp'", dir)
	}
	if filepath.Base(filepath.Dir(dir)) != ".config" {
		t.Errorf("ConfigDir() = %q; want parent to be '.config'", dir)
	}
}

func TestConfigDir_EnvOverride(t *testing.T) {
	t.Setenv("AWP_CONFIG_DIR", "/custom/test/path")
	t.Setenv("XDG_CONFIG_HOME", "/should/be/ignored")

	dir, err := ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir() error: %v", err)
	}
	if dir != "/custom/test/path" {
		t.Errorf("ConfigDir() = %q; want %q", dir, "/custom/test/path")
	}
}

func TestConfigDir_XDGFallback(t *testing.T) {
	t.Setenv("AWP_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "/xdg/config")

	dir, err := ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir() error: %v", err)
	}
	expected := filepath.Join("/xdg/config", "awp")
	if dir != expected {
		t.Errorf("ConfigDir() = %q; want %q", dir, expected)
	}
}

func TestConfigPath(t *testing.T) {
	path, err := ConfigPath()
	if err != nil {
		t.Fatalf("ConfigPath() error: %v", err)
	}
	if filepath.Base(path) != "config.json" {
		t.Errorf("ConfigPath() = %q; want to end with 'config.json'", path)
	}
}

func TestLoad_NonExistentFile(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.json")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	defaults := DefaultConfig()
	if cfg.Defaults.BranchPrefix != defaults.Defaults.BranchPrefix {
		t.Errorf("Load() should return defaults when file not found")
	}
}

func TestLoad_EmptyPath(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\") error: %v", err)
	}
	if cfg == nil {
		t.Error("Load(\"\") should not return nil config")
	}
}

func TestLoad_ValidFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	customConfig := map[string]interface{}{
		"pi": map[string]interface{}{
			"init_prompt": "custom init prompt",
		},
		"defaults": map[string]interface{}{
			"branch_prefix":   "feature/",
			"slug_max_length": 30,
		},
		"ui": map[string]interface{}{
			"theme": "dark",
		},
	}

	data, err := json.Marshal(customConfig)
	if err != nil {
		t.Fatalf("failed to marshal test config: %v", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Pi.InitPrompt != "custom init prompt" {
		t.Errorf("Pi.InitPrompt = %q; want %q", cfg.Pi.InitPrompt, "custom init prompt")
	}
	if cfg.Defaults.BranchPrefix != "feature/" {
		t.Errorf("Defaults.BranchPrefix = %q; want %q", cfg.Defaults.BranchPrefix, "feature/")
	}
	if cfg.Defaults.SlugMaxLength != 30 {
		t.Errorf("Defaults.SlugMaxLength = %d; want %d", cfg.Defaults.SlugMaxLength, 30)
	}
	if cfg.UI.Theme != "dark" {
		t.Errorf("UI.Theme = %q; want %q", cfg.UI.Theme, "dark")
	}
}

func TestLoad_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	if err := os.WriteFile(configPath, []byte(""), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error on empty file: %v", err)
	}

	// Empty file should yield defaults
	if cfg.Defaults.BranchPrefix != "task/" {
		t.Errorf("empty file: BranchPrefix = %q; want default", cfg.Defaults.BranchPrefix)
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	if err := os.WriteFile(configPath, []byte("not valid json"), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Error("Load() should return error for invalid JSON")
	}
}

func TestSave(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	cfg := DefaultConfig()
	cfg.Pi.InitPrompt = "custom-prompt"

	if err := cfg.Save(configPath); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("Save() should create config file")
	}

	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if loaded.Pi.InitPrompt != "custom-prompt" {
		t.Errorf("loaded.Pi.InitPrompt = %q; want %q", loaded.Pi.InitPrompt, "custom-prompt")
	}
}

func TestSave_CreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "nested", "dir", "config.json")

	cfg := DefaultConfig()

	if err := cfg.Save(configPath); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("Save() should create nested directories")
	}
}

// TestSave_Atomic_NoTempFileLeftOver pins the atomic-write contract from
// Cluster B.1: Save() must use the tmp+rename pattern (matching tickets.go
// and store.go), and must NOT leave a `.tmp` file on disk after success.
//
// CRASH SCENARIO: without atomic rename, an interrupted Save() (e.g., awp
// crashes mid-write) leaves config.json in a partial state, losing the user's
// settings. With tmp+rename, the rename is atomic on POSIX — readers either
// see the old file or the new file, never a half-written one.
//
// CORRECT-7 self-check:
//   C-onformance: file existence is binary (stat returns no error or IsNotExist)
//   O-rdering: N/A (single Save call)
//   R-ange: N/A
//   R-eference: filesystem only
//   E-xistence: tmp file must NOT exist; dest file MUST exist
//   C-ardinality: 0 tmp files expected; 1 dest file expected
//   T-ime: no time concerns
func TestSave_Atomic_NoTempFileLeftOver(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")
	tmpPath := configPath + ".tmp"

	cfg := DefaultConfig()
	cfg.UI.Theme = "atom-test"

	if err := cfg.Save(configPath); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// The tmp file must NOT exist after a successful Save.
	if _, err := os.Stat(tmpPath); err == nil {
		t.Errorf("Save() left tmp file at %s; expected atomic tmp+rename pattern.\n"+
			"Cluster B.1: without tmp+rename, an interrupted Save() can leave the destination\n"+
			"file in a partial state, losing the user's settings.", tmpPath)
	} else if !os.IsNotExist(err) {
		t.Errorf("unexpected stat error for %s: %v", tmpPath, err)
	}

	// The destination file MUST exist and contain the saved config.
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("destination file missing after Save(): %v", err)
	}

	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if loaded.UI.Theme != "atom-test" {
		t.Errorf("loaded.UI.Theme = %q; want %q", loaded.UI.Theme, "atom-test")
	}
}

// TestSave_Atomic_CleansTmpOnFailure pins the contract that a failed Save()
// removes the tmp file. Without cleanup, repeated failed saves accumulate
// orphaned config.json.tmp files in the user's config dir.
//
// This test makes Save() fail by pre-creating the destination as a directory.
// The atomic-rename Save() will:
//   1. Successfully write to config.json.tmp
//   2. Fail at rename() because the dest is a directory
//   3. (Contract) clean up the tmp file
//
// CORRECT-7 self-check:
//   C-onformance: tmp file must NOT exist after failed Save
//   O-rdering: N/A
//   R-ange: N/A
//   R-eference: filesystem only
//   E-xistence: tmp file must NOT exist
//   C-ardinality: 0 tmp files expected
//   T-ime: no time concerns
func TestSave_Atomic_CleansTmpOnFailure(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")
	tmpPath := configPath + ".tmp"

	// Make the destination path fail-rename: replace it with a directory
	// of the same name. The atomic-rename Save() will write tmp then fail
	// at rename because the target is a non-empty dir.
	if err := os.Mkdir(configPath, 0755); err != nil {
		t.Fatalf("mkdir-as-config: %v", err)
	}

	cfg := DefaultConfig()
	saveErr := cfg.Save(configPath)
	if saveErr == nil {
		// Save succeeded? Then rename worked, meaning the dir was empty
		// or didn't block rename. This is OS-dependent. Skip the assertion.
		t.Skip("Save unexpectedly succeeded; this test requires rename-failure semantics")
	}

	// The tmp file must be cleaned up after failed Save.
	if _, err := os.Stat(tmpPath); err == nil {
		t.Errorf("Save() left tmp file at %s after rename failure; tmp must be cleaned up.\n"+
			"Cluster B.1: rename failure must trigger tmp cleanup to avoid orphan accumulation.", tmpPath)
	}
}

// TestConfigGo_UsesAtomicWrite pins the source-level contract: Save() must
// use the tmp+rename pattern (matching tickets.go and store.go). Structural
// test that catches the regression of reverting to direct os.WriteFile.
//
// CORRECT-7 self-check:
//   C-onformance: literal substrings must all be present
//   O-rdering: N/A
//   R-ange: N/A
//   R-eference: source-file scan only
//   E-xistence: substrings must exist
//   C-ardinality: 1 occurrence of each expected
//   T-ime: no time concerns
func TestConfigGo_UsesAtomicWrite(t *testing.T) {
	src := readConfigGoSource(t)
	checks := []struct {
		pattern string
		why     string
	}{
		{"tmpPath := path + \".tmp\"", "must write to a tmp file before rename"},
		{"os.WriteFile(tmpPath, data, 0644)", "tmp file must be written via WriteFile"},
		{"os.Rename(tmpPath, path)", "tmp file must be atomically renamed to dest"},
	}
	for _, c := range checks {
		if !strings.Contains(src, c.pattern) {
			line := findConfigLineNumber(src, c.pattern)
			t.Errorf("config.go:%d missing required pattern %q (%s).\n"+
				"Cluster B.1: Save() must use tmp+rename for atomic write.\n"+
				"Match the pattern in internal/project/tickets.go:Save() or internal/project/store.go:Save().",
				line, c.pattern, c.why)
		}
	}

	// Negative check: Save() must NOT use os.WriteFile directly on the dest path.
	// (Allow the tmpPath form, just not the bare path.)
	if strings.Contains(src, "return os.WriteFile(path, data, 0644)") {
		t.Errorf("config.go Save() uses direct os.WriteFile on dest path; "+
			"this is non-atomic and can leave config.json truncated on crash.\n"+
			"Cluster B.1: use the tmp+rename pattern instead.")
	}
}

// --- helpers (config-specific) ---

func readConfigGoSource(t *testing.T) string {
	t.Helper()
	path := filepath.Join(findConfigProjectRoot(t), "internal", "config", "config.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func findConfigLineNumber(src, substr string) int {
	idx := strings.Index(src, substr)
	if idx < 0 {
		return 0
	}
	return strings.Count(src[:idx], "\n") + 1
}

func findConfigProjectRoot(t *testing.T) string {
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

func TestInitPrompt(t *testing.T) {
	t.Run("returns Pi.InitPrompt when set", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Pi.InitPrompt = "custom prompt"

		prompt := cfg.InitPrompt()
		if prompt != "custom prompt" {
			t.Errorf("InitPrompt() = %q; want %q", prompt, "custom prompt")
		}
	})

	t.Run("falls back to built-in default when empty", func(t *testing.T) {
		cfg := &Config{}

		prompt := cfg.InitPrompt()
		if prompt == "" {
			t.Error("InitPrompt() should return non-empty default")
		}
	})
}

func TestConfigStructure_Roundtrip(t *testing.T) {
	cfg := DefaultConfig()

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}

	var unmarshaled Config
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}

	if unmarshaled.Pi.InitPrompt != cfg.Pi.InitPrompt {
		t.Errorf("round-trip failed for Pi.InitPrompt")
	}
	if unmarshaled.UI.Theme != cfg.UI.Theme {
		t.Errorf("round-trip failed for UI.Theme")
	}
	if unmarshaled.Defaults.BranchPrefix != cfg.Defaults.BranchPrefix {
		t.Errorf("round-trip failed for Defaults.BranchPrefix")
	}
}

func TestPiPromptIsValidTemplate(t *testing.T) {
	cfg := DefaultConfig()
	if err := validateTemplate(cfg.Pi.InitPrompt); err != nil {
		t.Errorf("Pi.InitPrompt is not a valid Go template: %v", err)
	}
}

func TestLoadWithValidation_ValidFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	cfg := DefaultConfig()
	if err := cfg.Save(configPath); err != nil {
		t.Fatalf("failed to save test config: %v", err)
	}

	loaded, result, err := LoadWithValidation(configPath)
	if err != nil {
		t.Fatalf("LoadWithValidation() error: %v", err)
	}
	if loaded == nil {
		t.Error("LoadWithValidation() should return config")
	}
	if result == nil {
		t.Error("LoadWithValidation() should return validation result")
	}
	if result.HasErrors() {
		t.Errorf("valid config should not have errors:\n%s", result.FormatErrors())
	}
}

func TestLoadWithValidation_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	if err := os.WriteFile(configPath, []byte("not valid json"), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	_, result, err := LoadWithValidation(configPath)
	if err == nil {
		t.Error("LoadWithValidation() should return error for invalid JSON")
	}
	if result == nil {
		t.Error("LoadWithValidation() should return validation result for JSON errors")
	}
	if !result.HasErrors() {
		t.Error("validation result should have errors for invalid JSON")
	}
}

func TestLoadWithValidation_NonExistentFile(t *testing.T) {
	cfg, result, err := LoadWithValidation("/nonexistent/path/config.json")
	if err != nil {
		t.Fatalf("LoadWithValidation() error: %v", err)
	}
	if cfg == nil {
		t.Error("LoadWithValidation() should return default config when file not found")
	}
	if result == nil {
		t.Error("LoadWithValidation() should return validation result")
	}
}

func TestLoadWithValidation_InvalidBranchNaming(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	invalidConfig := map[string]interface{}{
		"defaults": map[string]interface{}{
			"branch_naming": "invalid-value",
		},
	}

	data, _ := json.Marshal(invalidConfig)
	os.WriteFile(configPath, data, 0644)

	cfg, result, err := LoadWithValidation(configPath)
	if err != nil {
		t.Fatalf("LoadWithValidation() unexpected error: %v", err)
	}
	if cfg == nil {
		t.Error("LoadWithValidation() should return config even with validation errors")
	}
	if !result.HasErrors() {
		t.Error("validation result should have errors for invalid branch_naming")
	}
}

func TestLoadWithValidation_InvalidPiPrompt(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	invalidConfig := map[string]interface{}{
		"pi": map[string]interface{}{
			"init_prompt": "{{.Broken",
		},
	}

	data, _ := json.Marshal(invalidConfig)
	os.WriteFile(configPath, data, 0644)

	_, result, err := LoadWithValidation(configPath)
	if err != nil {
		t.Fatalf("LoadWithValidation() unexpected error: %v", err)
	}
	if !result.HasErrors() {
		t.Error("validation result should have errors for invalid pi.init_prompt")
	}
}

func TestLoadWithValidation_InvalidTheme(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	invalidConfig := map[string]interface{}{
		"ui": map[string]interface{}{
			"theme": "nonexistent-theme",
		},
	}

	data, _ := json.Marshal(invalidConfig)
	os.WriteFile(configPath, data, 0644)

	_, result, err := LoadWithValidation(configPath)
	if err != nil {
		t.Fatalf("LoadWithValidation() unexpected error: %v", err)
	}
	// Unknown theme is a warning, not an error
	if result.HasErrors() {
		t.Errorf("unknown theme should be a warning, not error:\n%s", result.FormatErrors())
	}
	if !result.HasWarnings() {
		t.Error("unknown theme should produce a warning")
	}
}