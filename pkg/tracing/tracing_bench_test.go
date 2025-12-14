package tracing

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"ssw-logs-capture/pkg/types"

	"github.com/sirupsen/logrus"
)

// Helper function for benchmarks
func newBenchLogger() *logrus.Logger {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	return logger
}

// BenchmarkTracingOff benchmarks tracing with OFF mode
func BenchmarkTracingOff(b *testing.B) {
	logger := newBenchLogger()
	config := EnhancedTracingConfig{
		Enabled: false,
		Mode:    ModeOff,
	}

	tm, err := NewEnhancedTracingManager(config, logger)
	if err != nil {
		b.Fatal(err)
	}

	entry := &types.LogEntry{
		Message:  "benchmark message",
		SourceID: "test-source",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		tm.ShouldTraceLog(entry)
	}
}

// BenchmarkTracingSystemOnly benchmarks SYSTEM-ONLY mode
func BenchmarkTracingSystemOnly(b *testing.B) {
	logger := newBenchLogger()
	config := EnhancedTracingConfig{
		Enabled: true,
		Mode:    ModeSystemOnly,
	}

	tm, err := NewEnhancedTracingManager(config, logger)
	if err != nil {
		b.Fatal(err)
	}

	entry := &types.LogEntry{
		Message:  "benchmark message",
		SourceID: "test-source",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		tm.ShouldTraceLog(entry)
	}
}

// BenchmarkTracingHybrid_0Percent benchmarks HYBRID mode with 0% sampling
func BenchmarkTracingHybrid_0Percent(b *testing.B) {
	logger := newBenchLogger()
	config := EnhancedTracingConfig{
		Enabled:        true,
		Mode:           ModeHybrid,
		LogTracingRate: 0.0,
	}

	tm, err := NewEnhancedTracingManager(config, logger)
	if err != nil {
		b.Fatal(err)
	}

	entry := &types.LogEntry{
		Message:  "benchmark message",
		SourceID: "test-source",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		tm.ShouldTraceLog(entry)
	}
}

// BenchmarkTracingHybrid_1Percent benchmarks HYBRID mode with 1% sampling
func BenchmarkTracingHybrid_1Percent(b *testing.B) {
	logger := newBenchLogger()
	config := EnhancedTracingConfig{
		Enabled:        true,
		Mode:           ModeHybrid,
		LogTracingRate: 0.01,
	}

	tm, err := NewEnhancedTracingManager(config, logger)
	if err != nil {
		b.Fatal(err)
	}

	entry := &types.LogEntry{
		Message:  "benchmark message",
		SourceID: "test-source",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		tm.ShouldTraceLog(entry)
	}
}

// BenchmarkTracingHybrid_10Percent benchmarks HYBRID mode with 10% sampling
func BenchmarkTracingHybrid_10Percent(b *testing.B) {
	logger := newBenchLogger()
	config := EnhancedTracingConfig{
		Enabled:        true,
		Mode:           ModeHybrid,
		LogTracingRate: 0.10,
	}

	tm, err := NewEnhancedTracingManager(config, logger)
	if err != nil {
		b.Fatal(err)
	}

	entry := &types.LogEntry{
		Message:  "benchmark message",
		SourceID: "test-source",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		tm.ShouldTraceLog(entry)
	}
}

// BenchmarkTracingHybrid_50Percent benchmarks HYBRID mode with 50% sampling
func BenchmarkTracingHybrid_50Percent(b *testing.B) {
	logger := newBenchLogger()
	config := EnhancedTracingConfig{
		Enabled:        true,
		Mode:           ModeHybrid,
		LogTracingRate: 0.50,
	}

	tm, err := NewEnhancedTracingManager(config, logger)
	if err != nil {
		b.Fatal(err)
	}

	entry := &types.LogEntry{
		Message:  "benchmark message",
		SourceID: "test-source",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		tm.ShouldTraceLog(entry)
	}
}

// BenchmarkTracingHybrid_100Percent benchmarks HYBRID mode with 100% sampling
func BenchmarkTracingHybrid_100Percent(b *testing.B) {
	logger := newBenchLogger()
	config := EnhancedTracingConfig{
		Enabled:        true,
		Mode:           ModeHybrid,
		LogTracingRate: 1.0,
	}

	tm, err := NewEnhancedTracingManager(config, logger)
	if err != nil {
		b.Fatal(err)
	}

	entry := &types.LogEntry{
		Message:  "benchmark message",
		SourceID: "test-source",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		tm.ShouldTraceLog(entry)
	}
}

// BenchmarkTracingFullE2E benchmarks FULL-E2E mode
func BenchmarkTracingFullE2E(b *testing.B) {
	logger := newBenchLogger()
	config := EnhancedTracingConfig{
		Enabled: true,
		Mode:    ModeFullE2E,
	}

	tm, err := NewEnhancedTracingManager(config, logger)
	if err != nil {
		b.Fatal(err)
	}

	entry := &types.LogEntry{
		Message:  "benchmark message",
		SourceID: "test-source",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		tm.ShouldTraceLog(entry)
	}
}

