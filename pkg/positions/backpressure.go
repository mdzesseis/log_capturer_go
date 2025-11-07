package positions

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"ssw-logs-capture/internal/metrics"
)

// BackpressureConfig holds configuration for backpressure detection
type BackpressureConfig struct {
	Enabled               bool          `yaml:"enabled" json:"enabled"`
	CheckInterval         time.Duration `yaml:"check_interval" json:"check_interval"`
	HighThreshold         float64       `yaml:"high_threshold" json:"high_threshold"`     // 0.8 = 80%
	CriticalThreshold     float64       `yaml:"critical_threshold" json:"critical_threshold"` // 0.95 = 95%
	AutoFlushOnHigh       bool          `yaml:"auto_flush_on_high" json:"auto_flush_on_high"`
	AutoFlushOnCritical   bool          `yaml:"auto_flush_on_critical" json:"auto_flush_on_critical"`
	SlowDownOnCritical    bool          `yaml:"slow_down_on_critical" json:"slow_down_on_critical"`
	MaxQueueSize          int           `yaml:"max_queue_size" json:"max_queue_size"`
	RateWindow            time.Duration `yaml:"rate_window" json:"rate_window"` // Window for rate calculation
}

// BackpressureDetector monitors and detects backpressure in position system
type BackpressureDetector struct {
	mu                    sync.RWMutex
	config                *BackpressureConfig
	logger                *logrus.Logger
	ctx                   context.Context
	cancel                context.CancelFunc
	wg                    sync.WaitGroup

	// Metrics tracking
	updateCount           int64
	saveCount             int64
	lastUpdateCount       int64
	lastSaveCount         int64
	lastCheckTime         time.Time

	// Current state
	currentBackpressure   float64
	backpressureLevel     string // none|low|high|critical
	queueUtilization      float64
	updateRate            float64 // updates/sec
	saveRate              float64 // saves/sec

	// Callback for auto-flush
	flushCallback         func() error

	stats struct {
		mu                        sync.RWMutex
		totalChecks               int64
		highBackpressureEvents    int64
		criticalBackpressureEvents int64
		autoFlushTriggered        int64
		slowDownTriggered         int64
	}
}

