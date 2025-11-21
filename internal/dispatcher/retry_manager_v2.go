package dispatcher

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"ssw-logs-capture/internal/metrics"
	"ssw-logs-capture/pkg/dlq"
)

// retryItem represents an item waiting to be retried
type retryItem struct {
	item    dispatchItem
	readyAt time.Time
}

// RetryManagerV2 implements a centralized queue-based retry mechanism
// to avoid goroutine explosion from per-retry goroutines.
type RetryManagerV2 struct {
	config          DispatcherConfig
	logger          *logrus.Logger
	deadLetterQueue *dlq.DeadLetterQueue

	retryQueue   []retryItem
	queueMutex   sync.Mutex
	maxQueueSize int

	outputQueue chan<- dispatchItem

	ctx    context.Context
	cancel context.CancelFunc
	wg     *sync.WaitGroup
}

// NewRetryManagerV2 creates a new centralized retry manager
func NewRetryManagerV2(config DispatcherConfig, logger *logrus.Logger, deadLetterQueue *dlq.DeadLetterQueue) *RetryManagerV2 {
	maxQueueSize := 5000
	if config.QueueSize > 0 {
		// Use a fraction of the main queue size or default
		maxQueueSize = config.QueueSize / 2
		if maxQueueSize < 1000 {
			maxQueueSize = 1000
		}
		if maxQueueSize > 10000 {
			maxQueueSize = 10000
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &RetryManagerV2{
		config:          config,
		logger:          logger,
		deadLetterQueue: deadLetterQueue,
		retryQueue:      make([]retryItem, 0, maxQueueSize),
		maxQueueSize:    maxQueueSize,
		ctx:             ctx,
		cancel:          cancel,
		wg:              &sync.WaitGroup{},
	}
}

// SetOutputQueue sets the channel to send ready items back to the dispatcher
func (rm *RetryManagerV2) SetOutputQueue(queue chan<- dispatchItem) {
	rm.queueMutex.Lock()
	defer rm.queueMutex.Unlock()
	rm.outputQueue = queue
}

// Start begins the retry processing loop
func (rm *RetryManagerV2) Start() {
	rm.wg.Add(1)
	go rm.processLoop()

	rm.logger.WithFields(logrus.Fields{
		"max_queue_size": rm.maxQueueSize,
		"tick_interval":  "100ms",
	}).Info("RetryManagerV2 started")
}

// Stop gracefully shuts down the retry manager
func (rm *RetryManagerV2) Stop() {
	rm.logger.Info("Stopping RetryManagerV2...")
	rm.cancel()
	rm.wg.Wait()

	// Log final queue state
	rm.queueMutex.Lock()
	remaining := len(rm.retryQueue)
	rm.queueMutex.Unlock()

	if remaining > 0 {
		rm.logger.WithField("remaining_items", remaining).Warn("RetryManagerV2 stopped with items still in queue")
	} else {
		rm.logger.Info("RetryManagerV2 stopped cleanly")
	}
}

// ScheduleRetry adds an item to the retry queue with the appropriate delay
func (rm *RetryManagerV2) ScheduleRetry(item dispatchItem) {
	rm.queueMutex.Lock()
	defer rm.queueMutex.Unlock()

	// Check queue capacity
	if len(rm.retryQueue) >= rm.maxQueueSize {
		rm.logger.WithFields(logrus.Fields{
			"queue_size":     len(rm.retryQueue),
			"max_queue_size": rm.maxQueueSize,
			"source_id":      item.Entry.SourceID,
			"retries":        item.Retries,
		}).Warn("Retry queue full, sending to DLQ")

		// Send to DLQ
		rm.sendToDLQ(&item, fmt.Errorf("retry queue full (size: %d)", rm.maxQueueSize), "retry_queue_overflow", "all_sinks")
		metrics.DispatcherRetryDropsTotal.Inc()
		return
	}

	// Calculate delay with exponential backoff
	delay := rm.calculateBackoff(item.Retries)
	readyAt := time.Now().Add(delay)

	// Deep copy the entry to avoid race conditions
	entryCopy := item.Entry.DeepCopy()
	retryItemCopy := retryItem{
		item: dispatchItem{
			Entry:     entryCopy,
			Timestamp: item.Timestamp,
			Retries:   item.Retries,
		},
		readyAt: readyAt,
	}

	rm.retryQueue = append(rm.retryQueue, retryItemCopy)

	// Update metrics
	metrics.DispatcherRetryQueueSize.Set(float64(len(rm.retryQueue)))

	rm.logger.WithFields(logrus.Fields{
		"source_id":   item.Entry.SourceID,
		"retry_count": item.Retries,
		"delay":       delay,
		"ready_at":    readyAt.Format(time.RFC3339),
		"queue_size":  len(rm.retryQueue),
	}).Debug("Item scheduled for retry")
}

// ScheduleRetryBatch adds multiple items to the retry queue efficiently
func (rm *RetryManagerV2) ScheduleRetryBatch(items []dispatchItem) {
	if len(items) == 0 {
		return
	}

	rm.queueMutex.Lock()
	defer rm.queueMutex.Unlock()

	added := 0
	dropped := 0

	for i := range items {
		// Check queue capacity for each item
		if len(rm.retryQueue) >= rm.maxQueueSize {
			// Send remaining to DLQ
			rm.sendToDLQ(&items[i], fmt.Errorf("retry queue full"), "retry_queue_overflow", "all_sinks")
			metrics.DispatcherRetryDropsTotal.Inc()
			dropped++
			continue
		}

		// Calculate delay
		delay := rm.calculateBackoff(items[i].Retries)
		readyAt := time.Now().Add(delay)

		// Deep copy the entry
		entryCopy := items[i].Entry.DeepCopy()
		retryItemCopy := retryItem{
			item: dispatchItem{
				Entry:     entryCopy,
				Timestamp: items[i].Timestamp,
				Retries:   items[i].Retries,
			},
			readyAt: readyAt,
		}

		rm.retryQueue = append(rm.retryQueue, retryItemCopy)
		added++
	}

	// Update metrics
	metrics.DispatcherRetryQueueSize.Set(float64(len(rm.retryQueue)))

	if dropped > 0 {
		rm.logger.WithFields(logrus.Fields{
			"added":      added,
			"dropped":    dropped,
			"queue_size": len(rm.retryQueue),
		}).Warn("Batch retry scheduled with drops")
	} else {
		rm.logger.WithFields(logrus.Fields{
			"added":      added,
			"queue_size": len(rm.retryQueue),
		}).Debug("Batch retry scheduled")
	}
}

// processLoop runs the main ticker loop to check for ready items
func (rm *RetryManagerV2) processLoop() {
	defer rm.wg.Done()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-rm.ctx.Done():
			rm.logger.Debug("RetryManagerV2 processLoop exiting due to context cancellation")
			return
		case <-ticker.C:
			rm.processReadyItems()
		}
	}
}

