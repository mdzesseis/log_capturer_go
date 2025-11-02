package benchmarks

import (
	"context"
	"fmt"
	"os"
	"runtime/pprof"
	"testing"
	"time"

	"ssw-logs-capture/internal/dispatcher"
	"ssw-logs-capture/pkg/types"

	"github.com/sirupsen/logrus"
)

// TestCPUProfile_Sustained generates CPU profile during sustained load
func TestCPUProfile_Sustained(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping CPU profiling test in short mode")
	}

	// Create CPU profile file
	cpuFile, err := os.Create("/tmp/log-capturer-cpu.prof")
	if err != nil {
		t.Fatalf("Could not create CPU profile: %v", err)
	}
	defer cpuFile.Close()

	// Start CPU profiling
	if err := pprof.StartCPUProfile(cpuFile); err != nil {
		t.Fatalf("Could not start CPU profile: %v", err)
	}
	defer pprof.StopCPUProfile()

	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	config := dispatcher.DispatcherConfig{
		QueueSize:    50000,
		Workers:      4,
		BatchSize:    100,
		BatchTimeout: 100 * time.Millisecond,
		MaxRetries:   3,
		RetryDelay:   100 * time.Millisecond,
	}

	sink := NewBenchmarkSink("cpu-test-sink")

	d := dispatcher.NewDispatcher(config, nil, logger, nil)
	d.AddSink(sink)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := d.Start(ctx); err != nil {
		t.Fatalf("Failed to start dispatcher: %v", err)
	}
	defer d.Stop()

	// Run for 30 seconds at high load
	duration := 30 * time.Second
	logsPerSecond := 10000

	entry := types.LogEntry{
		Timestamp:  time.Now(),
		Message:    "CPU profiling test message with content to simulate real log processing workload",
		Level:      "info",
		SourceType: "cpu-test",
		SourceID:   "cpu-001",
		Labels: map[string]string{
			"test":   "cpu",
			"env":    "benchmark",
			"region": "us-west-2",
		},
	}

	t.Logf("Starting CPU profiling for %v at %d logs/sec...", duration, logsPerSecond)

	startTime := time.Now()
	totalSent := 0

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Send logs for this second
				for i := 0; i < logsPerSecond; i++ {
					d.Handle(ctx, entry.SourceType, entry.SourceID, entry.Message, entry.Labels)
				}
				totalSent += logsPerSecond

				elapsed := time.Since(startTime)
				if elapsed >= duration {
					cancel()
					return
				}

				if int(elapsed.Seconds())%5 == 0 {
					t.Logf("[%ds] Sent %d logs (%.0f logs/sec)",
						int(elapsed.Seconds()),
						totalSent,
						float64(totalSent)/elapsed.Seconds())
				}
			}
		}
	}()

	// Wait for test to complete
	<-ctx.Done()

	// Wait for processing to finish
	time.Sleep(2 * time.Second)

	totalProcessed := sink.sendCount.Load()
	actualDuration := time.Since(startTime)

	t.Logf("\nCPU Profiling Results:")
	t.Logf("  Duration: %v", actualDuration)
	t.Logf("  Logs Sent: %d", totalSent)
	t.Logf("  Logs Processed: %d", totalProcessed)
	t.Logf("  Throughput: %.0f logs/sec", float64(totalProcessed)/actualDuration.Seconds())
	t.Logf("  CPU Profile: /tmp/log-capturer-cpu.prof")
	t.Logf("\nTo analyze CPU profile, run:")
	t.Logf("  go tool pprof /tmp/log-capturer-cpu.prof")
	t.Logf("  (pprof) top10")
	t.Logf("  (pprof) list <function_name>")
}

// BenchmarkCPU_DispatcherHandleLogEntry benchmarks HandleLogEntry CPU usage
func BenchmarkCPU_DispatcherHandleLogEntry(b *testing.B) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := dispatcher.DispatcherConfig{
		QueueSize:    100000,
		Workers:      4,
		BatchSize:    100,
		BatchTimeout: 100 * time.Millisecond,
		MaxRetries:   3,
		RetryDelay:   100 * time.Millisecond,
	}

	sink := NewBenchmarkSink("cpu-bench-sink")

	d := dispatcher.NewDispatcher(config, nil, logger, nil)
	d.AddSink(sink)

	ctx := context.Background()
	if err := d.Start(ctx); err != nil {
		b.Fatalf("Failed to start dispatcher: %v", err)
	}
	defer d.Stop()

	entry := types.LogEntry{
		Timestamp:  time.Now(),
		Message:    "CPU benchmark message with realistic content length for testing",
		Level:      "info",
		SourceType: "benchmark",
		SourceID:   "cpu-bench-001",
		Labels: map[string]string{
			"benchmark": "cpu",
			"component": "dispatcher",
		},
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := d.Handle(ctx, entry.SourceType, entry.SourceID, entry.Message, entry.Labels); err != nil {
			b.Fatalf("Failed to handle entry: %v", err)
		}
	}
}

