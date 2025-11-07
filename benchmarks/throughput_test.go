package benchmarks

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ssw-logs-capture/internal/dispatcher"
	"ssw-logs-capture/pkg/types"

	"github.com/sirupsen/logrus"
)

// MockSink for benchmarking - just counts messages
type BenchmarkSink struct {
	mu         sync.Mutex
	name       string
	sendCount  atomic.Int64
	totalBytes atomic.Int64
}

func NewBenchmarkSink(name string) *BenchmarkSink {
	return &BenchmarkSink{
		name: name,
	}
}

func (bs *BenchmarkSink) Send(ctx context.Context, entries []types.LogEntry) error {
	bs.sendCount.Add(int64(len(entries)))

	// Simulate some work (count bytes)
	var bytes int64
	for _, entry := range entries {
		bytes += int64(len(entry.Message))
	}
	bs.totalBytes.Add(bytes)

	return nil
}

func (bs *BenchmarkSink) Start(ctx context.Context) error {
	return nil
}

func (bs *BenchmarkSink) Stop() error {
	return nil
}

func (bs *BenchmarkSink) Name() string {
	return bs.name
}

func (bs *BenchmarkSink) IsHealthy() bool {
	return true
}

func (bs *BenchmarkSink) GetStats() interface{} {
	return map[string]interface{}{
		"send_count":  bs.sendCount.Load(),
		"total_bytes": bs.totalBytes.Load(),
	}
}

// BenchmarkDispatcherThroughput_1K measures throughput with 1,000 logs
func BenchmarkDispatcherThroughput_1K(b *testing.B) {
	benchmarkDispatcherThroughput(b, 1000)
}

// BenchmarkDispatcherThroughput_10K measures throughput with 10,000 logs
func BenchmarkDispatcherThroughput_10K(b *testing.B) {
	benchmarkDispatcherThroughput(b, 10000)
}

// BenchmarkDispatcherThroughput_100K measures throughput with 100,000 logs
func BenchmarkDispatcherThroughput_100K(b *testing.B) {
	benchmarkDispatcherThroughput(b, 100000)
}

func benchmarkDispatcherThroughput(b *testing.B, numLogs int) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel) // Reduce noise in benchmarks

	config := dispatcher.DispatcherConfig{
		QueueSize:      100000,
		Workers:    4,
		BatchSize:      100,
		BatchTimeout: 100 * time.Millisecond,
		MaxRetries:     3,
		RetryDelay:   100 * time.Millisecond,
	}

	sink := NewBenchmarkSink("benchmark-sink")

	d := dispatcher.NewDispatcher(config, nil, logger, nil, nil)
	d.AddSink(sink)

	ctx := context.Background()
	if err := d.Start(ctx); err != nil {
		b.Fatalf("Failed to start dispatcher: %v", err)
	}
	defer d.Stop()

	// Pre-generate log entries to avoid allocation overhead in benchmark
	entries := make([]types.LogEntry, numLogs)
	for i := 0; i < numLogs; i++ {
		entries[i] = types.LogEntry{
			Timestamp:  time.Now(),
			Message:    fmt.Sprintf("Benchmark log message %d with some content to simulate real logs", i),
			Level:      "info",
			SourceType: "benchmark",
			SourceID:   "bench-001",
			Labels: map[string]string{
				"benchmark": "throughput",
				"iteration": fmt.Sprintf("%d", i),
			},
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	var totalProcessed atomic.Int64

	for i := 0; i < b.N; i++ {
		for j := 0; j < numLogs; j++ {
			if err := d.Handle(ctx, entries[j].SourceType, entries[j].SourceID, entries[j].Message, entries[j].Labels); err != nil {
				b.Errorf("Failed to handle log entry: %v", err)
			}
			totalProcessed.Add(1)
		}
	}

	b.StopTimer()

	// Wait for processing to complete
	time.Sleep(500 * time.Millisecond)

	// Report throughput
	duration := b.Elapsed()
	logsProcessed := totalProcessed.Load()
	throughput := float64(logsProcessed) / duration.Seconds()

	b.ReportMetric(throughput, "logs/sec")
	b.ReportMetric(float64(sink.totalBytes.Load())/float64(1024*1024), "MB_processed")
}

// BenchmarkDispatcherThroughput_Concurrent measures concurrent throughput
func BenchmarkDispatcherThroughput_Concurrent(b *testing.B) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := dispatcher.DispatcherConfig{
		QueueSize:      100000,
		Workers:    8, // More workers for concurrency
		BatchSize:      100,
		BatchTimeout: 50 * time.Millisecond,
		MaxRetries:     3,
		RetryDelay:   100 * time.Millisecond,
	}

	sink := NewBenchmarkSink("benchmark-sink")

	d := dispatcher.NewDispatcher(config, nil, logger, nil, nil)
	d.AddSink(sink)

	ctx := context.Background()
	if err := d.Start(ctx); err != nil {
		b.Fatalf("Failed to start dispatcher: %v", err)
	}
	defer d.Stop()

	entry := types.LogEntry{
		Timestamp:  time.Now(),
		Message:    "Concurrent benchmark log message with some realistic content for testing",
		Level:      "info",
		SourceType: "benchmark",
		SourceID:   "bench-concurrent",
		Labels: map[string]string{
			"benchmark": "concurrent",
		},
	}

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if err := d.Handle(ctx, entry.SourceType, entry.SourceID, entry.Message, entry.Labels); err != nil {
				b.Errorf("Failed to handle log entry: %v", err)
			}
		}
	})

	b.StopTimer()

	// Wait for processing
	time.Sleep(500 * time.Millisecond)

	// Report throughput
	duration := b.Elapsed()
	logsProcessed := sink.sendCount.Load()
	throughput := float64(logsProcessed) / duration.Seconds()

	b.ReportMetric(throughput, "logs/sec")
}

