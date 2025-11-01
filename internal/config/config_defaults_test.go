package config

import (
	"os"
	"testing"

	"ssw-logs-capture/pkg/types"
)

// TestDefaultConfigsEnabled tests that defaults are applied when enabled
func TestDefaultConfigsEnabled(t *testing.T) {
	// Create minimal config with defaults enabled
	config := &types.Config{}
	trueVal := true
	config.App.DefaultConfigs = &trueVal

	applyDefaults(config)

	// Verify defaults were applied
	if config.App.Name != "ssw-logs-capture" {
		t.Errorf("Expected default app name, got %s", config.App.Name)
	}
	if config.Server.Port != 8401 {
		t.Errorf("Expected default server port 8401, got %d", config.Server.Port)
	}
	if config.Dispatcher.QueueSize != 10000 {
		t.Errorf("Expected default queue size 10000, got %d", config.Dispatcher.QueueSize)
	}
}

// TestDefaultConfigsDisabled tests that defaults are NOT applied when disabled
func TestDefaultConfigsDisabled(t *testing.T) {
	// Create minimal config with defaults disabled
	config := &types.Config{}
	falseVal := false
	config.App.DefaultConfigs = &falseVal

	applyDefaults(config)

	// Verify defaults were NOT applied (values remain zero/empty)
	if config.App.Name != "" {
		t.Errorf("Expected empty app name with defaults disabled, got %s", config.App.Name)
	}
	if config.Server.Port != 0 {
		t.Errorf("Expected zero server port with defaults disabled, got %d", config.Server.Port)
	}
	if config.Dispatcher.QueueSize != 0 {
		t.Errorf("Expected zero queue size with defaults disabled, got %d", config.Dispatcher.QueueSize)
	}
}

// TestDefaultConfigsNil tests that defaults are applied when nil (default behavior)
func TestDefaultConfigsNil(t *testing.T) {
	// Create config with DefaultConfigs = nil (not specified)
	config := &types.Config{}
	// config.App.DefaultConfigs is nil

	applyDefaults(config)

	// Verify defaults WERE applied (nil means use default behavior = true)
	if config.App.Name != "ssw-logs-capture" {
		t.Errorf("Expected default app name with nil defaults, got %s", config.App.Name)
	}
	if config.Server.Port != 8401 {
		t.Errorf("Expected default server port with nil defaults, got %d", config.Server.Port)
	}
}

// TestDefaultConfigsEnvironmentOverride tests environment variable override
func TestDefaultConfigsEnvironmentOverride(t *testing.T) {
	// Set environment variable
	os.Setenv("SSW_DEFAULT_CONFIGS", "false")
	defer os.Unsetenv("SSW_DEFAULT_CONFIGS")

	// Create config with defaults enabled in struct
	config := &types.Config{}
	trueVal := true
	config.App.DefaultConfigs = &trueVal

	// shouldApplyDefaults should return false (env overrides)
	if shouldApplyDefaults(config) {
		t.Error("Expected shouldApplyDefaults to return false (env override)")
	}

	applyDefaults(config)

	// Verify defaults were NOT applied (env variable overrode the struct value)
	if config.App.Name != "" {
		t.Errorf("Expected empty app name with env override, got %s", config.App.Name)
	}
}

// TestFilesConfigExplicitlyEmpty tests that empty files_config is respected
func TestFilesConfigExplicitlyEmpty(t *testing.T) {
	config := &types.Config{}
	trueVal := true
	config.App.DefaultConfigs = &trueVal

	// Simulate markConfigAsLoaded marking files_config as explicitly empty
	config.FilesConfig.WatchDirectories = []string{} // Empty but not nil
	config.FilesConfig.IncludePatterns = []string{}  // Empty but not nil

	applyDefaults(config)

	// Verify defaults were NOT applied (empty slices respected)
	if len(config.FilesConfig.WatchDirectories) != 0 {
		t.Errorf("Expected empty WatchDirectories, got %v", config.FilesConfig.WatchDirectories)
	}
	if len(config.FilesConfig.IncludePatterns) != 0 {
		t.Errorf("Expected empty IncludePatterns, got %v", config.FilesConfig.IncludePatterns)
	}
}

// TestFilesConfigNotMentioned tests that nil files_config gets defaults
func TestFilesConfigNotMentioned(t *testing.T) {
	config := &types.Config{}
	trueVal := true
	config.App.DefaultConfigs = &trueVal

	// WatchDirectories and IncludePatterns are nil (not mentioned)
	// config.FilesConfig.WatchDirectories = nil (default)
	// config.FilesConfig.IncludePatterns = nil (default)

	applyDefaults(config)

	// Verify defaults WERE applied (nil means not mentioned)
	if config.FilesConfig.WatchDirectories == nil {
		t.Error("Expected WatchDirectories to be set with defaults")
	}
	if len(config.FilesConfig.WatchDirectories) == 0 {
		t.Error("Expected WatchDirectories to have default values")
	}
	if config.FilesConfig.IncludePatterns == nil {
		t.Error("Expected IncludePatterns to be set with defaults")
	}
	if len(config.FilesConfig.IncludePatterns) == 0 {
		t.Error("Expected IncludePatterns to have default values")
	}
}

// TestMarkConfigAsLoaded tests that markConfigAsLoaded correctly detects files_config
func TestMarkConfigAsLoaded(t *testing.T) {
	config := &types.Config{}

	yamlWithFilesConfig := []byte(`
app:
  name: "test"
files_config:
  watch_directories:
    # commented
  include_patterns:
    # commented
`)

	markConfigAsLoaded(config, yamlWithFilesConfig)

	// Verify that files_config was marked as defined (slices initialized as empty)
	if config.FilesConfig.WatchDirectories == nil {
		t.Error("Expected WatchDirectories to be initialized as empty slice")
	}
	if config.FilesConfig.IncludePatterns == nil {
		t.Error("Expected IncludePatterns to be initialized as empty slice")
	}
	if len(config.FilesConfig.WatchDirectories) != 0 {
		t.Errorf("Expected empty WatchDirectories, got %v", config.FilesConfig.WatchDirectories)
	}
	if len(config.FilesConfig.IncludePatterns) != 0 {
		t.Errorf("Expected empty IncludePatterns, got %v", config.FilesConfig.IncludePatterns)
	}
}

// TestMarkConfigAsLoaded_NoFilesConfig tests that slices remain nil when files_config not mentioned
func TestMarkConfigAsLoaded_NoFilesConfig(t *testing.T) {
	config := &types.Config{}

	yamlWithoutFilesConfig := []byte(`
app:
  name: "test"
server:
  port: 8401
`)

	markConfigAsLoaded(config, yamlWithoutFilesConfig)

	// Verify that files_config slices remain nil (not mentioned)
	if config.FilesConfig.WatchDirectories != nil {
		t.Error("Expected WatchDirectories to remain nil when not mentioned")
	}
	if config.FilesConfig.IncludePatterns != nil {
		t.Error("Expected IncludePatterns to remain nil when not mentioned")
	}
}
