package benchmarks

import (
	"context"
	"fmt"
	"runtime"
	"testing"
	"time"

	"ssw-logs-capture/internal/dispatcher"
	"ssw-logs-capture/pkg/types"

	"github.com/sirupsen/logrus"
)

// TestMemoryUsage_Sustained tests memory stability over time
func TestMemoryUsage_Sustained(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running memory test in short mode")
	}

	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	config := types.DispatcherConfig{
		QueueSize:      10000,
		WorkerCount:    4,
		BatchSize:      100,
		BatchTimeout:   "100ms",
		MaxRetries:     3,
		RetryBaseDelay: "100ms",
		RetryMaxDelay:  "5s",
	}

	sink := NewBenchmarkSink("memory-test-sink")

	d := dispatcher.NewDispatcher(config, nil, logger, nil)
	d.AddSink(sink)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := d.Start(ctx); err != nil {
		t.Fatalf("Failed to start dispatcher: %v", err)
	}
	defer d.Stop()

	// Force GC and get baseline memory
	runtime.GC()
	time.Sleep(100 * time.Millisecond)

	var baselineStats runtime.MemStats
	runtime.ReadMemStats(&baselineStats)

	t.Logf("Baseline Memory:")
	t.Logf("  Alloc: %.2f MB", float64(baselineStats.Alloc)/(1024*1024))
	t.Logf("  TotalAlloc: %.2f MB", float64(baselineStats.TotalAlloc)/(1024*1024))
	t.Logf("  Sys: %.2f MB", float64(baselineStats.Sys)/(1024*1024))
	t.Logf("  NumGC: %d", baselineStats.NumGC)
	t.Logf("  Goroutines: %d", runtime.NumGoroutine())

	// Run for 1 minute, sending 10k logs/sec
	duration := 60 * time.Second
	logsPerSecond := 10000
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	startTime := time.Now()
	samples := []runtime.MemStats{}

	entry := types.LogEntry{
		Timestamp:  time.Now(),
		Message:    "Memory test log message with reasonable content length to simulate production",
		Level:      "info",
		SourceType: "memory-test",
		SourceID:   "mem-001",
		Labels: map[string]string{
			"test": "memory",
			"env":  "benchmark",
		},
	}

	t.Logf("Starting sustained load test for %v at %d logs/sec...", duration, logsPerSecond)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Send logs for this second
				for i := 0; i < logsPerSecond; i++ {
					d.HandleLogEntry(ctx, entry)
				}

				// Sample memory every 10 seconds
				if int(time.Since(startTime).Seconds())%10 == 0 {
					var ms runtime.MemStats
					runtime.ReadMemStats(&ms)
					samples = append(samples, ms)

					t.Logf("[%ds] Alloc: %.2f MB, Sys: %.2f MB, NumGC: %d, Goroutines: %d",
						int(time.Since(startTime).Seconds()),
						float64(ms.Alloc)/(1024*1024),
						float64(ms.Sys)/(1024*1024),
						ms.NumGC,
						runtime.NumGoroutine())
				}

				if time.Since(startTime) >= duration {
					cancel()
					return
				}
			}
		}
	}()

	// Wait for test to complete
	<-ctx.Done()

	// Give time for processing to finish
	time.Sleep(2 * time.Second)

	// Final GC and measurement
	runtime.GC()
	time.Sleep(100 * time.Millisecond)

	var finalStats runtime.MemStats
	runtime.ReadMemStats(&finalStats)

	t.Logf("\nFinal Memory:")
	t.Logf("  Alloc: %.2f MB", float64(finalStats.Alloc)/(1024*1024))
	t.Logf("  TotalAlloc: %.2f MB", float64(finalStats.TotalAlloc)/(1024*1024))
	t.Logf("  Sys: %.2f MB", float64(finalStats.Sys)/(1024*1024))
	t.Logf("  NumGC: %d", finalStats.NumGC)
	t.Logf("  Goroutines: %d", runtime.NumGoroutine())

	// Analyze results
	allocDiff := int64(finalStats.Alloc) - int64(baselineStats.Alloc)
	allocDiffMB := float64(allocDiff) / (1024 * 1024)

	t.Logf("\nMemory Delta:")
	t.Logf("  Alloc Diff: %.2f MB", allocDiffMB)
	t.Logf("  Goroutine Diff: %d", runtime.NumGoroutine()-int(baselineStats.NumGC))

	// Check for memory leak
	// Allow 50MB growth for caches/buffers, but not continuous growth
	if allocDiffMB > 50 {
		t.Logf("WARNING: Memory increased by %.2f MB (may indicate leak)", allocDiffMB)

		// Check if memory is growing linearly
		if len(samples) >= 3 {
			firstAlloc := float64(samples[0].Alloc) / (1024 * 1024)
			midAlloc := float64(samples[len(samples)/2].Alloc) / (1024 * 1024)
			lastAlloc := float64(samples[len(samples)-1].Alloc) / (1024 * 1024)

			if lastAlloc > midAlloc*1.2 && midAlloc > firstAlloc*1.2 {
				t.Errorf("MEMORY LEAK DETECTED: Continuous growth pattern (%.2f -> %.2f -> %.2f MB)",
					firstAlloc, midAlloc, lastAlloc)
			}
		}
	} else {
		t.Logf("✓ Memory usage is stable (diff: %.2f MB)", allocDiffMB)
	}

	totalProcessed := sink.sendCount.Load()
	t.Logf("\nProcessed %d logs in %v (%.0f logs/sec)",
		totalProcessed,
		duration,
		float64(totalProcessed)/duration.Seconds())
}

