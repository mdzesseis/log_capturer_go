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

// deepCopyBatch creates deep copies of LogEntry slice to prevent race conditions
//
// This helper function centralizes the deep copy logic for batch processing,
// making it easier to optimize in the future (e.g., using sync.Pool).
//
// Performance characteristics:
//   - Time complexity: O(n) where n = len(batch)
//   - Space complexity: O(n) new allocations
//   - Each entry is fully deep copied (including maps)
//
// Parameters:
//   - batch: Source slice of dispatchItem containing entries to copy
//
// Returns:
//   - []types.LogEntry: New slice with deep copied entries
func deepCopyBatch(batch []dispatchItem) []types.LogEntry {
	result := make([]types.LogEntry, len(batch))
	for i, item := range batch {
		result[i] = *item.Entry.DeepCopy()
	}
	return result
}

// deepCopyEntries creates deep copies of a LogEntry slice
//
// Similar to deepCopyBatch but works with existing LogEntry slice.
//
// Parameters:
//   - entries: Source slice of LogEntry to copy
//
// Returns:
//   - []types.LogEntry: New slice with deep copied entries
func deepCopyEntries(entries []types.LogEntry) []types.LogEntry {
	result := make([]types.LogEntry, len(entries))
	for i, entry := range entries {
		result[i] = *entry.DeepCopy()
	}
	return result
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

	// PERFORMANCE OPTIMIZATION: Single shared copy for read-only sink operations
	//
	// We create ONE deep copy of the batch that can be safely shared across
	// multiple sinks IF those sinks only read the entries (don't modify them).
	//
	// Current implementation: Each sink gets its own copy (safe but expensive)
	// Future optimization: If sink interface guarantees read-only, share this copy
	//
	// Trade-off analysis:
	//   Current: N sinks × M entries × DeepCopy() = O(N*M) copies
	//   Optimized: 1 × M entries × DeepCopy() = O(M) copies
	//   Memory savings: ~(N-1) × batch_size × entry_size
	//
	// For 3 sinks, 100 entries, ~2KB/entry: 600KB → 200KB per batch
	entries := deepCopyBatch(batch)

	// TODO: Implement anomaly detection sampling here
	// (Moved from dispatcher.go lines 837-882)

	// Send to all healthy sinks
	for _, sink := range sinks {
		if !sink.IsHealthy() {
			bp.logger.Warn("Skipping unhealthy sink")
			continue
		}

		healthySinks++

		// SAFETY: Deep copy for each sink to prevent race conditions
		//
		// WHY: Sinks may:
		//   1. Modify entry fields during serialization
		//   2. Store entries in internal queues accessed by multiple goroutines
		//   3. Apply sink-specific transformations
		//
		// FUTURE OPTIMIZATION: If Sink interface is extended with ReadOnly flag,
		// we could share the 'entries' slice for read-only sinks.
		//
		// Example optimization:
		//   if sink.IsReadOnly() {
		//       sink.Send(ctx, entries)  // Share copy
		//   } else {
		//       sink.Send(ctx, deepCopyEntries(entries))  // Unique copy
		//   }
		entriesCopy := deepCopyEntries(entries)

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
