package config

import (
	"ssw-logs-capture/pkg/types"
	"strings"
	"testing"
)

// TestValidConfigPasses tests that a valid configuration passes validation (C12)
func TestValidConfigPasses(t *testing.T) {
	config := &types.Config{
		App: types.AppConfig{
			Name:      "test-app",
			Version:   "1.0.0",
			LogLevel:  "info",
			LogFormat: "json",
		},
		Server: types.ServerConfig{
			Enabled: true,
			Host:    "0.0.0.0",
			Port:    8080,
		},
		Metrics: types.MetricsConfig{
			Enabled: true,
			Port:    9090,
			Path:    "/metrics",
		},
		Dispatcher: types.DispatcherConfig{
			QueueSize:    1000,
			WorkerCount:  4,
			BatchSize:    100,
			BatchTimeout: "5s",
		},
		Sinks: types.SinksConfig{
			Loki: types.LokiConfig{
				Enabled:      true,
				URL:          "http://loki:3100",
				BatchSize:    100,
				BatchTimeout: "10s",
				Timeout:      "30s",
			},
		},
		Processing: types.ProcessingConfig{
			Enabled:       true,
			PipelinesFile: "pipelines.yaml",
		},
		Positions: types.PositionsConfig{
			Enabled:         true,
			Directory:       "/tmp/positions",
			MaxMemoryBuffer: 1000,
		},
	}

	err := ValidateConfig(config)
	if err != nil {
		t.Errorf("Valid config should pass validation, got error: %v", err)
	}
}

// TestInvalidServerPort tests port validation (C12)
func TestInvalidServerPort(t *testing.T) {
	testCases := []struct {
		name string
		port int
	}{
		{"zero port", 0},
		{"negative port", -1},
		{"port too large", 65536},
		{"port too large 2", 100000},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := &types.Config{
				App: types.AppConfig{Name: "test", Version: "1.0", LogLevel: "info", LogFormat: "json"},
				Server: types.ServerConfig{
					Enabled: true,
					Host:    "0.0.0.0",
					Port:    tc.port,
				},
				Sinks: types.SinksConfig{
					Loki: types.LokiConfig{Enabled: true, URL: "http://loki:3100", BatchSize: 100},
				},
				Dispatcher:  types.DispatcherConfig{QueueSize: 1000, WorkerCount: 4, BatchSize: 100},
				Positions:   types.PositionsConfig{Enabled: true, Directory: "/tmp", MaxMemoryBuffer: 1000},
			}

			err := ValidateConfig(config)
			if err == nil {
				t.Errorf("Invalid server port %d should fail validation", tc.port)
			}
			if !strings.Contains(err.Error(), "invalid server port") {
				t.Errorf("Expected 'invalid server port' error, got: %v", err)
			}
		})
	}
}

// TestInvalidLokiURL tests URL validation (C12)
func TestInvalidLokiURL(t *testing.T) {
	config := &types.Config{
		App: types.AppConfig{Name: "test", Version: "1.0", LogLevel: "info", LogFormat: "json"},
		Server: types.ServerConfig{Enabled: true, Host: "0.0.0.0", Port: 8080},
		Sinks: types.SinksConfig{
			Loki: types.LokiConfig{
				Enabled:   true,
				URL:       "",  // Empty URL
				BatchSize: 100,
			},
		},
		Dispatcher: types.DispatcherConfig{QueueSize: 1000, WorkerCount: 4, BatchSize: 100},
		Positions:  types.PositionsConfig{Enabled: true, Directory: "/tmp", MaxMemoryBuffer: 1000},
	}

	err := ValidateConfig(config)
	if err == nil {
		t.Error("Empty Loki URL should fail validation")
	}
	if !strings.Contains(err.Error(), "URL cannot be empty") {
		t.Errorf("Expected 'URL cannot be empty' error, got: %v", err)
	}
}

// TestNoSinksEnabled tests that at least one sink is required (C12)
func TestNoSinksEnabled(t *testing.T) {
	config := &types.Config{
		App: types.AppConfig{Name: "test", Version: "1.0", LogLevel: "info", LogFormat: "json"},
		Server: types.ServerConfig{Enabled: true, Host: "0.0.0.0", Port: 8080},
		Sinks: types.SinksConfig{
			Loki:      types.LokiConfig{Enabled: false},
			LocalFile: types.LocalFileSinkConfig{Enabled: false},
		},
		Dispatcher: types.DispatcherConfig{QueueSize: 1000, WorkerCount: 4, BatchSize: 100},
		Positions:  types.PositionsConfig{Enabled: true, Directory: "/tmp", MaxMemoryBuffer: 1000},
	}

	err := ValidateConfig(config)
	if err == nil {
		t.Error("Config with no sinks enabled should fail validation")
	}
	if !strings.Contains(err.Error(), "at least one sink must be enabled") {
		t.Errorf("Expected 'at least one sink must be enabled' error, got: %v", err)
	}
}

// TestPortConflict tests port conflict detection (C12)
func TestPortConflict(t *testing.T) {
	config := &types.Config{
		App: types.AppConfig{Name: "test", Version: "1.0", LogLevel: "info", LogFormat: "json"},
		Server: types.ServerConfig{
			Enabled: true,
			Host:    "0.0.0.0",
			Port:    8080,
		},
		Metrics: types.MetricsConfig{
			Enabled: true,
			Port:    8080,  // Same as server port!
			Path:    "/metrics",
		},
		Sinks: types.SinksConfig{
			Loki: types.LokiConfig{Enabled: true, URL: "http://loki:3100", BatchSize: 100},
		},
		Dispatcher: types.DispatcherConfig{QueueSize: 1000, WorkerCount: 4, BatchSize: 100},
		Positions:  types.PositionsConfig{Enabled: true, Directory: "/tmp", MaxMemoryBuffer: 1000},
	}

	err := ValidateConfig(config)
	if err == nil {
		t.Error("Port conflict should fail validation")
	}
	if !strings.Contains(err.Error(), "port conflict") {
		t.Errorf("Expected 'port conflict' error, got: %v", err)
	}
}

