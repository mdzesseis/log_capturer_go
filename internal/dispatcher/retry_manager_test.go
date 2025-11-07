package dispatcher

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"ssw-logs-capture/pkg/dlq"
	"ssw-logs-capture/pkg/types"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewRetryManager tests constructor validation
func TestNewRetryManager(t *testing.T) {
	config := DispatcherConfig{
		MaxRetries:  3,
		RetryDelay:  100 * time.Millisecond,
		DLQEnabled:  true,
	}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	dlqConfig := dlq.Config{
		Enabled:   true,
		Directory: t.TempDir(),
		QueueSize: 100,
	}
	deadLetterQueue := dlq.NewDeadLetterQueue(dlqConfig, logger)
	defer deadLetterQueue.Stop()

	ctx := context.Background()
	wg := &sync.WaitGroup{}
	maxConcurrentRetries := 10

	rm := NewRetryManager(config, logger, deadLetterQueue, ctx, wg, maxConcurrentRetries)

	require.NotNil(t, rm)
	assert.Equal(t, config.MaxRetries, rm.config.MaxRetries)
	assert.Equal(t, config.RetryDelay, rm.config.RetryDelay)
	assert.NotNil(t, rm.logger)
	assert.NotNil(t, rm.deadLetterQueue)
	assert.NotNil(t, rm.retrySemaphore)
	assert.Equal(t, maxConcurrentRetries, rm.maxConcurrentRetries)
	assert.Equal(t, maxConcurrentRetries, cap(rm.retrySemaphore))
}

// TestRetryManager_HandleFailedBatch_BelowMaxRetries tests retry scheduling
func TestRetryManager_HandleFailedBatch_BelowMaxRetries(t *testing.T) {
	config := DispatcherConfig{
		MaxRetries:  3,
		RetryDelay:  50 * time.Millisecond,
		DLQEnabled:  true,
	}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	dlqConfig := dlq.Config{
		Enabled:   true,
		Directory: t.TempDir(),
		QueueSize: 100,
	}
	deadLetterQueue := dlq.NewDeadLetterQueue(dlqConfig, logger)
	defer deadLetterQueue.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wg := &sync.WaitGroup{}

	rm := NewRetryManager(config, logger, deadLetterQueue, ctx, wg, 10)

	// Create batch with items below max retries
	batch := []dispatchItem{
		{
			Entry: types.LogEntry{
				Message:    "test message 1",
				Labels:     make(map[string]string),
				SourceType: "test",
				SourceID:   "test-1",
			},
			Retries: 1, // Below max retries
		},
	}

	queue := make(chan dispatchItem, 10)
	testErr := errors.New("send failed")

	rm.HandleFailedBatch(batch, testErr, queue)

	// Wait for retry to be scheduled
	time.Sleep(100 * time.Millisecond)

	// Should receive retried item from queue
	select {
	case item := <-queue:
		assert.Equal(t, 2, item.Retries, "Retries should be incremented")
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Timeout waiting for retry")
	}

	// Wait for goroutines to finish
	wg.Wait()
}

// TestRetryManager_HandleFailedBatch_ExceedsMaxRetries tests DLQ routing
func TestRetryManager_HandleFailedBatch_ExceedsMaxRetries(t *testing.T) {
	config := DispatcherConfig{
		MaxRetries:  3,
		RetryDelay:  10 * time.Millisecond,
		DLQEnabled:  true,
	}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	dlqConfig := dlq.Config{
		Enabled:   true,
		Directory: t.TempDir(),
		QueueSize: 100,
	}
	deadLetterQueue := dlq.NewDeadLetterQueue(dlqConfig, logger)
	defer deadLetterQueue.Stop()

	ctx := context.Background()
	wg := &sync.WaitGroup{}

	rm := NewRetryManager(config, logger, deadLetterQueue, ctx, wg, 10)

	// Create batch with items at max retries
	batch := []dispatchItem{
		{
			Entry: types.LogEntry{
				Message:    "test message",
				Labels:     make(map[string]string),
				SourceType: "test",
				SourceID:   "test-1",
				TraceID:    "trace-123",
			},
			Retries: 3, // At max retries
		},
	}

	queue := make(chan dispatchItem, 10)
	testErr := errors.New("send failed")

	rm.HandleFailedBatch(batch, testErr, queue)

	// Give time for DLQ processing
	time.Sleep(50 * time.Millisecond)

	// Queue should be empty (item sent to DLQ, not retried)
	assert.Empty(t, queue, "Queue should be empty when max retries exceeded")

	// Verify DLQ received the entry
	stats := deadLetterQueue.GetStats()
	assert.Greater(t, stats.TotalEntries, int64(0), "DLQ should have received entries")
}

