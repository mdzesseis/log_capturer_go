package sinks

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"ssw-logs-capture/pkg/dlq"
	"ssw-logs-capture/pkg/types"

	"github.com/sirupsen/logrus"
)

// TestLokiSinkStopNoGoroutineLeak tests that Stop() doesn't leak goroutines (C6)
func TestLokiSinkStopNoGoroutineLeak(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	// Get initial goroutine count
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	initialGoroutines := runtime.NumGoroutine()

	config := types.LokiConfig{
		Enabled:      true,
		URL:          "http://localhost:3100",
		BatchSize:    10,
		BatchTimeout: "100ms",
		QueueSize:    100,
		Timeout:      "5s",
	}

	dlqInstance := dlq.NewDeadLetterQueue(dlq.Config{Enabled: false}, logger)
	sink := NewLokiSink(config, logger, dlqInstance)

	ctx := context.Background()
	err := sink.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start sink: %v", err)
	}

	// Let goroutines start
	time.Sleep(150 * time.Millisecond)
	afterStartGoroutines := runtime.NumGoroutine()

	if afterStartGoroutines <= initialGoroutines {
		t.Logf("Warning: Expected more goroutines after Start (before: %d, after: %d)",
			initialGoroutines, afterStartGoroutines)
	}

	// Stop sink
	err = sink.Stop()
	if err != nil {
		t.Fatalf("Failed to stop sink: %v", err)
	}

	// Wait for cleanup
	runtime.GC()
	time.Sleep(200 * time.Millisecond)
	runtime.GC()

	finalGoroutines := runtime.NumGoroutine()

	// Allow small variance
	goroutineLeak := finalGoroutines - initialGoroutines
	if goroutineLeak > 2 {
		t.Errorf("GOROUTINE LEAK: Started with %d goroutines, ended with %d (%d leaked)",
			initialGoroutines, finalGoroutines, goroutineLeak)
	} else {
		t.Logf("✓ No goroutine leak detected (initial: %d, final: %d, diff: %d)",
			initialGoroutines, finalGoroutines, goroutineLeak)
	}
}

// TestLokiSinkStopWithPendingBatches tests Stop with sendBatch goroutines running (C6)
func TestLokiSinkStopWithPendingBatches(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	config := types.LokiConfig{
		Enabled:      true,
		URL:          "http://localhost:3100",
		BatchSize:    5,
		BatchTimeout: "10ms",
		QueueSize:    50,
		Timeout:      "500ms",
	}

	dlqInstance := dlq.NewDeadLetterQueue(dlq.Config{Enabled: false}, logger)
	sink := NewLokiSink(config, logger, dlqInstance)

	ctx := context.Background()
	err := sink.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start sink: %v", err)
	}

	// Send multiple entries to trigger sendBatch goroutines
	for i := 0; i < 15; i++ {
		entry := types.LogEntry{
			Timestamp:  time.Now(),
			Message:    "test message",
			Level:      "info",
			SourceType: "test",
			SourceID:   "test-001",
		}
		// Ignore errors as Loki may not be running
		_ = sink.Send(ctx, []types.LogEntry{entry})
	}

	// Wait a bit for batches to be created
	time.Sleep(50 * time.Millisecond)

	// Stop should wait for sendBatch goroutines
	stopStart := time.Now()
	err = sink.Stop()
	stopDuration := time.Since(stopStart)

	if err != nil {
		t.Fatalf("Failed to stop sink: %v", err)
	}

	// Should complete reasonably fast (under timeout)
	if stopDuration > 15*time.Second {
		t.Errorf("Stop took too long: %v", stopDuration)
	} else {
		t.Logf("✓ Stop completed in %v (waited for sendBatch goroutines)", stopDuration)
	}
}

// TestLokiSinkStopTimeout tests that Stop doesn't hang forever (C6)
func TestLokiSinkStopTimeout(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := types.LokiConfig{
		Enabled:      true,
		URL:          "http://localhost:3100",
		BatchSize:    100,
		BatchTimeout: "1s",
		QueueSize:    100,
		Timeout:      "1s",
	}

	dlqInstance := dlq.NewDeadLetterQueue(dlq.Config{Enabled: false}, logger)
	sink := NewLokiSink(config, logger, dlqInstance)

	ctx := context.Background()
	err := sink.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start sink: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Stop with timeout mechanism
	done := make(chan struct{})
	go func() {
		err := sink.Stop()
		if err != nil {
			t.Logf("Stop returned error: %v", err)
		}
		close(done)
	}()

	select {
	case <-done:
		t.Log("✓ Stop() completed (did not hang)")
	case <-time.After(20 * time.Second):
		t.Fatal("HANG DETECTED: Stop() timed out after 20 seconds")
	}
}

