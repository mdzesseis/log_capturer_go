package benchmarks

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"testing"
	"time"

	"ssw-logs-capture/internal/dispatcher"
	"ssw-logs-capture/pkg/types"

	"github.com/sirupsen/logrus"
)

// LatencyTrackingSink tracks latency for each log entry
type LatencyTrackingSink struct {
	BenchmarkSink
	mu        sync.Mutex
	latencies []time.Duration
}

func NewLatencyTrackingSink(name string) *LatencyTrackingSink {
	return &LatencyTrackingSink{
		BenchmarkSink: BenchmarkSink{name: name},
		latencies:     make([]time.Duration, 0, 10000),
	}
}

func (lts *LatencyTrackingSink) Send(ctx context.Context, entries []types.LogEntry) error {
	receiveTime := time.Now()

	// Track latency for each entry
	lts.mu.Lock()
	for _, entry := range entries {
		latency := receiveTime.Sub(entry.Timestamp)
		lts.latencies = append(lts.latencies, latency)
	}
	lts.mu.Unlock()

	return lts.BenchmarkSink.Send(ctx, entries)
}

func (lts *LatencyTrackingSink) GetLatencyStats() LatencyStats {
	lts.mu.Lock()
	defer lts.mu.Unlock()

	if len(lts.latencies) == 0 {
		return LatencyStats{}
	}

	// Make a copy and sort
	latencies := make([]time.Duration, len(lts.latencies))
	copy(latencies, lts.latencies)
	sort.Slice(latencies, func(i, j int) bool {
		return latencies[i] < latencies[j]
	})

	// Calculate percentiles
	p50 := latencies[len(latencies)*50/100]
	p90 := latencies[len(latencies)*90/100]
	p95 := latencies[len(latencies)*95/100]
	p99 := latencies[len(latencies)*99/100]

	// Calculate average
	var sum time.Duration
	for _, l := range latencies {
		sum += l
	}
	avg := sum / time.Duration(len(latencies))

	// Calculate min and max
	min := latencies[0]
	max := latencies[len(latencies)-1]

	return LatencyStats{
		Count: len(latencies),
		Min:   min,
		Max:   max,
		Avg:   avg,
		P50:   p50,
		P90:   p90,
		P95:   p95,
		P99:   p99,
	}
}

func (lts *LatencyTrackingSink) ResetLatencies() {
	lts.mu.Lock()
	defer lts.mu.Unlock()
	lts.latencies = lts.latencies[:0]
}

type LatencyStats struct {
	Count int
	Min   time.Duration
	Max   time.Duration
	Avg   time.Duration
	P50   time.Duration
	P90   time.Duration
	P95   time.Duration
	P99   time.Duration
}

func (ls LatencyStats) String() string {
	return fmt.Sprintf("Count: %d, Min: %v, Max: %v, Avg: %v, P50: %v, P90: %v, P95: %v, P99: %v",
		ls.Count, ls.Min, ls.Max, ls.Avg, ls.P50, ls.P90, ls.P95, ls.P99)
}

// TestLatency_EndToEnd measures end-to-end latency
func TestLatency_EndToEnd(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	config := types.DispatcherConfig{
		QueueSize:      10000,
		WorkerCount:    4,
		BatchSize:      100,
		BatchTimeout:   "50ms", // Lower timeout for latency test
		MaxRetries:     3,
		RetryBaseDelay: "10ms",
		RetryMaxDelay:  "1s",
	}

	sink := NewLatencyTrackingSink("latency-test-sink")

	d := dispatcher.NewDispatcher(config, nil, logger, nil)
	d.AddSink(sink)

	ctx := context.Background()
	if err := d.Start(ctx); err != nil {
		t.Fatalf("Failed to start dispatcher: %v", err)
	}
	defer d.Stop()

	// Send 10,000 logs
	numLogs := 10000

	t.Logf("Sending %d logs...", numLogs)

	for i := 0; i < numLogs; i++ {
		entry := types.LogEntry{
			Timestamp:  time.Now(), // Timestamp marks when log was created
			Message:    fmt.Sprintf("Latency test message %d", i),
			Level:      "info",
			SourceType: "latency-test",
			SourceID:   "latency-001",
			Labels: map[string]string{
				"test": "latency",
			},
		}

		if err := d.HandleLogEntry(ctx, entry); err != nil {
			t.Errorf("Failed to handle entry: %v", err)
		}
	}

	// Wait for all logs to be processed
	t.Log("Waiting for processing to complete...")
	time.Sleep(2 * time.Second)

	// Get latency statistics
	stats := sink.GetLatencyStats()

	t.Logf("\nEnd-to-End Latency Statistics:")
	t.Logf("  Samples: %d", stats.Count)
	t.Logf("  Min: %v", stats.Min)
	t.Logf("  Max: %v", stats.Max)
	t.Logf("  Avg: %v", stats.Avg)
	t.Logf("  P50: %v", stats.P50)
	t.Logf("  P90: %v", stats.P90)
	t.Logf("  P95: %v", stats.P95)
	t.Logf("  P99: %v", stats.P99)

	// Validate SLO: P99 should be < 500ms
	if stats.P99 > 500*time.Millisecond {
		t.Errorf("P99 latency (%v) exceeds SLO of 500ms", stats.P99)
	} else {
		t.Logf("✓ P99 latency (%v) meets SLO (< 500ms)", stats.P99)
	}

	// Validate P95 should be < 200ms
	if stats.P95 > 200*time.Millisecond {
		t.Logf("WARNING: P95 latency (%v) exceeds target of 200ms", stats.P95)
	} else {
		t.Logf("✓ P95 latency (%v) meets target (< 200ms)", stats.P95)
	}
}

