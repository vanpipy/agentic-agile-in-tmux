package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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