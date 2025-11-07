package tracing

import (
	"context"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"ssw-logs-capture/pkg/types"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// NOTE: These tests must be run with: go test -p 1 ./pkg/tracing/
// This is because Prometheus metrics are registered globally and cannot be re-registered
// within the same process. Running tests in parallel causes "duplicate metrics collector" panics.
// Alternatively, run individual tests: go test -run TestName ./pkg/tracing/

// Helper function to create a silent logger for tests
func newTestLogger() *logrus.Logger {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	return logger
}

// Helper function to create test config with valid defaults
func newTestConfig() EnhancedTracingConfig {
	return EnhancedTracingConfig{
		Enabled:        true,
		Mode:           ModeHybrid,
		ServiceName:    "test-service",
		ServiceVersion: "v1.0.0-test",
		Environment:    "test",
		Exporter:       "otlp", // Valid exporter
		Endpoint:       "http://localhost:4318/v1/traces",
		BatchTimeout:   time.Second,
		MaxBatchSize:   100,
		LogTracingRate: 0.0,
	}
}

// TestTracingManager_ModeSwitching tests mode switching functionality
func TestTracingManager_ModeSwitching(t *testing.T) {
	logger := newTestLogger()

	tests := []struct {
		name           string
		mode           TracingMode
		logTracingRate float64
		expectedTrace  bool
		description    string
	}{
		{
			name:           "mode_off",
			mode:           ModeOff,
			logTracingRate: 1.0,
			expectedTrace:  false,
			description:    "OFF mode should never trace logs",
		},
		{
			name:           "mode_system_only",
			mode:           ModeSystemOnly,
			logTracingRate: 1.0,
			expectedTrace:  false,
			description:    "SYSTEM-ONLY mode should not trace individual logs",
		},
		{
			name:           "mode_hybrid_0pct",
			mode:           ModeHybrid,
			logTracingRate: 0.0,
			expectedTrace:  false,
			description:    "HYBRID mode with 0% rate should not trace logs",
		},
		{
			name:           "mode_hybrid_100pct",
			mode:           ModeHybrid,
			logTracingRate: 1.0,
			expectedTrace:  true,
			description:    "HYBRID mode with 100% rate should trace all logs",
		},
		{
			name:           "mode_full_e2e",
			mode:           ModeFullE2E,
			logTracingRate: 0.0, // Should be ignored
			expectedTrace:  true,
			description:    "FULL-E2E mode should always trace all logs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := newTestConfig()
			config.Mode = tt.mode
			config.LogTracingRate = tt.logTracingRate

			tm, err := NewEnhancedTracingManager(config, logger)
			require.NoError(t, err, "Failed to create tracing manager")
			require.NotNil(t, tm, "Tracing manager should not be nil")

			entry := &types.LogEntry{
				Message:  "test message",
				SourceID: "test-source",
			}

			result := tm.ShouldTraceLog(entry)
			assert.Equal(t, tt.expectedTrace, result, tt.description)
		})
	}
}

// TestTracingManager_AdaptiveSampling tests adaptive sampling functionality
func TestTracingManager_AdaptiveSampling(t *testing.T) {
	logger := newTestLogger()

	config := EnhancedTracingConfig{
		Enabled:        true,
		Mode:           ModeHybrid,
		LogTracingRate: 0.0, // Start at 0%
		AdaptiveSampling: AdaptiveSamplingConfig{
			Enabled:          true,
			LatencyThreshold: 100 * time.Millisecond,
			SampleRate:       0.5, // 50% when triggered
		},
	}

	tm, err := NewEnhancedTracingManager(config, logger)
	require.NoError(t, err)
	require.NotNil(t, tm)

	// Before high latency, should not trace (0% rate)
	entry := &types.LogEntry{
		Message:  "test",
		SourceID: "test-source",
	}
	assert.False(t, tm.ShouldTraceLog(entry), "Should not trace at 0% base rate")

	// Simulate high latency
	tm.adaptiveSampler.RecordLatency(200 * time.Millisecond)

	// Should trigger adaptive sampling
	// Test probabilistically by running many times
	traced := 0
	iterations := 1000
	for i := 0; i < iterations; i++ {
		testEntry := &types.LogEntry{
			Message:  fmt.Sprintf("test-%d", i),
			SourceID: "test-source",
		}
		if tm.ShouldTraceLog(testEntry) {
			traced++
		}
	}

	// Expect ~50% traced (sample_rate=0.5)
	// Allow 10% margin of error (45%-55%)
	expectedMin := iterations * 45 / 100
	expectedMax := iterations * 55 / 100
	assert.GreaterOrEqual(t, traced, expectedMin, "Too few logs traced during adaptive sampling")
	assert.LessOrEqual(t, traced, expectedMax, "Too many logs traced during adaptive sampling")

	t.Logf("Traced %d out of %d logs (%.1f%%), expected ~50%%", traced, iterations, float64(traced)*100/float64(iterations))
}

