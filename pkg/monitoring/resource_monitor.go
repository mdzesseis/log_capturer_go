// Package monitoring provides system resource monitoring capabilities
package monitoring

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
)

// ResourceMonitor monitors system resources (goroutines, memory, file descriptors)
type ResourceMonitor struct {
	config        Config
	logger        *logrus.Logger
	metrics       Metrics
	metricsMutex  sync.RWMutex
	alerts        chan Alert
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
}

// Config holds configuration for resource monitoring
type Config struct {
	Enabled             bool          `yaml:"enabled"`               // Enable resource monitoring
	CheckInterval       time.Duration `yaml:"check_interval"`        // How often to check resources
	GoroutineThreshold  int           `yaml:"goroutine_threshold"`   // Alert when goroutines exceed this
	MemoryThresholdMB   int64         `yaml:"memory_threshold_mb"`   // Alert when memory exceeds this (MB)
	FDThreshold         int           `yaml:"fd_threshold"`          // Alert when file descriptors exceed this
	GrowthRateThreshold float64       `yaml:"growth_rate_threshold"` // Alert when growth rate exceeds this (%)
	AlertWebhookURL     string        `yaml:"alert_webhook_url"`     // Webhook URL for alerts
	AlertOnThreshold    bool          `yaml:"alert_on_threshold"`    // Send alerts when thresholds exceeded
}

// Metrics holds current resource metrics
type Metrics struct {
	Timestamp       time.Time `json:"timestamp"`
	Goroutines      int       `json:"goroutines"`
	MemoryAllocMB   int64     `json:"memory_alloc_mb"`
	MemoryTotalMB   int64     `json:"memory_total_mb"`
	MemorySysMB     int64     `json:"memory_sys_mb"`
	GCPauseMS       float64   `json:"gc_pause_ms"`
	FileDescriptors int       `json:"file_descriptors"`
	HeapObjects     uint64    `json:"heap_objects"`
	GoroutineGrowth float64   `json:"goroutine_growth"` // Growth rate in last interval
	MemoryGrowth    float64   `json:"memory_growth"`    // Growth rate in last interval
}

// Alert represents a resource alert
type Alert struct {
	Timestamp   time.Time       `json:"timestamp"`
	Type        string          `json:"type"`    // "goroutine", "memory", "fd", "growth"
	Severity    string          `json:"severity"` // "warning", "critical"
	Message     string          `json:"message"`
	CurrentValue interface{}    `json:"current_value"`
	Threshold   interface{}     `json:"threshold"`
	Metrics     Metrics         `json:"metrics"`
}

// NewResourceMonitor creates a new resource monitor instance
func NewResourceMonitor(config Config, logger *logrus.Logger) *ResourceMonitor {
	ctx, cancel := context.WithCancel(context.Background())

	return &ResourceMonitor{
		config:  config,
		logger:  logger,
		alerts:  make(chan Alert, 100), // Buffered channel for alerts
		ctx:     ctx,
		cancel:  cancel,
	}
}

// Start begins resource monitoring
func (rm *ResourceMonitor) Start() error {
	if !rm.config.Enabled {
		rm.logger.Info("Resource monitoring disabled")
		return nil
	}

	rm.logger.WithFields(logrus.Fields{
		"check_interval":       rm.config.CheckInterval,
		"goroutine_threshold":  rm.config.GoroutineThreshold,
		"memory_threshold_mb":  rm.config.MemoryThresholdMB,
		"fd_threshold":         rm.config.FDThreshold,
	}).Info("Starting resource monitor")

	// Start monitoring goroutine
	rm.wg.Add(1)
	go rm.monitorResources()

	// Start alert processor goroutine
	if rm.config.AlertOnThreshold {
		rm.wg.Add(1)
		go rm.processAlerts()
	}

	return nil
}

// Stop stops resource monitoring
func (rm *ResourceMonitor) Stop() error {
	if !rm.config.Enabled {
		return nil
	}

	rm.logger.Info("Stopping resource monitor")
	rm.cancel()

	// Wait for goroutines to finish with timeout
	done := make(chan struct{})
	go func() {
		rm.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		rm.logger.Info("Resource monitor stopped cleanly")
	case <-time.After(5 * time.Second):
		rm.logger.Warn("Timeout waiting for resource monitor to stop")
	}

	close(rm.alerts)
	return nil
}