// TestInvalidLogLevel tests log level validation (C12)
func TestInvalidLogLevel(t *testing.T) {
	config := &types.Config{
		App: types.AppConfig{
			Name:      "test",
			Version:   "1.0",
			LogLevel:  "invalid-level",
			LogFormat: "json",
		},
		Server: types.ServerConfig{Enabled: true, Host: "0.0.0.0", Port: 8080},
		Sinks: types.SinksConfig{
			Loki: types.LokiConfig{Enabled: true, URL: "http://loki:3100", BatchSize: 100},
		},
		Dispatcher: types.DispatcherConfig{QueueSize: 1000, WorkerCount: 4, BatchSize: 100},
		Positions:  types.PositionsConfig{Enabled: true, Directory: "/tmp", MaxMemoryBuffer: 1000},
	}

	err := ValidateConfig(config)
	if err == nil {
		t.Error("Invalid log level should fail validation")
	}
	if !strings.Contains(err.Error(), "invalid log level") {
		t.Errorf("Expected 'invalid log level' error, got: %v", err)
	}
}

// TestDispatcherLimits tests dispatcher validation (C12)
func TestDispatcherLimits(t *testing.T) {
	testCases := []struct {
		name        string
		queueSize   int
		workerCount int
		batchSize   int
		expectError bool
		errorMsg    string
	}{
		{"zero queue", 0, 4, 100, true, "queue size must be positive"},
		{"zero workers", 1000, 0, 100, true, "worker count must be positive"},
		{"zero batch", 1000, 4, 0, true, "batch size must be positive"},
		{"queue too large", 2000000, 4, 100, true, "queue size too large"},
		{"workers too many", 1000, 200, 100, true, "worker count too large"},
		{"valid config", 1000, 4, 100, false, ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := &types.Config{
				App:    types.AppConfig{Name: "test", Version: "1.0", LogLevel: "info", LogFormat: "json"},
				Server: types.ServerConfig{Enabled: true, Host: "0.0.0.0", Port: 8080},
				Dispatcher: types.DispatcherConfig{
					QueueSize:   tc.queueSize,
					WorkerCount: tc.workerCount,
					BatchSize:   tc.batchSize,
				},
				Sinks: types.SinksConfig{
					Loki: types.LokiConfig{Enabled: true, URL: "http://loki:3100", BatchSize: 100},
				},
				Positions: types.PositionsConfig{Enabled: true, Directory: "/tmp", MaxMemoryBuffer: 1000},
			}

			err := ValidateConfig(config)
			if tc.expectError {
				if err == nil {
					t.Errorf("%s: expected error containing '%s', got nil", tc.name, tc.errorMsg)
				} else if !strings.Contains(err.Error(), tc.errorMsg) {
					t.Errorf("%s: expected error containing '%s', got: %v", tc.name, tc.errorMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("%s: expected no error, got: %v", tc.name, err)
				}
			}
		})
	}
}

// TestInvalidDuration tests duration parsing validation (C12)
func TestInvalidDuration(t *testing.T) {
	config := &types.Config{
		App:    types.AppConfig{Name: "test", Version: "1.0", LogLevel: "info", LogFormat: "json"},
		Server: types.ServerConfig{
			Enabled:      true,
			Host:         "0.0.0.0",
			Port:         8080,
			ReadTimeout:  "invalid-duration",  // Invalid!
			WriteTimeout: "10s",
		},
		Sinks: types.SinksConfig{
			Loki: types.LokiConfig{Enabled: true, URL: "http://loki:3100", BatchSize: 100},
		},
		Dispatcher: types.DispatcherConfig{QueueSize: 1000, WorkerCount: 4, BatchSize: 100},
		Positions:  types.PositionsConfig{Enabled: true, Directory: "/tmp", MaxMemoryBuffer: 1000},
	}

	err := ValidateConfig(config)
	if err == nil {
		t.Error("Invalid duration should fail validation")
	}
	if !strings.Contains(err.Error(), "invalid read timeout") {
		t.Errorf("Expected 'invalid read timeout' error, got: %v", err)
	}
}

// TestRelativePathRejected tests that relative paths are rejected (C12)
func TestRelativePathRejected(t *testing.T) {
	config := &types.Config{
		App:    types.AppConfig{Name: "test", Version: "1.0", LogLevel: "info", LogFormat: "json"},
		Server: types.ServerConfig{Enabled: true, Host: "0.0.0.0", Port: 8080},
		Sinks: types.SinksConfig{
			LocalFile: types.LocalFileSinkConfig{
				Enabled:   true,
				Directory: "relative/path",  // Relative path!
			},
		},
		Dispatcher: types.DispatcherConfig{QueueSize: 1000, WorkerCount: 4, BatchSize: 100},
		Positions:  types.PositionsConfig{Enabled: true, Directory: "/tmp", MaxMemoryBuffer: 1000},
	}

	err := ValidateConfig(config)
	if err == nil {
		t.Error("Relative path should fail validation")
	}
	if !strings.Contains(err.Error(), "must be absolute path") {
		t.Errorf("Expected 'must be absolute path' error, got: %v", err)
	}
}