// TestTracingManager_OnDemand tests on-demand tracing functionality
func TestTracingManager_OnDemand(t *testing.T) {
	logger := newTestLogger()

	config := EnhancedTracingConfig{
		Enabled:        true,
		Mode:           ModeHybrid,
		LogTracingRate: 0.0, // Base rate 0%
		OnDemand: OnDemandConfig{
			Enabled: true,
		},
	}

	tm, err := NewEnhancedTracingManager(config, logger)
	require.NoError(t, err)
	require.NotNil(t, tm)

	sourceID := "test-source-123"
	entry := &types.LogEntry{
		Message:  "test message",
		SourceID: sourceID,
	}

	// Initially should not trace (0% rate)
	assert.False(t, tm.ShouldTraceLog(entry), "Should not trace before on-demand enabled")

	// Enable on-demand tracing with 100% rate for 1 hour
	tm.EnableOnDemandTracing(sourceID, 1.0, 1*time.Hour)

	// Should now trace (on-demand enabled)
	assert.True(t, tm.ShouldTraceLog(entry), "Should trace after on-demand enabled")

	// Disable on-demand
	tm.DisableOnDemandTracing(sourceID)

	// Should stop tracing (back to base rate of 0%)
	assert.False(t, tm.ShouldTraceLog(entry), "Should not trace after on-demand disabled")
}

// TestTracingManager_OnDemandExpiration tests that on-demand rules expire
func TestTracingManager_OnDemandExpiration(t *testing.T) {
	logger := newTestLogger()

	config := EnhancedTracingConfig{
		Enabled:        true,
		Mode:           ModeHybrid,
		LogTracingRate: 0.0,
		OnDemand: OnDemandConfig{
			Enabled: true,
		},
	}

	tm, err := NewEnhancedTracingManager(config, logger)
	require.NoError(t, err)

	sourceID := "test-source-expiry"
	entry := &types.LogEntry{
		Message:  "test",
		SourceID: sourceID,
	}

	// Enable on-demand tracing for 100ms
	tm.EnableOnDemandTracing(sourceID, 1.0, 100*time.Millisecond)

	// Should trace immediately
	assert.True(t, tm.ShouldTraceLog(entry), "Should trace when on-demand is active")

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)

	// Should no longer trace (expired)
	assert.False(t, tm.ShouldTraceLog(entry), "Should not trace after on-demand expired")
}

// TestTracingManager_ConcurrentAccess tests thread-safety
func TestTracingManager_ConcurrentAccess(t *testing.T) {
	logger := newTestLogger()

	config := EnhancedTracingConfig{
		Enabled:        true,
		Mode:           ModeHybrid,
		LogTracingRate: 0.1, // 10% base rate
		OnDemand: OnDemandConfig{
			Enabled: true,
		},
	}

	tm, err := NewEnhancedTracingManager(config, logger)
	require.NoError(t, err)

	// Run multiple goroutines concurrently
	// This test should be run with: go test -race
	var wg sync.WaitGroup
	numGoroutines := 10
	iterationsPerGoroutine := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			sourceID := fmt.Sprintf("source-%d", id)

			for j := 0; j < iterationsPerGoroutine; j++ {
				entry := &types.LogEntry{
					Message:  fmt.Sprintf("test-%d-%d", id, j),
					SourceID: sourceID,
				}

				// Read operation
				_ = tm.ShouldTraceLog(entry)

				// Write operation (enable/disable on-demand)
				if j%10 == 0 {
					tm.EnableOnDemandTracing(sourceID, 1.0, 10*time.Second)
				}
				if j%10 == 5 {
					tm.DisableOnDemandTracing(sourceID)
				}
			}
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	t.Log("Concurrent access test completed successfully")
}

