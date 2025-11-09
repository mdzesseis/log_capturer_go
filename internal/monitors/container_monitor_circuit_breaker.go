package monitors

import (
	"context"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"ssw-logs-capture/internal/metrics"
)

// containerLogStats tracks log volume for a single container
type containerLogStats struct {
	containerID   string
	containerName string
	count         int64
	lastSeen      time.Time
}

// circuitBreaker detects self-monitoring loops by tracking per-container log volume
// and automatically excluding containers that generate excessive logs (>90% of total)
type circuitBreaker struct {
	mu         sync.RWMutex
	stats      map[string]*containerLogStats // key: containerID
	threshold  float64                        // 0.90 = 90%
	windowSize time.Duration                  // 1 minute
	logger     *logrus.Logger
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	monitor    *ContainerMonitor // reference to parent
}

// newCircuitBreaker creates a new circuit breaker
func newCircuitBreaker(monitor *ContainerMonitor) *circuitBreaker {
	ctx, cancel := context.WithCancel(context.Background())

	cb := &circuitBreaker{
		stats:      make(map[string]*containerLogStats),
		threshold:  0.90, // 90%
		windowSize: 1 * time.Minute,
		logger:     monitor.logger,
		ctx:        ctx,
		cancel:     cancel,
		monitor:    monitor,
	}

	// Start cleanup goroutine
	cb.wg.Add(1)
	go cb.runCleanup()

	// Start detection goroutine
	cb.wg.Add(1)
	go cb.runDetection()

	return cb
}

// trackLog records a log entry for a container
// Thread-safe: uses mutex to protect shared state
func (cb *circuitBreaker) trackLog(containerID, containerName string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	stats, exists := cb.stats[containerID]
	if !exists {
		stats = &containerLogStats{
			containerID:   containerID,
			containerName: containerName,
			count:         0,
			lastSeen:      time.Now(),
		}
		cb.stats[containerID] = stats
	}

	stats.count++
	stats.lastSeen = time.Now()
}

// runCleanup periodically removes old stats to prevent memory leaks
// Runs in its own goroutine with proper lifecycle management
func (cb *circuitBreaker) runCleanup() {
	defer cb.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-cb.ctx.Done():
			return
		case <-ticker.C:
			cb.cleanup()
		}
	}
}

// cleanup removes stats older than 2 * windowSize
// This prevents unbounded memory growth
func (cb *circuitBreaker) cleanup() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cutoff := time.Now().Add(-2 * cb.windowSize)

	for containerID, stats := range cb.stats {
		if stats.lastSeen.Before(cutoff) {
			delete(cb.stats, containerID)
		}
	}
}

// runDetection periodically checks for self-monitoring loops
// Runs in its own goroutine with proper lifecycle management
func (cb *circuitBreaker) runDetection() {
	defer cb.wg.Done()

	ticker := time.NewTicker(10 * time.Second) // Check every 10 seconds
	defer ticker.Stop()

	for {
		select {
		case <-cb.ctx.Done():
			return
		case <-ticker.C:
			cb.detectSelfMonitoring()
		}
	}
}

// detectSelfMonitoring analyzes stats and triggers if threshold exceeded
func (cb *circuitBreaker) detectSelfMonitoring() {
	cb.mu.RLock()

	// Calculate total logs in window
	var totalLogs int64
	cutoff := time.Now().Add(-cb.windowSize)

	recentStats := make([]*containerLogStats, 0, len(cb.stats))
	for _, stats := range cb.stats {
		if stats.lastSeen.After(cutoff) {
			totalLogs += stats.count
			// Create copy to avoid holding read lock during processing
			statsCopy := *stats
			recentStats = append(recentStats, &statsCopy)
		}
	}

	cb.mu.RUnlock()

	// Need minimum sample size to avoid false positives
	if totalLogs < 100 {
		return
	}

	// Check each container
	for _, stats := range recentStats {
		ratio := float64(stats.count) / float64(totalLogs)

		if ratio >= cb.threshold {
			cb.handleSelfMonitoringDetected(stats, ratio, totalLogs)
		}
	}
}

// handleSelfMonitoringDetected triggers when a container exceeds threshold
func (cb *circuitBreaker) handleSelfMonitoringDetected(stats *containerLogStats, ratio float64, totalLogs int64) {
	cb.logger.Warn("self-monitoring loop detected - auto-excluding container",
		"container_id", stats.containerID,
		"container_name", stats.containerName,
		"log_percentage", ratio*100,
		"log_count", stats.count,
		"total_logs", totalLogs,
		"threshold", cb.threshold*100,
	)

	// Record metric
	metrics.RecordError("container_monitor", "self_monitoring_detected")

	// Add to exclusion list
	// Lock ordering: circuitBreaker.mu -> monitor.mu (if needed)
	cb.monitor.mu.Lock()
	cb.monitor.excludeNames = append(cb.monitor.excludeNames, stats.containerName)
	cb.monitor.excludeNames = append(cb.monitor.excludeNames, stats.containerID)
	cb.monitor.mu.Unlock()

	// Reset stats for this container
	cb.mu.Lock()
	delete(cb.stats, stats.containerID)
	cb.mu.Unlock()

	cb.logger.Info("container auto-excluded from monitoring",
		"container_id", stats.containerID,
		"container_name", stats.containerName,
	)
}

// stop shuts down the circuit breaker gracefully
func (cb *circuitBreaker) stop() {
	cb.cancel()
	cb.wg.Wait()
}
