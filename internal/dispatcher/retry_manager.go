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

// RetryManager handles retry logic and dead letter queue integration
type RetryManager struct {
	config             DispatcherConfig
	logger             *logrus.Logger
	deadLetterQueue    *dlq.DeadLetterQueue
	retrySemaphore     chan struct{}
	maxConcurrentRetries int
	ctx                context.Context
	wg                 *sync.WaitGroup
}

// NewRetryManager creates a new retry manager instance
func NewRetryManager(
	config DispatcherConfig,
	logger *logrus.Logger,
	dlq *dlq.DeadLetterQueue,
	ctx context.Context,
	wg *sync.WaitGroup,
	maxConcurrentRetries int,
) *RetryManager {
	return &RetryManager{
		config:             config,
		logger:             logger,
		deadLetterQueue:    dlq,
		retrySemaphore:     make(chan struct{}, maxConcurrentRetries),
		maxConcurrentRetries: maxConcurrentRetries,
		ctx:                ctx,
		wg:                 wg,
	}
}

// HandleFailedBatch processes a batch that failed delivery
//
// For each item in the batch:
//  - If retries < maxRetries: Schedule retry with exponential backoff
//  - If retries >= maxRetries: Send to DLQ
//  - If retry queue full: Send directly to DLQ to prevent goroutine explosion
func (rm *RetryManager) HandleFailedBatch(batch []dispatchItem, err error, queue chan<- dispatchItem) {
	for i := range batch {
		if batch[i].Retries < rm.config.MaxRetries {
			rm.scheduleRetry(&batch[i], queue)
		} else {
			rm.sendToDLQ(&batch[i], err, "max_retries_exceeded", "all_sinks")
		}
	}
}

// scheduleRetry schedules a retry for a failed item with exponential backoff
func (rm *RetryManager) scheduleRetry(itemPtr *dispatchItem, queue chan<- dispatchItem) {
	itemPtr.Retries++
	retryDelay := rm.config.RetryDelay * time.Duration(itemPtr.Retries)

	// Try to acquire semaphore slot
	select {
	case rm.retrySemaphore <- struct{}{}:
		// Successfully acquired - create retry goroutine
		rm.wg.Add(1)
		go rm.retryWorker(itemPtr, retryDelay, queue)

	default:
		// Semaphore full - too many concurrent retries
		// Send directly to DLQ to prevent goroutine explosion
		rm.logger.WithFields(logrus.Fields{
			"retries":                itemPtr.Retries,
			"max_concurrent_retries": rm.maxConcurrentRetries,
			"source_type":            itemPtr.Entry.SourceType,
			"source_id":              itemPtr.Entry.SourceID,
		}).Warn("Retry queue full - sending to DLQ to prevent goroutine explosion")

		rm.sendToDLQ(itemPtr, fmt.Errorf("retry queue full"), "retry_queue_full", "all_sinks")
		metrics.RecordError("dispatcher", "retry_queue_full")
	}
}

// retryWorker is a goroutine that waits and retries a failed item
func (rm *RetryManager) retryWorker(itemPtr *dispatchItem, delay time.Duration, queue chan<- dispatchItem) {
	defer rm.wg.Done()
	defer func() { <-rm.retrySemaphore }() // Release semaphore

	timer := time.NewTimer(delay)
	defer func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}()

	select {
	case <-timer.C:
		// Try to re-queue
		select {
		case queue <- *itemPtr:
			rm.logger.WithField("retries", itemPtr.Retries).Debug("Item rescheduled successfully")
		case <-rm.ctx.Done():
			// Context cancelled during re-queue
			return
		default:
			// Queue full - send to DLQ
			rm.logger.Warn("Failed to reschedule item, queue full")
			rm.sendToDLQ(itemPtr, fmt.Errorf("queue full on retry"), "queue_full_on_retry", "all_sinks")
		}

	case <-rm.ctx.Done():
		// Context cancelled during wait
		return
	}
}

// sendToDLQ sends a failed entry to the Dead Letter Queue
func (rm *RetryManager) sendToDLQ(itemPtr *dispatchItem, err error, errorType, failedSink string) {
	if rm.config.DLQEnabled && rm.deadLetterQueue != nil {
		context := map[string]string{
			"worker_id": "retry_manager",
			"timestamp": time.Now().Format(time.RFC3339),
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
				"trace_id":    itemPtr.Entry.TraceID,
				"source_type": itemPtr.Entry.SourceType,
				"source_id":   itemPtr.Entry.SourceID,
				"failed_sink": failedSink,
				"error_type":  errorType,
				"error":       err.Error(),
				"retry_count": itemPtr.Retries,
				"dlq_error":   dlqErr.Error(),
			}).Error("Failed to send entry to DLQ")
			return
		}

		rm.logger.WithFields(logrus.Fields{
			"trace_id":    itemPtr.Entry.TraceID,
			"source_type": itemPtr.Entry.SourceType,
			"source_id":   itemPtr.Entry.SourceID,
			"failed_sink": failedSink,
			"error_type":  errorType,
			"retry_count": itemPtr.Retries,
		}).Debug("Entry sent to DLQ")
	}
}

// GetRetryStats returns statistics about the retry queue
func (rm *RetryManager) GetRetryStats() map[string]interface{} {
	currentRetries := len(rm.retrySemaphore)
	utilization := float64(currentRetries) / float64(rm.maxConcurrentRetries)

	return map[string]interface{}{
		"current_retries":        currentRetries,
		"max_concurrent_retries": rm.maxConcurrentRetries,
		"utilization":            utilization,
		"available_slots":        rm.maxConcurrentRetries - currentRetries,
	}
}

// HandleCircuitBreaker handles the case when all sinks fail
//
// To prevent goroutine explosion during cascading failures,
// we send items directly to DLQ instead of retrying
func (rm *RetryManager) HandleCircuitBreaker(batch []dispatchItem, err error) {
	rm.logger.WithFields(logrus.Fields{
		"batch_size": len(batch),
	}).Warn("Circuit breaker triggered - all sinks failed, sending to DLQ")

	for i := range batch {
		rm.sendToDLQ(&batch[i], err, "all_sinks_failed", "all_sinks")
	}
}
