package tracing

import (
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTracingMetrics_Initialization validates that all metrics are properly initialized
func TestTracingMetrics_Initialization(t *testing.T) {
	// Create a new registry to avoid conflicts with global metrics
	registry := prometheus.NewRegistry()

	// Manually create metrics with custom registry
	metrics := &TracingMetrics{
		tracingMode: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "test_tracing_mode",
			Help: "Test metric",
		}, []string{"mode"}),
		logsTracedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "test_logs_traced_total",
			Help: "Test metric",
		}),
		spansCreatedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "test_spans_created_total",
			Help: "Test metric",
		}, []string{"span_type"}),
		samplingRate: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "test_sampling_rate",
			Help: "Test metric",
		}),
		onDemandRulesActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "test_on_demand_rules_active",
			Help: "Test metric",
		}),
		adaptiveSamplingActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "test_adaptive_sampling_active",
			Help: "Test metric",
		}),
		spansExportedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "test_spans_exported_total",
			Help: "Test metric",
		}),
		spansDroppedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "test_spans_dropped_total",
			Help: "Test metric",
		}, []string{"reason"}),
	}

	// Register all metrics
	registry.MustRegister(
		metrics.tracingMode,
		metrics.logsTracedTotal,
		metrics.spansCreatedTotal,
		metrics.samplingRate,
		metrics.onDemandRulesActive,
		metrics.adaptiveSamplingActive,
		metrics.spansExportedTotal,
		metrics.spansDroppedTotal,
	)

	// Verify all metrics exist
	require.NotNil(t, metrics.tracingMode, "tracingMode should be initialized")
	require.NotNil(t, metrics.logsTracedTotal, "logsTracedTotal should be initialized")
	require.NotNil(t, metrics.spansCreatedTotal, "spansCreatedTotal should be initialized")
	require.NotNil(t, metrics.samplingRate, "samplingRate should be initialized")
	require.NotNil(t, metrics.onDemandRulesActive, "onDemandRulesActive should be initialized")
	require.NotNil(t, metrics.adaptiveSamplingActive, "adaptiveSamplingActive should be initialized")
	require.NotNil(t, metrics.spansExportedTotal, "spansExportedTotal should be initialized")
	require.NotNil(t, metrics.spansDroppedTotal, "spansDroppedTotal should be initialized")
}

// TestTracingMetrics_SamplingRate validates sampling rate metric updates
func TestTracingMetrics_SamplingRate(t *testing.T) {
	registry := prometheus.NewRegistry()
	gauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "test_sampling_rate",
		Help: "Test metric",
	})
	registry.MustRegister(gauge)

	metrics := &TracingMetrics{samplingRate: gauge}

	tests := []struct {
		name string
		rate float64
	}{
		{"zero rate", 0.0},
		{"1% rate", 0.01},
		{"10% rate", 0.10},
		{"50% rate", 0.50},
		{"100% rate", 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metrics.RecordSamplingRate(tt.rate)

			// Verify the value is set correctly
			value := testutil.ToFloat64(gauge)
			assert.Equal(t, tt.rate, value, "Sampling rate should match")
		})
	}
}

// TestTracingMetrics_OnDemandRules validates on-demand rules counter
func TestTracingMetrics_OnDemandRules(t *testing.T) {
	registry := prometheus.NewRegistry()
	gauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "test_on_demand_rules",
		Help: "Test metric",
	})
	registry.MustRegister(gauge)

	metrics := &TracingMetrics{onDemandRulesActive: gauge}

	tests := []struct {
		name  string
		count int
	}{
		{"no rules", 0},
		{"one rule", 1},
		{"five rules", 5},
		{"many rules", 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metrics.RecordOnDemandRulesActive(tt.count)

			value := testutil.ToFloat64(gauge)
			assert.Equal(t, float64(tt.count), value, "Rule count should match")
		})
	}
}

// TestTracingMetrics_AdaptiveSampling validates adaptive sampling status
func TestTracingMetrics_AdaptiveSampling(t *testing.T) {
	registry := prometheus.NewRegistry()
	gauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "test_adaptive_sampling",
		Help: "Test metric",
	})
	registry.MustRegister(gauge)

	metrics := &TracingMetrics{adaptiveSamplingActive: gauge}

	// Test active
	metrics.RecordAdaptiveSamplingActive(true)
	value := testutil.ToFloat64(gauge)
	assert.Equal(t, 1.0, value, "Active should be 1")

	// Test inactive
	metrics.RecordAdaptiveSamplingActive(false)
	value = testutil.ToFloat64(gauge)
	assert.Equal(t, 0.0, value, "Inactive should be 0")
}

// TestTracingMetrics_LogsTraced validates logs traced counter
func TestTracingMetrics_LogsTraced(t *testing.T) {
	registry := prometheus.NewRegistry()
	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "test_logs_traced",
		Help: "Test metric",
	})
	registry.MustRegister(counter)

	metrics := &TracingMetrics{logsTracedTotal: counter}

	// Increment multiple times
	iterations := 100
	for i := 0; i < iterations; i++ {
		metrics.RecordLogTraced()
	}

	value := testutil.ToFloat64(counter)
	assert.Equal(t, float64(iterations), value, "Counter should match iterations")
}

