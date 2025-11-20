package batching

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"ssw-logs-capture/pkg/types"

	"github.com/sirupsen/logrus"
)

// AdaptiveBatcher implements intelligent batching with dynamic sizing
type AdaptiveBatcher struct {
	config       AdaptiveBatchConfig
	logger       *logrus.Logger

	// Current batch settings
	currentBatchSize   int32
	currentFlushDelay  int64 // nanoseconds

	// Performance tracking
	averageLatency     int64 // nanoseconds
	throughputCounter  int64
	lastFlushTime      int64 // unix nanoseconds

	// Batch state
	batch          []*types.LogEntry
	batchMutex     sync.Mutex
	flushTimer     *time.Timer
	timerMutex     sync.Mutex

	// Control
	ctx        context.Context
	cancel     context.CancelFunc
	isRunning  bool
	flushChan  chan []*types.LogEntry

	// Statistics
	stats      BatchingStats
	statsMutex sync.RWMutex
}

// AdaptiveBatchConfig configuration for adaptive batching
type AdaptiveBatchConfig struct {
	MinBatchSize       int           `yaml:"min_batch_size"`
	MaxBatchSize       int           `yaml:"max_batch_size"`
	InitialBatchSize   int           `yaml:"initial_batch_size"`
	MinFlushDelay      time.Duration `yaml:"min_flush_delay"`
	MaxFlushDelay      time.Duration `yaml:"max_flush_delay"`
	InitialFlushDelay  time.Duration `yaml:"initial_flush_delay"`
	AdaptationInterval time.Duration `yaml:"adaptation_interval"`
	LatencyThreshold   time.Duration `yaml:"latency_threshold"`
	ThroughputTarget   int           `yaml:"throughput_target"`
	BufferSize         int           `yaml:"buffer_size"`
}

// BatchingStats statistics for batching performance
type BatchingStats struct {
	TotalBatches       int64   `json:"total_batches"`
	TotalItems         int64   `json:"total_items"`
	CurrentBatchSize   int32   `json:"current_batch_size"`
	CurrentFlushDelay  int64   `json:"current_flush_delay_ms"`
	AverageLatency     int64   `json:"average_latency_ms"`
	ThroughputPerSec   float64 `json:"throughput_per_sec"`
	AdaptationCount    int64   `json:"adaptation_count"`
	BackpressureEvents int64   `json:"backpressure_events"`
}

// NewAdaptiveBatcher creates a new adaptive batcher
func NewAdaptiveBatcher(config AdaptiveBatchConfig, logger *logrus.Logger) *AdaptiveBatcher {
	// Set defaults if not provided
	if config.MinBatchSize <= 0 {
		config.MinBatchSize = 10
	}
	if config.MaxBatchSize <= 0 {
		config.MaxBatchSize = 1000
	}
	if config.InitialBatchSize <= 0 {
		config.InitialBatchSize = 100
	}
	if config.MinFlushDelay == 0 {
		config.MinFlushDelay = 50 * time.Millisecond
	}
	if config.MaxFlushDelay == 0 {
		config.MaxFlushDelay = 10 * time.Second
	}
	if config.InitialFlushDelay == 0 {
		config.InitialFlushDelay = 1 * time.Second
	}
	if config.AdaptationInterval == 0 {
		config.AdaptationInterval = 30 * time.Second
	}
	if config.LatencyThreshold == 0 {
		config.LatencyThreshold = 500 * time.Millisecond
	}
	if config.ThroughputTarget <= 0 {
		config.ThroughputTarget = 1000
	}
	if config.BufferSize <= 0 {
		config.BufferSize = 10000
	}

	ctx, cancel := context.WithCancel(context.Background())

	batcher := &AdaptiveBatcher{
		config:            config,
		logger:            logger,
		currentBatchSize:  int32(config.InitialBatchSize),
		currentFlushDelay: int64(config.InitialFlushDelay),
		ctx:               ctx,
		cancel:            cancel,
		flushChan:         make(chan []*types.LogEntry, config.BufferSize/config.MaxBatchSize),
		lastFlushTime:     time.Now().UnixNano(),
		batch:             make([]*types.LogEntry, 0, config.MaxBatchSize),
	}

	return batcher
}

// Start starts the adaptive batcher
func (ab *AdaptiveBatcher) Start() error {
	ab.isRunning = true

	// Start adaptation loop
	go ab.adaptationLoop()

	ab.logger.Info("Adaptive batcher started")
	return nil
}