// NewBackpressureDetector creates a new backpressure detector
func NewBackpressureDetector(config *BackpressureConfig, logger *logrus.Logger) *BackpressureDetector {
	if config == nil {
		config = &BackpressureConfig{
			Enabled:             true,
			CheckInterval:       1 * time.Second,
			HighThreshold:       0.8,
			CriticalThreshold:   0.95,
			AutoFlushOnHigh:     true,
			AutoFlushOnCritical: true,
			SlowDownOnCritical:  false,
			MaxQueueSize:        10000,
			RateWindow:          10 * time.Second,
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &BackpressureDetector{
		config:            config,
		logger:            logger,
		ctx:               ctx,
		cancel:            cancel,
		lastCheckTime:     time.Now(),
		backpressureLevel: "none",
	}
}

// Start begins backpressure monitoring
func (bd *BackpressureDetector) Start() error {
	if !bd.config.Enabled {
		bd.logger.Info("Backpressure detector disabled", nil)
		return nil
	}

	bd.logger.Info("Starting backpressure detector", map[string]interface{}{
		"check_interval":      bd.config.CheckInterval.String(),
		"high_threshold":      bd.config.HighThreshold,
		"critical_threshold":  bd.config.CriticalThreshold,
		"auto_flush_on_high":  bd.config.AutoFlushOnHigh,
	})

	bd.wg.Add(1)
	go bd.monitorLoop()

	return nil
}

// Stop stops the backpressure detector
func (bd *BackpressureDetector) Stop() error {
	if !bd.config.Enabled {
		return nil
	}

	bd.logger.Info("Stopping backpressure detector", nil)
	bd.cancel()
	bd.wg.Wait()

	bd.logger.Info("Backpressure detector stopped", nil)
	return nil
}

// monitorLoop periodically checks for backpressure
func (bd *BackpressureDetector) monitorLoop() {
	defer bd.wg.Done()

	ticker := time.NewTicker(bd.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-bd.ctx.Done():
			return
		case <-ticker.C:
			bd.checkBackpressure()
		}
	}
}

// checkBackpressure calculates current backpressure and takes action if needed
func (bd *BackpressureDetector) checkBackpressure() {
	bd.mu.Lock()
	defer bd.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(bd.lastCheckTime).Seconds()

	if elapsed == 0 {
		return
	}

	// Calculate rates (operations per second)
	updateDelta := bd.updateCount - bd.lastUpdateCount
	saveDelta := bd.saveCount - bd.lastSaveCount

	bd.updateRate = float64(updateDelta) / elapsed
	bd.saveRate = float64(saveDelta) / elapsed

	// Calculate backpressure based on multiple factors
	backpressure := bd.calculateBackpressure()

	// Update state
	bd.currentBackpressure = backpressure
	bd.lastUpdateCount = bd.updateCount
	bd.lastSaveCount = bd.saveCount
	bd.lastCheckTime = now

	// Determine backpressure level
	previousLevel := bd.backpressureLevel
	if backpressure >= bd.config.CriticalThreshold {
		bd.backpressureLevel = "critical"
	} else if backpressure >= bd.config.HighThreshold {
		bd.backpressureLevel = "high"
	} else if backpressure >= 0.5 {
		bd.backpressureLevel = "low"
	} else {
		bd.backpressureLevel = "none"
	}

	// Update statistics
	bd.stats.mu.Lock()
	bd.stats.totalChecks++
	if bd.backpressureLevel == "high" && previousLevel != "high" && previousLevel != "critical" {
		bd.stats.highBackpressureEvents++
	}
	if bd.backpressureLevel == "critical" && previousLevel != "critical" {
		bd.stats.criticalBackpressureEvents++
	}
	bd.stats.mu.Unlock()

	// Record metrics for both file and container managers
	metrics.UpdatePositionBackpressure("file", backpressure)
	metrics.UpdatePositionBackpressure("container", backpressure)

	// Log if level changed
	if previousLevel != bd.backpressureLevel && bd.backpressureLevel != "none" {
		bd.logger.Warn("Backpressure level changed", map[string]interface{}{
			"previous_level":    previousLevel,
			"current_level":     bd.backpressureLevel,
			"backpressure":      backpressure,
			"update_rate":       bd.updateRate,
			"save_rate":         bd.saveRate,
			"queue_utilization": bd.queueUtilization,
		})
	}

	// Take action based on backpressure level
	bd.takeAction()
}

// calculateBackpressure computes backpressure indicator (0.0 to 1.0+)
func (bd *BackpressureDetector) calculateBackpressure() float64 {
	var factors []float64

	// Factor 1: Queue utilization (if available)
	if bd.queueUtilization > 0 {
		factors = append(factors, bd.queueUtilization)
	}

	// Factor 2: Update rate vs save rate ratio
	if bd.saveRate > 0 {
		rateRatio := bd.updateRate / bd.saveRate
		// Normalize: ratio of 1.0 = 0 backpressure, ratio > 2.0 = high backpressure
		if rateRatio > 1.0 {
			rateFactor := math.Min((rateRatio - 1.0) / 2.0, 1.0)
			factors = append(factors, rateFactor)
		}
	}

	// Factor 3: Absolute update rate (high update rate = potential backpressure)
	if bd.updateRate > 1000 {
		// More than 1000 updates/sec is considered high
		highRateFactor := math.Min((bd.updateRate - 1000) / 5000, 1.0)
		factors = append(factors, highRateFactor)
	}

	// If no factors, no backpressure
	if len(factors) == 0 {
		return 0.0
	}

	// Return the maximum factor (most severe backpressure indicator)
	maxFactor := 0.0
	for _, factor := range factors {
		if factor > maxFactor {
			maxFactor = factor
		}
	}

	return math.Min(maxFactor, 1.0) // Clamp to [0, 1]
}

// takeAction performs actions based on backpressure level
func (bd *BackpressureDetector) takeAction() {
	switch bd.backpressureLevel {
	case "critical":
		if bd.config.AutoFlushOnCritical && bd.flushCallback != nil {
			bd.logger.Warn("Critical backpressure detected, triggering immediate flush", map[string]interface{}{
				"backpressure": bd.currentBackpressure,
			})

			bd.stats.mu.Lock()
			bd.stats.autoFlushTriggered++
			bd.stats.mu.Unlock()

			// Release lock before calling callback to avoid deadlock
			bd.mu.Unlock()
			if err := bd.flushCallback(); err != nil {
				bd.logger.Error("Auto-flush failed", map[string]interface{}{
					"error": err.Error(),
				})
			}
			bd.mu.Lock() // Re-acquire lock
		}

		if bd.config.SlowDownOnCritical {
			bd.logger.Warn("Critical backpressure: slowing down updates", nil)
			bd.stats.mu.Lock()
			bd.stats.slowDownTriggered++
			bd.stats.mu.Unlock()
			// Note: Actual slowdown would be implemented in buffer_manager
		}

	case "high":
		if bd.config.AutoFlushOnHigh && bd.flushCallback != nil {
			bd.logger.Warn("High backpressure detected, triggering flush", map[string]interface{}{
				"backpressure": bd.currentBackpressure,
			})

			bd.stats.mu.Lock()
			bd.stats.autoFlushTriggered++
			bd.stats.mu.Unlock()

			bd.mu.Unlock()
			if err := bd.flushCallback(); err != nil {
				bd.logger.Error("Auto-flush failed", map[string]interface{}{
					"error": err.Error(),
				})
			}
			bd.mu.Lock()
		}
	}
}

// RecordUpdate increments the update counter
func (bd *BackpressureDetector) RecordUpdate() {
	bd.mu.Lock()
	defer bd.mu.Unlock()
	bd.updateCount++
}

// RecordSave increments the save counter
func (bd *BackpressureDetector) RecordSave() {
	bd.mu.Lock()
	defer bd.mu.Unlock()
	bd.saveCount++
}

// UpdateQueueUtilization updates the queue utilization metric
func (bd *BackpressureDetector) UpdateQueueUtilization(current, max int) {
	bd.mu.Lock()
	defer bd.mu.Unlock()

	if max > 0 {
		bd.queueUtilization = float64(current) / float64(max)
	} else {
		bd.queueUtilization = 0
	}
}

// GetBackpressure returns the current backpressure value
func (bd *BackpressureDetector) GetBackpressure() float64 {
	bd.mu.RLock()
	defer bd.mu.RUnlock()
	return bd.currentBackpressure
}

// GetBackpressureLevel returns the current backpressure level
func (bd *BackpressureDetector) GetBackpressureLevel() string {
	bd.mu.RLock()
	defer bd.mu.RUnlock()
	return bd.backpressureLevel
}

// SetFlushCallback sets the callback function for auto-flush
func (bd *BackpressureDetector) SetFlushCallback(callback func() error) {
	bd.mu.Lock()
	defer bd.mu.Unlock()
	bd.flushCallback = callback
}

// GetStats returns detector statistics
func (bd *BackpressureDetector) GetStats() map[string]interface{} {
	bd.mu.RLock()
	backpressure := bd.currentBackpressure
	level := bd.backpressureLevel
	updateRate := bd.updateRate
	saveRate := bd.saveRate
	queueUtil := bd.queueUtilization
	bd.mu.RUnlock()

	bd.stats.mu.RLock()
	defer bd.stats.mu.RUnlock()

	return map[string]interface{}{
		"enabled":                      bd.config.Enabled,
		"current_backpressure":         backpressure,
		"backpressure_level":           level,
		"update_rate_per_second":       updateRate,
		"save_rate_per_second":         saveRate,
		"queue_utilization":            queueUtil,
		"total_checks":                 bd.stats.totalChecks,
		"high_backpressure_events":     bd.stats.highBackpressureEvents,
		"critical_backpressure_events": bd.stats.criticalBackpressureEvents,
		"auto_flush_triggered":         bd.stats.autoFlushTriggered,
		"slow_down_triggered":          bd.stats.slowDownTriggered,
		"high_threshold":               bd.config.HighThreshold,
		"critical_threshold":           bd.config.CriticalThreshold,
	}
}

// IsBackpressureHigh returns true if backpressure is high or critical
func (bd *BackpressureDetector) IsBackpressureHigh() bool {
	bd.mu.RLock()
	defer bd.mu.RUnlock()
	return bd.backpressureLevel == "high" || bd.backpressureLevel == "critical"
}

// ResetCounters resets update and save counters (useful for testing)
func (bd *BackpressureDetector) ResetCounters() {
	bd.mu.Lock()
	defer bd.mu.Unlock()

	bd.updateCount = 0
	bd.saveCount = 0
	bd.lastUpdateCount = 0
	bd.lastSaveCount = 0
	bd.lastCheckTime = time.Now()
}