// BenchmarkMemoryAllocation_LogEntry measures memory allocations per log entry
func BenchmarkMemoryAllocation_LogEntry(b *testing.B) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := types.DispatcherConfig{
		QueueSize:      10000,
		WorkerCount:    2,
		BatchSize:      100,
		BatchTimeout:   "100ms",
		MaxRetries:     1,
		RetryBaseDelay: "10ms",
		RetryMaxDelay:  "1s",
	}

	sink := NewBenchmarkSink("memory-bench-sink")

	d := dispatcher.NewDispatcher(config, nil, logger, nil)
	d.AddSink(sink)

	ctx := context.Background()
	if err := d.Start(ctx); err != nil {
		b.Fatalf("Failed to start dispatcher: %v", err)
	}
	defer d.Stop()

	entry := types.LogEntry{
		Timestamp:  time.Now(),
		Message:    "Allocation benchmark message",
		Level:      "info",
		SourceType: "benchmark",
		SourceID:   "alloc-001",
		Labels: map[string]string{
			"benchmark": "memory",
		},
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if err := d.HandleLogEntry(ctx, entry); err != nil {
			b.Fatalf("Failed to handle entry: %v", err)
		}
	}
}

// BenchmarkMemoryAllocation_Batch measures batch processing allocations
func BenchmarkMemoryAllocation_Batch(b *testing.B) {
	sink := NewBenchmarkSink("batch-bench-sink")
	ctx := context.Background()

	entries := make([]types.LogEntry, 100)
	for i := 0; i < 100; i++ {
		entries[i] = types.LogEntry{
			Timestamp:  time.Now(),
			Message:    fmt.Sprintf("Batch allocation test message %d", i),
			Level:      "info",
			SourceType: "benchmark",
			SourceID:   fmt.Sprintf("batch-%d", i),
			Labels: map[string]string{
				"batch": "true",
			},
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if err := sink.Send(ctx, entries); err != nil {
			b.Fatalf("Failed to send batch: %v", err)
		}
	}
}

// TestMemoryLeak_GoroutineCleanup verifies goroutines are cleaned up
func TestMemoryLeak_GoroutineCleanup(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := types.DispatcherConfig{
		QueueSize:      1000,
		WorkerCount:    4,
		BatchSize:      10,
		BatchTimeout:   "50ms",
		MaxRetries:     1,
		RetryBaseDelay: "10ms",
		RetryMaxDelay:  "100ms",
	}

	// Get baseline goroutine count
	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	initialGoroutines := runtime.NumGoroutine()

	t.Logf("Initial goroutines: %d", initialGoroutines)

	// Create and destroy multiple dispatchers
	for cycle := 0; cycle < 10; cycle++ {
		sink := NewBenchmarkSink(fmt.Sprintf("leak-test-sink-%d", cycle))

		d := dispatcher.NewDispatcher(config, nil, logger, nil)
		d.AddSink(sink)

		ctx := context.Background()
		if err := d.Start(ctx); err != nil {
			t.Fatalf("Failed to start dispatcher: %v", err)
		}

		// Send some logs
		for i := 0; i < 100; i++ {
			entry := types.LogEntry{
				Timestamp:  time.Now(),
				Message:    fmt.Sprintf("Leak test message %d", i),
				Level:      "info",
				SourceType: "leak-test",
				SourceID:   fmt.Sprintf("leak-%d", cycle),
			}
			d.HandleLogEntry(ctx, entry)
		}

		// Stop dispatcher
		if err := d.Stop(); err != nil {
			t.Logf("Stop error: %v", err)
		}

		// Wait for cleanup
		time.Sleep(100 * time.Millisecond)
	}

	// Final GC and check
	runtime.GC()
	time.Sleep(200 * time.Millisecond)

	finalGoroutines := runtime.NumGoroutine()
	t.Logf("Final goroutines: %d", finalGoroutines)

	goroutineLeak := finalGoroutines - initialGoroutines

	// Allow some variance (5 goroutines)
	if goroutineLeak > 5 {
		t.Errorf("GOROUTINE LEAK: %d goroutines leaked after 10 cycles", goroutineLeak)
	} else {
		t.Logf("✓ No significant goroutine leak (diff: %d)", goroutineLeak)
	}
}