// TestLatency_UnderLoad measures latency under sustained load
func TestLatency_UnderLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping latency under load test in short mode")
	}

	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	config := types.DispatcherConfig{
		QueueSize:      50000,
		WorkerCount:    8,
		BatchSize:      100,
		BatchTimeout:   "50ms",
		MaxRetries:     3,
		RetryBaseDelay: "10ms",
		RetryMaxDelay:  "1s",
	}

	sink := NewLatencyTrackingSink("load-latency-sink")

	d := dispatcher.NewDispatcher(config, nil, logger, nil)
	d.AddSink(sink)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := d.Start(ctx); err != nil {
		t.Fatalf("Failed to start dispatcher: %v", err)
	}
	defer d.Stop()

	// Run for 60 seconds at 5k logs/sec
	duration := 60 * time.Second
	logsPerSecond := 5000

	t.Logf("Starting latency test under load for %v at %d logs/sec...", duration, logsPerSecond)

	startTime := time.Now()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				for i := 0; i < logsPerSecond; i++ {
					entry := types.LogEntry{
						Timestamp:  time.Now(),
						Message:    "Latency under load test message",
						Level:      "info",
						SourceType: "load-test",
						SourceID:   "load-001",
					}
					d.HandleLogEntry(ctx, entry)
				}

				if time.Since(startTime) >= duration {
					cancel()
					return
				}

				// Report latency every 10 seconds
				if int(time.Since(startTime).Seconds())%10 == 0 {
					stats := sink.GetLatencyStats()
					t.Logf("[%ds] P50: %v, P95: %v, P99: %v",
						int(time.Since(startTime).Seconds()),
						stats.P50, stats.P95, stats.P99)
				}
			}
		}
	}()

	<-ctx.Done()

	// Wait for processing
	time.Sleep(2 * time.Second)

	// Final statistics
	stats := sink.GetLatencyStats()

	t.Logf("\nLatency Under Load Results:")
	t.Logf("  Duration: %v", duration)
	t.Logf("  Target Rate: %d logs/sec", logsPerSecond)
	t.Logf("  Samples: %d", stats.Count)
	t.Logf("  Min: %v", stats.Min)
	t.Logf("  Max: %v", stats.Max)
	t.Logf("  Avg: %v", stats.Avg)
	t.Logf("  P50: %v", stats.P50)
	t.Logf("  P90: %v", stats.P90)
	t.Logf("  P95: %v", stats.P95)
	t.Logf("  P99: %v", stats.P99)

	// Validate latency stays reasonable under load
	if stats.P99 > 1*time.Second {
		t.Errorf("P99 latency (%v) too high under load", stats.P99)
	} else {
		t.Logf("✓ P99 latency (%v) acceptable under load", stats.P99)
	}
}

// BenchmarkLatency_SingleEntry measures latency of single entry
func BenchmarkLatency_SingleEntry(b *testing.B) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := types.DispatcherConfig{
		QueueSize:      1000,
		WorkerCount:    2,
		BatchSize:      10,
		BatchTimeout:   "10ms",
		MaxRetries:     1,
		RetryBaseDelay: "1ms",
		RetryMaxDelay:  "10ms",
	}

	sink := NewLatencyTrackingSink("single-entry-bench")

	d := dispatcher.NewDispatcher(config, nil, logger, nil)
	d.AddSink(sink)

	ctx := context.Background()
	if err := d.Start(ctx); err != nil {
		b.Fatalf("Failed to start dispatcher: %v", err)
	}
	defer d.Stop()

	entry := types.LogEntry{
		Timestamp:  time.Now(),
		Message:    "Single entry latency benchmark",
		Level:      "info",
		SourceType: "benchmark",
		SourceID:   "single-001",
	}

	b.ResetTimer()

	latencies := make([]time.Duration, b.N)

	for i := 0; i < b.N; i++ {
		start := time.Now()
		if err := d.HandleLogEntry(ctx, entry); err != nil {
			b.Fatalf("Failed to handle entry: %v", err)
		}
		latencies[i] = time.Since(start)
	}

	b.StopTimer()

	// Calculate latency statistics
	sort.Slice(latencies, func(i, j int) bool {
		return latencies[i] < latencies[j]
	})

	p50 := latencies[len(latencies)*50/100]
	p99 := latencies[len(latencies)*99/100]

	b.ReportMetric(float64(p50.Nanoseconds())/1000000, "p50_ms")
	b.ReportMetric(float64(p99.Nanoseconds())/1000000, "p99_ms")
}

