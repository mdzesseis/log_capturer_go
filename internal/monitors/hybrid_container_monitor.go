package monitors

import (
	"context"
	"fmt"
	"sync"
	"time"

	"ssw-logs-capture/internal/metrics"
	"ssw-logs-capture/pkg/types"

	dockerTypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/sirupsen/logrus"
)

// HybridContainerMonitor is a smart monitor that routes containers to optimal monitoring strategies.
//
// Architecture Decision (FASE 7 - Hybrid Monitor):
//
// This monitor implements a HYBRID approach that combines:
// - FILE-BASED monitoring for json-file driver containers (ZERO LEAKS)
// - STREAMING fallback for other log drivers (journald, syslog, etc.)
//
// Why Hybrid?
// - File monitoring has ZERO goroutine/FD leaks (proven in FASE 6H.1)
// - Docker's json-file driver is the most common (>95% of containers)
// - Streaming is still needed for non-json-file drivers
//
// Routing Logic:
// 1. Container starts → Docker Events detected
// 2. Inspect container → Get log driver type
// 3. Route decision:
//    - json-file + log path exists → FILE MONITOR (zero leaks!)
//    - Other drivers → STREAMING FALLBACK
//
// Expected Performance:
// - >95% containers via file monitor (zero leaks)
// - <5% containers via streaming (controlled leaks)
// - Overall leak rate: <0.5 gor/min (target: ZERO)
//
// Components:
// - FileMonitor: Handles file-based log reading (fsnotify + polling)
// - ContainerMonitor: Fallback streaming monitor for non-json-file drivers
// - MetadataCache: Caches container metadata to reduce Docker API calls
// - Docker Events: Real-time container lifecycle tracking
type HybridContainerMonitor struct {
	dockerClient   *client.Client
	fileMonitor    *FileMonitor
	streamMonitor  *ContainerMonitor
	metadataCache  *MetadataCache
	dispatcher     types.Dispatcher
	logger         *logrus.Logger

	// State management
	monitoredContainers map[string]*MonitoredContainerInfo
	mu                  sync.RWMutex

	// Context & lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Configuration
	config HybridMonitorConfig
}

// HybridMonitorConfig configures the hybrid monitor behavior
type HybridMonitorConfig struct {
	// MetadataCacheTTL is the time-to-live for cached container metadata
	// Recommended: 5 minutes
	MetadataCacheTTL time.Duration

	// PreferFileMonitor when true, prioritizes file monitoring even if log path is uncertain
	// Recommended: true (file monitor is more stable)
	PreferFileMonitor bool

	// EnableDockerEvents when true, monitors Docker events for real-time container discovery
	// Recommended: true
	EnableDockerEvents bool
}

// MonitoredContainerInfo tracks metadata about a monitored container
type MonitoredContainerInfo struct {
	ContainerID string
	LogDriver   string
	LogPath     string
	Metadata    *ContainerMetadata
	MonitorType string // "file" or "stream"
	StartedAt   time.Time
}