// BenchmarkCPU_LabelProcessing benchmarks label processing overhead
func BenchmarkCPU_LabelProcessing(b *testing.B) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := dispatcher.DispatcherConfig{
		QueueSize:    10000,
		Workers:      2,
		BatchSize:    50,
		BatchTimeout: 100 * time.Millisecond,
		MaxRetries:   1,
		RetryDelay:   10 * time.Millisecond,
	}

	sink := NewBenchmarkSink("label-bench-sink")

	d := dispatcher.NewDispatcher(config, nil, logger, nil)
	d.AddSink(sink)

	ctx := context.Background()
	if err := d.Start(ctx); err != nil {
		b.Fatalf("Failed to start dispatcher: %v", err)
	}
	defer d.Stop()

	// Entry with many labels (simulate real-world scenario)
	entry := types.LogEntry{
		Timestamp:  time.Now(),
		Message:    "Label processing benchmark",
		Level:      "info",
		SourceType: "benchmark",
		SourceID:   "label-001",
		Labels: map[string]string{
			"app":         "log-capturer",
			"env":         "production",
			"region":      "us-west-2",
			"az":          "us-west-2a",
			"instance":    "i-1234567890abcdef0",
			"version":     "1.0.0",
			"component":   "dispatcher",
			"team":        "platform",
			"cost_center": "engineering",
			"deployment":  "blue-green",
		},
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := d.Handle(ctx, entry.SourceType, entry.SourceID, entry.Message, entry.Labels); err != nil {
			b.Fatalf("Failed to handle entry: %v", err)
		}
	}
}

// BenchmarkCPU_BatchProcessing benchmarks batch processing efficiency
func BenchmarkCPU_BatchProcessing(b *testing.B) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	// Test different batch sizes
	batchSizes := []int{10, 50, 100, 500, 1000}

	for _, batchSize := range batchSizes {
		b.Run(fmt.Sprintf("BatchSize_%d", batchSize), func(b *testing.B) {
			config := dispatcher.DispatcherConfig{
				QueueSize:    100000,
				Workers:      4,
				BatchSize:    batchSize,
				BatchTimeout: 100 * time.Millisecond,
				MaxRetries:   1,
				RetryDelay:   10 * time.Millisecond,
			}

			sink := NewBenchmarkSink(fmt.Sprintf("batch-bench-%d", batchSize))

			d := dispatcher.NewDispatcher(config, nil, logger, nil)
			d.AddSink(sink)

			ctx := context.Background()
			if err := d.Start(ctx); err != nil {
				b.Fatalf("Failed to start dispatcher: %v", err)
			}
			defer d.Stop()

			entry := types.LogEntry{
				Timestamp:  time.Now(),
				Message:    "Batch processing benchmark",
				Level:      "info",
				SourceType: "benchmark",
				SourceID:   fmt.Sprintf("batch-%d", batchSize),
			}

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				if err := d.Handle(ctx, entry.SourceType, entry.SourceID, entry.Message, entry.Labels); err != nil {
					b.Fatalf("Failed to handle entry: %v", err)
				}
			}
		})
	}
}

// BenchmarkCPU_WorkerConcurrency benchmarks different worker counts
func BenchmarkCPU_WorkerConcurrency(b *testing.B) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	// Test different worker counts
	workerCounts := []int{1, 2, 4, 8, 16}

	for _, workerCount := range workerCounts {
		b.Run(fmt.Sprintf("Workers_%d", workerCount), func(b *testing.B) {
			config := dispatcher.DispatcherConfig{
				QueueSize:    100000,
				Workers:      workerCount,
				BatchSize:    100,
				BatchTimeout: 100 * time.Millisecond,
				MaxRetries:   1,
				RetryDelay:   10 * time.Millisecond,
			}

			sink := NewBenchmarkSink(fmt.Sprintf("worker-bench-%d", workerCount))

			d := dispatcher.NewDispatcher(config, nil, logger, nil)
			d.AddSink(sink)

			ctx := context.Background()
			if err := d.Start(ctx); err != nil {
				b.Fatalf("Failed to start dispatcher: %v", err)
			}
			defer d.Stop()

			entry := types.LogEntry{
				Timestamp:  time.Now(),
				Message:    "Worker concurrency benchmark",
				Level:      "info",
				SourceType: "benchmark",
				SourceID:   fmt.Sprintf("worker-%d", workerCount),
			}

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				if err := d.Handle(ctx, entry.SourceType, entry.SourceID, entry.Message, entry.Labels); err != nil {
					b.Fatalf("Failed to handle entry: %v", err)
				}
			}
		})
	}
}
