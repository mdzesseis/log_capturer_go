package tracing

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"ssw-logs-capture/pkg/types"

	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// TracingMode defines the operational mode for tracing
type TracingMode string

const (
	// ModeOff disables all tracing
	ModeOff TracingMode = "off"

	// ModeSystemOnly traces only system operations (batches, sinks, etc.) but not individual logs
	ModeSystemOnly TracingMode = "system-only"

	// ModeHybrid traces system operations + sampled individual logs (configurable rate + adaptive + on-demand)
	ModeHybrid TracingMode = "hybrid"

	// ModeFullE2E traces every log entry end-to-end (100% sampling)
	ModeFullE2E TracingMode = "full-e2e"
)

// EnhancedTracingConfig extends the original config with hybrid tracing support
type EnhancedTracingConfig struct {
	Enabled          bool                      `yaml:"enabled"`
	Mode             TracingMode               `yaml:"mode"`
	ServiceName      string                    `yaml:"service_name"`
	ServiceVersion   string                    `yaml:"service_version"`
	Environment      string                    `yaml:"environment"`
	Exporter         string                    `yaml:"exporter"`
	Endpoint         string                    `yaml:"endpoint"`
	BatchTimeout     time.Duration             `yaml:"batch_timeout"`
	MaxBatchSize     int                       `yaml:"max_batch_size"`
	Headers          map[string]string         `yaml:"headers"`
	LogTracingRate   float64                   `yaml:"log_tracing_rate"`
	AdaptiveSampling AdaptiveSamplingConfig    `yaml:"adaptive_sampling"`
	OnDemand         OnDemandConfig            `yaml:"on_demand"`
}

// AdaptiveSamplingConfig configures adaptive sampling based on latency
type AdaptiveSamplingConfig struct {
	Enabled           bool          `yaml:"enabled"`
	LatencyThreshold  time.Duration `yaml:"latency_threshold"`
	SampleRate        float64       `yaml:"sample_rate"`
	WindowSize        time.Duration `yaml:"window_size"`
}

// OnDemandConfig configures on-demand tracing control via API
type OnDemandConfig struct {
	Enabled     bool   `yaml:"enabled"`
	APIEndpoint string `yaml:"api_endpoint"`
}

// EnhancedTracingManager manages distributed tracing with 4 operational modes
type EnhancedTracingManager struct {
	config   EnhancedTracingConfig
	logger   *logrus.Logger
	provider *trace.TracerProvider
	tracer   oteltrace.Tracer

	// Adaptive sampling
	adaptiveSampler *AdaptiveSampler

	// On-demand control
	onDemandCtrl *OnDemandController

	// Prometheus metrics
	metrics *TracingMetrics

	// Hot-reload support
	mu sync.RWMutex

	// Internal counters
	logsTracedCount   int64
	spansCreatedCount int64
}

// NewEnhancedTracingManager creates a new enhanced tracing manager
func NewEnhancedTracingManager(config EnhancedTracingConfig, logger *logrus.Logger) (*EnhancedTracingManager, error) {
	if !config.Enabled {
		return &EnhancedTracingManager{
			config: config,
			logger: logger,
			tracer: otel.Tracer("noop"),
		}, nil
	}

	// Validate mode
	if !isValidMode(config.Mode) {
		return nil, fmt.Errorf("invalid tracing mode: %s (valid: off, system-only, hybrid, full-e2e)", config.Mode)
	}

	// In full-e2e mode, force log_tracing_rate to 1.0
	if config.Mode == ModeFullE2E {
		config.LogTracingRate = 1.0
	}

	tm := &EnhancedTracingManager{
		config:  config,
		logger:  logger,
		metrics: NewTracingMetrics(),
	}

	if err := tm.initialize(); err != nil {
		return nil, err
	}

	// Initialize adaptive sampler
	if config.Mode == ModeHybrid && config.AdaptiveSampling.Enabled {
		tm.adaptiveSampler = NewAdaptiveSampler(config.AdaptiveSampling, logger)
		// Record initial adaptive sampling state
		tm.metrics.RecordAdaptiveSamplingActive(true)
	} else {
		tm.metrics.RecordAdaptiveSamplingActive(false)
	}

	// Initialize on-demand controller
	if config.Mode == ModeHybrid && config.OnDemand.Enabled {
		tm.onDemandCtrl = NewOnDemandController()
		// Record initial on-demand rules (0 at start)
		tm.metrics.RecordOnDemandRulesActive(0)
	} else {
		tm.metrics.RecordOnDemandRulesActive(0)
	}

	// Record initial mode in metrics
	tm.metrics.RecordMode(config.Mode)
	tm.metrics.RecordSamplingRate(config.LogTracingRate)

	return tm, nil
}

