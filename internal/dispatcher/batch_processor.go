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

// ProcessBatch processes a batch of dispatch items and sends to sinks
//
// This method:
//  1. Creates deep copies of entries to avoid race conditions
//  2. Runs anomaly detection on sampled entries
//  3. Sends batch to all healthy sinks
//  4. Tracks success/failure rates
//  5. Records metrics and statistics
//
// Returns:
//   - successCount: Number of sinks that successfully received the batch
//   - healthySinks: Number of healthy sinks attempted
//   - lastError: Last error encountered (if any)
func (bp *BatchProcessor) ProcessBatch(
	ctx context.Context,
	batch []dispatchItem,
	sinks []types.Sink,
	anomalyDetector interface{}, // TODO: Type this properly
) (successCount, healthySinks int, lastErr error) {

	if len(batch) == 0 {
		return 0, 0, nil
	}

	startTime := time.Now()

	// Create deep copies to avoid race conditions
	entries := make([]types.LogEntry, len(batch))
	for i, item := range batch {
		entries[i] = *item.Entry.DeepCopy()
	}

	// TODO: Implement anomaly detection sampling here
	// (Moved from dispatcher.go lines 837-882)

	// Send to all healthy sinks
	for _, sink := range sinks {
		if !sink.IsHealthy() {
			bp.logger.Warn("Skipping unhealthy sink")
			continue
		}

		healthySinks++

		// Deep copy for each sink to prevent race conditions
		entriesCopy := make([]types.LogEntry, len(entries))
		for i, entry := range entries {
			entriesCopy[i] = *entry.DeepCopy()
		}

		sendCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
		err := sink.Send(sendCtx, entriesCopy)
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
//  - Collects up to BatchSize items
//  - Returns early on timeout (BatchTimeout)
//  - Returns early on context cancellation
//
// Returns collected batch and a boolean indicating if timeout occurred
func (bp *BatchProcessor) CollectBatch(
	ctx context.Context,
	queue <-chan dispatchItem,
) ([]dispatchItem, bool) {

	batch := make([]dispatchItem, 0, bp.config.BatchSize)
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

	// Reset timer after first item
	if !timer.Stop() {
		<-timer.C
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
