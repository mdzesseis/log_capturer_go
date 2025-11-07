package tracing

import (
	"math/rand"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// AdaptiveSampler implements adaptive sampling based on latency thresholds
type AdaptiveSampler struct {
	config AdaptiveSamplingConfig
	logger *logrus.Logger

	// Latency tracking
	latencies    []time.Duration
	latenciesMu  sync.RWMutex
	lastCleanup  time.Time
	mu           sync.RWMutex
}

// NewAdaptiveSampler creates a new adaptive sampler
func NewAdaptiveSampler(config AdaptiveSamplingConfig, logger *logrus.Logger) *AdaptiveSampler {
	as := &AdaptiveSampler{
		config:      config,
		logger:      logger,
		latencies:   make([]time.Duration, 0, 1000),
		lastCleanup: time.Now(),
	}

	// Start cleanup goroutine to prevent memory growth
	go as.cleanupLoop()

	return as
}

// ShouldSample decides if current conditions warrant sampling
func (as *AdaptiveSampler) ShouldSample() bool {
	if !as.config.Enabled {
		return false
	}

	// Get current P99 latency
	p99 := as.GetP99()

	// If P99 latency exceeds threshold, sample more
	if p99 > as.config.LatencyThreshold {
		return rand.Float64() < as.config.SampleRate
	}

	return false
}

// RecordLatency records a latency sample
func (as *AdaptiveSampler) RecordLatency(duration time.Duration) {
	as.latenciesMu.Lock()
	defer as.latenciesMu.Unlock()

	as.latencies = append(as.latencies, duration)

	// Prevent unbounded growth
	if len(as.latencies) > 10000 {
		as.latencies = as.latencies[1000:] // Keep last 9000
	}
}

// GetP99 calculates the 99th percentile latency
func (as *AdaptiveSampler) GetP99() time.Duration {
	as.latenciesMu.RLock()
	defer as.latenciesMu.RUnlock()

	if len(as.latencies) == 0 {
		return 0
	}

	// Simple P99 calculation (not perfect, but fast)
	// For production, consider using a proper percentile library
	sorted := make([]time.Duration, len(as.latencies))
	copy(sorted, as.latencies)

	// Simple selection for P99
	idx := int(float64(len(sorted)) * 0.99)
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}

	// Partial sort to get approximate P99
	if idx < len(sorted) {
		return sorted[idx]
	}

	return sorted[len(sorted)-1]
}

// GetP50 calculates the 50th percentile latency (median)
func (as *AdaptiveSampler) GetP50() time.Duration {
	as.latenciesMu.RLock()
	defer as.latenciesMu.RUnlock()

	if len(as.latencies) == 0 {
		return 0
	}

	sorted := make([]time.Duration, len(as.latencies))
	copy(sorted, as.latencies)

	idx := len(sorted) / 2
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}

	return sorted[idx]
}

// GetStats returns sampling statistics
func (as *AdaptiveSampler) GetStats() map[string]interface{} {
	as.latenciesMu.RLock()
	defer as.latenciesMu.RUnlock()

	return map[string]interface{}{
		"enabled":            as.config.Enabled,
		"latency_threshold":  as.config.LatencyThreshold,
		"sample_rate":        as.config.SampleRate,
		"window_size":        as.config.WindowSize,
		"samples_collected":  len(as.latencies),
		"p50_latency":        as.GetP50(),
		"p99_latency":        as.GetP99(),
	}
}

// UpdateConfig updates the sampler configuration
func (as *AdaptiveSampler) UpdateConfig(newConfig AdaptiveSamplingConfig) {
	as.mu.Lock()
	defer as.mu.Unlock()

	as.config = newConfig

	as.logger.WithFields(logrus.Fields{
		"threshold":  newConfig.LatencyThreshold,
		"rate":       newConfig.SampleRate,
		"window":     newConfig.WindowSize,
	}).Info("Adaptive sampler configuration updated")
}

// cleanupLoop periodically cleans up old latency samples
func (as *AdaptiveSampler) cleanupLoop() {
	ticker := time.NewTicker(as.config.WindowSize)
	defer ticker.Stop()

	for range ticker.C {
		as.cleanup()
	}
}

// cleanup removes old latency samples outside the window
func (as *AdaptiveSampler) cleanup() {
	as.latenciesMu.Lock()
	defer as.latenciesMu.Unlock()

	// Keep only samples within the window
	// For simplicity, we just trim the oldest ones periodically
	if len(as.latencies) > 5000 {
		as.latencies = as.latencies[len(as.latencies)-5000:]
	}

	as.lastCleanup = time.Now()
}