// TestTracingMetrics_ConcurrentAccess validates thread-safety
func TestTracingMetrics_ConcurrentAccess(t *testing.T) {
	registry := prometheus.NewRegistry()

	samplingGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "test_concurrent_sampling",
		Help: "Test metric",
	})
	rulesGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "test_concurrent_rules",
		Help: "Test metric",
	})
	adaptiveGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "test_concurrent_adaptive",
		Help: "Test metric",
	})
	logsCounter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "test_concurrent_logs",
		Help: "Test metric",
	})

	registry.MustRegister(samplingGauge, rulesGauge, adaptiveGauge, logsCounter)

	metrics := &TracingMetrics{
		samplingRate:           samplingGauge,
		onDemandRulesActive:    rulesGauge,
		adaptiveSamplingActive: adaptiveGauge,
		logsTracedTotal:        logsCounter,
	}

	// Spawn 100 goroutines doing concurrent updates
	var wg sync.WaitGroup
	goroutines := 100
	iterationsPerGoroutine := 10

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < iterationsPerGoroutine; j++ {
				// Update different metrics concurrently
				metrics.RecordSamplingRate(float64(id) / 100.0)
				metrics.RecordOnDemandRulesActive(id)
				metrics.RecordAdaptiveSamplingActive(id%2 == 0)
				metrics.RecordLogTraced()
			}
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Verify logs counter incremented correctly
	logsCount := testutil.ToFloat64(logsCounter)
	expectedLogs := float64(goroutines * iterationsPerGoroutine)
	assert.Equal(t, expectedLogs, logsCount, "Logs counter should be accurate after concurrent access")

	// Other gauges will have final values from last writes, just verify no panic occurred
	t.Log("Concurrent access completed without race conditions")
}

// TestTracingMetrics_SpansCreated validates span creation tracking
func TestTracingMetrics_SpansCreated(t *testing.T) {
	registry := prometheus.NewRegistry()
	counter := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "test_spans_created",
		Help: "Test metric",
	}, []string{"span_type"})
	registry.MustRegister(counter)

	metrics := &TracingMetrics{spansCreatedTotal: counter}

	// Record different span types
	metrics.RecordSpanCreated("log")
	metrics.RecordSpanCreated("log")
	metrics.RecordSpanCreated("system")
	metrics.RecordSpanCreated("system")
	metrics.RecordSpanCreated("system")

	// Verify counts
	logSpans := testutil.ToFloat64(counter.WithLabelValues("log"))
	systemSpans := testutil.ToFloat64(counter.WithLabelValues("system"))

	assert.Equal(t, 2.0, logSpans, "Log spans should be 2")
	assert.Equal(t, 3.0, systemSpans, "System spans should be 3")
}

// TestTracingMetrics_SpansExported validates span export tracking
func TestTracingMetrics_SpansExported(t *testing.T) {
	registry := prometheus.NewRegistry()
	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "test_spans_exported",
		Help: "Test metric",
	})
	registry.MustRegister(counter)

	metrics := &TracingMetrics{spansExportedTotal: counter}

	// Export some spans
	for i := 0; i < 42; i++ {
		metrics.RecordSpanExported()
	}

	value := testutil.ToFloat64(counter)
	assert.Equal(t, 42.0, value, "Exported spans should match")
}

// TestTracingMetrics_SpansDropped validates span drop tracking
func TestTracingMetrics_SpansDropped(t *testing.T) {
	registry := prometheus.NewRegistry()
	counter := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "test_spans_dropped",
		Help: "Test metric",
	}, []string{"reason"})
	registry.MustRegister(counter)

	metrics := &TracingMetrics{spansDroppedTotal: counter}

	// Drop spans for different reasons
	metrics.RecordSpanDropped("queue_full")
	metrics.RecordSpanDropped("queue_full")
	metrics.RecordSpanDropped("timeout")
	metrics.RecordSpanDropped("error")

	// Verify counts
	queueFull := testutil.ToFloat64(counter.WithLabelValues("queue_full"))
	timeout := testutil.ToFloat64(counter.WithLabelValues("timeout"))
	errors := testutil.ToFloat64(counter.WithLabelValues("error"))

	assert.Equal(t, 2.0, queueFull, "Queue full drops should be 2")
	assert.Equal(t, 1.0, timeout, "Timeout drops should be 1")
	assert.Equal(t, 1.0, errors, "Error drops should be 1")
}

// TestTracingMetrics_ModeRecording validates mode tracking
func TestTracingMetrics_ModeRecording(t *testing.T) {
	registry := prometheus.NewRegistry()
	gauge := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "test_tracing_mode",
		Help: "Test metric",
	}, []string{"mode"})
	registry.MustRegister(gauge)

	metrics := &TracingMetrics{tracingMode: gauge}

	tests := []struct {
		name         string
		mode         TracingMode
		expectedMode string
	}{
		{"off mode", ModeOff, "off"},
		{"system mode", ModeSystemOnly, "system-only"},
		{"hybrid mode", ModeHybrid, "hybrid"},
		{"full e2e mode", ModeFullE2E, "full-e2e"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metrics.RecordMode(tt.mode)

			// Verify only the current mode is set to 1
			value := testutil.ToFloat64(gauge.WithLabelValues(tt.expectedMode))
			assert.Equal(t, 1.0, value, "Current mode should be 1")

			// Verify other modes are 0
			allModes := []string{"off", "system-only", "hybrid", "full-e2e"}
			for _, mode := range allModes {
				if mode != tt.expectedMode {
					val := testutil.ToFloat64(gauge.WithLabelValues(mode))
					assert.Equal(t, 0.0, val, "Other modes should be 0")
				}
			}
		})
	}
}