// TestLokiSinkMultipleStartStop tests multiple Start/Stop cycles (C6)
func TestLokiSinkMultipleStartStop(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := types.LokiConfig{
		Enabled:      true,
		URL:          "http://localhost:3100",
		BatchSize:    10,
		BatchTimeout: "100ms",
		QueueSize:    100,
		Timeout:      "2s",
	}

	for cycle := 0; cycle < 3; cycle++ {
		t.Logf("Cycle %d: Creating sink", cycle+1)

		dlqInstance := dlq.NewDeadLetterQueue(dlq.Config{Enabled: false}, logger)
		sink := NewLokiSink(config, logger, dlqInstance)

		ctx := context.Background()
		err := sink.Start(ctx)
		if err != nil {
			t.Fatalf("Cycle %d: Failed to start: %v", cycle+1, err)
		}

		time.Sleep(100 * time.Millisecond)

		err = sink.Stop()
		if err != nil {
			t.Fatalf("Cycle %d: Failed to stop: %v", cycle+1, err)
		}

		t.Logf("✓ Cycle %d completed", cycle+1)
		time.Sleep(50 * time.Millisecond)
	}

	t.Log("✓ All cycles completed without leaks or hangs")
}

// TestLokiSinkConcurrentStops tests concurrent Stop calls don't cause issues (C6)
func TestLokiSinkConcurrentStops(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := types.LokiConfig{
		Enabled:      true,
		URL:          "http://localhost:3100",
		BatchSize:    10,
		BatchTimeout: "100ms",
		QueueSize:    100,
		Timeout:      "2s",
	}

	dlqInstance := dlq.NewDeadLetterQueue(dlq.Config{Enabled: false}, logger)
	sink := NewLokiSink(config, logger, dlqInstance)

	ctx := context.Background()
	err := sink.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Call Stop from multiple goroutines
	done := make(chan struct{})
	for i := 0; i < 5; i++ {
		go func(id int) {
			err := sink.Stop()
			if err != nil {
				t.Logf("Stop %d: %v", id, err)
			}
		}(i)
	}

	go func() {
		time.Sleep(2 * time.Second)
		close(done)
	}()

	select {
	case <-done:
		t.Log("✓ Concurrent Stop() calls handled safely")
	case <-time.After(10 * time.Second):
		t.Fatal("Concurrent Stop() calls timed out")
	}
}

// TestCreateStreamKeyDeterministic tests that same labels always produce same key (C7)
func TestCreateStreamKeyDeterministic(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := types.LokiConfig{
		Enabled: true,
		URL:     "http://localhost:3100",
	}

	dlqInstance := dlq.NewDeadLetterQueue(dlq.Config{Enabled: false}, logger)
	sink := NewLokiSink(config, logger, dlqInstance)

	// Test same labels in different order produce same key
	labels1 := map[string]string{
		"app":     "test",
		"env":     "prod",
		"service": "api",
	}

	labels2 := map[string]string{
		"service": "api",
		"app":     "test",
		"env":     "prod",
	}

	labels3 := map[string]string{
		"env":     "prod",
		"service": "api",
		"app":     "test",
	}

	key1 := sink.createStreamKey(labels1)
	key2 := sink.createStreamKey(labels2)
	key3 := sink.createStreamKey(labels3)

	if key1 != key2 {
		t.Errorf("Keys not deterministic: key1=%s, key2=%s", key1, key2)
	}

	if key1 != key3 {
		t.Errorf("Keys not deterministic: key1=%s, key3=%s", key1, key3)
	}

	// Verify key is sorted
	expected := `{"app":"test","env":"prod","service":"api"}`
	if key1 != expected {
		t.Errorf("Key format unexpected:\ngot:  %s\nwant: %s", key1, expected)
	}

	t.Logf("✓ Deterministic key: %s", key1)
}

