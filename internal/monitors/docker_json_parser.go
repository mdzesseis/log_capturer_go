package monitors

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"ssw-logs-capture/pkg/types"
)

// DockerJSONLog represents the structure of Docker's json-file log format
//
// Docker's json-file driver writes logs in this format:
// {"log":"actual log message\n","stream":"stdout","time":"2023-01-01T12:00:00.123456789Z"}
//
// Fields:
//   - Log: The actual log message (includes trailing newline)
//   - Stream: Either "stdout" or "stderr"
//   - Time: RFC3339Nano timestamp (high-precision)
type DockerJSONLog struct {
	Log    string `json:"log"`
	Stream string `json:"stream"`
	Time   string `json:"time"`
}

// ParseDockerJSONLogLine parses a single line from Docker's json-file log format
//
// This function:
// - Parses Docker's JSON log format
// - Extracts timestamp (RFC3339Nano format)
// - Creates a fully-enriched LogEntry with container metadata
// - Handles parse errors gracefully
// - Strips trailing newlines from log messages
// - Returns nil for empty lines (filter optimization)
//
// Container Metadata Enrichment:
// The resulting LogEntry includes these standard labels:
// - container_id: Short container ID (12 chars)
// - container_name: Normalized container name (without leading /)
// - image: Container image name
// - stream: stdout or stderr
// - state: Container state (running, exited, etc.)
// - hostname: Container hostname
// - command: Container command
// - All container labels prefixed with "container_label_"
// - Network information if available
//
// Parameters:
//   - line: Raw line from Docker json-file log
//   - containerID: Full container ID (for sourceID generation)
//   - metadata: Container metadata for enrichment
//
// Returns:
//   - *types.LogEntry: Parsed and enriched log entry
//   - error: Parse error (invalid JSON, missing fields, etc.)
//
// Thread-safety: Safe for concurrent use (no shared state)
//
// Performance:
// - Uses types.AcquireLogEntry() for pool optimization
// - Efficient string operations
// - Minimal allocations
func ParseDockerJSONLogLine(line string, containerID string, metadata *ContainerMetadata) (*types.LogEntry, error) {
	// Optimization: Skip empty lines immediately
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return nil, nil
	}

	// Parse Docker JSON format
	var dockerLog DockerJSONLog
	if err := json.Unmarshal([]byte(line), &dockerLog); err != nil {
		return nil, fmt.Errorf("failed to parse docker JSON log: %w", err)
	}

	// Parse timestamp (Docker uses RFC3339Nano for high-precision)
	timestamp, err := time.Parse(time.RFC3339Nano, dockerLog.Time)
	if err != nil {
		// Fallback to current time if timestamp parse fails
		// This ensures logs are not lost due to timestamp issues
		timestamp = time.Now().UTC()
	} else {
		// Ensure UTC for consistency with Loki
		timestamp = timestamp.UTC()
	}

	// Acquire LogEntry from pool for performance
	entry := types.AcquireLogEntry()

	// Set core fields
	entry.Message = strings.TrimSuffix(dockerLog.Log, "\n")
	entry.Timestamp = timestamp
	entry.ProcessedAt = time.Now().UTC()
	entry.SourceType = "docker"
	entry.SourceID = containerID[:12] // Short container ID (standard Docker format)

	// Enrich with container metadata
	enrichLogEntryWithMetadata(entry, containerID, metadata, dockerLog.Stream)

	return entry, nil
}