// initialize sets up the tracing provider
func (tm *EnhancedTracingManager) initialize() error {
	// Create exporter based on configuration
	exporter, err := tm.createExporter()
	if err != nil {
		return fmt.Errorf("failed to create trace exporter: %w", err)
	}

	// Create resource
	res, err := tm.createResource()
	if err != nil {
		return fmt.Errorf("failed to create trace resource: %w", err)
	}

	// Create tracer provider
	tm.provider = trace.NewTracerProvider(
		trace.WithBatcher(exporter,
			trace.WithBatchTimeout(tm.config.BatchTimeout),
			trace.WithMaxExportBatchSize(tm.config.MaxBatchSize),
		),
		trace.WithResource(res),
		// Note: We handle sampling manually based on mode
		trace.WithSampler(trace.AlwaysSample()),
	)

	// Set global tracer provider
	otel.SetTracerProvider(tm.provider)

	// Set global propagator
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// Get tracer
	tm.tracer = otel.Tracer(tm.config.ServiceName)

	tm.logger.WithFields(logrus.Fields{
		"service_name": tm.config.ServiceName,
		"exporter":     tm.config.Exporter,
		"endpoint":     tm.config.Endpoint,
		"mode":         tm.config.Mode,
		"log_rate":     tm.config.LogTracingRate,
	}).Info("Enhanced distributed tracing initialized")

	return nil
}

// createExporter creates the appropriate trace exporter
func (tm *EnhancedTracingManager) createExporter() (trace.SpanExporter, error) {
	switch tm.config.Exporter {
	case "jaeger":
		return jaeger.New(jaeger.WithCollectorEndpoint(jaeger.WithEndpoint(tm.config.Endpoint)))

	case "otlp":
		opts := []otlptracehttp.Option{
			otlptracehttp.WithEndpoint(tm.config.Endpoint),
			otlptracehttp.WithInsecure(), // TODO: Support TLS
		}

		// Add headers if configured
		if len(tm.config.Headers) > 0 {
			opts = append(opts, otlptracehttp.WithHeaders(tm.config.Headers))
		}

		return otlptrace.New(context.Background(), otlptracehttp.NewClient(opts...))

	case "console":
		// For development/debugging
		return otlptrace.New(context.Background(), otlptracehttp.NewClient(
			otlptracehttp.WithEndpoint("http://localhost:4318"),
			otlptracehttp.WithInsecure(),
		))

	default:
		return nil, fmt.Errorf("unsupported exporter: %s", tm.config.Exporter)
	}
}

// createResource creates the trace resource
func (tm *EnhancedTracingManager) createResource() (*resource.Resource, error) {
	return resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(tm.config.ServiceName),
			semconv.ServiceVersion(tm.config.ServiceVersion),
			semconv.DeploymentEnvironment(tm.config.Environment),
			attribute.String("tracing.mode", string(tm.config.Mode)),
		),
	)
}

