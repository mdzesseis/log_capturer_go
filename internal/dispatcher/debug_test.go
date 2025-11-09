package dispatcher

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

// TestBackpressureDebug - debug version to understand the flow
func TestBackpressureDebug(t *testing.T) {
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
	logger.SetLevel(logrus.DebugLevel)

	dispatcher := NewDispatcher(config, nil, logger, nil, nil)
	require.NotNil(t, dispatcher)

	dispatcher.isRunning = true
	defer func() { dispatcher.isRunning = false }()

	ctx := context.Background()

	// Add exactly 95 items
	for i := 0; i < 95; i++ {
		err := dispatcher.Handle(ctx, "test", fmt.Sprintf("source-%d", i), fmt.Sprintf("message-%d", i), nil)
		if err != nil {
			t.Fatalf("Failed to add item %d: %v", i, err)
		}
	}

	queueDepth := len(dispatcher.queue)
	t.Logf("After adding 95 items, queue depth: %d", queueDepth)

	// Try to add 96th item
	err := dispatcher.Handle(ctx, "test", "source-96", "message-96", nil)
	t.Logf("Attempt to add 96th item: err=%v", err)
	t.Logf("Current queue depth: %d", len(dispatcher.queue))
}