// monitorResources runs periodic resource checks
func (rm *ResourceMonitor) monitorResources() {
	defer rm.wg.Done()

	ticker := time.NewTicker(rm.config.CheckInterval)
	defer ticker.Stop()

	var previousMetrics Metrics

	for {
		select {
		case <-rm.ctx.Done():
			return

		case <-ticker.C:
			currentMetrics := rm.collectMetrics()

			// Calculate growth rates
			if previousMetrics.Timestamp.IsZero() {
				previousMetrics = currentMetrics
				continue
			}

			currentMetrics.GoroutineGrowth = rm.calculateGrowthRate(
				previousMetrics.Goroutines,
				currentMetrics.Goroutines,
			)

			currentMetrics.MemoryGrowth = rm.calculateGrowthRate(
				int(previousMetrics.MemoryAllocMB),
				int(currentMetrics.MemoryAllocMB),
			)

			// Store metrics
			rm.metricsMutex.Lock()
			rm.metrics = currentMetrics
			rm.metricsMutex.Unlock()

			// Check thresholds and generate alerts
			rm.checkThresholds(currentMetrics)

			// Log metrics
			rm.logger.WithFields(logrus.Fields{
				"goroutines":        currentMetrics.Goroutines,
				"memory_alloc_mb":   currentMetrics.MemoryAllocMB,
				"file_descriptors":  currentMetrics.FileDescriptors,
				"goroutine_growth":  fmt.Sprintf("%.2f%%", currentMetrics.GoroutineGrowth),
				"memory_growth":     fmt.Sprintf("%.2f%%", currentMetrics.MemoryGrowth),
			}).Debug("Resource metrics collected")

			previousMetrics = currentMetrics
		}
	}
}

// collectMetrics collects current system metrics
func (rm *ResourceMonitor) collectMetrics() Metrics {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	metrics := Metrics{
		Timestamp:   time.Now().UTC(),
		Goroutines:      runtime.NumGoroutine(),
		MemoryAllocMB:   int64(memStats.Alloc / 1024 / 1024),
		MemoryTotalMB:   int64(memStats.TotalAlloc / 1024 / 1024),
		MemorySysMB:     int64(memStats.Sys / 1024 / 1024),
		GCPauseMS:       float64(memStats.PauseNs[(memStats.NumGC+255)%256]) / 1e6,
		HeapObjects:     memStats.HeapObjects,
		FileDescriptors: rm.getFileDescriptorCount(),
	}

	return metrics
}

// getFileDescriptorCount returns current file descriptor count (Linux/Unix)
func (rm *ResourceMonitor) getFileDescriptorCount() int {
	var rLimit syscall.Rlimit
	err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rLimit)
	if err != nil {
		return -1
	}

	// Note: This is the limit, not the current count
	// For actual count, would need to read /proc/self/fd/ on Linux
	// For now, returning -1 to indicate not implemented
	return -1
}

// calculateGrowthRate calculates percentage growth rate
func (rm *ResourceMonitor) calculateGrowthRate(previous, current int) float64 {
	if previous == 0 {
		return 0
	}
	return ((float64(current) - float64(previous)) / float64(previous)) * 100.0
}

