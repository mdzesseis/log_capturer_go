package dispatcher

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

// TestBackpressureDebug2 - check actual flow
func TestBackpressureDebug2(t *testing.T) {
	queueSize := 10 // Small queue for easier testing
	config := DispatcherConfig{
		QueueSize:    queueSize,
		Workers:      1,
		BatchSize:    10,
		BatchTimeout: 5 * time.Second,
		MaxRetries:   3,
		RetryDelay:   100 * time.Millisecond,
	}

	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)
	logger.SetFormatter(&logrus.TextFormatter{
		ForceColors:   true,
		FullTimestamp: true,
	})

	dispatcher := NewDispatcher(config, nil, logger, nil, nil)
	require.NotNil(t, dispatcher)

	dispatcher.isRunning = true
	defer func() { dispatcher.isRunning = false }()

	ctx := context.Background()

	// Add items until queue is at 95%
	// 95% of 10 = 9.5, so we'll add 9 items then try to add 10th
	for i := 0; i < 9; i++ {
		err := dispatcher.Handle(ctx, "test", fmt.Sprintf("source-%d", i), fmt.Sprintf("message-%d", i), map[string]string{"id": fmt.Sprintf("%d", i)})
		if err != nil {
			t.Fatalf("Failed to add item %d: %v (queue depth: %d)", i, err, len(dispatcher.queue))
		}
		t.Logf("Added item %d, queue depth now: %d", i, len(dispatcher.queue))
	}

	queueDepth := len(dispatcher.queue)
	t.Logf("After adding 9 items, queue depth: %d/%d (%.1f%%)", queueDepth, queueSize, float64(queueDepth)/float64(queueSize)*100)

	// Try to add 10th item - should trigger backpressure (9/10 = 90%, but next would be 10/10 = 100%)
	// Wait, our threshold is 95%, so at 9/10=90%, we won't trigger yet
	err := dispatcher.Handle(ctx, "test", "source-9", "message-9", map[string]string{"id": "9"})
	t.Logf("Attempt to add 10th item (queue was at %d): err=%v", queueDepth, err)
	t.Logf("Current queue depth: %d/%d (%.1f%%)", len(dispatcher.queue), queueSize, float64(len(dispatcher.queue))/float64(queueSize)*100)

	// Now queue should be full (10/10 = 100%)
	// Try to add 11th item - should definitely trigger backpressure
	if err == nil {
		err = dispatcher.Handle(ctx, "test", "source-10", "message-10", map[string]string{"id": "10"})
		t.Logf("Attempt to add 11th item (queue at %d): err=%v", len(dispatcher.queue), err)
	}
}