// ShouldTraceLog decides if a log entry should be traced based on current mode and sampling
func (tm *EnhancedTracingManager) ShouldTraceLog(entry *types.LogEntry) bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	switch tm.config.Mode {
	case ModeOff:
		return false

	case ModeSystemOnly:
		// Only trace system operations, not individual logs
		return false

	case ModeFullE2E:
		// Trace every log
		return true

	case ModeHybrid:
		// Check on-demand override first (highest priority)
		if tm.onDemandCtrl != nil && tm.onDemandCtrl.ShouldTrace(entry.SourceID) {
			return true
		}

		// Check adaptive sampling (high latency triggers sampling)
		if tm.adaptiveSampler != nil {
			shouldSample := tm.adaptiveSampler.ShouldSample()
			// Update adaptive sampling active metric
			tm.metrics.RecordAdaptiveSamplingActive(shouldSample)
			if shouldSample {
				return true
			}
		}

		// Check base sampling rate
		return rand.Float64() < tm.config.LogTracingRate

	default:
		return false
	}
}

// CreateLogSpan creates a span for a log entry (if sampling decides to trace it)
func (tm *EnhancedTracingManager) CreateLogSpan(ctx context.Context, entry *types.LogEntry) (context.Context, oteltrace.Span) {
	if !tm.ShouldTraceLog(entry) {
		return ctx, nil
	}

	spanName := fmt.Sprintf("log.process[%s]", entry.SourceType)
	ctx, span := tm.tracer.Start(ctx, spanName,
		oteltrace.WithAttributes(
			attribute.String("log.source_id", entry.SourceID),
			attribute.String("log.source_type", entry.SourceType),
			attribute.Int("log.size", len(entry.Message)),
			attribute.String("tracing.mode", string(tm.config.Mode)),
		),
	)

	// Add trace_id and span_id to log labels for correlation
	if span.SpanContext().HasTraceID() {
		if entry.Labels == nil {
			entry.Labels = make(map[string]string)
		}
		entry.Labels["trace_id"] = span.SpanContext().TraceID().String()
		entry.Labels["span_id"] = span.SpanContext().SpanID().String()
	}

	tm.logsTracedCount++
	tm.spansCreatedCount++

	// Record metrics
	tm.metrics.RecordLogTraced()
	tm.metrics.RecordSpanCreated("log")

	return ctx, span
}

// CreateSystemSpan creates a span for system operations (always traced regardless of mode, except ModeOff)
func (tm *EnhancedTracingManager) CreateSystemSpan(ctx context.Context, operationName string) (context.Context, oteltrace.Span) {
	tm.mu.RLock()
	mode := tm.config.Mode
	tm.mu.RUnlock()

	if mode == ModeOff {
		return ctx, nil
	}

	ctx, span := tm.tracer.Start(ctx, operationName,
		oteltrace.WithAttributes(
			attribute.String("operation.type", "system"),
			attribute.String("tracing.mode", string(mode)),
		),
	)

	tm.spansCreatedCount++

	// Record metrics
	tm.metrics.RecordSpanCreated("system")

	return ctx, span
}

// ReloadConfig hot-reloads the tracing configuration (supports mode switching)
func (tm *EnhancedTracingManager) ReloadConfig(newConfig EnhancedTracingConfig) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Validate new mode
	if !isValidMode(newConfig.Mode) {
		return fmt.Errorf("invalid tracing mode: %s", newConfig.Mode)
	}

	// Force log_tracing_rate to 1.0 in full-e2e mode
	if newConfig.Mode == ModeFullE2E {
		newConfig.LogTracingRate = 1.0
	}

	oldMode := tm.config.Mode
	tm.config = newConfig

	// Reinitialize adaptive sampler if needed
	if newConfig.Mode == ModeHybrid && newConfig.AdaptiveSampling.Enabled {
		if tm.adaptiveSampler == nil {
			tm.adaptiveSampler = NewAdaptiveSampler(newConfig.AdaptiveSampling, tm.logger)
		} else {
			tm.adaptiveSampler.UpdateConfig(newConfig.AdaptiveSampling)
		}
		tm.metrics.RecordAdaptiveSamplingActive(true)
	} else {
		tm.metrics.RecordAdaptiveSamplingActive(false)
	}

	// Reinitialize on-demand controller if needed
	if newConfig.Mode == ModeHybrid && newConfig.OnDemand.Enabled {
		if tm.onDemandCtrl == nil {
			tm.onDemandCtrl = NewOnDemandController()
			tm.metrics.RecordOnDemandRulesActive(0)
		} else {
			// Keep existing rules count
			tm.metrics.RecordOnDemandRulesActive(len(tm.onDemandCtrl.rules))
		}
	} else {
		tm.metrics.RecordOnDemandRulesActive(0)
	}

	// Update metrics
	tm.metrics.RecordMode(newConfig.Mode)
	tm.metrics.RecordSamplingRate(newConfig.LogTracingRate)

	tm.logger.WithFields(logrus.Fields{
		"old_mode": oldMode,
		"new_mode": newConfig.Mode,
		"log_rate": newConfig.LogTracingRate,
	}).Info("Tracing configuration reloaded")

	return nil
}