// Stop stops the adaptive batcher
func (ab *AdaptiveBatcher) Stop() error {
	if !ab.isRunning {
		return nil
	}

	ab.cancel()
	ab.isRunning = false

	// Flush remaining items
	ab.batchMutex.Lock()
	if len(ab.batch) > 0 {
		ab.flushBatchUnsafe()
	}
	ab.batchMutex.Unlock()

	close(ab.flushChan)
	ab.logger.Info("Adaptive batcher stopped")
	return nil
}

// Add adds an entry to the batch
// P1 FIX: Recebe ponteiro para evitar pass lock by value
func (ab *AdaptiveBatcher) Add(entry *types.LogEntry) error {
	if !ab.isRunning {
		return ErrBatcherStopped
	}

	ab.batchMutex.Lock()
	defer ab.batchMutex.Unlock()

	// Add to batch (usando ponteiro)
	ab.batch = append(ab.batch, entry)
	atomic.AddInt64(&ab.stats.TotalItems, 1)

	// Check if we should flush based on current batch size
	currentSize := int(atomic.LoadInt32(&ab.currentBatchSize))
	if len(ab.batch) >= currentSize {
		ab.flushBatchUnsafe()
		return nil
	}

	// Set or reset flush timer
	ab.resetFlushTimer()

	return nil
}

// GetBatch returns the next batch of entries (blocking)
// P1 FIX: Retorna ponteiros para evitar pass lock by value
func (ab *AdaptiveBatcher) GetBatch(ctx context.Context) ([]*types.LogEntry, error) {
	select {
	case batch := <-ab.flushChan:
		return batch, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-ab.ctx.Done():
		return nil, ErrBatcherStopped
	}
}

// TryGetBatch returns the next batch of entries (non-blocking)
// P1 FIX: Retorna ponteiros para evitar pass lock by value
func (ab *AdaptiveBatcher) TryGetBatch() ([]*types.LogEntry, bool) {
	select {
	case batch := <-ab.flushChan:
		return batch, true
	default:
		return nil, false
	}
}

// resetFlushTimer resets the flush timer with current delay
func (ab *AdaptiveBatcher) resetFlushTimer() {
	ab.timerMutex.Lock()
	defer ab.timerMutex.Unlock()

	if ab.flushTimer != nil {
		ab.flushTimer.Stop()
	}

	delay := time.Duration(atomic.LoadInt64(&ab.currentFlushDelay))
	ab.flushTimer = time.AfterFunc(delay, func() {
		ab.batchMutex.Lock()
		defer ab.batchMutex.Unlock()
		if len(ab.batch) > 0 {
			ab.flushBatchUnsafe()
		}
	})
}

// flushBatchUnsafe flushes the current batch (must be called with batchMutex held)
func (ab *AdaptiveBatcher) flushBatchUnsafe() {
	if len(ab.batch) == 0 {
		return
	}

	start := time.Now()

	// Create copy of batch for sending (ponteiros)
	batchCopy := make([]*types.LogEntry, len(ab.batch))
	copy(batchCopy, ab.batch)

	// Reset batch
	ab.batch = ab.batch[:0]

	// Try to send batch (non-blocking)
	select {
	case ab.flushChan <- batchCopy:
		// Successfully sent
		atomic.AddInt64(&ab.stats.TotalBatches, 1)

		// Update latency tracking
		latency := time.Since(start).Nanoseconds()
		ab.updateLatency(latency)

		// Update flush time
		atomic.StoreInt64(&ab.lastFlushTime, time.Now().UnixNano())

	default:
		// Channel full - backpressure
		atomic.AddInt64(&ab.stats.BackpressureEvents, 1)
		ab.logger.Warn("Batch channel full, dropping batch")

		// Put items back in batch to retry later
		ab.batch = append(ab.batch, batchCopy...)
	}

	// Stop flush timer
	ab.timerMutex.Lock()
	if ab.flushTimer != nil {
		ab.flushTimer.Stop()
		ab.flushTimer = nil
	}
	ab.timerMutex.Unlock()
}