// BenchmarkSpanCreation benchmarks span creation and ending
func BenchmarkSpanCreation(b *testing.B) {
	logger := newBenchLogger()
	config := EnhancedTracingConfig{
		Enabled: true,
		Mode:    ModeFullE2E,
	}

	tm, err := NewEnhancedTracingManager(config, logger)
	if err != nil {
		b.Fatal(err)
	}

	ctx := context.Background()
	entry := &types.LogEntry{
		Message:  "benchmark message",
		SourceID: "test-source",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, span := tm.CreateLogSpan(ctx, entry)
		if span != nil {
			span.End()
		}
	}
}

// BenchmarkSpanCreation_WithAttributes benchmarks span creation with attributes
func BenchmarkSpanCreation_WithAttributes(b *testing.B) {
	logger := newBenchLogger()
	config := EnhancedTracingConfig{
		Enabled: true,
		Mode:    ModeFullE2E,
	}

	tm, err := NewEnhancedTracingManager(config, logger)
	if err != nil {
		b.Fatal(err)
	}

	ctx := context.Background()
	entry := &types.LogEntry{
		Message:  "benchmark message with lots of attributes",
		SourceID: "test-source-12345",
		Labels: types.NewLabelsCOWFromMap(map[string]string{
			"level":       "info",
			"service":     "test-service",
			"environment": "production",
			"version":     "v1.2.3",
			"host":        "host-123",
		}),
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, span := tm.CreateLogSpan(ctx, entry)
		if span != nil {
			span.End()
		}
	}
}

// BenchmarkOnDemandCheck benchmarks on-demand tracing check
func BenchmarkOnDemandCheck(b *testing.B) {
	logger := newBenchLogger()
	config := EnhancedTracingConfig{
		Enabled:        true,
		Mode:           ModeHybrid,
		LogTracingRate: 0.0,
		OnDemand: OnDemandConfig{
			Enabled: true,
		},
	}

	tm, err := NewEnhancedTracingManager(config, logger)
	if err != nil {
		b.Fatal(err)
	}

	// Enable on-demand for one source
	tm.EnableOnDemandTracing("hot-source", 1.0, 1*time.Hour)

	entry := &types.LogEntry{
		Message:  "benchmark message",
		SourceID: "hot-source",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		tm.ShouldTraceLog(entry)
	}
}

// BenchmarkOnDemandCheck_MultipleRules benchmarks with multiple on-demand rules
func BenchmarkOnDemandCheck_MultipleRules(b *testing.B) {
	logger := newBenchLogger()
	config := EnhancedTracingConfig{
		Enabled:        true,
		Mode:           ModeHybrid,
		LogTracingRate: 0.0,
		OnDemand: OnDemandConfig{
			Enabled: true,
		},
	}

	tm, err := NewEnhancedTracingManager(config, logger)
	if err != nil {
		b.Fatal(err)
	}

	// Enable on-demand for 10 sources
	for i := 0; i < 10; i++ {
		sourceID := fmt.Sprintf("source-%d", i)
		tm.EnableOnDemandTracing(sourceID, 1.0, 1*time.Hour)
	}

	entry := &types.LogEntry{
		Message:  "benchmark message",
		SourceID: "source-5",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		tm.ShouldTraceLog(entry)
	}
}

// BenchmarkAdaptiveSamplingCheck benchmarks adaptive sampling check
func BenchmarkAdaptiveSamplingCheck(b *testing.B) {
	logger := newBenchLogger()
	config := EnhancedTracingConfig{
		Enabled:        true,
		Mode:           ModeHybrid,
		LogTracingRate: 0.0,
		AdaptiveSampling: AdaptiveSamplingConfig{
			Enabled:          true,
			LatencyThreshold: 100 * time.Millisecond,
			SampleRate:       0.1,
		},
	}

	tm, err := NewEnhancedTracingManager(config, logger)
	if err != nil {
		b.Fatal(err)
	}

	// Trigger adaptive sampling
	tm.adaptiveSampler.RecordLatency(200 * time.Millisecond)

	entry := &types.LogEntry{
		Message:  "benchmark message",
		SourceID: "test-source",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		tm.ShouldTraceLog(entry)
	}
}

// BenchmarkConcurrentTracing benchmarks concurrent tracing decisions
func BenchmarkConcurrentTracing(b *testing.B) {
	logger := newBenchLogger()
	config := EnhancedTracingConfig{
		Enabled:        true,
		Mode:           ModeHybrid,
		LogTracingRate: 0.1,
	}

	tm, err := NewEnhancedTracingManager(config, logger)
	if err != nil {
		b.Fatal(err)
	}

	entry := &types.LogEntry{
		Message:  "benchmark message",
		SourceID: "test-source",
	}

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			tm.ShouldTraceLog(entry)
		}
	})
}

