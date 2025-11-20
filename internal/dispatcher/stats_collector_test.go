package dispatcher

import (
	"context"
	"sync"
	"testing"
	"time"

	"ssw-logs-capture/pkg/backpressure"
	"ssw-logs-capture/pkg/types"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewStatsCollector tests constructor validation
func TestNewStatsCollector(t *testing.T) {
	stats := &types.DispatcherStats{
		TotalProcessed:   0,
		ErrorCount:       0,
		Throttled:        0,
		QueueSize:        0,
		QueueCapacity:    1000,
		SinkDistribution: make(map[string]int64),
	}
	statsMutex := &sync.RWMutex{}
	config := DispatcherConfig{
		QueueSize:    1000,
		Workers:      4,
		BatchSize:    100,
		BatchTimeout: 1 * time.Second,
	}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	queue := make(chan dispatchItem, 1000)

	sc := NewStatsCollector(stats, statsMutex, config, logger, queue)

	require.NotNil(t, sc)
	assert.NotNil(t, sc.stats)
	assert.NotNil(t, sc.statsMutex)
	assert.Equal(t, config.QueueSize, sc.config.QueueSize)
	assert.NotNil(t, sc.logger)
}

// TestStatsCollector_UpdateStats tests thread-safe stat updates
func TestStatsCollector_UpdateStats(t *testing.T) {
	stats := &types.DispatcherStats{
		TotalProcessed:   0,
		ErrorCount:       0,
		SinkDistribution: make(map[string]int64),
	}
	statsMutex := &sync.RWMutex{}
	config := DispatcherConfig{QueueSize: 1000}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	queue := make(chan dispatchItem, 1000)

	sc := NewStatsCollector(stats, statsMutex, config, logger, queue)

	// Update stats
	sc.UpdateStats(func(s *types.DispatcherStats) {
		s.TotalProcessed = 100
		s.ErrorCount = 5
	})

	// Verify updates
	assert.Equal(t, int64(100), sc.stats.TotalProcessed)
	assert.Equal(t, int64(5), sc.stats.ErrorCount)
}

// TestStatsCollector_GetStats tests safe stats retrieval with deep copy
func TestStatsCollector_GetStats(t *testing.T) {
	stats := &types.DispatcherStats{
		TotalProcessed: 100,
		ErrorCount:     10,
		QueueSize:      50,
		SinkDistribution: map[string]int64{
			"loki":  80,
			"local": 20,
		},
	}
	statsMutex := &sync.RWMutex{}
	config := DispatcherConfig{QueueSize: 1000}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	queue := make(chan dispatchItem, 1000)

	sc := NewStatsCollector(stats, statsMutex, config, logger, queue)

	// Get stats copy
	statsCopy := sc.GetStats()

	assert.Equal(t, int64(100), statsCopy.TotalProcessed)
	assert.Equal(t, int64(10), statsCopy.ErrorCount)
	assert.Equal(t, int64(80), statsCopy.SinkDistribution["loki"])

	// Modify copy - should not affect original
	statsCopy.TotalProcessed = 999
	statsCopy.SinkDistribution["loki"] = 999

	assert.Equal(t, int64(100), stats.TotalProcessed, "Original should be unchanged")
	assert.Equal(t, int64(80), stats.SinkDistribution["loki"], "Original map should be unchanged")
}

// TestStatsCollector_ConcurrentUpdates tests thread safety with concurrent updates
func TestStatsCollector_ConcurrentUpdates(t *testing.T) {
	stats := &types.DispatcherStats{
		TotalProcessed:   0,
		ErrorCount:       0,
		SinkDistribution: make(map[string]int64),
	}
	statsMutex := &sync.RWMutex{}
	config := DispatcherConfig{QueueSize: 1000}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	queue := make(chan dispatchItem, 1000)

	sc := NewStatsCollector(stats, statsMutex, config, logger, queue)

	// Concurrently update stats
	var wg sync.WaitGroup
	numGoroutines := 10
	updatesPerGoroutine := 100

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < updatesPerGoroutine; j++ {
				sc.UpdateStats(func(s *types.DispatcherStats) {
					s.TotalProcessed++
				})
			}
		}()
	}

	wg.Wait()

	// Verify final count
	finalStats := sc.GetStats()
	expected := int64(numGoroutines * updatesPerGoroutine)
	assert.Equal(t, expected, finalStats.TotalProcessed, "All updates should be reflected")
}

