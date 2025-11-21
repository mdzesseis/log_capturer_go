// Package dispatcher - Batch processing component
package dispatcher

import (
	"context"
	"fmt"
	"time"

	"ssw-logs-capture/internal/metrics"
	"ssw-logs-capture/pkg/types"

	"github.com/sirupsen/logrus"
)

// BatchProcessor handles batch collection and processing logic
type BatchProcessor struct {
	config          DispatcherConfig
	logger          *logrus.Logger
	enhancedMetrics *metrics.EnhancedMetrics
}

// NewBatchProcessor creates a new batch processor instance
func NewBatchProcessor(config DispatcherConfig, logger *logrus.Logger, enhancedMetrics *metrics.EnhancedMetrics) *BatchProcessor {
	return &BatchProcessor{
		config:          config,
		logger:          logger,
		enhancedMetrics: enhancedMetrics,
	}
}

// shallowCopyBatch creates a slice of LogEntry from dispatchItems.
// CRITICAL OPTIMIZATION: It does NOT perform a deep copy of the underlying Maps (Labels).
// It relies on the contract that Sinks must treat the LogEntry as READ-ONLY
// or perform their own copy if mutation is required.
//
// This reduces GC pressure significantly during high throughput.
func shallowCopyBatch(batch []dispatchItem) []types.LogEntry {
	result := make([]types.LogEntry, len(batch))
	for i, item := range batch {
		// We dereference item.Entry to get a struct copy,
		// but the map pointers (Labels) inside are shared.
		if item.Entry != nil {
			result[i] = *item.Entry
		}
	}
	return result
}

// ProcessBatch processes a batch of dispatch items and sends to sinks
func (bp *BatchProcessor) ProcessBatch(
	ctx context.Context,
	batch []dispatchItem,
	sinks []types.Sink,
	anomalyDetector interface{},
) (successCount, healthySinks int, lastErr error) {

	if len(batch) == 0 {
		return 0, 0, nil
	}

	startTime := time.Now()

	// PERFORMANCE OPTIMIZATION: Create ONE shallow copy for all sinks.
	// This reduces allocations from O(N*Sinks) to O(N).
	entries := shallowCopyBatch(batch)

	// Send to all healthy sinks
	for _, sink := range sinks {
		if !sink.IsHealthy() {
			// Logic to log occasionally could be added here to reduce noise
			continue
		}

		healthySinks++

		sendCtx, cancel := context.WithTimeout(ctx, 120*time.Second)

		// We pass the SHARED 'entries' slice.
		// Sinks MUST NOT modify these entries.
		err := sink.Send(sendCtx, entries)
		cancel()

		if err != nil {
			bp.logger.WithError(err).Error("Failed to send batch to sink")
			lastErr = err
		} else {
			successCount++
		}
	}

	duration := time.Since(startTime)

	// Record metrics
	metrics.RecordProcessingDuration("dispatcher", "batch_processing", duration)

	if bp.enhancedMetrics != nil {
		bp.enhancedMetrics.RecordBatchingStats("dispatcher", "batch_size", float64(len(batch)))
		bp.enhancedMetrics.RecordBatchingStats("dispatcher", "flush_time", float64(duration.Milliseconds()))

		fillRate := (float64(len(batch)) / float64(bp.config.BatchSize)) * 100.0
		bp.enhancedMetrics.RecordBatchingStats("dispatcher", "batch_fill_rate", fillRate)
	}

	bp.logger.WithFields(logrus.Fields{
		"batch_size":    len(batch),
		"success_count": successCount,
		"duration_ms":   duration.Milliseconds(),
	}).Debug("Batch processed")

	return successCount, healthySinks, lastErr
}

// CollectBatch collects items from queue into a batch
//
// This method implements adaptive batching:
//   - Collects up to BatchSize items
//   - Returns early on timeout (BatchTimeout)
//   - Returns early on context cancellation
//
// Returns collected batch and a boolean indicating if timeout occurred
func (bp *BatchProcessor) CollectBatch(
	ctx context.Context,
	queue <-chan dispatchItem,
) ([]dispatchItem, bool) {

	batch := make([]dispatchItem, 0, bp.config.BatchSize)

	// Optimization: Reuse timer pattern or use Ticker in caller if possible.
	// For now, keeping Timer but ensuring clean stop.
	timer := time.NewTimer(bp.config.BatchTimeout)
	defer func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}()

	// Collect first item (blocking)
	select {
	case <-ctx.Done():
		return batch, false
	case item := <-queue:
		batch = append(batch, item)
	case <-timer.C:
		return batch, true
	}

	// Reset timer after first item - reuse the existing timer
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(bp.config.BatchTimeout)

	// Collect remaining items (non-blocking until batch full or timeout)
	for {
		if len(batch) >= bp.config.BatchSize {
			return batch, false // Batch full
		}

		select {
		case <-ctx.Done():
			return batch, false
		case item := <-queue:
			batch = append(batch, item)
		case <-timer.C:
			return batch, true // Timeout
		}
	}
}

// ValidateBatch validates a batch of entries before processing
func (bp *BatchProcessor) ValidateBatch(batch []dispatchItem) error {
	if len(batch) == 0 {
		return fmt.Errorf("empty batch")
	}
	if len(batch) > bp.config.BatchSize {
		return fmt.Errorf("batch size %d exceeds maximum %d", len(batch), bp.config.BatchSize)
	}
	return nil
}