// enrichLogEntryWithMetadata adds comprehensive container metadata to a LogEntry
//
// This function populates the LogEntry's Labels map with:
// - Standard Docker metadata (container_id, name, image, stream)
// - Container state information
// - Network information
// - All container labels (prefixed to avoid conflicts)
// - Runtime information (hostname, command)
//
// Label Prefixing Strategy:
// - Direct labels: container_id, container_name, image, stream, state
// - Container labels: "container_label_" prefix (e.g., "container_label_env=prod")
// - Network labels: "network_" prefix (e.g., "network_bridge=172.17.0.2")
//
// This enrichment enables:
// - Powerful filtering in Loki/Grafana
// - Correlation across containers
// - Troubleshooting with full context
// - Kubernetes-style label selectors
//
// Parameters:
//   - entry: LogEntry to enrich (modified in-place)
//   - containerID: Full container ID
//   - metadata: Container metadata
//   - stream: "stdout" or "stderr"
//
// Thread-safety: Modifies entry, must be called before sharing entry across goroutines
func enrichLogEntryWithMetadata(entry *types.LogEntry, containerID string, metadata *ContainerMetadata, stream string) {
	// Initialize labels map (types.AcquireLogEntry() already pre-allocates)
	if entry.Labels == nil {
		entry.Labels = make(map[string]string, 16) // Generous pre-allocation
	}

	// Core container identification
	entry.Labels["container_id"] = containerID[:12] // Short ID
	entry.Labels["container_name"] = metadata.Name
	entry.Labels["image"] = metadata.Image
	entry.Labels["stream"] = stream

	// Container state
	if metadata.State != "" {
		entry.Labels["state"] = metadata.State
	}

	// Runtime information
	if metadata.Hostname != "" {
		entry.Labels["hostname"] = metadata.Hostname
	}
	if metadata.Command != "" {
		entry.Labels["command"] = metadata.Command
	}
	if metadata.Platform != "" {
		entry.Labels["platform"] = metadata.Platform
	}

	// Copy container labels with prefix (prevent conflicts with system labels)
	for k, v := range metadata.Labels {
		// Skip internal Docker Compose labels to reduce cardinality
		if shouldIncludeLabel(k) {
			entry.Labels["container_label_"+k] = v
		}
	}

	// Network information (useful for debugging network issues)
	if len(metadata.Networks) > 0 {
		// Primary network
		entry.Labels["network"] = metadata.Networks[0]

		// IP addresses for each network
		for networkName, ipAddress := range metadata.IPAddresses {
			entry.Labels["network_"+networkName] = ipAddress
		}
	}

	// Standard labels for compatibility with existing infrastructure
	entry.Labels["source"] = "docker"
	entry.Labels["service"] = "ssw-log-capturer"
}

// shouldIncludeLabel determines if a container label should be included in log entry
//
// Filters out high-cardinality or internal Docker labels to:
// - Reduce Prometheus/Loki cardinality
// - Avoid storage bloat
// - Improve query performance
//
// Excluded labels:
// - Docker Compose internal labels (com.docker.compose.*)
// - OpenContainers metadata (org.opencontainers.*)
// - Build-time labels (typically not useful at runtime)
//
// Parameters:
//   - labelKey: Container label key
//
// Returns:
//   - bool: true if label should be included
func shouldIncludeLabel(labelKey string) bool {
	// Exclude Docker Compose internal labels
	if strings.HasPrefix(labelKey, "com.docker.compose.") {
		return false
	}

	// Exclude OpenContainers metadata
	if strings.HasPrefix(labelKey, "org.opencontainers.") {
		return false
	}

	// Exclude build-time labels (typically not useful)
	excludedLabels := map[string]bool{
		"desktop.docker.io/wsl-distro": true,
		"com.docker.desktop.plugin":    true,
	}

	return !excludedLabels[labelKey]
}

// ParseDockerJSONLogBatch parses multiple log lines in batch for efficiency
//
// This function:
// - Processes multiple lines in a single call
// - Skips invalid lines (continues on error)
// - Returns only successfully parsed entries
// - Optimized for high-throughput scenarios
//
// Use this when:
// - Reading from file with multiple lines buffered
// - Batch processing is more efficient than line-by-line
// - Some parse failures are acceptable (best-effort)
//
// Parameters:
//   - lines: Slice of raw log lines
//   - containerID: Full container ID
//   - metadata: Container metadata
//
// Returns:
//   - []*types.LogEntry: Successfully parsed entries
//   - []error: Parse errors (parallel to lines, nil for success)
func ParseDockerJSONLogBatch(lines []string, containerID string, metadata *ContainerMetadata) ([]*types.LogEntry, []error) {
	entries := make([]*types.LogEntry, 0, len(lines))
	errors := make([]error, len(lines))

	for i, line := range lines {
		entry, err := ParseDockerJSONLogLine(line, containerID, metadata)
		if err != nil {
			errors[i] = err
			continue
		}

		if entry != nil {
			entries = append(entries, entry)
		}
	}

	return entries, errors
}

// ValidateDockerJSONFormat validates if a line is valid Docker JSON format
//
// Quick validation without full parsing. Useful for:
// - Pre-flight checks before processing
// - Determining if file is Docker json-file format
// - Error detection in log pipelines
//
// Parameters:
//   - line: Raw log line
//
// Returns:
//   - bool: true if valid Docker JSON format
func ValidateDockerJSONFormat(line string) bool {
	if strings.TrimSpace(line) == "" {
		return false
	}

	var dockerLog DockerJSONLog
	if err := json.Unmarshal([]byte(line), &dockerLog); err != nil {
		return false
	}

	// Validate required fields
	if dockerLog.Log == "" || dockerLog.Time == "" {
		return false
	}

	// Validate stream value
	if dockerLog.Stream != "stdout" && dockerLog.Stream != "stderr" {
		return false
	}

	// Validate timestamp format
	_, err := time.Parse(time.RFC3339Nano, dockerLog.Time)
	return err == nil
}
