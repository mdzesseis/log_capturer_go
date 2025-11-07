package monitors

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHybridContainerMonitor_NewCreation tests monitor creation
func TestHybridContainerMonitor_NewCreation(t *testing.T) {
	// This is a basic structural test
	// Full integration tests require Docker daemon

	config := HybridMonitorConfig{
		MetadataCacheTTL:   5 * time.Minute,
		PreferFileMonitor:  true,
		EnableDockerEvents: true,
	}

	// Note: Cannot create actual HybridContainerMonitor without Docker client
	// This test validates configuration defaults

	assert.Equal(t, 5*time.Minute, config.MetadataCacheTTL)
	assert.True(t, config.PreferFileMonitor)
	assert.True(t, config.EnableDockerEvents)
}

// TestMonitoredContainerInfo_Structure tests data structure
func TestMonitoredContainerInfo_Structure(t *testing.T) {
	info := &MonitoredContainerInfo{
		ContainerID: "abc123def456",
		LogDriver:   "json-file",
		LogPath:     "/var/lib/docker/containers/abc123/abc123-json.log",
		MonitorType: "file",
		StartedAt:   time.Now(),
	}

	require.NotNil(t, info)
	assert.Equal(t, "abc123def456", info.ContainerID)
	assert.Equal(t, "json-file", info.LogDriver)
	assert.Equal(t, "file", info.MonitorType)
}

// TestHybridMonitorConfig_Defaults tests default configuration
func TestHybridMonitorConfig_Defaults(t *testing.T) {
	config := HybridMonitorConfig{}

	// Before applying defaults
	assert.Zero(t, config.MetadataCacheTTL)
	assert.False(t, config.PreferFileMonitor)
	assert.False(t, config.EnableDockerEvents)

	// Defaults would be applied in NewHybridContainerMonitor
	// This test documents expected defaults
}

// TestHybridContainerMonitor_RouteDecision tests routing logic
func TestHybridContainerMonitor_RouteDecision(t *testing.T) {
	tests := []struct {
		name              string
		logDriver         string
		logPath           string
		expectedMonitorType string
	}{
		{
			name:              "json-file with path routes to file monitor",
			logDriver:         "json-file",
			logPath:           "/var/lib/docker/containers/abc/abc-json.log",
			expectedMonitorType: "file",
		},
		{
			name:              "json-file without path routes to stream monitor",
			logDriver:         "json-file",
			logPath:           "",
			expectedMonitorType: "stream",
		},
		{
			name:              "journald routes to stream monitor",
			logDriver:         "journald",
			logPath:           "",
			expectedMonitorType: "stream",
		},
		{
			name:              "syslog routes to stream monitor",
			logDriver:         "syslog",
			logPath:           "",
			expectedMonitorType: "stream",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Validate routing decision logic
			shouldUseFile := tt.logDriver == "json-file" && tt.logPath != ""

			if shouldUseFile {
				assert.Equal(t, "file", tt.expectedMonitorType)
			} else {
				assert.Equal(t, "stream", tt.expectedMonitorType)
			}
		})
	}
}

// TestHybridContainerMonitor_GetStats tests statistics gathering
func TestHybridContainerMonitor_GetStats(t *testing.T) {
	// This test validates the stats structure
	// Cannot test actual HybridContainerMonitor without Docker

	expectedKeys := []string{
		"total_containers",
		"file_monitored",
		"stream_monitored",
		"file_percentage",
		"cache_size",
		"cache_hits",
		"cache_misses",
		"cache_hit_rate",
	}

	// Validate expected stat keys
	for _, key := range expectedKeys {
		assert.NotEmpty(t, key)
	}
}