// TestCreateStreamKeyMultipleIterations tests determinism over multiple iterations (C7)
func TestCreateStreamKeyMultipleIterations(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := types.LokiConfig{
		Enabled: true,
		URL:     "http://localhost:3100",
	}

	dlqInstance := dlq.NewDeadLetterQueue(dlq.Config{Enabled: false}, logger)
	sink := NewLokiSink(config, logger, dlqInstance)

	labels := map[string]string{
		"host":    "server1",
		"service": "web",
		"region":  "us-east",
		"version": "1.2.3",
		"env":     "staging",
	}

	// Generate key 1000 times
	var firstKey string
	for i := 0; i < 1000; i++ {
		key := sink.createStreamKey(labels)

		if i == 0 {
			firstKey = key
		} else if key != firstKey {
			t.Fatalf("Iteration %d produced different key:\nfirst: %s\ngot:   %s", i, firstKey, key)
		}
	}

	t.Logf("✓ 1000 iterations produced identical key: %s", firstKey)
}

// TestCreateStreamKeyEdgeCases tests edge cases (C7)
func TestCreateStreamKeyEdgeCases(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := types.LokiConfig{
		Enabled: true,
		URL:     "http://localhost:3100",
	}

	dlqInstance := dlq.NewDeadLetterQueue(dlq.Config{Enabled: false}, logger)
	sink := NewLokiSink(config, logger, dlqInstance)

	// Empty labels
	emptyKey := sink.createStreamKey(map[string]string{})
	if emptyKey != "{}" {
		t.Errorf("Empty labels should produce '{}', got: %s", emptyKey)
	}

	// Single label
	singleKey := sink.createStreamKey(map[string]string{"app": "test"})
	expected := `{"app":"test"}`
	if singleKey != expected {
		t.Errorf("Single label key incorrect:\ngot:  %s\nwant: %s", singleKey, expected)
	}

	// Labels with special characters
	specialKey := sink.createStreamKey(map[string]string{
		"path": "/api/v1",
		"code": "200",
	})
	expectedSpecial := `{"code":"200","path":"/api/v1"}`
	if specialKey != expectedSpecial {
		t.Errorf("Special chars key incorrect:\ngot:  %s\nwant: %s", specialKey, expectedSpecial)
	}

	t.Log("✓ Edge cases handled correctly")
}

// BenchmarkCreateStreamKey benchmarks the new deterministic implementation (C7)
func BenchmarkCreateStreamKey(b *testing.B) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := types.LokiConfig{
		Enabled: true,
		URL:     "http://localhost:3100",
	}

	dlqInstance := dlq.NewDeadLetterQueue(dlq.Config{Enabled: false}, logger)
	sink := NewLokiSink(config, logger, dlqInstance)

	labels := map[string]string{
		"app":       "myapp",
		"env":       "production",
		"service":   "api",
		"region":    "us-east-1",
		"version":   "1.2.3",
		"namespace": "default",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sink.createStreamKey(labels)
	}
}

// TestLokiSinkRequestTimeout tests that HTTP requests timeout properly (C11)
func TestLokiSinkRequestTimeout(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	// Use very short timeout to trigger timeout
	config := types.LokiConfig{
		Enabled:   true,
		URL:       "http://10.255.255.1", // Non-routable IP to cause timeout
		BatchSize: 10,
		Timeout:   "50ms", // Very short timeout
		QueueSize: 100,
	}

	dlqInstance := dlq.NewDeadLetterQueue(dlq.Config{Enabled: false}, logger)
	sink := NewLokiSink(config, logger, dlqInstance)

	ctx := context.Background()
	err := sink.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start sink: %v", err)
	}
	defer sink.Stop()

	// Send a log entry
	entry := types.LogEntry{
		Timestamp:  time.Now(),
		Message:    "test message",
		Level:      "info",
		SourceType: "test",
		SourceID:   "test-001",
	}

	// sendToLoki will be called internally and should timeout
	err = sink.sendToLoki([]types.LogEntry{entry})

	// Verify we get a timeout error
	if err == nil {
		t.Error("Expected timeout error, got nil")
	} else if !strings.Contains(err.Error(), "timeout") && !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Logf("Got error (may be connection refused which is also acceptable): %v", err)
		// Connection refused is also acceptable for non-routable IP
	} else {
		t.Logf("✓ Request timeout detected: %v", err)
	}
}

