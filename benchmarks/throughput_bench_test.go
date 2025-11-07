// Package benchmarks provides performance benchmarks for critical system components
package benchmarks

import (
	"context"
	"fmt"
	"testing"
	"time"

	"ssw-logs-capture/internal/dispatcher"
	"ssw-logs-capture/pkg/types"

	"github.com/sirupsen/logrus"
)

// MockNullSink is a sink that discards all logs (for measuring dispatcher throughput only)
type MockNullSink struct {
	name string
}

func (m *MockNullSink) Send(ctx context.Context, entries []types.LogEntry) error {
	// Discard logs immediately (no processing)
	return nil
}

func (m *MockNullSink) IsHealthy() bool {
	return true
}

func (m *MockNullSink) Start(ctx context.Context) error {
	return nil
}

func (m *MockNullSink) Stop() error {
	return nil
}

func (m *MockNullSink) GetQueueUtilization() float64 {
	return 0.0
}

func (m *MockNullSink) GetStats() interface{} {
	return nil
}

// BenchmarkDispatcherThroughput measures end-to-end dispatcher throughput
//
// This benchmark measures the throughput of the entire dispatcher pipeline:
//   - Entry creation and validation
//   - Queue management
//   - Worker distribution
//   - Batch processing
//   - Sink delivery
//
// Metrics measured:
//   - ops/sec: Number of log entries processed per second
//   - ns/op: Nanoseconds per operation
//   - B/op: Bytes allocated per operation
//   - allocs/op: Number of allocations per operation
//
// Usage:
//   go test -bench=BenchmarkDispatcherThroughput -benchmem ./benchmarks/
func BenchmarkDispatcherThroughput(b *testing.B) {
	config := dispatcher.DispatcherConfig{
		QueueSize:    10000,
		Workers:      4,
		BatchSize:    100,
		BatchTimeout: 100 * time.Millisecond,
	}

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel) // Minimize logging overhead

	d := dispatcher.NewDispatcher(config, nil, logger, nil, nil)

	// Add null sink to discard logs (measure dispatcher only)
	mockSink := &MockNullSink{name: "null"}
	d.AddSink(mockSink)

	ctx := context.Background()
	if err := d.Start(ctx); err != nil {
		b.Fatalf("Failed to start dispatcher: %v", err)
	}
	defer d.Stop()

	// Pre-allocate test data to avoid allocation overhead in benchmark
	labels := map[string]string{
		"environment": "benchmark",
		"service":     "test",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		err := d.Handle(ctx, "benchmark", "source-1", fmt.Sprintf("benchmark message %d", i), labels)
		if err != nil {
			b.Errorf("Handle failed: %v", err)
		}
	}

	b.StopTimer()

	// Wait for queue to drain
	time.Sleep(500 * time.Millisecond)
}

// BenchmarkDispatcherThroughputParallel measures concurrent dispatcher throughput
//
// This benchmark measures dispatcher performance under concurrent load from
// multiple goroutines, simulating production scenarios with multiple log sources.
//
// Usage:
//   go test -bench=BenchmarkDispatcherThroughputParallel -benchmem ./benchmarks/
func BenchmarkDispatcherThroughputParallel(b *testing.B) {
	config := dispatcher.DispatcherConfig{
		QueueSize:    10000,
		Workers:      4,
		BatchSize:    100,
		BatchTimeout: 100 * time.Millisecond,
	}

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	d := dispatcher.NewDispatcher(config, nil, logger, nil, nil)
	mockSink := &MockNullSink{name: "null"}
	d.AddSink(mockSink)

	ctx := context.Background()
	if err := d.Start(ctx); err != nil {
		b.Fatalf("Failed to start dispatcher: %v", err)
	}
	defer d.Stop()

	labels := map[string]string{
		"environment": "benchmark",
		"service":     "test",
	}

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			err := d.Handle(ctx, "benchmark", fmt.Sprintf("source-%d", i%10), fmt.Sprintf("message %d", i), labels)
			if err != nil {
				b.Errorf("Handle failed: %v", err)
			}
			i++
		}
	})

	b.StopTimer()
	time.Sleep(500 * time.Millisecond)
}