// TestRetryManager_ScheduleRetry_WithExponentialBackoff tests retry delay calculation
func TestRetryManager_ScheduleRetry_WithExponentialBackoff(t *testing.T) {
	config := DispatcherConfig{
		MaxRetries:  5,
		RetryDelay:  10 * time.Millisecond, // Base delay
		DLQEnabled:  false,
	}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wg := &sync.WaitGroup{}

	rm := NewRetryManager(config, logger, nil, ctx, wg, 10)

	queue := make(chan dispatchItem, 10)

	tests := []struct {
		retries      int
		expectedDelay time.Duration
	}{
		{1, 10 * time.Millisecond},  // 10ms * 1
		{2, 20 * time.Millisecond},  // 10ms * 2
		{3, 30 * time.Millisecond},  // 10ms * 3
	}

	for _, tt := range tests {
		t.Run(string(rune(tt.retries)), func(t *testing.T) {
			item := dispatchItem{
				Entry: types.LogEntry{
					Message: "test",
					Labels:  make(map[string]string),
				},
				Retries: tt.retries - 1, // Will be incremented
			}

			start := time.Now()
			rm.scheduleRetry(item, queue)

			// Wait for retry
			select {
			case <-queue:
				duration := time.Since(start)
				// Allow some margin for scheduling
				assert.GreaterOrEqual(t, duration, tt.expectedDelay, "Delay should be at least expected")
				assert.Less(t, duration, tt.expectedDelay+100*time.Millisecond, "Delay should not be too long")
			case <-time.After(tt.expectedDelay + 200*time.Millisecond):
				t.Fatal("Timeout waiting for retry")
			}
		})
	}

	// Wait for all retry goroutines
	wg.Wait()
}

// TestRetryManager_SemaphoreLimit tests concurrent retry limiting
func TestRetryManager_SemaphoreLimit(t *testing.T) {
	config := DispatcherConfig{
		MaxRetries:  5,
		RetryDelay:  200 * time.Millisecond, // Longer delay
		DLQEnabled:  true,
	}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	dlqConfig := dlq.Config{
		Enabled:   true,
		Directory: t.TempDir(),
		QueueSize: 100,
	}
	deadLetterQueue := dlq.NewDeadLetterQueue(dlqConfig, logger)
	defer deadLetterQueue.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wg := &sync.WaitGroup{}
	maxConcurrentRetries := 3

	rm := NewRetryManager(config, logger, deadLetterQueue, ctx, wg, maxConcurrentRetries)

	queue := make(chan dispatchItem, 100)

	// Try to schedule more retries than the semaphore allows
	numItems := 10
	batch := make([]dispatchItem, numItems)
	for i := 0; i < numItems; i++ {
		batch[i] = dispatchItem{
			Entry: types.LogEntry{
				Message:    "test",
				Labels:     make(map[string]string),
				SourceType: "test",
				SourceID:   "test",
				TraceID:    "trace",
			},
			Retries: 1,
		}
	}

	// Schedule all retries
	testErr := errors.New("test error")
	rm.HandleFailedBatch(batch, testErr, queue)

	// Give time for processing
	time.Sleep(100 * time.Millisecond)

	// Check stats - some items should have been sent to DLQ due to semaphore limit
	stats := rm.GetRetryStats()
	currentRetries := stats["current_retries"].(int)
	assert.LessOrEqual(t, currentRetries, maxConcurrentRetries, "Should not exceed max concurrent retries")

	// Cancel and wait for cleanup
	cancel()
	wg.Wait()
}

// TestRetryManager_Stop_GracefulShutdown tests graceful shutdown
func TestRetryManager_Stop_GracefulShutdown(t *testing.T) {
	config := DispatcherConfig{
		MaxRetries:  3,
		RetryDelay:  50 * time.Millisecond,
		DLQEnabled:  false,
	}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}

	rm := NewRetryManager(config, logger, nil, ctx, wg, 10)

	queue := make(chan dispatchItem, 10)

	// Schedule a retry
	item := dispatchItem{
		Entry: types.LogEntry{
			Message: "test",
			Labels:  make(map[string]string),
		},
		Retries: 1,
	}
	rm.scheduleRetry(item, queue)

	// Cancel context (simulating Stop)
	cancel()

	// Wait with timeout to ensure goroutines finish
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success - all goroutines finished
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for retry goroutines to finish")
	}
}

// TestRetryManager_HandleCircuitBreaker tests circuit breaker handling
func TestRetryManager_HandleCircuitBreaker(t *testing.T) {
	config := DispatcherConfig{
		MaxRetries:  3,
		RetryDelay:  10 * time.Millisecond,
		DLQEnabled:  true,
	}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	dlqConfig := dlq.Config{
		Enabled:   true,
		Directory: t.TempDir(),
		QueueSize: 100,
	}
	deadLetterQueue := dlq.NewDeadLetterQueue(dlqConfig, logger)
	defer deadLetterQueue.Stop()

	ctx := context.Background()
	wg := &sync.WaitGroup{}

	rm := NewRetryManager(config, logger, deadLetterQueue, ctx, wg, 10)

	// Create batch
	batch := []dispatchItem{
		{
			Entry: types.LogEntry{
				Message:    "test1",
				Labels:     make(map[string]string),
				SourceType: "test",
				SourceID:   "test-1",
				TraceID:    "trace-1",
			},
			Retries: 0,
		},
		{
			Entry: types.LogEntry{
				Message:    "test2",
				Labels:     make(map[string]string),
				SourceType: "test",
				SourceID:   "test-2",
				TraceID:    "trace-2",
			},
			Retries: 0,
		},
	}

	testErr := errors.New("all sinks failed")
	rm.HandleCircuitBreaker(batch, testErr)

	// Give time for DLQ processing
	time.Sleep(50 * time.Millisecond)

	// Verify all items sent to DLQ
	stats := deadLetterQueue.GetStats()
	assert.GreaterOrEqual(t, stats.TotalEntries, int64(2), "All items should be in DLQ")
}