// updateLatency updates the running average latency
func (ab *AdaptiveBatcher) updateLatency(latency int64) {
	// Simple exponential moving average
	currentAvg := atomic.LoadInt64(&ab.averageLatency)
	if currentAvg == 0 {
		atomic.StoreInt64(&ab.averageLatency, latency)
	} else {
		// Weight: 90% old, 10% new
		newAvg := (currentAvg*9 + latency) / 10
		atomic.StoreInt64(&ab.averageLatency, newAvg)
	}
}

// adaptationLoop continuously adapts batch size and flush delay based on performance
func (ab *AdaptiveBatcher) adaptationLoop() {
	ticker := time.NewTicker(ab.config.AdaptationInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ab.adaptParameters()
		case <-ab.ctx.Done():
			return
		}
	}
}

// adaptParameters adapts batch size and flush delay based on current performance
func (ab *AdaptiveBatcher) adaptParameters() {
	currentLatency := atomic.LoadInt64(&ab.averageLatency)
	currentThroughput := ab.calculateThroughput()

	// Get current parameters
	currentBatchSize := int(atomic.LoadInt32(&ab.currentBatchSize))
	currentFlushDelay := time.Duration(atomic.LoadInt64(&ab.currentFlushDelay))

	// Adaptation logic
	newBatchSize := currentBatchSize
	newFlushDelay := currentFlushDelay
	adapted := false

	// If latency is too high, reduce batch size and flush delay
	if currentLatency > int64(ab.config.LatencyThreshold) {
		if currentBatchSize > ab.config.MinBatchSize {
			newBatchSize = maxInt(ab.config.MinBatchSize, currentBatchSize*8/10) // Reduce by 20%
			adapted = true
		}
		if currentFlushDelay > ab.config.MinFlushDelay {
			newFlushDelay = maxDuration(ab.config.MinFlushDelay, currentFlushDelay*8/10) // Reduce by 20%
			adapted = true
		}
	} else if currentThroughput < float64(ab.config.ThroughputTarget) {
		// If throughput is low and latency is acceptable, increase batch size
		if currentBatchSize < ab.config.MaxBatchSize {
			newBatchSize = minInt(ab.config.MaxBatchSize, currentBatchSize*12/10) // Increase by 20%
			adapted = true
		}
		if currentFlushDelay < ab.config.MaxFlushDelay {
			newFlushDelay = minDuration(ab.config.MaxFlushDelay, currentFlushDelay*11/10) // Increase by 10%
			adapted = true
		}
	}

	// Apply changes if adapted
	if adapted {
		atomic.StoreInt32(&ab.currentBatchSize, int32(newBatchSize))
		atomic.StoreInt64(&ab.currentFlushDelay, int64(newFlushDelay))
		atomic.AddInt64(&ab.stats.AdaptationCount, 1)

		ab.logger.WithFields(logrus.Fields{
			"old_batch_size":    currentBatchSize,
			"new_batch_size":    newBatchSize,
			"old_flush_delay":   currentFlushDelay,
			"new_flush_delay":   newFlushDelay,
			"current_latency":   time.Duration(currentLatency),
			"current_throughput": currentThroughput,
		}).Debug("Adapted batching parameters")
	}
}

// calculateThroughput calculates current throughput in items per second
func (ab *AdaptiveBatcher) calculateThroughput() float64 {
	now := time.Now().UnixNano()
	lastFlush := atomic.LoadInt64(&ab.lastFlushTime)

	if lastFlush == 0 {
		return 0
	}

	timeDiff := float64(now - lastFlush) / 1e9 // Convert to seconds
	if timeDiff == 0 {
		return 0
	}

	totalItems := atomic.LoadInt64(&ab.stats.TotalItems)
	atomic.StoreInt64(&ab.throughputCounter, totalItems)

	return float64(totalItems) / timeDiff
}

// GetStats returns current batching statistics
func (ab *AdaptiveBatcher) GetStats() BatchingStats {
	ab.statsMutex.RLock()
	defer ab.statsMutex.RUnlock()

	stats := ab.stats
	stats.CurrentBatchSize = atomic.LoadInt32(&ab.currentBatchSize)
	stats.CurrentFlushDelay = atomic.LoadInt64(&ab.currentFlushDelay) / 1e6 // Convert to milliseconds
	stats.AverageLatency = atomic.LoadInt64(&ab.averageLatency) / 1e6        // Convert to milliseconds
	stats.ThroughputPerSec = ab.calculateThroughput()

	return stats
}

// Helper functions
func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Errors
var (
	ErrBatcherStopped = fmt.Errorf("batcher is stopped")
)