// BenchmarkLogEntryPool measures sync.Pool performance for LogEntry
//
// This benchmark compares allocation performance with and without sync.Pool,
// demonstrating the memory optimization benefits.
//
// Metrics:
//   - WithPool: Using types.AcquireLogEntry() and Release()
//   - WithoutPool: Direct allocation with make()
//
// Expected results:
//   - WithPool: ~60-80% fewer allocations
//   - WithPool: ~50-70% less memory per operation
//
// Usage:
//   go test -bench=BenchmarkLogEntryPool -benchmem ./benchmarks/
func BenchmarkLogEntryPool(b *testing.B) {
	b.Run("WithPool", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			entry := types.AcquireLogEntry()
			entry.Message = "test message"
			entry.SourceType = "file"
			entry.SourceID = "test-source"
			entry.SetLabel("key1", "value1")
			entry.SetLabel("key2", "value2")
			entry.SetField("field1", 123)
			entry.Release()
		}
	})

	b.Run("WithoutPool", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			entry := &types.LogEntry{
				Labels: make(map[string]string),
				Fields: make(map[string]interface{}),
			}
			entry.Message = "test message"
			entry.SourceType = "file"
			entry.SourceID = "test-source"
			entry.SetLabel("key1", "value1")
			entry.SetLabel("key2", "value2")
			entry.SetField("field1", 123)
			_ = entry
		}
	})
}

// BenchmarkDeepCopy measures DeepCopy performance
//
// This benchmark measures the cost of DeepCopy operations, which are
// critical in batch processing and multi-sink scenarios.
//
// Usage:
//   go test -bench=BenchmarkDeepCopy -benchmem ./benchmarks/
func BenchmarkDeepCopy(b *testing.B) {
	// Create a typical log entry
	entry := &types.LogEntry{
		Message:    "test message with some content",
		SourceType: "file",
		SourceID:   "/var/log/app.log",
		Level:      "info",
		Timestamp:  time.Now(),
		Labels: map[string]string{
			"env":     "production",
			"service": "api",
			"region":  "us-east-1",
		},
		Fields: map[string]interface{}{
			"user_id":    12345,
			"request_id": "req-abc123",
			"duration":   123.45,
		},
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		copied := entry.DeepCopy()
		_ = copied
	}
}

// BenchmarkBatchProcessing measures batch processing throughput
//
// This benchmark simulates the batch processor's core loop:
//   1. Collect batch
//   2. Deep copy entries
//   3. Send to sink
//
// Usage:
//   go test -bench=BenchmarkBatchProcessing -benchmem ./benchmarks/
func BenchmarkBatchProcessing(b *testing.B) {
	batchSizes := []int{10, 50, 100, 500, 1000}

	for _, size := range batchSizes {
		b.Run(fmt.Sprintf("BatchSize_%d", size), func(b *testing.B) {
			// Create sample batch
			batch := make([]types.LogEntry, size)
			for i := 0; i < size; i++ {
				batch[i] = types.LogEntry{
					Message:    fmt.Sprintf("log message %d", i),
					SourceType: "file",
					SourceID:   "test-source",
					Timestamp:  time.Now(),
					Labels: map[string]string{
						"index": fmt.Sprintf("%d", i),
					},
					Fields: make(map[string]interface{}),
				}
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				// Simulate batch processing: deep copy
				copied := make([]types.LogEntry, len(batch))
				for j, entry := range batch {
					copied[j] = *entry.DeepCopy()
				}
				_ = copied
			}
		})
	}
}

// BenchmarkMapOperations measures map access performance
//
// LogEntry uses maps extensively for Labels and Fields. This benchmark
// measures the overhead of thread-safe map operations.
//
// Usage:
//   go test -bench=BenchmarkMapOperations -benchmem ./benchmarks/
func BenchmarkMapOperations(b *testing.B) {
	entry := &types.LogEntry{
		Labels: make(map[string]string),
		Fields: make(map[string]interface{}),
	}

	b.Run("SetLabel", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			entry.SetLabel("key", "value")
		}
	})

	b.Run("GetLabel", func(b *testing.B) {
		entry.SetLabel("key", "value")
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _ = entry.GetLabel("key")
		}
	})

	b.Run("SetField", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			entry.SetField("key", 123)
		}
	})

	b.Run("GetField", func(b *testing.B) {
		entry.SetField("key", 123)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _ = entry.GetField("key")
		}
	})
}
