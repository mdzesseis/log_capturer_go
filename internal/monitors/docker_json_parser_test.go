package monitors

import (
	"strings"
	"testing"
	"time"

	"ssw-logs-capture/pkg/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseDockerJSONLogLine tests basic JSON parsing
func TestParseDockerJSONLogLine(t *testing.T) {
	containerID := "abc123def456789"
	metadata := &ContainerMetadata{
		ID:       containerID,
		Name:     "test-container",
		Image:    "nginx:latest",
		Labels:   map[string]string{"env": "prod"},
		State:    "running",
		Hostname: "web-server",
	}

	t.Run("valid stdout log", func(t *testing.T) {
		line := `{"log":"Hello World\n","stream":"stdout","time":"2023-01-01T12:00:00.123456789Z"}`

		entry, err := ParseDockerJSONLogLine(line, containerID, metadata)
		require.NoError(t, err)
		require.NotNil(t, entry)

		assert.Equal(t, "Hello World", entry.Message)
		assert.Equal(t, "stdout", entry.Labels["stream"])
		assert.Equal(t, "docker", entry.SourceType)
		assert.Equal(t, containerID[:12], entry.SourceID)
		assert.Equal(t, "test-container", entry.Labels["container_name"])
		assert.Equal(t, "nginx:latest", entry.Labels["image"])

		// Verify timestamp is UTC
		assert.Equal(t, time.UTC, entry.Timestamp.Location())
	})

	t.Run("valid stderr log", func(t *testing.T) {
		line := `{"log":"Error occurred\n","stream":"stderr","time":"2023-01-01T12:00:00.123456789Z"}`

		entry, err := ParseDockerJSONLogLine(line, containerID, metadata)
		require.NoError(t, err)
		require.NotNil(t, entry)

		assert.Equal(t, "Error occurred", entry.Message)
		assert.Equal(t, "stderr", entry.Labels["stream"])
	})

	t.Run("empty line", func(t *testing.T) {
		entry, err := ParseDockerJSONLogLine("", containerID, metadata)
		require.NoError(t, err)
		assert.Nil(t, entry, "empty line should return nil entry")
	})

	t.Run("whitespace only", func(t *testing.T) {
		entry, err := ParseDockerJSONLogLine("   \n\t  ", containerID, metadata)
		require.NoError(t, err)
		assert.Nil(t, entry, "whitespace-only line should return nil entry")
	})

	t.Run("invalid JSON", func(t *testing.T) {
		line := `{invalid json}`

		entry, err := ParseDockerJSONLogLine(line, containerID, metadata)
		assert.Error(t, err)
		assert.Nil(t, entry)
		assert.Contains(t, err.Error(), "failed to parse docker JSON log")
	})

	t.Run("missing required fields", func(t *testing.T) {
		line := `{"log":"test"}`

		entry, err := ParseDockerJSONLogLine(line, containerID, metadata)
		require.NoError(t, err) // Parser is lenient, falls back to current time
		require.NotNil(t, entry)
	})

	t.Run("invalid timestamp format", func(t *testing.T) {
		line := `{"log":"test\n","stream":"stdout","time":"invalid-timestamp"}`

		entry, err := ParseDockerJSONLogLine(line, containerID, metadata)
		require.NoError(t, err) // Fallback to current time
		require.NotNil(t, entry)
		assert.Equal(t, "test", entry.Message)
		// Timestamp should be set to current time (fallback)
		assert.True(t, entry.Timestamp.After(time.Now().Add(-1*time.Second)))
	})

	t.Run("multiline log message", func(t *testing.T) {
		line := `{"log":"Line 1\nLine 2\nLine 3\n","stream":"stdout","time":"2023-01-01T12:00:00.123456789Z"}`

		entry, err := ParseDockerJSONLogLine(line, containerID, metadata)
		require.NoError(t, err)
		require.NotNil(t, entry)

		// Last newline is stripped
		assert.Equal(t, "Line 1\nLine 2\nLine 3", entry.Message)
	})

	t.Run("special characters", func(t *testing.T) {
		line := `{"log":"Special chars: é, ñ, 中文, 🚀\n","stream":"stdout","time":"2023-01-01T12:00:00.123456789Z"}`

		entry, err := ParseDockerJSONLogLine(line, containerID, metadata)
		require.NoError(t, err)
		require.NotNil(t, entry)

		assert.Equal(t, "Special chars: é, ñ, 中文, 🚀", entry.Message)
	})

	t.Run("long log line", func(t *testing.T) {
		longMessage := strings.Repeat("A", 10000)
		line := `{"log":"` + longMessage + `\n","stream":"stdout","time":"2023-01-01T12:00:00.123456789Z"}`

		entry, err := ParseDockerJSONLogLine(line, containerID, metadata)
		require.NoError(t, err)
		require.NotNil(t, entry)

		assert.Equal(t, longMessage, entry.Message)
	})
}

// TestEnrichLogEntryWithMetadata tests metadata enrichment
func TestEnrichLogEntryWithMetadata(t *testing.T) {
	containerID := "abc123def456789"

	t.Run("full metadata enrichment", func(t *testing.T) {
		metadata := &ContainerMetadata{
			ID:       containerID,
			Name:     "web-app",
			Image:    "nginx:alpine",
			State:    "running",
			Hostname: "web-01",
			Command:  "nginx",
			Platform: "linux",
			Labels: map[string]string{
				"env":     "prod",
				"service": "web",
				"version": "1.0.0",
			},
			Networks:    []string{"bridge", "custom"},
			IPAddresses: map[string]string{"bridge": "172.17.0.2", "custom": "10.0.1.5"},
		}

		entry := types.AcquireLogEntry()
		defer entry.Release()

		enrichLogEntryWithMetadata(entry, containerID, metadata, "stdout")

		// Core fields
		assert.Equal(t, containerID[:12], entry.Labels["container_id"])
		assert.Equal(t, "web-app", entry.Labels["container_name"])
		assert.Equal(t, "nginx:alpine", entry.Labels["image"])
		assert.Equal(t, "stdout", entry.Labels["stream"])

		// State
		assert.Equal(t, "running", entry.Labels["state"])

		// Runtime info
		assert.Equal(t, "web-01", entry.Labels["hostname"])
		assert.Equal(t, "nginx", entry.Labels["command"])
		assert.Equal(t, "linux", entry.Labels["platform"])

		// Container labels (prefixed)
		assert.Equal(t, "prod", entry.Labels["container_label_env"])
		assert.Equal(t, "web", entry.Labels["container_label_service"])
		assert.Equal(t, "1.0.0", entry.Labels["container_label_version"])

		// Network info
		assert.Equal(t, "bridge", entry.Labels["network"])
		assert.Equal(t, "172.17.0.2", entry.Labels["network_bridge"])
		assert.Equal(t, "10.0.1.5", entry.Labels["network_custom"])

		// Standard labels
		assert.Equal(t, "docker", entry.Labels["source"])
		assert.Equal(t, "ssw-log-capturer", entry.Labels["service"])
	})

	t.Run("minimal metadata", func(t *testing.T) {
		metadata := &ContainerMetadata{
			ID:    containerID,
			Name:  "minimal",
			Image: "alpine",
		}

		entry := types.AcquireLogEntry()
		defer entry.Release()

		enrichLogEntryWithMetadata(entry, containerID, metadata, "stderr")

		assert.Equal(t, "minimal", entry.Labels["container_name"])
		assert.Equal(t, "alpine", entry.Labels["image"])
		assert.Equal(t, "stderr", entry.Labels["stream"])
		assert.NotContains(t, entry.Labels, "hostname")
		assert.NotContains(t, entry.Labels, "command")
	})

	t.Run("nil labels map", func(t *testing.T) {
		metadata := &ContainerMetadata{
			ID:     containerID,
			Name:   "test",
			Image:  "test:latest",
			Labels: nil,
		}

		entry := types.AcquireLogEntry()
		defer entry.Release()

		enrichLogEntryWithMetadata(entry, containerID, metadata, "stdout")

		assert.NotNil(t, entry.Labels)
		assert.Equal(t, "test", entry.Labels["container_name"])
	})
}

// TestShouldIncludeLabel tests label filtering
func TestShouldIncludeLabel(t *testing.T) {
	tests := []struct {
		name      string
		labelKey  string
		shouldInclude bool
	}{
		{
			name:          "custom label",
			labelKey:      "env",
			shouldInclude: true,
		},
		{
			name:          "service label",
			labelKey:      "service",
			shouldInclude: true,
		},
		{
			name:          "docker compose project",
			labelKey:      "com.docker.compose.project",
			shouldInclude: false,
		},
		{
			name:          "docker compose config-hash",
			labelKey:      "com.docker.compose.config-hash",
			shouldInclude: false,
		},
		{
			name:          "opencontainers image",
			labelKey:      "org.opencontainers.image.source",
			shouldInclude: false,
		},
		{
			name:          "desktop docker wsl",
			labelKey:      "desktop.docker.io/wsl-distro",
			shouldInclude: false,
		},
		{
			name:          "docker desktop plugin",
			labelKey:      "com.docker.desktop.plugin",
			shouldInclude: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shouldIncludeLabel(tt.labelKey)
			assert.Equal(t, tt.shouldInclude, result)
		})
	}
}