// TestTracingManager_SpanCreation tests span creation
func TestTracingManager_SpanCreation(t *testing.T) {
	logger := newTestLogger()

	config := EnhancedTracingConfig{
		Enabled:        true,
		Mode:           ModeFullE2E,
		LogTracingRate: 1.0,
	}

	tm, err := NewEnhancedTracingManager(config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	entry := &types.LogEntry{
		Message:  "test message",
		SourceID: "test-source",
	}

	// Create log span
	spanCtx, span := tm.CreateLogSpan(ctx, entry)
	require.NotNil(t, spanCtx, "Span context should not be nil")
	require.NotNil(t, span, "Span should not be nil")

	// End span
	span.End()
}

// TestTracingManager_HybridModeProbability tests hybrid mode sampling probability
func TestTracingManager_HybridModeProbability(t *testing.T) {
	logger := newTestLogger()

	testCases := []struct {
		name           string
		rate           float64
		expectedMin    float64
		expectedMax    float64
		iterations     int
	}{
		{
			name:        "1_percent",
			rate:        0.01,
			expectedMin: 0.005,
			expectedMax: 0.015,
			iterations:  10000,
		},
		{
			name:        "10_percent",
			rate:        0.10,
			expectedMin: 0.08,
			expectedMax: 0.12,
			iterations:  5000,
		},
		{
			name:        "50_percent",
			rate:        0.50,
			expectedMin: 0.48,
			expectedMax: 0.52,
			iterations:  2000,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := EnhancedTracingConfig{
				Enabled:        true,
				Mode:           ModeHybrid,
				LogTracingRate: tc.rate,
			}

			tm, err := NewEnhancedTracingManager(config, logger)
			require.NoError(t, err)

			traced := 0
			for i := 0; i < tc.iterations; i++ {
				entry := &types.LogEntry{
					Message:  fmt.Sprintf("test-%d", i),
					SourceID: "test-source",
				}
				if tm.ShouldTraceLog(entry) {
					traced++
				}
			}

			actualRate := float64(traced) / float64(tc.iterations)
			t.Logf("Rate: %.2f%%, Expected: %.2f%%, Actual: %.2f%%",
				tc.rate*100, tc.rate*100, actualRate*100)

			assert.GreaterOrEqual(t, actualRate, tc.expectedMin,
				"Actual rate too low")
			assert.LessOrEqual(t, actualRate, tc.expectedMax,
				"Actual rate too high")
		})
	}
}

// TestTracingManager_MultipleOnDemandRules tests multiple concurrent on-demand rules
func TestTracingManager_MultipleOnDemandRules(t *testing.T) {
	logger := newTestLogger()

	config := EnhancedTracingConfig{
		Enabled:        true,
		Mode:           ModeHybrid,
		LogTracingRate: 0.0,
		OnDemand: OnDemandConfig{
			Enabled: true,
		},
	}

	tm, err := NewEnhancedTracingManager(config, logger)
	require.NoError(t, err)

	// Enable on-demand for multiple sources
	sources := []string{"source-1", "source-2", "source-3"}
	for _, sourceID := range sources {
		tm.EnableOnDemandTracing(sourceID, 1.0, 1*time.Hour)
	}

	// All sources should be traced
	for _, sourceID := range sources {
		entry := &types.LogEntry{
			Message:  "test",
			SourceID: sourceID,
		}
		assert.True(t, tm.ShouldTraceLog(entry),
			fmt.Sprintf("Source %s should be traced", sourceID))
	}

	// Disable one source
	tm.DisableOnDemandTracing("source-2")

	// source-2 should not be traced
	entry2 := &types.LogEntry{
		Message:  "test",
		SourceID: "source-2",
	}
	assert.False(t, tm.ShouldTraceLog(entry2), "source-2 should not be traced after disable")

	// Other sources should still be traced
	entry1 := &types.LogEntry{
		Message:  "test",
		SourceID: "source-1",
	}
	assert.True(t, tm.ShouldTraceLog(entry1), "source-1 should still be traced")
}

