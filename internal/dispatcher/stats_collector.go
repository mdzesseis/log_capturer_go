// Package dispatcher - Statistics collection component
package dispatcher

import (
	"context"
	"runtime"
	"sync"
	"time"

	"ssw-logs-capture/internal/metrics"
	"ssw-logs-capture/pkg/backpressure"
	"ssw-logs-capture/pkg/types"

	"github.com/sirupsen/logrus"
)

// StatsCollector handles statistics collection and metrics reporting
type StatsCollector struct {
	stats      *types.DispatcherStats
	statsMutex *sync.RWMutex
	config     DispatcherConfig
	logger     *logrus.Logger
	queue      <-chan dispatchItem
}

// NewStatsCollector creates a new statistics collector instance
func NewStatsCollector(
	stats *types.DispatcherStats,
	statsMutex *sync.RWMutex,
	config DispatcherConfig,
	logger *logrus.Logger,
	queue <-chan dispatchItem,
) *StatsCollector {
	return &StatsCollector{
		stats:      stats,
		statsMutex: statsMutex,
		config:     config,
		logger:     logger,
		queue:      queue,
	}
}

// UpdateStats updates statistics in a thread-safe manner
func (sc *StatsCollector) UpdateStats(fn func(*types.DispatcherStats)) {
	sc.statsMutex.Lock()
	defer sc.statsMutex.Unlock()
	fn(sc.stats)
}

// GetStats returns a safe copy of current statistics
func (sc *StatsCollector) GetStats() types.DispatcherStats {
	sc.statsMutex.RLock()
	defer sc.statsMutex.RUnlock()

	// Create deep copy
	statsCopy := *sc.stats

	// Deep copy the sink distribution map
	statsCopy.SinkDistribution = make(map[string]int64, len(sc.stats.SinkDistribution))
	for k, v := range sc.stats.SinkDistribution {
		statsCopy.SinkDistribution[k] = v
	}

	return statsCopy
}

// RunStatsUpdater runs periodic statistics updates in a goroutine
//
// This goroutine:
//  - Updates queue size metrics
//  - Calculates logs/second throughput
//  - Updates Prometheus metrics
//  - Monitors retry queue utilization
//  - Logs warnings for high utilization
func (sc *StatsCollector) RunStatsUpdater(ctx context.Context, getRetryStats func() map[string]interface{}) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	var lastProcessed int64
	lastCheck := time.Now()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			now := time.Now()

			sc.statsMutex.Lock()
			currentProcessed := sc.stats.TotalProcessed
			sc.stats.QueueSize = len(sc.queue)
			sc.statsMutex.Unlock()

			// Calculate throughput
			duration := now.Sub(lastCheck).Seconds()
			if duration > 0 {
				processedSinceLast := currentProcessed - lastProcessed
				logsPerSecond := float64(processedSinceLast) / duration
				metrics.LogsPerSecond.WithLabelValues("dispatcher").Set(logsPerSecond)
			}

			lastProcessed = currentProcessed
			lastCheck = now

			// Update queue metrics
			queueUtilization := float64(sc.stats.QueueSize) / float64(sc.config.QueueSize)
			metrics.DispatcherQueueUtilization.Set(queueUtilization)
			metrics.SetQueueSize("dispatcher", "main", sc.stats.QueueSize)

			// Monitor retry queue
			if getRetryStats != nil {
				retryStats := getRetryStats()
				currentRetries := retryStats["current_retries"].(int)
				retryUtilization := retryStats["utilization"].(float64)

				metrics.SetQueueSize("dispatcher", "retry", currentRetries)

				// Warn if retry queue is getting full (>80%)
				if retryUtilization > 0.8 {
					sc.logger.WithFields(logrus.Fields{
						"current_retries":        currentRetries,
						"max_concurrent_retries": retryStats["max_concurrent_retries"],
						"utilization":            retryUtilization,
					}).Warn("Retry queue utilization high - potential goroutine leak risk")
				}
			}
		}
	}
}

// UpdateBackpressureMetrics calculates and updates backpressure metrics
//
// This method collects:
//  - Queue utilization
//  - Memory utilization
//  - CPU utilization (estimated)
//  - I/O utilization (estimated)
//  - Error rate
func (sc *StatsCollector) UpdateBackpressureMetrics(backpressureManager *backpressure.Manager) {
	if backpressureManager == nil {
		return
	}

	// Calculate queue utilization
	sc.statsMutex.RLock()
	queueSize := sc.stats.QueueSize
	totalProcessed := sc.stats.TotalProcessed
	errorCount := sc.stats.ErrorCount
	sc.statsMutex.RUnlock()

	queueUtilization := float64(queueSize) / float64(sc.config.QueueSize)

	// Collect memory metrics
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// Calculate memory utilization (assuming 512MB limit)
	memoryUtilization := float64(memStats.Alloc) / (512 * 1024 * 1024)
	if memoryUtilization > 1.0 {
		memoryUtilization = 1.0
	}

	// Estimate CPU and I/O based on queue load
	cpuUtilization := queueUtilization * 0.8
	ioUtilization := queueUtilization * 0.6

	// Calculate error rate
	var errorRate float64
	if totalProcessed > 0 {
		errorRate = float64(errorCount) / float64(totalProcessed)
	}

	// Update backpressure manager
	backpressureManager.UpdateMetrics(backpressure.Metrics{
		QueueUtilization:  queueUtilization,
		MemoryUtilization: memoryUtilization,
		CPUUtilization:    cpuUtilization,
		IOUtilization:     ioUtilization,
		ErrorRate:         errorRate,
	})
}

// IncrementProcessed increments the processed counter
func (sc *StatsCollector) IncrementProcessed() {
	sc.UpdateStats(func(stats *types.DispatcherStats) {
		stats.TotalProcessed++
		stats.LastProcessedTime = time.Now()
	})
}

// IncrementErrors increments the error counter
func (sc *StatsCollector) IncrementErrors() {
	sc.UpdateStats(func(stats *types.DispatcherStats) {
		stats.ErrorCount++
	})
}

// IncrementThrottled increments the throttled counter
func (sc *StatsCollector) IncrementThrottled() {
	sc.UpdateStats(func(stats *types.DispatcherStats) {
		stats.Throttled++
	})
}

// UpdateQueueSize updates the current queue size
func (sc *StatsCollector) UpdateQueueSize() {
	sc.UpdateStats(func(stats *types.DispatcherStats) {
		stats.QueueSize = len(sc.queue)
	})
}

// UpdateSinkDistribution updates sink distribution statistics
func (sc *StatsCollector) UpdateSinkDistribution(sinkType string, count int) {
	sc.UpdateStats(func(stats *types.DispatcherStats) {
		stats.SinkDistribution[sinkType] += int64(count)
	})
}