// BenchmarkDispatcherThroughput_WithDedup measures throughput with deduplication enabled
func BenchmarkDispatcherThroughput_WithDedup(b *testing.B) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := dispatcher.DispatcherConfig{
		QueueSize:      100000,
		Workers:    4,
		BatchSize:      100,
		BatchTimeout: 100 * time.Millisecond,
		MaxRetries:     3,
		RetryDelay:   100 * time.Millisecond,
	}

	sink := NewBenchmarkSink("benchmark-sink")

	d := dispatcher.NewDispatcher(config, nil, logger, nil, nil)
	d.AddSink(sink)

	ctx := context.Background()
	if err := d.Start(ctx); err != nil {
		b.Fatalf("Failed to start dispatcher: %v", err)
	}
	defer d.Stop()

	const numLogs = 10000
	entries := make([]types.LogEntry, numLogs)

	// Generate entries with 50% duplicates
	for i := 0; i < numLogs; i++ {
		messageID := i / 2 // Every 2 entries share the same message
		entries[i] = types.LogEntry{
			Timestamp:  time.Now(),
			Message:    fmt.Sprintf("Dedup benchmark message %d", messageID),
			Level:      "info",
			SourceType: "benchmark",
			SourceID:   "bench-dedup",
			Labels: map[string]string{
				"benchmark": "dedup",
			},
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		for j := 0; j < numLogs; j++ {
			if err := d.Handle(ctx, entries[j].SourceType, entries[j].SourceID, entries[j].Message, entries[j].Labels); err != nil {
				b.Errorf("Failed to handle log entry: %v", err)
			}
		}
	}

	b.StopTimer()

	time.Sleep(500 * time.Millisecond)

	// Report metrics
	duration := b.Elapsed()
	logsProcessed := sink.sendCount.Load()
	throughput := float64(numLogs*b.N) / duration.Seconds()
	dedupRate := (1.0 - float64(logsProcessed)/float64(numLogs*b.N)) * 100

	b.ReportMetric(throughput, "logs/sec")
	b.ReportMetric(dedupRate, "dedup_%")
}

// BenchmarkSinkWrite measures just sink write performance
func BenchmarkSinkWrite(b *testing.B) {
	sink := NewBenchmarkSink("benchmark-sink")
	ctx := context.Background()

	entries := make([]types.LogEntry, 100)
	for i := 0; i < 100; i++ {
		entries[i] = types.LogEntry{
			Timestamp:  time.Now(),
			Message:    fmt.Sprintf("Sink benchmark message %d", i),
			Level:      "info",
			SourceType: "benchmark",
			SourceID:   "bench-sink",
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if err := sink.Send(ctx, entries); err != nil {
			b.Errorf("Failed to send: %v", err)
		}
	}

	throughput := float64(100*b.N) / b.Elapsed().Seconds()
	b.ReportMetric(throughput, "logs/sec")
}