// TestTracingManager_DisabledTracing tests behavior when tracing is disabled
func TestTracingManager_DisabledTracing(t *testing.T) {
	logger := newTestLogger()

	config := EnhancedTracingConfig{
		Enabled: false, // Tracing disabled
		Mode:    ModeFullE2E,
	}

	tm, err := NewEnhancedTracingManager(config, logger)
	require.NoError(t, err)

	entry := &types.LogEntry{
		Message:  "test",
		SourceID: "test-source",
	}

	// Should never trace when disabled
	assert.False(t, tm.ShouldTraceLog(entry), "Should not trace when tracing is disabled")

	// Span creation should return nil span
	ctx := context.Background()
	spanCtx, span := tm.CreateLogSpan(ctx, entry)
	assert.NotNil(t, spanCtx, "Context should be returned even when disabled")
	assert.Nil(t, span, "Span should be nil when tracing is disabled")
}

// TestTracingManager_MetricsRecording tests that metrics are recorded correctly
func TestTracingManager_MetricsRecording(t *testing.T) {
	logger := newTestLogger()

	config := EnhancedTracingConfig{
		Enabled:        true,
		Mode:           ModeHybrid,
		LogTracingRate: 1.0, // 100% to ensure tracing
	}

	tm, err := NewEnhancedTracingManager(config, logger)
	require.NoError(t, err)

	// Record some traces
	for i := 0; i < 10; i++ {
		entry := &types.LogEntry{
			Message:  fmt.Sprintf("test-%d", i),
			SourceID: "test-source",
		}
		if tm.ShouldTraceLog(entry) {
			ctx := context.Background()
			_, span := tm.CreateLogSpan(ctx, entry)
			if span != nil {
				span.End()
			}
		}
	}

	// Note: Actual metric values would need to be checked via Prometheus
	// This test just ensures no panics occur during metric recording
	t.Log("Metrics recording test completed without errors")
}

// TestTracingManager_NilEntry tests handling of nil log entries
func TestTracingManager_NilEntry(t *testing.T) {
	logger := newTestLogger()

	config := EnhancedTracingConfig{
		Enabled:        true,
		Mode:           ModeFullE2E,
		LogTracingRate: 1.0,
	}

	tm, err := NewEnhancedTracingManager(config, logger)
	require.NoError(t, err)

	// Should handle nil entry gracefully
	assert.False(t, tm.ShouldTraceLog(nil), "Should return false for nil entry")

	ctx := context.Background()
	spanCtx, span := tm.CreateLogSpan(ctx, nil)
	assert.NotNil(t, spanCtx, "Should return context even for nil entry")
	// Span behavior for nil entry depends on implementation
	if span != nil {
		span.End()
	}
}

// TestTracingManager_ContextPropagation tests that trace context is properly propagated
func TestTracingManager_ContextPropagation(t *testing.T) {
	logger := newTestLogger()

	config := EnhancedTracingConfig{
		Enabled:        true,
		Mode:           ModeFullE2E,
		LogTracingRate: 1.0,
	}

	tm, err := NewEnhancedTracingManager(config, logger)
	require.NoError(t, err)

	// Create parent span
	parentCtx := context.Background()
	entry := &types.LogEntry{
		Message:  "parent",
		SourceID: "test",
	}

	ctx1, span1 := tm.CreateLogSpan(parentCtx, entry)
	require.NotNil(t, span1)

	// Create child span using propagated context
	childEntry := &types.LogEntry{
		Message:  "child",
		SourceID: "test",
	}

	ctx2, span2 := tm.CreateLogSpan(ctx1, childEntry)
	require.NotNil(t, span2)

	// Both contexts should be different (child context contains parent trace)
	assert.NotEqual(t, parentCtx, ctx1, "Parent context should be modified")
	assert.NotEqual(t, ctx1, ctx2, "Child context should be different")

	// Clean up
	span2.End()
	span1.End()
}