// checkThresholds checks if any thresholds are exceeded
func (rm *ResourceMonitor) checkThresholds(metrics Metrics) {
	// Check goroutine threshold
	if rm.config.GoroutineThreshold > 0 && metrics.Goroutines > rm.config.GoroutineThreshold {
		rm.sendAlert(Alert{
			Timestamp:   time.Now().UTC(),
			Type:         "goroutine",
			Severity:     rm.determineSeverity(metrics.Goroutines, rm.config.GoroutineThreshold),
			Message:      fmt.Sprintf("Goroutine count (%d) exceeded threshold (%d)", metrics.Goroutines, rm.config.GoroutineThreshold),
			CurrentValue: metrics.Goroutines,
			Threshold:    rm.config.GoroutineThreshold,
			Metrics:      metrics,
		})
	}

	// Check memory threshold
	if rm.config.MemoryThresholdMB > 0 && metrics.MemoryAllocMB > rm.config.MemoryThresholdMB {
		rm.sendAlert(Alert{
			Timestamp:   time.Now().UTC(),
			Type:         "memory",
			Severity:     rm.determineSeverity(int(metrics.MemoryAllocMB), int(rm.config.MemoryThresholdMB)),
			Message:      fmt.Sprintf("Memory usage (%d MB) exceeded threshold (%d MB)", metrics.MemoryAllocMB, rm.config.MemoryThresholdMB),
			CurrentValue: metrics.MemoryAllocMB,
			Threshold:    rm.config.MemoryThresholdMB,
			Metrics:      metrics,
		})
	}

	// Check file descriptor threshold
	if rm.config.FDThreshold > 0 && metrics.FileDescriptors > rm.config.FDThreshold {
		rm.sendAlert(Alert{
			Timestamp:   time.Now().UTC(),
			Type:         "file_descriptor",
			Severity:     "warning",
			Message:      fmt.Sprintf("File descriptor count (%d) exceeded threshold (%d)", metrics.FileDescriptors, rm.config.FDThreshold),
			CurrentValue: metrics.FileDescriptors,
			Threshold:    rm.config.FDThreshold,
			Metrics:      metrics,
		})
	}

	// Check growth rate thresholds
	if rm.config.GrowthRateThreshold > 0 {
		if metrics.GoroutineGrowth > rm.config.GrowthRateThreshold {
			rm.sendAlert(Alert{
				Timestamp:   time.Now().UTC(),
				Type:         "growth",
				Severity:     "warning",
				Message:      fmt.Sprintf("Goroutine growth rate (%.2f%%) exceeded threshold (%.2f%%)", metrics.GoroutineGrowth, rm.config.GrowthRateThreshold),
				CurrentValue: metrics.GoroutineGrowth,
				Threshold:    rm.config.GrowthRateThreshold,
				Metrics:      metrics,
			})
		}

		if metrics.MemoryGrowth > rm.config.GrowthRateThreshold {
			rm.sendAlert(Alert{
				Timestamp:   time.Now().UTC(),
				Type:         "growth",
				Severity:     "warning",
				Message:      fmt.Sprintf("Memory growth rate (%.2f%%) exceeded threshold (%.2f%%)", metrics.MemoryGrowth, rm.config.GrowthRateThreshold),
				CurrentValue: metrics.MemoryGrowth,
				Threshold:    rm.config.GrowthRateThreshold,
				Metrics:      metrics,
			})
		}
	}
}

// determineSeverity determines alert severity based on how much threshold is exceeded
func (rm *ResourceMonitor) determineSeverity(current, threshold int) string {
	ratio := float64(current) / float64(threshold)
	if ratio > 2.0 {
		return "critical"
	} else if ratio > 1.5 {
		return "high"
	}
	return "warning"
}

// sendAlert sends an alert to the alert channel
func (rm *ResourceMonitor) sendAlert(alert Alert) {
	select {
	case rm.alerts <- alert:
		// Alert sent successfully
	default:
		// Alert channel full, log warning
		rm.logger.Warn("Alert channel full, dropping alert")
	}
}

// processAlerts processes alerts from the channel
func (rm *ResourceMonitor) processAlerts() {
	defer rm.wg.Done()

	for {
		select {
		case <-rm.ctx.Done():
			return

		case alert, ok := <-rm.alerts:
			if !ok {
				return // Channel closed
			}

			// Log alert
			rm.logger.WithFields(logrus.Fields{
				"type":          alert.Type,
				"severity":      alert.Severity,
				"current_value": alert.CurrentValue,
				"threshold":     alert.Threshold,
			}).Warn(alert.Message)

			// TODO: Send to webhook if configured
			// if rm.config.AlertWebhookURL != "" {
			//     rm.sendWebhookAlert(alert)
			// }
		}
	}
}

// GetMetrics returns current metrics (thread-safe)
func (rm *ResourceMonitor) GetMetrics() Metrics {
	rm.metricsMutex.RLock()
	defer rm.metricsMutex.RUnlock()
	return rm.metrics
}

// GetAlertChannel returns the alert channel for external consumers
func (rm *ResourceMonitor) GetAlertChannel() <-chan Alert {
	return rm.alerts
}