// processReadyItems finds and sends items whose retry time has arrived
func (rm *RetryManagerV2) processReadyItems() {
	rm.queueMutex.Lock()

	if len(rm.retryQueue) == 0 {
		rm.queueMutex.Unlock()
		return
	}

	now := time.Now()
	var readyItems []dispatchItem
	var remainingItems []retryItem

	// Separate ready items from not-ready items
	for _, ri := range rm.retryQueue {
		if now.After(ri.readyAt) || now.Equal(ri.readyAt) {
			readyItems = append(readyItems, ri.item)
		} else {
			remainingItems = append(remainingItems, ri)
		}
	}

	// Update queue with remaining items
	// Reuse capacity to reduce allocations
	rm.retryQueue = rm.retryQueue[:0]
	rm.retryQueue = append(rm.retryQueue, remainingItems...)

	currentSize := len(rm.retryQueue)

	// Update metrics
	metrics.DispatcherRetryQueueSize.Set(float64(currentSize))

	rm.queueMutex.Unlock()

	// Send ready items to output queue (outside lock)
	if len(readyItems) > 0 && rm.outputQueue != nil {
		sent := 0
		for _, item := range readyItems {
			select {
			case <-rm.ctx.Done():
				rm.logger.WithField("unsent_items", len(readyItems)-sent).Warn("Context cancelled while sending retry items")
				return
			case rm.outputQueue <- item:
				sent++
			default:
				// Output queue is full, re-queue the item
				rm.requeueItem(item)
			}
		}

		if sent > 0 {
			rm.logger.WithFields(logrus.Fields{
				"sent":            sent,
				"remaining_queue": currentSize,
			}).Debug("Retry items sent to dispatcher")
		}
	}
}