// NewHybridContainerMonitor creates a new hybrid container monitor
//
// Parameters:
//   - dockerClient: Docker API client
//   - fileMonitor: File monitor instance (pre-configured)
//   - streamMonitor: Container monitor instance (for fallback)
//   - dispatcher: Log dispatcher
//   - logger: Logger instance
//   - config: Hybrid monitor configuration
//
// Returns:
//   - *HybridContainerMonitor: Ready-to-use instance
func NewHybridContainerMonitor(
	dockerClient *client.Client,
	fileMonitor *FileMonitor,
	streamMonitor *ContainerMonitor,
	dispatcher types.Dispatcher,
	logger *logrus.Logger,
	config HybridMonitorConfig,
) *HybridContainerMonitor {
	// Apply defaults
	if config.MetadataCacheTTL == 0 {
		config.MetadataCacheTTL = 5 * time.Minute
	}
	if !config.EnableDockerEvents {
		config.EnableDockerEvents = true
	}
	if !config.PreferFileMonitor {
		config.PreferFileMonitor = true
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &HybridContainerMonitor{
		dockerClient:        dockerClient,
		fileMonitor:         fileMonitor,
		streamMonitor:       streamMonitor,
		metadataCache:       NewMetadataCache(config.MetadataCacheTTL),
		dispatcher:          dispatcher,
		logger:              logger,
		monitoredContainers: make(map[string]*MonitoredContainerInfo),
		ctx:                 ctx,
		cancel:              cancel,
		config:              config,
	}
}

// Start starts the hybrid container monitor
//
// This method:
// - Performs initial container discovery
// - Routes each container to optimal monitor
// - Starts Docker events listener (if enabled)
// - Updates metrics
func (hcm *HybridContainerMonitor) Start(ctx context.Context) error {
	hcm.logger.Info("Starting Hybrid Container Monitor")

	// Set component health
	metrics.SetComponentHealth("monitor", "hybrid_container_monitor", true)

	// Initial discovery of existing containers
	logInfos, err := DiscoverContainerLogFiles(ctx, hcm.dockerClient)
	if err != nil {
		metrics.SetComponentHealth("monitor", "hybrid_container_monitor", false)
		return fmt.Errorf("failed to discover containers: %w", err)
	}

	hcm.logger.WithField("count", len(logInfos)).Info("Discovered containers")

	// Start monitoring all discovered containers
	for _, info := range logInfos {
		if err := hcm.MonitorContainer(ctx, info); err != nil {
			hcm.logger.WithError(err).
				WithField("container_id", info.ContainerID).
				Warn("Failed to start monitoring container")
		}
	}

	// Start Docker events listener
	if hcm.config.EnableDockerEvents {
		hcm.wg.Add(1)
		go hcm.handleDockerEvents()
	}

	hcm.logger.WithFields(logrus.Fields{
		"total_containers":   len(hcm.monitoredContainers),
		"file_monitored":     hcm.countByType("file"),
		"stream_monitored":   hcm.countByType("stream"),
	}).Info("Hybrid Container Monitor started successfully")

	return nil
}

// Stop stops the hybrid container monitor
func (hcm *HybridContainerMonitor) Stop() error {
	hcm.logger.Info("Stopping Hybrid Container Monitor")

	// Update component health
	metrics.SetComponentHealth("monitor", "hybrid_container_monitor", false)

	hcm.cancel()
	hcm.wg.Wait()

	// Stop monitoring all containers
	hcm.mu.Lock()
	containerIDs := make([]string, 0, len(hcm.monitoredContainers))
	for id := range hcm.monitoredContainers {
		containerIDs = append(containerIDs, id)
	}
	hcm.mu.Unlock()

	for _, id := range containerIDs {
		hcm.StopMonitoring(id)
	}

	hcm.logger.Info("Hybrid Container Monitor stopped")
	return nil
}

// MonitorContainer routes a container to the optimal monitoring strategy
//
// Decision logic:
// 1. json-file driver + log path exists → FILE MONITOR (preferred, zero leaks)
// 2. Other drivers → STREAMING FALLBACK
//
// This method:
// - Caches container metadata
// - Makes routing decision
// - Delegates to appropriate monitor
// - Tracks monitoring state
func (hcm *HybridContainerMonitor) MonitorContainer(ctx context.Context, info *ContainerLogInfo) error {
	// Cache metadata
	hcm.metadataCache.Set(info.ContainerID, info.Metadata)

	monitorInfo := &MonitoredContainerInfo{
		ContainerID: info.ContainerID,
		LogDriver:   info.LogDriver,
		LogPath:     info.LogPath,
		Metadata:    info.Metadata,
		StartedAt:   time.Now(),
	}

	// Decision: File monitor or streaming fallback?
	if info.LogDriver == "json-file" && info.LogPath != "" {
		// USE FILE MONITOR (zero leaks!)
		if err := hcm.monitorViaFile(ctx, info); err != nil {
			return err
		}
		monitorInfo.MonitorType = "file"

		hcm.logger.WithFields(logrus.Fields{
			"container_id":   info.ContainerID[:12],
			"container_name": info.Metadata.Name,
			"log_path":       info.LogPath,
		}).Info("Monitoring container via file monitor")

	} else {
		// FALLBACK to streaming
		if err := hcm.monitorViaStream(ctx, info); err != nil {
			return err
		}
		monitorInfo.MonitorType = "stream"

		hcm.logger.WithFields(logrus.Fields{
			"container_id":   info.ContainerID[:12],
			"container_name": info.Metadata.Name,
			"log_driver":     info.LogDriver,
		}).Info("Monitoring container via streaming (fallback)")
	}

	// Track monitored container
	hcm.mu.Lock()
	hcm.monitoredContainers[info.ContainerID] = monitorInfo
	hcm.mu.Unlock()

	// Update metrics
	hcm.updateMetrics()

	return nil
}

// monitorViaFile sets up file-based monitoring for a container
//
// This method:
// - Creates a handler function that parses Docker JSON logs
// - Enriches logs with container metadata
// - Dispatches logs to the dispatcher
// - Delegates to FileMonitor for actual file reading
func (hcm *HybridContainerMonitor) monitorViaFile(ctx context.Context, info *ContainerLogInfo) error {
	// Add file to file monitor
	// Note: FileMonitor will handle fsnotify + polling automatically
	// The FileMonitor will use its own handler internally
	labels := map[string]string{
		"container_id":   info.ContainerID[:12],
		"container_name": info.Metadata.Name,
		"image":          info.Metadata.Image,
		"monitor_type":   "hybrid_file",
	}

	return hcm.fileMonitor.AddFile(info.LogPath, labels)
}

// monitorViaStream sets up streaming-based monitoring for a container
//
// This method:
// - Delegates to ContainerMonitor for streaming
// - Used as fallback for non-json-file drivers
// - May have controlled goroutine leaks (FASE 6H.1 implementation)
func (hcm *HybridContainerMonitor) monitorViaStream(ctx context.Context, info *ContainerLogInfo) error {
	// Use existing ContainerMonitor (streaming) as fallback
	// This reuses the FASE 6H.1 implementation (short-lived streams)
	// Note: ContainerMonitor doesn't expose StartMonitoring() directly,
	// so we would need to add that method or use internal API.
	// For now, we'll return an error indicating this needs to be implemented.
	return fmt.Errorf("streaming fallback not yet implemented")
}

// StopMonitoring stops monitoring a specific container
func (hcm *HybridContainerMonitor) StopMonitoring(containerID string) error {
	hcm.mu.Lock()
	monitorInfo, exists := hcm.monitoredContainers[containerID]
	delete(hcm.monitoredContainers, containerID)
	hcm.mu.Unlock()

	if !exists {
		return nil // Not monitored
	}

	// Remove from metadata cache
	hcm.metadataCache.Delete(containerID)

	// Stop based on monitor type
	if monitorInfo.MonitorType == "file" {
		// Remove from file monitor
		if err := hcm.fileMonitor.RemoveFile(monitorInfo.LogPath); err != nil {
			hcm.logger.WithError(err).
				WithField("log_path", monitorInfo.LogPath).
				Warn("Failed to remove file from monitor")
		}
	} else {
		// Stop streaming monitor
		// Note: Need to implement this in ContainerMonitor
		hcm.logger.WithField("container_id", containerID[:12]).
			Warn("Streaming monitor stop not yet implemented")
	}

	hcm.logger.WithFields(logrus.Fields{
		"container_id": containerID[:12],
		"monitor_type": monitorInfo.MonitorType,
	}).Info("Stopped monitoring container")

	// Update metrics
	hcm.updateMetrics()

	return nil
}

// handleDockerEvents listens to Docker events for real-time container discovery
//
// This goroutine:
// - Subscribes to Docker container events
// - Handles start/stop/die events
// - Automatically adds/removes containers
// - Runs until context is cancelled
func (hcm *HybridContainerMonitor) handleDockerEvents() {
	defer hcm.wg.Done()

	hcm.logger.Info("Starting Docker events listener")

	eventFilters := filters.NewArgs(
		filters.Arg("type", "container"),
	)

	eventsCh, errsCh := hcm.dockerClient.Events(hcm.ctx, dockerTypes.EventsOptions{
		Filters: eventFilters,
	})

	for {
		select {
		case <-hcm.ctx.Done():
			hcm.logger.Info("Docker events listener stopped")
			return

		case err := <-errsCh:
			if err != nil {
				hcm.logger.WithError(err).Error("Docker events error")
				metrics.RecordError("hybrid_container_monitor", "events_error")
				time.Sleep(5 * time.Second) // Backoff
			}

		case event := <-eventsCh:
			hcm.handleContainerEvent(event)
		}
	}
}

// handleContainerEvent processes a Docker container event
func (hcm *HybridContainerMonitor) handleContainerEvent(event events.Message) {
	switch event.Action {
	case "start":
		hcm.handleContainerStart(event)
	case "die", "stop", "kill":
		hcm.handleContainerStop(event)
	}
}

// handleContainerStart handles container start events
func (hcm *HybridContainerMonitor) handleContainerStart(event events.Message) {
	containerID := event.Actor.ID

	hcm.logger.WithFields(logrus.Fields{
		"container_id":   containerID[:12],
		"container_name": event.Actor.Attributes["name"],
	}).Debug("Container started event")

	// Wait a moment for container to be fully ready
	time.Sleep(1 * time.Second)

	// Fetch container info
	logInfos, err := DiscoverContainerLogFiles(hcm.ctx, hcm.dockerClient)
	if err != nil {
		hcm.logger.WithError(err).Error("Failed to discover containers after start event")
		return
	}

	// Find the started container
	for _, info := range logInfos {
		if info.ContainerID == containerID {
			if err := hcm.MonitorContainer(hcm.ctx, info); err != nil {
				hcm.logger.WithError(err).
					WithField("container_id", containerID[:12]).
					Error("Failed to start monitoring container")
			}
			return
		}
	}
}

// handleContainerStop handles container stop events
func (hcm *HybridContainerMonitor) handleContainerStop(event events.Message) {
	containerID := event.Actor.ID

	hcm.logger.WithFields(logrus.Fields{
		"container_id":   containerID[:12],
		"container_name": event.Actor.Attributes["name"],
	}).Debug("Container stopped event")

	if err := hcm.StopMonitoring(containerID); err != nil {
		hcm.logger.WithError(err).
			WithField("container_id", containerID[:12]).
			Error("Failed to stop monitoring container")
	}
}

// GetStats returns monitoring statistics
func (hcm *HybridContainerMonitor) GetStats() map[string]interface{} {
	hcm.mu.RLock()
	defer hcm.mu.RUnlock()

	fileCount := 0
	streamCount := 0

	for _, info := range hcm.monitoredContainers {
		if info.MonitorType == "file" {
			fileCount++
		} else {
			streamCount++
		}
	}

	// Get cache stats
	cacheSize, cacheHits, cacheMisses := hcm.metadataCache.GetStats()
	cacheHitRate := 0.0
	if cacheHits+cacheMisses > 0 {
		cacheHitRate = float64(cacheHits) / float64(cacheHits+cacheMisses)
	}

	return map[string]interface{}{
		"total_containers":   len(hcm.monitoredContainers),
		"file_monitored":     fileCount,
		"stream_monitored":   streamCount,
		"file_percentage":    float64(fileCount) / float64(len(hcm.monitoredContainers)) * 100.0,
		"cache_size":         cacheSize,
		"cache_hits":         cacheHits,
		"cache_misses":       cacheMisses,
		"cache_hit_rate":     cacheHitRate,
	}
}

// countByType counts containers by monitor type
func (hcm *HybridContainerMonitor) countByType(monitorType string) int {
	hcm.mu.RLock()
	defer hcm.mu.RUnlock()

	count := 0
	for _, info := range hcm.monitoredContainers {
		if info.MonitorType == monitorType {
			count++
		}
	}
	return count
}

// updateMetrics updates Prometheus metrics
func (hcm *HybridContainerMonitor) updateMetrics() {
	hcm.mu.RLock()
	defer hcm.mu.RUnlock()

	fileCount := 0
	streamCount := 0

	for _, info := range hcm.monitoredContainers {
		if info.MonitorType == "file" {
			fileCount++
		} else {
			streamCount++
		}
	}

	// Update custom metrics
	metrics.SetActiveTasks("hybrid_container_monitors", "file", fileCount)
	metrics.SetActiveTasks("hybrid_container_monitors", "stream", streamCount)
	metrics.UpdateTotalContainersMonitored(len(hcm.monitoredContainers))
}

// IsHealthy checks if the monitor is healthy
func (hcm *HybridContainerMonitor) IsHealthy() bool {
	// Check if context is still active
	select {
	case <-hcm.ctx.Done():
		return false
	default:
		return true
	}
}

// GetStatus returns monitor status
func (hcm *HybridContainerMonitor) GetStatus() types.MonitorStatus {
	return types.MonitorStatus{
		Name:      "hybrid_container_monitor",
		IsRunning: hcm.IsHealthy(),
		IsHealthy: hcm.IsHealthy(),
	}
}

// GetMonitoredContainers returns a list of monitored containers
func (hcm *HybridContainerMonitor) GetMonitoredContainers() []map[string]interface{} {
	hcm.mu.RLock()
	defer hcm.mu.RUnlock()

	result := make([]map[string]interface{}, 0, len(hcm.monitoredContainers))
	for _, info := range hcm.monitoredContainers {
		result = append(result, map[string]interface{}{
			"container_id":   info.ContainerID[:12],
			"container_name": info.Metadata.Name,
			"image":          info.Metadata.Image,
			"log_driver":     info.LogDriver,
			"log_path":       info.LogPath,
			"monitor_type":   info.MonitorType,
			"started_at":     info.StartedAt,
			"uptime_seconds": time.Since(info.StartedAt).Seconds(),
		})
	}

	return result
}

// Ensure HybridContainerMonitor implements Monitor interface
var _ Monitor = (*HybridContainerMonitor)(nil)

// Monitor interface (if not already defined elsewhere)
type Monitor interface {
	Start(ctx context.Context) error
	Stop() error
	IsHealthy() bool
	GetStatus() types.MonitorStatus
}