// TestStatsCollector_IncrementProcessed tests processed counter
func TestStatsCollector_IncrementProcessed(t *testing.T) {
	stats := &types.DispatcherStats{
		TotalProcessed: 0,
		SinkDistribution: make(map[string]int64),
	}
	statsMutex := &sync.RWMutex{}
	config := DispatcherConfig{QueueSize: 1000}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	queue := make(chan dispatchItem, 1000)

	sc := NewStatsCollector(stats, statsMutex, config, logger, queue)

	// Increment multiple times
	for i := 0; i < 5; i++ {
		sc.IncrementProcessed()
	}

	finalStats := sc.GetStats()
	assert.Equal(t, int64(5), finalStats.TotalProcessed)
	assert.False(t, finalStats.LastProcessedTime.IsZero(), "LastProcessedTime should be set")
}

// TestStatsCollector_IncrementErrors tests error counter
func TestStatsCollector_IncrementErrors(t *testing.T) {
	stats := &types.DispatcherStats{
		ErrorCount: 0,
		SinkDistribution: make(map[string]int64),
	}
	statsMutex := &sync.RWMutex{}
	config := DispatcherConfig{QueueSize: 1000}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	queue := make(chan dispatchItem, 1000)

	sc := NewStatsCollector(stats, statsMutex, config, logger, queue)

	// Increment errors
	for i := 0; i < 3; i++ {
		sc.IncrementErrors()
	}

	finalStats := sc.GetStats()
	assert.Equal(t, int64(3), finalStats.ErrorCount)
}

// TestStatsCollector_IncrementThrottled tests throttled counter
func TestStatsCollector_IncrementThrottled(t *testing.T) {
	stats := &types.DispatcherStats{
		Throttled: 0,
		SinkDistribution: make(map[string]int64),
	}
	statsMutex := &sync.RWMutex{}
	config := DispatcherConfig{QueueSize: 1000}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	queue := make(chan dispatchItem, 1000)

	sc := NewStatsCollector(stats, statsMutex, config, logger, queue)

	// Increment throttled
	for i := 0; i < 7; i++ {
		sc.IncrementThrottled()
	}

	finalStats := sc.GetStats()
	assert.Equal(t, int64(7), finalStats.Throttled)
}

// TestStatsCollector_UpdateQueueSize tests queue size updates
func TestStatsCollector_UpdateQueueSize(t *testing.T) {
	stats := &types.DispatcherStats{
		QueueSize: 0,
		SinkDistribution: make(map[string]int64),
	}
	statsMutex := &sync.RWMutex{}
	config := DispatcherConfig{QueueSize: 1000}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	queue := make(chan dispatchItem, 1000)

	// Add items to queue
	for i := 0; i < 50; i++ {
		queue <- dispatchItem{
			Entry: &types.LogEntry{
				Message: "test",
				Labels:  make(map[string]string),
			},
		}
	}

	sc := NewStatsCollector(stats, statsMutex, config, logger, queue)

	sc.UpdateQueueSize()

	finalStats := sc.GetStats()
	assert.Equal(t, 50, finalStats.QueueSize)
}

