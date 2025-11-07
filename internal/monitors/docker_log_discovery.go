package monitors

import (
	"context"
	"fmt"
	"os"

	dockerTypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
)

// ContainerLogInfo represents comprehensive information about a container's logging configuration
type ContainerLogInfo struct {
	// Container identification
	ContainerID string
	LogDriver   string
	LogPath     string

	// Rich metadata for log enrichment
	Metadata *ContainerMetadata
}

// ContainerMetadata contains all relevant metadata about a container for log enrichment
type ContainerMetadata struct {
	// Basic container information
	ID      string
	Name    string
	Image   string
	Labels  map[string]string

	// Timestamps
	Created string
	Started string

	// Runtime information
	State       string
	Status      string
	Platform    string

	// Network information (optional)
	Networks    []string
	IPAddresses map[string]string // network_name -> ip_address

	// Additional context
	Hostname    string
	Command     string
}

// DiscoverContainerLogFiles discovers all running containers and their log file configurations.
//
// This function:
// - Lists all running containers via Docker API
// - Inspects each container to determine log driver and log file path
// - Collects rich metadata for log enrichment
// - Handles errors gracefully (skips problematic containers)
// - Works with json-file driver containers (standard Docker logging)
//
// The returned ContainerLogInfo slice contains:
// - Containers with json-file driver: LogPath will be populated
// - Containers with other drivers: LogPath will be empty (fallback to streaming)
//
// Parameters:
//   - ctx: Context for cancellation and timeout control
//   - dockerClient: Docker API client for container operations
//
// Returns:
//   - []*ContainerLogInfo: Slice of container log configurations
//   - error: Error if Docker API fails completely
//
// Thread-safety: Safe for concurrent use
func DiscoverContainerLogFiles(ctx context.Context, dockerClient *client.Client) ([]*ContainerLogInfo, error) {
	// List all running containers
	containers, err := dockerClient.ContainerList(ctx, dockerTypes.ContainerListOptions{
		All: false, // Only running containers
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	logInfos := make([]*ContainerLogInfo, 0, len(containers))

	for _, cnt := range containers {
		// Inspect container to get detailed configuration
		inspect, err := dockerClient.ContainerInspect(ctx, cnt.ID)
		if err != nil {
			// Skip problematic containers but log the issue
			// This ensures discovery continues even if one container fails
			continue
		}

		// Extract log driver type
		logDriver := inspect.HostConfig.LogConfig.Type
		if logDriver == "" {
			// Fallback to default if not specified
			logDriver = "json-file"
		}

		// Build rich metadata
		metadata := buildContainerMetadata(&inspect)

		// Create ContainerLogInfo
		info := &ContainerLogInfo{
			ContainerID: cnt.ID,
			LogDriver:   logDriver,
			Metadata:    metadata,
		}

		// Only populate LogPath for json-file driver
		if logDriver == "json-file" {
			logPath := inspect.LogPath
			if logPath == "" {
				// Fallback to standard Docker log path format
				logPath = fmt.Sprintf("/var/lib/docker/containers/%s/%s-json.log",
					cnt.ID, cnt.ID)
			}

			// Verify file exists before adding
			if _, err := os.Stat(logPath); err == nil {
				info.LogPath = logPath
			} else {
				// Log file doesn't exist, mark as streaming-only
				info.LogPath = ""
			}
		}

		logInfos = append(logInfos, info)
	}

	return logInfos, nil
}

// buildContainerMetadata extracts comprehensive metadata from Docker inspect result
//
// This helper function:
// - Extracts all relevant container metadata
// - Handles nil/empty fields gracefully
// - Normalizes container names (removes leading "/")
// - Collects network information
// - Creates deep copies of maps for thread-safety
//
// Parameters:
//   - inspect: Docker container inspect result
//
// Returns:
//   - *ContainerMetadata: Rich metadata structure
func buildContainerMetadata(inspect *dockerTypes.ContainerJSON) *ContainerMetadata {
	metadata := &ContainerMetadata{
		ID:      inspect.ID,
		Name:    normalizeContainerName(inspect.Name),
		Image:   inspect.Config.Image,
		Labels:  deepCopyLabels(inspect.Config.Labels),
		Created: inspect.Created,
		Platform: inspect.Platform,
		Hostname: inspect.Config.Hostname,
	}

	// Extract state information
	if inspect.State != nil {
		metadata.State = inspect.State.Status
		metadata.Status = fmt.Sprintf("%s (running: %v)", inspect.State.Status, inspect.State.Running)
		// StartedAt is a string in JSON format, use as-is
		if inspect.State.StartedAt != "" {
			metadata.Started = inspect.State.StartedAt
		}
	}

	// Extract command (first arg only to keep it concise)
	if len(inspect.Config.Cmd) > 0 {
		metadata.Command = inspect.Config.Cmd[0]
	} else if len(inspect.Path) > 0 {
		metadata.Command = inspect.Path
	}

	// Extract network information
	if inspect.NetworkSettings != nil {
		metadata.Networks = make([]string, 0, len(inspect.NetworkSettings.Networks))
		metadata.IPAddresses = make(map[string]string, len(inspect.NetworkSettings.Networks))

		for networkName, network := range inspect.NetworkSettings.Networks {
			metadata.Networks = append(metadata.Networks, networkName)
			if network.IPAddress != "" {
				metadata.IPAddresses[networkName] = network.IPAddress
			}
		}
	}

	return metadata
}

// normalizeContainerName removes Docker's leading "/" from container names
//
// Docker returns container names with a leading slash (e.g., "/my-container").
// This function normalizes it to just "my-container".
func normalizeContainerName(name string) string {
	if len(name) > 0 && name[0] == '/' {
		return name[1:]
	}
	return name
}

// deepCopyLabels creates a thread-safe deep copy of container labels
//
// This is critical for concurrency:
// - Maps are reference types in Go
// - Multiple goroutines may access container metadata
// - Deep copy prevents race conditions
//
// Parameters:
//   - labels: Original labels map (may be nil)
//
// Returns:
//   - map[string]string: Independent copy of labels
func deepCopyLabels(labels map[string]string) map[string]string {
	if labels == nil {
		return make(map[string]string)
	}

	copy := make(map[string]string, len(labels))
	for k, v := range labels {
		copy[k] = v
	}
	return copy
}

// FilterContainersByLogDriver filters containers by log driver type
//
// Utility function for selecting containers based on their log driver.
// Useful for separating json-file containers (file monitoring) from
// others (streaming fallback).
//
// Parameters:
//   - containers: Slice of ContainerLogInfo
//   - logDriver: Log driver type to filter by (e.g., "json-file")
//
// Returns:
//   - []*ContainerLogInfo: Filtered slice
func FilterContainersByLogDriver(containers []*ContainerLogInfo, logDriver string) []*ContainerLogInfo {
	filtered := make([]*ContainerLogInfo, 0)
	for _, container := range containers {
		if container.LogDriver == logDriver {
			filtered = append(filtered, container)
		}
	}
	return filtered
}

// GetContainerCount returns summary statistics about discovered containers
//
// Provides quick insight into container distribution by log driver.
//
// Parameters:
//   - containers: Slice of ContainerLogInfo
//
// Returns:
//   - total: Total number of containers
//   - jsonFile: Number using json-file driver
//   - other: Number using other drivers
func GetContainerCount(containers []*ContainerLogInfo) (total, jsonFile, other int) {
	total = len(containers)
	for _, container := range containers {
		if container.LogDriver == "json-file" {
			jsonFile++
		} else {
			other++
		}
	}
	return total, jsonFile, other
}