// BenchmarkConcurrentSpanCreation benchmarks concurrent span creation
func BenchmarkConcurrentSpanCreation(b *testing.B) {
	logger := newBenchLogger()
	config := EnhancedTracingConfig{
		Enabled: true,
		Mode:    ModeFullE2E,
	}

	tm, err := NewEnhancedTracingManager(config, logger)
	if err != nil {
		b.Fatal(err)
	}

	ctx := context.Background()
	entry := &types.LogEntry{
		Message:  "benchmark message",
		SourceID: "test-source",
	}

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, span := tm.CreateLogSpan(ctx, entry)
			if span != nil {
				span.End()
			}
		}
	})
}

// BenchmarkModeSwitch benchmarks the overhead of checking different modes
func BenchmarkModeSwitch(b *testing.B) {
	logger := newBenchLogger()

	modes := []struct {
		name string
		mode TracingMode
	}{
		{"off", ModeOff},
		{"system_only", ModeSystemOnly},
		{"hybrid", ModeHybrid},
		{"full_e2e", ModeFullE2E},
	}

	for _, m := range modes {
		b.Run(m.name, func(b *testing.B) {
			config := EnhancedTracingConfig{
				Enabled:        true,
				Mode:           m.mode,
				LogTracingRate: 0.1,
			}

			tm, err := NewEnhancedTracingManager(config, logger)
			if err != nil {
				b.Fatal(err)
			}

			entry := &types.LogEntry{
				Message:  "benchmark message",
				SourceID: "test-source",
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				tm.ShouldTraceLog(entry)
			}
		})
	}
}

// BenchmarkMemoryAllocation benchmarks memory allocations across modes
func BenchmarkMemoryAllocation(b *testing.B) {
	logger := newBenchLogger()

	testCases := []struct {
		name string
		mode TracingMode
		rate float64
	}{
		{"off", ModeOff, 0.0},
		{"hybrid_0pct", ModeHybrid, 0.0},
		{"hybrid_1pct", ModeHybrid, 0.01},
		{"hybrid_10pct", ModeHybrid, 0.10},
		{"full_e2e", ModeFullE2E, 1.0},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			config := EnhancedTracingConfig{
				Enabled:        true,
				Mode:           tc.mode,
				LogTracingRate: tc.rate,
			}

			tm, err := NewEnhancedTracingManager(config, logger)
			if err != nil {
				b.Fatal(err)
			}

			ctx := context.Background()
			entry := &types.LogEntry{
				Message:  "benchmark message with some length to simulate real logs",
				SourceID: "test-source-id-12345",
				Labels: types.NewLabelsCOWFromMap(map[string]string{
					"level": "info",
					"app":   "test",
				}),
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				if tm.ShouldTraceLog(entry) {
					_, span := tm.CreateLogSpan(ctx, entry)
					if span != nil {
						span.End()
					}
				}
			}
		})
	}
}

// BenchmarkLogEntrySize benchmarks with different log entry sizes
func BenchmarkLogEntrySize(b *testing.B) {
	logger := newBenchLogger()
	config := EnhancedTracingConfig{
		Enabled:        true,
		Mode:           ModeHybrid,
		LogTracingRate: 0.1,
	}

	tm, err := NewEnhancedTracingManager(config, logger)
	if err != nil {
		b.Fatal(err)
	}

	sizes := []struct {
		name    string
		message string
	}{
		{"small_64B", "Small log message"},
		{"medium_256B", "Medium log message with more details about what happened in the system and some context " +
			"that might be useful for debugging purposes when things go wrong in production environments"},
		{"large_1KB", generateLargeMessage(1024)},
	}

	for _, size := range sizes {
		b.Run(size.name, func(b *testing.B) {
			entry := &types.LogEntry{
				Message:  size.message,
				SourceID: "test-source",
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				tm.ShouldTraceLog(entry)
			}
		})
	}
}

// Helper function to generate large messages
func generateLargeMessage(size int) string {
	msg := ""
	for len(msg) < size {
		msg += "This is a sample log message that will be repeated to reach the desired size. "
	}
	return msg[:size]
}

// BenchmarkEndToEndFlow benchmarks complete end-to-end flow
func BenchmarkEndToEndFlow(b *testing.B) {
	logger := newBenchLogger()
	config := EnhancedTracingConfig{
		Enabled:        true,
		Mode:           ModeHybrid,
		LogTracingRate: 0.1,
		OnDemand: OnDemandConfig{
			Enabled: true,
		},
		AdaptiveSampling: AdaptiveSamplingConfig{
			Enabled:          true,
			LatencyThreshold: 100 * time.Millisecond,
			SampleRate:       0.2,
		},
	}

	tm, err := NewEnhancedTracingManager(config, logger)
	if err != nil {
		b.Fatal(err)
	}

	ctx := context.Background()
	entry := &types.LogEntry{
		Message:  "Production-like log message with details",
		SourceID: "production-service-12345",
		Labels: types.NewLabelsCOWFromMap(map[string]string{
			"level":       "info",
			"service":     "api-gateway",
			"environment": "production",
		}),
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Check if should trace
		if tm.ShouldTraceLog(entry) {
			// Create span
			_, span := tm.CreateLogSpan(ctx, entry)
			if span != nil {
				// Simulate some work
				// In real scenario, this would be actual log processing
				span.End()
			}
		}
	}
}
