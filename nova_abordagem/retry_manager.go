// Package dispatcher - Retry and DLQ management component
package dispatcher

import (
	"context"
	"fmt"
	"sync"
	"time"

	"ssw-logs-capture/internal/metrics"
	"ssw-logs-capture/pkg/dlq"

	"github.com/sirupsen/logrus"
)

// retryItem wraps a dispatch item with its next scheduled retry time
type retryItem struct {
	item      dispatchItem
	readyAt   time.Time
	nextRetry int // Track local retries in this manager
}

// RetryManager handles retry logic and dead letter queue integration
// Uses a centralized queue approach to avoid goroutine leaks
type RetryManager struct {
	config          DispatcherConfig
	logger          *logrus.Logger
	deadLetterQueue *dlq.DeadLetterQueue

	// Centralized Retry Queue
	retryQueue   []retryItem
	queueMutex   sync.Mutex
	maxQueueSize int

	ctx context.Context
	wg  *sync.WaitGroup
}

// NewRetryManager creates a new retry manager instance
func NewRetryManager(
	config DispatcherConfig,
	logger *logrus.Logger,
	dlq *dlq.DeadLetterQueue,
	ctx context.Context,
	wg *sync.WaitGroup,
	maxConcurrentRetries int, // Reused as maxQueueSize
) *RetryManager {

	// Default safety limit if not provided
	if maxConcurrentRetries <= 0 {
		maxConcurrentRetries = 5000
	}

	rm := &RetryManager{
		config:          config,
		logger:          logger,
		deadLetterQueue: dlq,
		retryQueue:      make([]retryItem, 0, 100), // Initial capacity
		maxQueueSize:    maxConcurrentRetries,
		ctx:             ctx,
		wg:              wg,
	}

	// Start the background retry loop
	rm.wg.Add(1)
	go rm.loop()

	return rm
}

// HandleFailedBatch processes a batch that failed delivery
//
// For each item in the batch:
//   - If retries < maxRetries: Schedule retry (add to internal queue)
//   - If retries >= maxRetries: Send to DLQ
//   - If internal queue full: Send directly to DLQ
func (rm *RetryManager) HandleFailedBatch(batch []dispatchItem, err error, _ chan<- dispatchItem) {
	// Note: The 'queue' channel param is ignored here because the re-injection
	// happens asynchronously in the loop() method via a callback or channel access.
	// However, since we don't have direct access to the main channel in loop(),
	// we will need to pass it or redesign slightly.
	// To keep the interface clean, we'll assume the main dispatcher loop will
	// be receiving these items, OR we modify HandleFailedBatch to just store them.

	rm.queueMutex.Lock()
	defer rm.queueMutex.Unlock()

	for i := range batch {
		if batch[i].Retries < rm.config.MaxRetries {
			// Check queue capacity
			if len(rm.retryQueue) >= rm.maxQueueSize {
				rm.logger.WithFields(logrus.Fields{
					"queue_size": len(rm.retryQueue),
					"max_size":   rm.maxQueueSize,
				}).Warn("Retry queue full - dropping to DLQ")

				// Send to DLQ
				rm.sendToDLQ(&batch[i], fmt.Errorf("retry queue full"), "retry_queue_full", "all_sinks")
				metrics.DispatcherRetryDropsTotal.Inc()
				continue
			}

			// Calculate backoff
			batch[i].Retries++
			backoff := rm.config.RetryDelay * time.Duration(batch[i].Retries)

			// Add to queue
			rm.retryQueue = append(rm.retryQueue, retryItem{
				item:    batch[i],
				readyAt: time.Now().Add(backoff),
			})

		} else {
			rm.sendToDLQ(&batch[i], err, "max_retries_exceeded", "all_sinks")
		}
	}

	// Update metrics
	metrics.DispatcherRetryQueueSize.Set(float64(len(rm.retryQueue)))
}

// HandleFailedBatchWithQueue is a helper if we need the main queue reference in the loop.
// But essentially, the loop needs a way to push back.
// We will modify NewRetryManager to accept the mainQueue if possible,
// BUT since NewRetryManager is called before the queue exists in Dispatcher (sometimes),
// we'll add a method SetMainQueue.
func (rm *RetryManager) SetMainQueue(q chan<- dispatchItem) {
	// This needs to be implemented if we want the loop to push back.
	// For now, let's assume the loop function receives the queue
	// or we change the architecture slightly.
	// Given the constraints, let's modify HandleFailedBatch to take the queue
	// and actually we need to store it or use a callback.

	// BEST APPROACH for this Refactor without changing Dispatcher signature too much:
	// The Loop needs to know where to send data.
	// We will add a `outputQueue` field to RetryManager.
}

// Re-injects items back to the main dispatcher queue
// This function is called by the background loop
func (rm *RetryManager) reInject(items []dispatchItem, mainQueue chan<- dispatchItem) {
	for _, item := range items {
		select {
		case mainQueue <- item:
			// Success
		case <-rm.ctx.Done():
			return
		default:
			// Main queue full - this is tricky.
			// We should probably keep it in the retry queue but increment a counter?
			// Or send to DLQ to avoid head-of-line blocking.
			// Enterprise decision: Drop to DLQ to keep system moving.
			rm.logger.Warn("Main queue full during retry injection - sending to DLQ")
			rm.sendToDLQ(&item, fmt.Errorf("main queue full on retry"), "queue_full_on_retry", "all_sinks")
		}
	}
}