// TestParseDockerJSONLogBatch tests batch parsing
func TestParseDockerJSONLogBatch(t *testing.T) {
	containerID := "abc123def456789"
	metadata := &ContainerMetadata{
		ID:    containerID,
		Name:  "test",
		Image: "test:latest",
	}

	t.Run("all valid lines", func(t *testing.T) {
		lines := []string{
			`{"log":"Line 1\n","stream":"stdout","time":"2023-01-01T12:00:00.000Z"}`,
			`{"log":"Line 2\n","stream":"stdout","time":"2023-01-01T12:00:01.000Z"}`,
			`{"log":"Line 3\n","stream":"stderr","time":"2023-01-01T12:00:02.000Z"}`,
		}

		entries, errors := ParseDockerJSONLogBatch(lines, containerID, metadata)

		assert.Equal(t, 3, len(entries))
		assert.Equal(t, 3, len(errors))
		assert.NoError(t, errors[0])
		assert.NoError(t, errors[1])
		assert.NoError(t, errors[2])

		assert.Equal(t, "Line 1", entries[0].Message)
		assert.Equal(t, "Line 2", entries[1].Message)
		assert.Equal(t, "Line 3", entries[2].Message)
	})

	t.Run("mixed valid and invalid", func(t *testing.T) {
		lines := []string{
			`{"log":"Valid 1\n","stream":"stdout","time":"2023-01-01T12:00:00.000Z"}`,
			`{invalid json}`,
			`{"log":"Valid 2\n","stream":"stdout","time":"2023-01-01T12:00:01.000Z"}`,
			``,
			`{"log":"Valid 3\n","stream":"stdout","time":"2023-01-01T12:00:02.000Z"}`,
		}

		entries, errors := ParseDockerJSONLogBatch(lines, containerID, metadata)

		assert.Equal(t, 3, len(entries), "should have 3 valid entries")
		assert.Equal(t, 5, len(errors), "should have 5 error entries")

		assert.NoError(t, errors[0])
		assert.Error(t, errors[1])
		assert.NoError(t, errors[2])
		assert.NoError(t, errors[3]) // Empty line, no error but no entry
		assert.NoError(t, errors[4])
	})

	t.Run("empty batch", func(t *testing.T) {
		entries, errors := ParseDockerJSONLogBatch([]string{}, containerID, metadata)

		assert.Equal(t, 0, len(entries))
		assert.Equal(t, 0, len(errors))
	})
}

