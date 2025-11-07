package tracing

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// TracingMetrics holds all Prometheus metrics for the tracing system
type TracingMetrics struct {
	// Current tracing mode (off=0, system-only=1, hybrid=2, full-e2e=3)
	tracingMode *prometheus.GaugeVec

	// Total number of logs that were traced
	logsTracedTotal prometheus.Counter

	// Total number of spans created by type (log vs system)
	spansCreatedTotal *prometheus.CounterVec

	// Current sampling rate
	samplingRate prometheus.Gauge

	// Number of active on-demand rules
	onDemandRulesActive prometheus.Gauge

	// Adaptive sampling status (0=inactive, 1=active)
	adaptiveSamplingActive prometheus.Gauge

	// Total number of spans exported successfully
	spansExportedTotal prometheus.Counter

	// Total number of spans dropped by reason
	spansDroppedTotal *prometheus.CounterVec
}

// NewTracingMetrics creates and registers all tracing-related Prometheus metrics
func NewTracingMetrics() *TracingMetrics {
	return &TracingMetrics{
		tracingMode: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "log_capturer_tracing_mode",
			Help: "Current tracing mode (0=off, 1=system-only, 2=hybrid, 3=full-e2e)",
		}, []string{"mode"}),

		logsTracedTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "log_capturer_tracing_logs_traced_total",
			Help: "Total number of individual log entries that were traced",
		}),

		spansCreatedTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "log_capturer_tracing_spans_created_total",
			Help: "Total number of spans created by type",
		}, []string{"span_type"}),

		samplingRate: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "log_capturer_tracing_sampling_rate",
			Help: "Current log sampling rate for hybrid mode (0.0 to 1.0)",
		}),

		onDemandRulesActive: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "log_capturer_tracing_on_demand_rules_active",
			Help: "Number of active on-demand tracing rules",
		}),

		adaptiveSamplingActive: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "log_capturer_tracing_adaptive_sampling_active",
			Help: "Adaptive sampling status (0=inactive, 1=active)",
		}),

		spansExportedTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "log_capturer_tracing_spans_exported_total",
			Help: "Total number of spans successfully exported to collector",
		}),

		spansDroppedTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "log_capturer_tracing_spans_dropped_total",
			Help: "Total number of spans dropped by reason",
		}, []string{"reason"}),
	}
}

// RecordMode updates the current tracing mode metric
func (m *TracingMetrics) RecordMode(mode TracingMode) {
	// Reset all mode gauges first
	m.tracingMode.WithLabelValues("off").Set(0)
	m.tracingMode.WithLabelValues("system-only").Set(0)
	m.tracingMode.WithLabelValues("hybrid").Set(0)
	m.tracingMode.WithLabelValues("full-e2e").Set(0)

	// Set current mode to 1
	switch mode {
	case ModeOff:
		m.tracingMode.WithLabelValues("off").Set(1)
	case ModeSystemOnly:
		m.tracingMode.WithLabelValues("system-only").Set(1)
	case ModeHybrid:
		m.tracingMode.WithLabelValues("hybrid").Set(1)
	case ModeFullE2E:
		m.tracingMode.WithLabelValues("full-e2e").Set(1)
	}
}

// RecordLogTraced increments the counter of traced logs
func (m *TracingMetrics) RecordLogTraced() {
	m.logsTracedTotal.Inc()
}

// RecordSpanCreated increments the counter of created spans by type
func (m *TracingMetrics) RecordSpanCreated(spanType string) {
	m.spansCreatedTotal.WithLabelValues(spanType).Inc()
}

// RecordSamplingRate updates the current sampling rate
func (m *TracingMetrics) RecordSamplingRate(rate float64) {
	m.samplingRate.Set(rate)
}

// RecordOnDemandRulesActive updates the number of active on-demand rules
func (m *TracingMetrics) RecordOnDemandRulesActive(count int) {
	m.onDemandRulesActive.Set(float64(count))
}

// RecordAdaptiveSamplingActive updates the adaptive sampling status
func (m *TracingMetrics) RecordAdaptiveSamplingActive(active bool) {
	if active {
		m.adaptiveSamplingActive.Set(1)
	} else {
		m.adaptiveSamplingActive.Set(0)
	}
}

// RecordSpanExported increments the counter of exported spans
func (m *TracingMetrics) RecordSpanExported() {
	m.spansExportedTotal.Inc()
}

// RecordSpanDropped increments the counter of dropped spans by reason
func (m *TracingMetrics) RecordSpanDropped(reason string) {
	m.spansDroppedTotal.WithLabelValues(reason).Inc()
}