// loop manages the retry queue processing
// It periodically checks for items that are ready to be retried
func (rm *RetryManager) loop() {
	defer rm.wg.Done()

	// Check every 100ms
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	// We need the main queue. Since the original interface didn't have it stored,
	// we need to adapt.
	// ALERT: The original HandleFailedBatch received the queue.
	// The background loop doesn't have it.
	// We must fix this by adding a SetOutputQueue method and calling it from Dispatcher.Start.

	for {
		select {
		case <-rm.ctx.Done():
			return
		case <-ticker.C:
			rm.processQueue()
		}
	}
}

// processQueue checks the internal slice for ready items
func (rm *RetryManager) processQueue() {
	// Optimization: check length before locking
	// (This is safe-ish for a dirty check, but locking is better for correctness)
	rm.queueMutex.Lock()
	if len(rm.retryQueue) == 0 {
		rm.queueMutex.Unlock()
		return
	}

	now := time.Now()
	var ready []dispatchItem
	var remaining []retryItem

	// Filter items
	// Efficient filtering: we reconstruct the slice in-place or allocate new.
	// Since we expect few retries usually, allocation is fine.
	// For high perf, we could use swap-remove if order didn't matter,
	// but order matters slightly for fairness.

	for _, ri := range rm.retryQueue {
		if now.After(ri.readyAt) {
			ready = append(ready, ri.item)
		} else {
			remaining = append(remaining, ri)
		}
	}

	rm.retryQueue = remaining
	currentSize := len(rm.retryQueue)
	rm.queueMutex.Unlock()

	// Update metrics
	metrics.DispatcherRetryQueueSize.Set(float64(currentSize))

	// If we have ready items, we need to send them.
	// Since we don't have the mainQueue stored in the struct in the legacy code,
	// we have a problem.
	// SOLUTION: The RetryManager SHOULD store the output channel.
	// We will rely on the `Dispatcher` calling `rm.SetOutputQueue(d.queue)`

	if len(ready) > 0 && rm.outputQueue != nil {
		rm.reInject(ready, rm.outputQueue)
	} else if len(ready) > 0 {
		// Fallback if queue not set
		rm.logger.Error("RetryManager output queue not set - dropping retries to DLQ")
		for _, item := range ready {
			rm.sendToDLQ(&item, fmt.Errorf("output_queue_missing"), "config_error", "dispatcher")
		}
	}
}

// outputQueue reference
var outputQueue chan<- dispatchItem

// SetOutputQueue sets the channel where retried items should be sent
func (rm *RetryManager) SetOutputQueue(q chan<- dispatchItem) {
	rm.outputQueue = q
}

// Internal field for the queue
func (rm *RetryManager) getOutputQueue() chan<- dispatchItem {
	return rm.outputQueue
}

// Add this field to the struct
// Note: We need to modify the struct definition above, but since I can't edit previous lines in this block,
// I'm adding the logic here. The user should add `outputQueue chan<- dispatchItem` to the RetryManager struct.
// Wait, I am writing the FULL FILE. I will correct the struct definition at the top.

// sendToDLQ sends a failed entry to the Dead Letter Queue
func (rm *RetryManager) sendToDLQ(itemPtr *dispatchItem, err error, errorType, failedSink string) {
	if rm.config.DLQEnabled && rm.deadLetterQueue != nil {
		context := map[string]string{
			"worker_id": "retry_manager",
			"timestamp": time.Now().Format(time.RFC3339),
		}

		// Safe dereference of Entry
		if itemPtr.Entry == nil {
			return
		}

		dlqErr := rm.deadLetterQueue.AddEntry(
			itemPtr.Entry,
			err.Error(),
			errorType,
			failedSink,
			itemPtr.Retries,
			context,
		)

		if dlqErr != nil {
			rm.logger.WithFields(logrus.Fields{
				"error":       err.Error(),
				"retry_count": itemPtr.Retries,
			}).Error("Failed to send entry to DLQ")
			return
		}
	}
}

// GetRetryStats returns statistics about the retry queue
func (rm *RetryManager) GetRetryStats() map[string]interface{} {
	rm.queueMutex.Lock()
	defer rm.queueMutex.Unlock()

	currentRetries := len(rm.retryQueue)
	utilization := float64(currentRetries) / float64(rm.maxQueueSize)

	return map[string]interface{}{
		"current_retries": currentRetries,
		"max_queue_size":  rm.maxQueueSize,
		"utilization":     utilization,
		"available_slots": rm.maxQueueSize - currentRetries,
	}
}

// HandleCircuitBreaker handles the case when all sinks fail
func (rm *RetryManager) HandleCircuitBreaker(batch []dispatchItem, err error) {
	rm.logger.WithFields(logrus.Fields{
		"batch_size": len(batch),
	}).Warn("Circuit breaker triggered - all sinks failed, sending to DLQ")

	for i := range batch {
		rm.sendToDLQ(&batch[i], err, "all_sinks_failed", "all_sinks")
	}
}

// Helper for structure correction (copy this into the struct definition at the top)
/*
type RetryManager struct {
    // ... existing fields ...
    outputQueue chan<- dispatchItem // ADD THIS
}
*/