// EnableOnDemandTracing enables on-demand tracing for a specific source
func (tm *EnhancedTracingManager) EnableOnDemandTracing(sourceID string, rate float64, duration time.Duration) error {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if tm.config.Mode != ModeHybrid {
		return fmt.Errorf("on-demand tracing only available in hybrid mode (current: %s)", tm.config.Mode)
	}

	if tm.onDemandCtrl == nil {
		return fmt.Errorf("on-demand control not enabled")
	}

	tm.onDemandCtrl.Enable(sourceID, rate, duration)

	// Update metrics
	activeRules := len(tm.onDemandCtrl.rules)
	tm.metrics.RecordOnDemandRulesActive(activeRules)

	tm.logger.WithFields(logrus.Fields{
		"source_id": sourceID,
		"rate":      rate,
		"duration":  duration,
	}).Info("On-demand tracing enabled")

	return nil
}

// DisableOnDemandTracing disables on-demand tracing for a specific source
func (tm *EnhancedTracingManager) DisableOnDemandTracing(sourceID string) error {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if tm.onDemandCtrl == nil {
		return fmt.Errorf("on-demand control not enabled")
	}

	tm.onDemandCtrl.Disable(sourceID)

	// Update metrics
	activeRules := len(tm.onDemandCtrl.rules)
	tm.metrics.RecordOnDemandRulesActive(activeRules)

	tm.logger.WithField("source_id", sourceID).Info("On-demand tracing disabled")

	return nil
}

// GetStatus returns the current tracing status
func (tm *EnhancedTracingManager) GetStatus() map[string]interface{} {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	status := map[string]interface{}{
		"enabled":           tm.config.Enabled,
		"mode":              string(tm.config.Mode),
		"log_tracing_rate":  tm.config.LogTracingRate,
		"logs_traced":       tm.logsTracedCount,
		"spans_created":     tm.spansCreatedCount,
	}

	if tm.config.Mode == ModeHybrid {
		status["adaptive_sampling"] = tm.config.AdaptiveSampling.Enabled
		status["on_demand_enabled"] = tm.config.OnDemand.Enabled

		if tm.onDemandCtrl != nil {
			status["on_demand_rules"] = tm.onDemandCtrl.GetActiveRules()
		}
	}

	return status
}

// GetTracer returns the tracer instance
func (tm *EnhancedTracingManager) GetTracer() oteltrace.Tracer {
	return tm.tracer
}

// Shutdown gracefully shuts down the tracing provider
func (tm *EnhancedTracingManager) Shutdown(ctx context.Context) error {
	if tm.provider != nil {
		return tm.provider.Shutdown(ctx)
	}
	return nil
}

// GetMode returns the current tracing mode
func (tm *EnhancedTracingManager) GetMode() TracingMode {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.config.Mode
}

// RecordLatency records latency for adaptive sampling
func (tm *EnhancedTracingManager) RecordLatency(duration time.Duration) {
	if tm.adaptiveSampler != nil {
		tm.adaptiveSampler.RecordLatency(duration)
	}
}

// isValidMode checks if the tracing mode is valid
func isValidMode(mode TracingMode) bool {
	switch mode {
	case ModeOff, ModeSystemOnly, ModeHybrid, ModeFullE2E:
		return true
	default:
		return false
	}
}
