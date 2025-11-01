package dispatcher

import (
	"context"
	"ssw-logs-capture/pkg/types"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// TestDispatcherBatchRaceCondition tests that batches don't have race conditions (C5)
func TestDispatcherBatchRaceCondition(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := DispatcherConfig{
		QueueSize:    100,
		Workers:      2,
		BatchSize:    10,
		BatchTimeout: 100 * time.Millisecond,
		MaxRetries:   1,
		RetryDelay:   10 * time.Millisecond,
	}

	dispatcher := NewDispatcher(config, nil, logger, nil)

	// Add a mock sink that processes slowly to create race opportunities
	mockSink := &slowSink{delay: 10 * time.Millisecond}
	dispatcher.AddSink(mockSink)

	ctx := context.Background()
	err := dispatcher.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start dispatcher: %v", err)
	}
	defer dispatcher.Stop()

	// Send many entries concurrently to trigger race conditions
	var wg sync.WaitGroup
	numGoroutines := 10
	entriesPerGoroutine := 20

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < entriesPerGoroutine; j++ {
				labels := map[string]string{
					"goroutine": string(rune(id)),
					"index":     string(rune(j)),
				}
				err := dispatcher.Handle(ctx, "test", "test-001", "test message", labels)
				if err != nil {
					t.Logf("Handle error (expected during load): %v", err)
				}
				time.Sleep(time.Millisecond)
			}
		}(i)
	}

	wg.Wait()
	time.Sleep(500 * time.Millisecond) // Let batches process

	t.Logf("✓ No race conditions detected with %d concurrent goroutines", numGoroutines)
}

// slowSink simulates a slow sink to create race condition opportunities
type slowSink struct {
	delay time.Duration
	mu    sync.Mutex
	count int
}

func (s *slowSink) Start(ctx context.Context) error {
	return nil
}

func (s *slowSink) Send(ctx context.Context, entries []types.LogEntry) error {
	s.mu.Lock()
	s.count += len(entries)
	s.mu.Unlock()

	// Simulate slow processing
	time.Sleep(s.delay)

	// Access entry fields to trigger race detector if there's sharing
	for i := range entries {
		_ = entries[i].CopyLabels() // Thread-safe access
		_ = entries[i].Message
	}

	return nil
}

func (s *slowSink) Stop() error {
	return nil
}

func (s *slowSink) IsHealthy() bool {
	return true
}

func (s *slowSink) GetQueueUtilization() float64 {
	return 0.0
}
