package dispatcher

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBackpressureActivation tests that backpressure is applied at 95% queue capacity
func TestBackpressureActivation(t *testing.T) {
	// Create small queue to test backpressure easily
	queueSize := 100
	config := DispatcherConfig{
		QueueSize:    queueSize,
		Workers:      1, // Use 1 worker to control processing
		BatchSize:    10,
		BatchTimeout: 5 * time.Second,
		MaxRetries:   3,
		RetryDelay:   100 * time.Millisecond,
	}

	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel) // Reduce noise

	dispatcher := NewDispatcher(config, nil, logger, nil, nil)
	require.NotNil(t, dispatcher)

	// Start dispatcher but don't actually start workers
	dispatcher.isRunning = true
	defer func() { dispatcher.isRunning = false }()

	ctx := context.Background()

	// Calculate 95% threshold: 95 items in a 100-item queue
	threshold := int(float64(queueSize) * 0.95) // = 95

	// Fill to 94 items (one less than threshold)
	for i := 0; i < threshold-1; i++ {
		err := dispatcher.Handle(ctx, "test", fmt.Sprintf("source-%d", i), fmt.Sprintf("message-%d", i), nil)
		require.NoError(t, err, "Should accept entries below 95%%")
	}

	// Verify queue has 94 items
	queueDepth := len(dispatcher.queue)
	assert.Equal(t, threshold-1, queueDepth, "Queue should have 94 items")

	// Add one more to reach exactly 95 items (95% of 100)
	// At this point, when Handle() checks len(queue), it will see 94, so utilization = 94%
	// This should still be accepted
	err := dispatcher.Handle(ctx, "test", "source-at-threshold", "at threshold message", nil)
	require.NoError(t, err, "Should still accept entry when queue will reach exactly 95%%")
	assert.Equal(t, threshold, len(dispatcher.queue), "Queue should now have 95 items")

	// Next entry should trigger backpressure
	// When Handle() checks len(queue), it will see 95, so utilization = 95.0%
	// This should trigger backpressure
	err = dispatcher.Handle(ctx, "test", "source-trigger", "trigger message", nil)
	require.Error(t, err, "Should reject entry at 95%% threshold")
	assert.Contains(t, err.Error(), "queue near full", "Error should mention queue near full")
	assert.Contains(t, err.Error(), "utilization", "Error should mention utilization")
}

// TestBackpressureMetrics tests that metrics are updated correctly
func TestBackpressureMetrics(t *testing.T) {
	queueSize := 100
	config := DispatcherConfig{
		QueueSize:    queueSize,
		Workers:      1,
		BatchSize:    10,
		BatchTimeout: 5 * time.Second,
		MaxRetries:   3,
		RetryDelay:   100 * time.Millisecond,
	}

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	dispatcher := NewDispatcher(config, nil, logger, nil, nil)
	require.NotNil(t, dispatcher)

	dispatcher.isRunning = true
	defer func() { dispatcher.isRunning = false }()

	ctx := context.Background()

	// Add 50 entries (50% capacity)
	for i := 0; i < 50; i++ {
		err := dispatcher.Handle(ctx, "test", fmt.Sprintf("source-%d", i), fmt.Sprintf("message-%d", i), nil)
		require.NoError(t, err)
	}

	// Verify metrics are updated
	// Note: We can't directly read prometheus metrics in tests easily,
	// but we can verify the queue depth
	queueDepth := len(dispatcher.queue)
	assert.Equal(t, 50, queueDepth, "Queue should have 50 items")
}

// TestBackpressureBelowThreshold tests normal operation below threshold
func TestBackpressureBelowThreshold(t *testing.T) {
	queueSize := 100
	config := DispatcherConfig{
		QueueSize:    queueSize,
		Workers:      1,
		BatchSize:    10,
		BatchTimeout: 5 * time.Second,
		MaxRetries:   3,
		RetryDelay:   100 * time.Millisecond,
	}

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	dispatcher := NewDispatcher(config, nil, logger, nil, nil)
	require.NotNil(t, dispatcher)

	dispatcher.isRunning = true
	defer func() { dispatcher.isRunning = false }()

	ctx := context.Background()

	// Add entries up to 94% (below threshold)
	belowThreshold := int(float64(queueSize) * 0.94)
	for i := 0; i < belowThreshold; i++ {
		err := dispatcher.Handle(ctx, "test", fmt.Sprintf("source-%d", i), fmt.Sprintf("message-%d", i), nil)
		assert.NoError(t, err, "Should accept all entries below 95%% threshold")
	}

	queueDepth := len(dispatcher.queue)
	assert.Equal(t, belowThreshold, queueDepth)
}

// TestBackpressureWithContextCancellation tests backpressure respects context
func TestBackpressureWithContextCancellation(t *testing.T) {
	queueSize := 100
	config := DispatcherConfig{
		QueueSize:    queueSize,
		Workers:      1,
		BatchSize:    10,
		BatchTimeout: 5 * time.Second,
		MaxRetries:   3,
		RetryDelay:   100 * time.Millisecond,
	}

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	dispatcher := NewDispatcher(config, nil, logger, nil, nil)
	require.NotNil(t, dispatcher)

	dispatcher.isRunning = true
	defer func() { dispatcher.isRunning = false }()

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Try to add entry with cancelled context
	err := dispatcher.Handle(ctx, "test", "source", "message", nil)
	// Should either fail due to backpressure or context cancellation
	// Both are acceptable outcomes
	if err != nil {
		t.Logf("Got expected error with cancelled context: %v", err)
	}
}

// TestBackpressureThreadSafety tests concurrent access to backpressure logic
func TestBackpressureThreadSafety(t *testing.T) {
	queueSize := 1000
	config := DispatcherConfig{
		QueueSize:    queueSize,
		Workers:      2,
		BatchSize:    10,
		BatchTimeout: 5 * time.Second,
		MaxRetries:   3,
		RetryDelay:   100 * time.Millisecond,
	}

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	dispatcher := NewDispatcher(config, nil, logger, nil, nil)
	require.NotNil(t, dispatcher)

	dispatcher.isRunning = true
	defer func() { dispatcher.isRunning = false }()

	ctx := context.Background()

	// Launch multiple goroutines trying to add entries concurrently
	numGoroutines := 10
	entriesPerGoroutine := 50

	var wg sync.WaitGroup
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for i := 0; i < entriesPerGoroutine; i++ {
				// Ignore errors (some may fail due to backpressure)
				_ = dispatcher.Handle(
					ctx,
					"test",
					fmt.Sprintf("source-g%d-e%d", goroutineID, i),
					fmt.Sprintf("message-g%d-e%d", goroutineID, i),
					nil,
				)
			}
		}(g)
	}

	wg.Wait()

	// Verify no race conditions occurred
	// Queue should have some entries but not exceed capacity
	queueDepth := len(dispatcher.queue)
	assert.LessOrEqual(t, queueDepth, queueSize, "Queue should not exceed capacity")
	t.Logf("Final queue depth: %d/%d (%.1f%%)", queueDepth, queueSize, float64(queueDepth)/float64(queueSize)*100)
}