// TestValidateDockerJSONFormat tests format validation
func TestValidateDockerJSONFormat(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		isValid bool
	}{
		{
			name:    "valid stdout",
			line:    `{"log":"test\n","stream":"stdout","time":"2023-01-01T12:00:00.123456789Z"}`,
			isValid: true,
		},
		{
			name:    "valid stderr",
			line:    `{"log":"error\n","stream":"stderr","time":"2023-01-01T12:00:00.123456789Z"}`,
			isValid: true,
		},
		{
			name:    "invalid json",
			line:    `{invalid}`,
			isValid: false,
		},
		{
			name:    "missing log field",
			line:    `{"stream":"stdout","time":"2023-01-01T12:00:00.123456789Z"}`,
			isValid: false,
		},
		{
			name:    "missing time field",
			line:    `{"log":"test\n","stream":"stdout"}`,
			isValid: false,
		},
		{
			name:    "invalid stream value",
			line:    `{"log":"test\n","stream":"invalid","time":"2023-01-01T12:00:00.123456789Z"}`,
			isValid: false,
		},
		{
			name:    "invalid timestamp",
			line:    `{"log":"test\n","stream":"stdout","time":"not-a-timestamp"}`,
			isValid: false,
		},
		{
			name:    "empty line",
			line:    "",
			isValid: false,
		},
		{
			name:    "whitespace only",
			line:    "   \n\t  ",
			isValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidateDockerJSONFormat(tt.line)
			assert.Equal(t, tt.isValid, result)
		})
	}
}

// BenchmarkParseDockerJSONLogLine benchmarks parsing performance
func BenchmarkParseDockerJSONLogLine(b *testing.B) {
	containerID := "abc123def456789"
	metadata := &ContainerMetadata{
		ID:    containerID,
		Name:  "bench-container",
		Image: "nginx:latest",
		Labels: map[string]string{
			"env":     "prod",
			"service": "web",
		},
	}

	line := `{"log":"Sample log message for benchmarking\n","stream":"stdout","time":"2023-01-01T12:00:00.123456789Z"}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		entry, err := ParseDockerJSONLogLine(line, containerID, metadata)
		if err != nil {
			b.Fatal(err)
		}
		if entry != nil {
			entry.Release() // Return to pool
		}
	}
}

// BenchmarkEnrichLogEntryWithMetadata benchmarks metadata enrichment
func BenchmarkEnrichLogEntryWithMetadata(b *testing.B) {
	containerID := "abc123def456789"
	metadata := &ContainerMetadata{
		ID:       containerID,
		Name:     "bench-container",
		Image:    "nginx:latest",
		State:    "running",
		Hostname: "web-01",
		Labels: map[string]string{
			"env":     "prod",
			"service": "web",
			"version": "1.0.0",
		},
		Networks:    []string{"bridge"},
		IPAddresses: map[string]string{"bridge": "172.17.0.2"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		entry := types.AcquireLogEntry()
		enrichLogEntryWithMetadata(entry, containerID, metadata, "stdout")
		entry.Release()
	}
}

// BenchmarkValidateDockerJSONFormat benchmarks validation
func BenchmarkValidateDockerJSONFormat(b *testing.B) {
	line := `{"log":"test message\n","stream":"stdout","time":"2023-01-01T12:00:00.123456789Z"}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ValidateDockerJSONFormat(line)
	}
}