// requeueItem adds an item back to the retry queue if output was full
func (rm *RetryManagerV2) requeueItem(item dispatchItem) {
	rm.queueMutex.Lock()
	defer rm.queueMutex.Unlock()

	if len(rm.retryQueue) >= rm.maxQueueSize {
		// Queue is full, send to DLQ
		rm.sendToDLQ(&item, fmt.Errorf("retry queue full during requeue"), "retry_requeue_overflow", "all_sinks")
		metrics.DispatcherRetryDropsTotal.Inc()
		return
	}

	// Add with a small delay to prevent tight loop
	ri := retryItem{
		item:    item,
		readyAt: time.Now().Add(50 * time.Millisecond),
	}
	rm.retryQueue = append(rm.retryQueue, ri)
	metrics.DispatcherRetryQueueSize.Set(float64(len(rm.retryQueue)))
}

// calculateBackoff computes the delay for a retry attempt using exponential backoff
func (rm *RetryManagerV2) calculateBackoff(retryCount int) time.Duration {
	baseDelay := rm.config.RetryDelay
	if baseDelay == 0 {
		baseDelay = 1 * time.Second
	}

	// Exponential backoff: baseDelay * 2^retryCount
	// Cap at 30 seconds to prevent excessive delays
	multiplier := 1 << retryCount // 2^retryCount
	delay := baseDelay * time.Duration(multiplier)

	maxDelay := 30 * time.Second
	if delay > maxDelay {
		delay = maxDelay
	}

	return delay
}

// sendToDLQ sends a failed item to the dead letter queue
func (rm *RetryManagerV2) sendToDLQ(item *dispatchItem, err error, errorType, failedSink string) {
	if rm.deadLetterQueue == nil {
		rm.logger.WithFields(logrus.Fields{
			"source_id":  item.Entry.SourceID,
			"error":      err.Error(),
			"error_type": errorType,
		}).Warn("DLQ not available, item dropped")
		return
	}

	dlqErr := rm.deadLetterQueue.AddEntry(
		item.Entry,
		err.Error(),
		errorType,
		failedSink,
		item.Retries,
		map[string]string{
			"component":      "retry_manager_v2",
			"original_queue": "retry_queue",
		},
	)

	if dlqErr != nil {
		rm.logger.WithFields(logrus.Fields{
			"source_id": item.Entry.SourceID,
			"error":     dlqErr.Error(),
		}).Error("Failed to send item to DLQ")
	}
}

// GetQueueSize returns the current number of items in the retry queue
func (rm *RetryManagerV2) GetQueueSize() int {
	rm.queueMutex.Lock()
	defer rm.queueMutex.Unlock()
	return len(rm.retryQueue)
}

// GetStats returns statistics about the retry manager
func (rm *RetryManagerV2) GetStats() map[string]interface{} {
	rm.queueMutex.Lock()
	defer rm.queueMutex.Unlock()

	var oldestReadyAt, newestReadyAt time.Time
	if len(rm.retryQueue) > 0 {
		oldestReadyAt = rm.retryQueue[0].readyAt
		newestReadyAt = rm.retryQueue[0].readyAt

		for _, ri := range rm.retryQueue {
			if ri.readyAt.Before(oldestReadyAt) {
				oldestReadyAt = ri.readyAt
			}
			if ri.readyAt.After(newestReadyAt) {
				newestReadyAt = ri.readyAt
			}
		}
	}

	return map[string]interface{}{
		"queue_size":       len(rm.retryQueue),
		"max_queue_size":   rm.maxQueueSize,
		"utilization":      float64(len(rm.retryQueue)) / float64(rm.maxQueueSize),
		"oldest_ready_at":  oldestReadyAt,
		"newest_ready_at":  newestReadyAt,
		"has_output_queue": rm.outputQueue != nil,
	}
}