// BenchmarkLatency_Batch measures batch latency
func BenchmarkLatency_Batch(b *testing.B) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	batchSizes := []int{10, 100, 1000}

	for _, size := range batchSizes {
		b.Run(fmt.Sprintf("Size_%d", size), func(b *testing.B) {
			config := types.DispatcherConfig{
				QueueSize:      100000,
				WorkerCount:    4,
				BatchSize:      size,
				BatchTimeout:   "100ms",
				MaxRetries:     1,
				RetryBaseDelay: "1ms",
				RetryMaxDelay:  "10ms",
			}

			sink := NewLatencyTrackingSink(fmt.Sprintf("batch-bench-%d", size))

			d := dispatcher.NewDispatcher(config, nil, logger, nil)
			d.AddSink(sink)

			ctx := context.Background()
			if err := d.Start(ctx); err != nil {
				b.Fatalf("Failed to start dispatcher: %v", err)
			}
			defer d.Stop()

			entry := types.LogEntry{
				Timestamp:  time.Now(),
				Message:    "Batch latency benchmark",
				Level:      "info",
				SourceType: "benchmark",
				SourceID:   fmt.Sprintf("batch-%d", size),
			}

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				for j := 0; j < size; j++ {
					if err := d.HandleLogEntry(ctx, entry); err != nil {
						b.Fatalf("Failed to handle entry: %v", err)
					}
				}
			}
		})
	}
}

// TestLatency_QueueSaturation measures latency when queue is nearly full
func TestLatency_QueueSaturation(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	config := types.DispatcherConfig{
		QueueSize:      1000, // Small queue to test saturation
		WorkerCount:    2,
		BatchSize:      50,
		BatchTimeout:   "100ms",
		MaxRetries:     1,
		RetryBaseDelay: "10ms",
		RetryMaxDelay:  "100ms",
	}

	sink := NewLatencyTrackingSink("saturation-test-sink")

	d := dispatcher.NewDispatcher(config, nil, logger, nil)
	d.AddSink(sink)

	ctx := context.Background()
	if err := d.Start(ctx); err != nil {
		t.Fatalf("Failed to start dispatcher: %v", err)
	}
	defer d.Stop()

	// Send logs to fill queue
	numLogs := 5000 // 5x queue size

	t.Logf("Sending %d logs to saturate queue (size: %d)...", numLogs, config.QueueSize)

	latencies := make([]time.Duration, numLogs)

	for i := 0; i < numLogs; i++ {
		entry := types.LogEntry{
			Timestamp:  time.Now(),
			Message:    fmt.Sprintf("Saturation test message %d", i),
			Level:      "info",
			SourceType: "saturation-test",
			SourceID:   "sat-001",
		}

		start := time.Now()
		if err := d.HandleLogEntry(ctx, entry); err != nil {
			t.Errorf("Failed to handle entry %d: %v", i, err)
		}
		latencies[i] = time.Since(start)
	}

	// Wait for processing
	time.Sleep(2 * time.Second)

	// Analyze latencies
	sort.Slice(latencies, func(i, j int) bool {
		return latencies[i] < latencies[j]
	})

	var sum time.Duration
	for _, l := range latencies {
		sum += l
	}
	avg := sum / time.Duration(len(latencies))

	p50 := latencies[len(latencies)*50/100]
	p95 := latencies[len(latencies)*95/100]
	p99 := latencies[len(latencies)*99/100]
	max := latencies[len(latencies)-1]

	t.Logf("\nQueue Saturation Latency:")
	t.Logf("  Avg: %v", avg)
	t.Logf("  P50: %v", p50)
	t.Logf("  P95: %v", p95)
	t.Logf("  P99: %v", p99)
	t.Logf("  Max: %v", max)

	// Under saturation, latency should degrade gracefully (not timeout)
	if max > 10*time.Second {
		t.Errorf("Max latency (%v) too high under saturation", max)
	} else {
		t.Logf("✓ System handles saturation gracefully (max: %v)", max)
	}
}
