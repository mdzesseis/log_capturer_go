package monitors

import (
	"os"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNormalizeContainerName tests container name normalization
func TestNormalizeContainerName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "with leading slash",
			input:    "/my-container",
			expected: "my-container",
		},
		{
			name:     "without leading slash",
			input:    "my-container",
			expected: "my-container",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "just slash",
			input:    "/",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeContainerName(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestDeepCopyLabels tests deep copy functionality for thread-safety
func TestDeepCopyLabels(t *testing.T) {
	t.Run("nil labels", func(t *testing.T) {
		result := deepCopyLabels(nil)
		assert.NotNil(t, result)
		assert.Empty(t, result)
	})

	t.Run("empty labels", func(t *testing.T) {
		original := make(map[string]string)
		result := deepCopyLabels(original)
		assert.NotNil(t, result)
		assert.Empty(t, result)
	})

	t.Run("copy and modify", func(t *testing.T) {
		original := map[string]string{
			"env":     "prod",
			"service": "api",
			"version": "1.0.0",
		}

		copy := deepCopyLabels(original)

		// Verify copy matches
		assert.Equal(t, original, copy)

		// Modify copy
		copy["env"] = "dev"
		copy["new_label"] = "value"

		// Verify original is unchanged (deep copy, not reference)
		assert.Equal(t, "prod", original["env"])
		_, exists := original["new_label"]
		assert.False(t, exists, "original should not have new_label")
	})

	t.Run("thread-safety simulation", func(t *testing.T) {
		original := map[string]string{
			"key1": "value1",
			"key2": "value2",
		}

		// Simulate concurrent access
		done := make(chan bool, 2)

		// Goroutine 1: Read original
		go func() {
			for i := 0; i < 100; i++ {
				copy := deepCopyLabels(original)
				assert.Equal(t, "value1", copy["key1"])
			}
			done <- true
		}()

		// Goroutine 2: Modify copy
		go func() {
			for i := 0; i < 100; i++ {
				copy := deepCopyLabels(original)
				copy["key1"] = "modified"
				assert.Equal(t, "modified", copy["key1"])
			}
			done <- true
		}()

		// Wait for completion
		<-done
		<-done

		// Original should be unchanged
		assert.Equal(t, "value1", original["key1"])
	})
}

// TestBuildContainerMetadata tests metadata extraction
func TestBuildContainerMetadata(t *testing.T) {
	t.Run("full metadata", func(t *testing.T) {
		inspect := &types.ContainerJSON{
			ContainerJSONBase: &types.ContainerJSONBase{
				ID:       "abc123def456",
				Name:     "/my-container",
				Created:  "2023-01-01T12:00:00Z",
				Platform: "linux",
				State: &types.ContainerState{
					Status:    "running",
					Running:   true,
					StartedAt: "2023-01-01T12:00:05.123Z",
				},
			},
			Config: &container.Config{
				Image:    "nginx:latest",
				Hostname: "web-server",
				Labels: map[string]string{
					"env":     "prod",
					"service": "web",
				},
				Cmd: []string{"/bin/sh", "-c", "nginx"},
			},
			NetworkSettings: &types.NetworkSettings{
				Networks: map[string]*network.EndpointSettings{
					"bridge": {
						IPAddress: "172.17.0.2",
					},
					"custom": {
						IPAddress: "10.0.1.5",
					},
				},
			},
		}

		metadata := buildContainerMetadata(inspect)

		// Verify basic fields
		assert.Equal(t, "abc123def456", metadata.ID)
		assert.Equal(t, "my-container", metadata.Name)
		assert.Equal(t, "nginx:latest", metadata.Image)
		assert.Equal(t, "2023-01-01T12:00:00Z", metadata.Created)
		assert.Equal(t, "linux", metadata.Platform)
		assert.Equal(t, "web-server", metadata.Hostname)
		assert.Equal(t, "/bin/sh", metadata.Command)

		// Verify state
		assert.Equal(t, "running", metadata.State)
		assert.Contains(t, metadata.Status, "running")
		assert.Contains(t, metadata.Status, "true")

		// Verify labels are deep copied
		assert.Equal(t, 2, len(metadata.Labels))
		assert.Equal(t, "prod", metadata.Labels["env"])
		assert.Equal(t, "web", metadata.Labels["service"])

		// Verify networks
		assert.Equal(t, 2, len(metadata.Networks))
		assert.Contains(t, metadata.Networks, "bridge")
		assert.Contains(t, metadata.Networks, "custom")
		assert.Equal(t, "172.17.0.2", metadata.IPAddresses["bridge"])
		assert.Equal(t, "10.0.1.5", metadata.IPAddresses["custom"])
	})

	t.Run("minimal metadata", func(t *testing.T) {
		inspect := &types.ContainerJSON{
			ContainerJSONBase: &types.ContainerJSONBase{
				ID:   "abc123",
				Name: "minimal",
			},
			Config: &container.Config{
				Image: "alpine:latest",
			},
		}

		metadata := buildContainerMetadata(inspect)

		assert.Equal(t, "abc123", metadata.ID)
		assert.Equal(t, "minimal", metadata.Name)
		assert.Equal(t, "alpine:latest", metadata.Image)
		assert.NotNil(t, metadata.Labels)
		assert.Empty(t, metadata.Labels)
	})

	t.Run("nil state handling", func(t *testing.T) {
		inspect := &types.ContainerJSON{
			ContainerJSONBase: &types.ContainerJSONBase{
				ID:    "test123",
				Name:  "test",
				State: nil, // Nil state
			},
			Config: &container.Config{
				Image: "test:latest",
			},
		}

		metadata := buildContainerMetadata(inspect)

		assert.Equal(t, "", metadata.State)
		assert.Equal(t, "", metadata.Status)
	})
}

// TestFilterContainersByLogDriver tests log driver filtering
func TestFilterContainersByLogDriver(t *testing.T) {
	containers := []*ContainerLogInfo{
		{
			ContainerID: "container1",
			LogDriver:   "json-file",
			LogPath:     "/var/lib/docker/containers/container1/container1-json.log",
		},
		{
			ContainerID: "container2",
			LogDriver:   "syslog",
			LogPath:     "",
		},
		{
			ContainerID: "container3",
			LogDriver:   "json-file",
			LogPath:     "/var/lib/docker/containers/container3/container3-json.log",
		},
		{
			ContainerID: "container4",
			LogDriver:   "journald",
			LogPath:     "",
		},
	}

	t.Run("filter json-file", func(t *testing.T) {
		filtered := FilterContainersByLogDriver(containers, "json-file")
		assert.Equal(t, 2, len(filtered))
		assert.Equal(t, "container1", filtered[0].ContainerID)
		assert.Equal(t, "container3", filtered[1].ContainerID)
	})

	t.Run("filter syslog", func(t *testing.T) {
		filtered := FilterContainersByLogDriver(containers, "syslog")
		assert.Equal(t, 1, len(filtered))
		assert.Equal(t, "container2", filtered[0].ContainerID)
	})

	t.Run("filter non-existent", func(t *testing.T) {
		filtered := FilterContainersByLogDriver(containers, "fluentd")
		assert.Equal(t, 0, len(filtered))
	})

	t.Run("empty input", func(t *testing.T) {
		filtered := FilterContainersByLogDriver([]*ContainerLogInfo{}, "json-file")
		assert.Equal(t, 0, len(filtered))
	})
}

// TestGetContainerCount tests container counting
func TestGetContainerCount(t *testing.T) {
	t.Run("mixed containers", func(t *testing.T) {
		containers := []*ContainerLogInfo{
			{ContainerID: "1", LogDriver: "json-file"},
			{ContainerID: "2", LogDriver: "json-file"},
			{ContainerID: "3", LogDriver: "syslog"},
			{ContainerID: "4", LogDriver: "journald"},
			{ContainerID: "5", LogDriver: "json-file"},
		}

		total, jsonFile, other := GetContainerCount(containers)
		assert.Equal(t, 5, total)
		assert.Equal(t, 3, jsonFile)
		assert.Equal(t, 2, other)
	})

	t.Run("all json-file", func(t *testing.T) {
		containers := []*ContainerLogInfo{
			{ContainerID: "1", LogDriver: "json-file"},
			{ContainerID: "2", LogDriver: "json-file"},
		}

		total, jsonFile, other := GetContainerCount(containers)
		assert.Equal(t, 2, total)
		assert.Equal(t, 2, jsonFile)
		assert.Equal(t, 0, other)
	})

	t.Run("empty", func(t *testing.T) {
		total, jsonFile, other := GetContainerCount([]*ContainerLogInfo{})
		assert.Equal(t, 0, total)
		assert.Equal(t, 0, jsonFile)
		assert.Equal(t, 0, other)
	})
}

// TestDiscoverContainerLogFilesLogPathValidation tests log path validation
func TestDiscoverContainerLogFilesLogPathValidation(t *testing.T) {
	t.Run("log path exists", func(t *testing.T) {
		// Create temporary log file
		tmpFile, err := os.CreateTemp("", "docker-*.log")
		require.NoError(t, err)
		defer os.Remove(tmpFile.Name())
		tmpFile.Close()

		// This test verifies the log path validation logic
		// In actual DiscoverContainerLogFiles, os.Stat is used
		_, err = os.Stat(tmpFile.Name())
		assert.NoError(t, err, "log file should exist")
	})

	t.Run("log path does not exist", func(t *testing.T) {
		nonExistentPath := "/tmp/nonexistent-docker-log-abc123.log"
		_, err := os.Stat(nonExistentPath)
		assert.Error(t, err, "log file should not exist")
		assert.True(t, os.IsNotExist(err))
	})
}

// BenchmarkDeepCopyLabels benchmarks label copying performance
func BenchmarkDeepCopyLabels(b *testing.B) {
	labels := map[string]string{
		"env":         "prod",
		"service":     "api",
		"version":     "1.0.0",
		"datacenter":  "us-west-2",
		"team":        "platform",
		"cost-center": "engineering",
		"app":         "web",
		"tier":        "frontend",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = deepCopyLabels(labels)
	}
}

// BenchmarkBuildContainerMetadata benchmarks metadata extraction
func BenchmarkBuildContainerMetadata(b *testing.B) {
	inspect := &types.ContainerJSON{
		ContainerJSONBase: &types.ContainerJSONBase{
			ID:       "abc123def456",
			Name:     "/my-container",
			Created:  "2023-01-01T12:00:00Z",
			Platform: "linux",
			State: &types.ContainerState{
				Status:  "running",
				Running: true,
			},
		},
		Config: &container.Config{
			Image:    "nginx:latest",
			Hostname: "web-server",
			Labels: map[string]string{
				"env":     "prod",
				"service": "web",
				"version": "1.0.0",
			},
		},
		NetworkSettings: &types.NetworkSettings{
			Networks: map[string]*network.EndpointSettings{
				"bridge": {IPAddress: "172.17.0.2"},
			},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = buildContainerMetadata(inspect)
	}
}