// TestStatsCollector_UpdateSinkDistribution tests sink distribution tracking
func TestStatsCollector_UpdateSinkDistribution(t *testing.T) {
	stats := &types.DispatcherStats{
		SinkDistribution: make(map[string]int64),
	}
	statsMutex := &sync.RWMutex{}
	config := DispatcherConfig{QueueSize: 1000}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	queue := make(chan dispatchItem, 1000)

	sc := NewStatsCollector(stats, statsMutex, config, logger, queue)

	// Update sink distribution
	sc.UpdateSinkDistribution("loki", 10)
	sc.UpdateSinkDistribution("local", 5)
	sc.UpdateSinkDistribution("loki", 15) // Add more to loki

	finalStats := sc.GetStats()
	assert.Equal(t, int64(25), finalStats.SinkDistribution["loki"])
	assert.Equal(t, int64(5), finalStats.SinkDistribution["local"])
}

// TestStatsCollector_RunStatsUpdater tests periodic statistics updates
func TestStatsCollector_RunStatsUpdater(t *testing.T) {
	stats := &types.DispatcherStats{
		TotalProcessed:   0,
		QueueSize:        0,
		SinkDistribution: make(map[string]int64),
	}
	statsMutex := &sync.RWMutex{}
	config := DispatcherConfig{QueueSize: 1000}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	queue := make(chan dispatchItem, 1000)

	sc := NewStatsCollector(stats, statsMutex, config, logger, queue)

	// Mock retry stats function
	getRetryStats := func() map[string]interface{} {
		return map[string]interface{}{
			"current_retries":        5,
			"max_concurrent_retries": 10,
			"utilization":            0.5,
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Start stats updater in background
	go sc.RunStatsUpdater(ctx, getRetryStats)

	// Simulate processing
	sc.UpdateStats(func(s *types.DispatcherStats) {
		s.TotalProcessed = 100
	})

	// Wait for at least one update cycle
	time.Sleep(200 * time.Millisecond)

	// Add more processing
	sc.UpdateStats(func(s *types.DispatcherStats) {
		s.TotalProcessed = 200
	})

	// Wait for another update cycle
	time.Sleep(200 * time.Millisecond)

	// Stop updater
	cancel()

	// Give time for goroutine to exit
	time.Sleep(100 * time.Millisecond)

	// Stats should have been updated
	finalStats := sc.GetStats()
	assert.Equal(t, int64(200), finalStats.TotalProcessed)
}

// TestStatsCollector_RunStatsUpdater_HighRetryUtilization tests high retry queue warning
func TestStatsCollector_RunStatsUpdater_HighRetryUtilization(t *testing.T) {
	stats := &types.DispatcherStats{
		SinkDistribution: make(map[string]int64),
	}
	statsMutex := &sync.RWMutex{}
	config := DispatcherConfig{QueueSize: 1000}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel) // Use error level to avoid log spam
	queue := make(chan dispatchItem, 1000)

	sc := NewStatsCollector(stats, statsMutex, config, logger, queue)

	// Mock retry stats with high utilization
	getRetryStats := func() map[string]interface{} {
		return map[string]interface{}{
			"current_retries":        9,
			"max_concurrent_retries": 10,
			"utilization":            0.9, // 90% - should trigger warning
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Run stats updater (will exit when context is cancelled)
	sc.RunStatsUpdater(ctx, getRetryStats)

	// Should complete without hanging
}

// TestStatsCollector_UpdateBackpressureMetrics tests backpressure metric calculation
func TestStatsCollector_UpdateBackpressureMetrics(t *testing.T) {
	stats := &types.DispatcherStats{
		TotalProcessed:   1000,
		ErrorCount:       50,
		QueueSize:        500,
		SinkDistribution: make(map[string]int64),
	}
	statsMutex := &sync.RWMutex{}
	config := DispatcherConfig{QueueSize: 1000}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	queue := make(chan dispatchItem, 1000)

	sc := NewStatsCollector(stats, statsMutex, config, logger, queue)

	// Create backpressure manager
	bpConfig := backpressure.Config{
		LowThreshold:    0.6,
		MediumThreshold: 0.75,
		HighThreshold:   0.9,
	}
	bpManager := backpressure.NewManager(bpConfig, logger)

	// Update backpressure metrics
	sc.UpdateBackpressureMetrics(bpManager)

	// Verify metrics were updated (no panic)
	// Actual values depend on implementation, just verify it runs
}

// TestStatsCollector_UpdateBackpressureMetrics_NilManager tests nil manager handling
func TestStatsCollector_UpdateBackpressureMetrics_NilManager(t *testing.T) {
	stats := &types.DispatcherStats{
		TotalProcessed:   100,
		SinkDistribution: make(map[string]int64),
	}
	statsMutex := &sync.RWMutex{}
	config := DispatcherConfig{QueueSize: 1000}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	queue := make(chan dispatchItem, 1000)

	sc := NewStatsCollector(stats, statsMutex, config, logger, queue)

	// Should not panic with nil manager
	assert.NotPanics(t, func() {
		sc.UpdateBackpressureMetrics(nil)
	})
}

// TestStatsCollector_Stop_ContextCancellation tests graceful stop via context
func TestStatsCollector_Stop_ContextCancellation(t *testing.T) {
	stats := &types.DispatcherStats{
		SinkDistribution: make(map[string]int64),
	}
	statsMutex := &sync.RWMutex{}
	config := DispatcherConfig{QueueSize: 1000}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	queue := make(chan dispatchItem, 1000)

	sc := NewStatsCollector(stats, statsMutex, config, logger, queue)

	ctx, cancel := context.WithCancel(context.Background())

	// Start stats updater
	done := make(chan struct{})
	go func() {
		sc.RunStatsUpdater(ctx, nil)
		close(done)
	}()

	// Wait a bit then cancel
	time.Sleep(50 * time.Millisecond)
	cancel()

	// Should exit quickly
	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("Stats updater did not stop in time")
	}
}

// BenchmarkStatsCollector_UpdateStats benchmarks stat updates
func BenchmarkStatsCollector_UpdateStats(b *testing.B) {
	stats := &types.DispatcherStats{
		TotalProcessed:   0,
		SinkDistribution: make(map[string]int64),
	}
	statsMutex := &sync.RWMutex{}
	config := DispatcherConfig{QueueSize: 1000}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	queue := make(chan dispatchItem, 1000)

	sc := NewStatsCollector(stats, statsMutex, config, logger, queue)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sc.UpdateStats(func(s *types.DispatcherStats) {
			s.TotalProcessed++
		})
	}
}

// BenchmarkStatsCollector_GetStats benchmarks stats retrieval
func BenchmarkStatsCollector_GetStats(b *testing.B) {
	stats := &types.DispatcherStats{
		TotalProcessed: 1000,
		SinkDistribution: map[string]int64{
			"loki":  500,
			"local": 500,
		},
	}
	statsMutex := &sync.RWMutex{}
	config := DispatcherConfig{QueueSize: 1000}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	queue := make(chan dispatchItem, 1000)

	sc := NewStatsCollector(stats, statsMutex, config, logger, queue)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sc.GetStats()
	}
}

// BenchmarkStatsCollector_ConcurrentAccess benchmarks concurrent read/write
func BenchmarkStatsCollector_ConcurrentAccess(b *testing.B) {
	stats := &types.DispatcherStats{
		TotalProcessed:   0,
		SinkDistribution: make(map[string]int64),
	}
	statsMutex := &sync.RWMutex{}
	config := DispatcherConfig{QueueSize: 1000}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	queue := make(chan dispatchItem, 1000)

	sc := NewStatsCollector(stats, statsMutex, config, logger, queue)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			// Mix of reads and writes
			if b.N%2 == 0 {
				sc.UpdateStats(func(s *types.DispatcherStats) {
					s.TotalProcessed++
				})
			} else {
				_ = sc.GetStats()
			}
		}
	})
}