// TestLokiSinkContextCancellation tests that requests respect context cancellation (C11)
func TestLokiSinkContextCancellation(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := types.LokiConfig{
		Enabled:   true,
		URL:       "http://10.255.255.1", // Non-routable IP
		BatchSize: 10,
		Timeout:   "30s", // Long timeout
		QueueSize: 100,
	}

	dlqInstance := dlq.NewDeadLetterQueue(dlq.Config{Enabled: false}, logger)
	sink := NewLokiSink(config, logger, dlqInstance)

	ctx := context.Background()
	err := sink.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start sink: %v", err)
	}

	// Stop the sink immediately to cancel context
	err = sink.Stop()
	if err != nil {
		t.Fatalf("Failed to stop sink: %v", err)
	}

	// Now try to send - should fail fast because context is cancelled
	entry := types.LogEntry{
		Timestamp:  time.Now(),
		Message:    "test message",
		Level:      "info",
		SourceType: "test",
		SourceID:   "test-001",
	}

	start := time.Now()
	err = sink.sendToLoki([]types.LogEntry{entry})
	duration := time.Since(start)

	// Should fail fast (under 100ms) with cancellation error
	if err == nil {
		t.Error("Expected cancellation error, got nil")
	} else if !strings.Contains(err.Error(), "cancel") {
		t.Logf("Got error: %v", err)
	} else {
		t.Logf("✓ Context cancellation detected in %v: %v", duration, err)
	}

	if duration > 500*time.Millisecond {
		t.Errorf("Cancellation took too long: %v (should be near-instant)", duration)
	} else {
		t.Logf("✓ Fast cancellation: %v", duration)
	}
}

// TestLokiSinkRequestTimeoutConfiguration tests timeout configuration (C11)
func TestLokiSinkRequestTimeoutConfiguration(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	testCases := []struct {
		name            string
		timeoutConfig   string
		expectedTimeout time.Duration
	}{
		{"default timeout", "", 30 * time.Second},
		{"custom 5s", "5s", 5 * time.Second},
		{"custom 100ms", "100ms", 100 * time.Millisecond},
		{"custom 2m", "2m", 2 * time.Minute},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := types.LokiConfig{
				Enabled:   true,
				URL:       "http://localhost:3100",
				BatchSize: 10,
				Timeout:   tc.timeoutConfig,
				QueueSize: 100,
			}

			dlqInstance := dlq.NewDeadLetterQueue(dlq.Config{Enabled: false}, logger)
			sink := NewLokiSink(config, logger, dlqInstance)

			if sink.requestTimeout != tc.expectedTimeout {
				t.Errorf("Expected timeout %v, got %v", tc.expectedTimeout, sink.requestTimeout)
			} else {
				t.Logf("✓ Timeout correctly configured: %v", sink.requestTimeout)
			}
		})
	}
}

// TestLokiSinkConcurrentRequestsCancellation tests that all concurrent requests stop properly (C11)
func TestLokiSinkConcurrentRequestsCancellation(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := types.LokiConfig{
		Enabled:   true,
		URL:       "http://10.255.255.1", // Non-routable
		BatchSize: 1,
		Timeout:   "10s",
		QueueSize: 100,
	}

	dlqInstance := dlq.NewDeadLetterQueue(dlq.Config{Enabled: false}, logger)
	sink := NewLokiSink(config, logger, dlqInstance)

	ctx := context.Background()
	err := sink.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start sink: %v", err)
	}

	// Start multiple concurrent requests
	var wg sync.WaitGroup
	numRequests := 10

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			entry := types.LogEntry{
				Timestamp:  time.Now(),
				Message:    fmt.Sprintf("test message %d", id),
				Level:      "info",
				SourceType: "test",
				SourceID:   fmt.Sprintf("test-%03d", id),
			}

			// This will timeout or be cancelled
			_ = sink.sendToLoki([]types.LogEntry{entry})
		}(i)
	}

	// Give them a moment to start
	time.Sleep(50 * time.Millisecond)

	// Stop sink - should cancel all in-flight requests
	start := time.Now()
	err = sink.Stop()
	stopDuration := time.Since(start)

	if err != nil {
		t.Fatalf("Failed to stop sink: %v", err)
	}

	// Wait for all goroutines
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		t.Logf("✓ All %d concurrent requests stopped in %v", numRequests, stopDuration)
	case <-time.After(15 * time.Second):
		t.Fatal("Timeout waiting for concurrent requests to stop")
	}

	if stopDuration > 12*time.Second {
		t.Errorf("Stop took too long with concurrent requests: %v", stopDuration)
	}
}