// TestRetryManager_GetRetryStats tests statistics retrieval
func TestRetryManager_GetRetryStats(t *testing.T) {
	config := DispatcherConfig{
		MaxRetries:  3,
		RetryDelay:  100 * time.Millisecond,
		DLQEnabled:  false,
	}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wg := &sync.WaitGroup{}
	maxConcurrentRetries := 5

	rm := NewRetryManager(config, logger, nil, ctx, wg, maxConcurrentRetries)

	// Initially empty
	stats := rm.GetRetryStats()
	assert.Equal(t, 0, stats["current_retries"])
	assert.Equal(t, maxConcurrentRetries, stats["max_concurrent_retries"])
	assert.Equal(t, 0.0, stats["utilization"])
	assert.Equal(t, maxConcurrentRetries, stats["available_slots"])

	// Schedule some retries
	queue := make(chan dispatchItem, 10)
	for i := 0; i < 2; i++ {
		item := dispatchItem{
			Entry: types.LogEntry{
				Message: "test",
				Labels:  make(map[string]string),
			},
			Retries: 1,
		}
		rm.scheduleRetry(item, queue)
	}

	// Give time for retries to be scheduled
	time.Sleep(50 * time.Millisecond)

	stats = rm.GetRetryStats()
	currentRetries := stats["current_retries"].(int)
	assert.Greater(t, currentRetries, 0, "Should have active retries")
	assert.LessOrEqual(t, currentRetries, maxConcurrentRetries)

	// Cleanup
	cancel()
	wg.Wait()
}

// TestRetryManager_ConcurrentRetries tests race condition handling
func TestRetryManager_ConcurrentRetries(t *testing.T) {
	config := DispatcherConfig{
		MaxRetries:  5,
		RetryDelay:  10 * time.Millisecond,
		DLQEnabled:  false,
	}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wg := &sync.WaitGroup{}

	rm := NewRetryManager(config, logger, nil, ctx, wg, 20)

	queue := make(chan dispatchItem, 100)

	// Concurrently schedule multiple retries
	var schedulerWg sync.WaitGroup
	numGoroutines := 10
	itemsPerGoroutine := 5

	schedulerWg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer schedulerWg.Done()
			for j := 0; j < itemsPerGoroutine; j++ {
				item := dispatchItem{
					Entry: types.LogEntry{
						Message: "concurrent test",
						Labels:  make(map[string]string),
					},
					Retries: 1,
				}
				rm.scheduleRetry(item, queue)
			}
		}(i)
	}

	schedulerWg.Wait()

	// Give time for retries to complete
	time.Sleep(200 * time.Millisecond)

	// Should have received items in queue
	assert.Greater(t, len(queue), 0, "Should have retried items")

	// Cleanup
	cancel()
	wg.Wait()
}

// BenchmarkRetryManager_ScheduleRetry benchmarks retry scheduling
func BenchmarkRetryManager_ScheduleRetry(b *testing.B) {
	config := DispatcherConfig{
		MaxRetries:  3,
		RetryDelay:  1 * time.Millisecond,
		DLQEnabled:  false,
	}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wg := &sync.WaitGroup{}

	rm := NewRetryManager(config, logger, nil, ctx, wg, 100)

	queue := make(chan dispatchItem, 10000)

	item := dispatchItem{
		Entry: types.LogEntry{
			Message: "benchmark",
			Labels:  make(map[string]string),
		},
		Retries: 1,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rm.scheduleRetry(item, queue)
	}

	// Cleanup
	cancel()
	wg.Wait()
}

// BenchmarkRetryManager_HandleFailedBatch benchmarks batch failure handling
func BenchmarkRetryManager_HandleFailedBatch(b *testing.B) {
	config := DispatcherConfig{
		MaxRetries:  3,
		RetryDelay:  1 * time.Millisecond,
		DLQEnabled:  false,
	}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wg := &sync.WaitGroup{}

	rm := NewRetryManager(config, logger, nil, ctx, wg, 100)

	queue := make(chan dispatchItem, 10000)

	batch := make([]dispatchItem, 10)
	for i := 0; i < 10; i++ {
		batch[i] = dispatchItem{
			Entry: types.LogEntry{
				Message: "benchmark",
				Labels:  make(map[string]string),
			},
			Retries: 1,
		}
	}

	testErr := errors.New("benchmark error")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rm.HandleFailedBatch(batch, testErr, queue)
	}

	// Cleanup
	cancel()
	wg.Wait()
}
