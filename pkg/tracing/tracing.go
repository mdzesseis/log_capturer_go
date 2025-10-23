package tracing

import (
	"context"
	"fmt"
	"net/http"
	"time"

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
	"go.opentelemetry.io/otel/codes"
)

// TracingConfig configures distributed tracing
type TracingConfig struct {
	Enabled      bool    `yaml:"enabled"`
	ServiceName  string  `yaml:"service_name"`
	ServiceVersion string `yaml:"service_version"`
	Environment  string  `yaml:"environment"`
	Exporter     string  `yaml:"exporter"` // "jaeger", "otlp", "console"
	Endpoint     string  `yaml:"endpoint"`
	SampleRate   float64 `yaml:"sample_rate"`
	BatchTimeout time.Duration `yaml:"batch_timeout"`
	MaxBatchSize int     `yaml:"max_batch_size"`
	Headers      map[string]string `yaml:"headers"`
}

// DefaultTracingConfig returns default tracing configuration
func DefaultTracingConfig() TracingConfig {
	return TracingConfig{
		Enabled:      false,
		ServiceName:  "ssw-logs-capture",
		ServiceVersion: "v1.0.0",
		Environment:  "production",
		Exporter:     "otlp",
		Endpoint:     "http://localhost:4318/v1/traces",
		SampleRate:   1.0,
		BatchTimeout: 5 * time.Second,
		MaxBatchSize: 512,
		Headers:      make(map[string]string),
	}
}

// TracingManager manages distributed tracing
type TracingManager struct {
	config   TracingConfig
	logger   *logrus.Logger
	provider *trace.TracerProvider
	tracer   oteltrace.Tracer
}

// NewTracingManager creates a new tracing manager
func NewTracingManager(config TracingConfig, logger *logrus.Logger) (*TracingManager, error) {
	if !config.Enabled {
		return &TracingManager{
			config: config,
			logger: logger,
			tracer: otel.Tracer("noop"),
		}, nil
	}

	tm := &TracingManager{
		config: config,
		logger: logger,
	}

	if err := tm.initialize(); err != nil {
		return nil, err
	}

	return tm, nil
}

// initialize sets up the tracing provider
func (tm *TracingManager) initialize() error {
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
		trace.WithSampler(trace.TraceIDRatioBased(tm.config.SampleRate)),
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
		"sample_rate":  tm.config.SampleRate,
	}).Info("Distributed tracing initialized")

	return nil
}

// createExporter creates the appropriate trace exporter
func (tm *TracingManager) createExporter() (trace.SpanExporter, error) {
	switch tm.config.Exporter {
	case "jaeger":
		return jaeger.New(jaeger.WithCollectorEndpoint(jaeger.WithEndpoint(tm.config.Endpoint)))

	case "otlp":
		opts := []otlptracehttp.Option{
			otlptracehttp.WithEndpoint(tm.config.Endpoint),
		}

		// Add headers if configured
		if len(tm.config.Headers) > 0 {
			opts = append(opts, otlptracehttp.WithHeaders(tm.config.Headers))
		}

		return otlptrace.New(context.Background(), otlptracehttp.NewClient(opts...))

	case "console":
		// For development/debugging
		// For console/debug, use OTLP with no endpoint
		return otlptrace.New(context.Background(), otlptracehttp.NewClient(
			otlptracehttp.WithEndpoint("http://localhost:4318"),
			otlptracehttp.WithInsecure(),
		))

	default:
		return nil, fmt.Errorf("unsupported exporter: %s", tm.config.Exporter)
	}
}

// createResource creates the trace resource
func (tm *TracingManager) createResource() (*resource.Resource, error) {
	return resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(tm.config.ServiceName),
			semconv.ServiceVersion(tm.config.ServiceVersion),
			semconv.DeploymentEnvironment(tm.config.Environment),
		),
	)
}

// GetTracer returns the tracer instance
func (tm *TracingManager) GetTracer() oteltrace.Tracer {
	return tm.tracer
}

// Shutdown gracefully shuts down the tracing provider
func (tm *TracingManager) Shutdown(ctx context.Context) error {
	if tm.provider != nil {
		return tm.provider.Shutdown(ctx)
	}
	return nil
}

// TraceableContext wraps context with tracing utilities
type TraceableContext struct {
	ctx    context.Context
	span   oteltrace.Span
	tracer oteltrace.Tracer
}

// NewTraceableContext creates a new traceable context
func NewTraceableContext(ctx context.Context, tracer oteltrace.Tracer, operationName string) *TraceableContext {
	ctx, span := tracer.Start(ctx, operationName)
	return &TraceableContext{
		ctx:    ctx,
		span:   span,
		tracer: tracer,
	}
}

// Context returns the underlying context
func (tc *TraceableContext) Context() context.Context {
	return tc.ctx
}

// Span returns the current span
func (tc *TraceableContext) Span() oteltrace.Span {
	return tc.span
}

// SetAttribute adds an attribute to the current span
func (tc *TraceableContext) SetAttribute(key string, value interface{}) {
	var attr attribute.KeyValue

	switch v := value.(type) {
	case string:
		attr = attribute.String(key, v)
	case int:
		attr = attribute.Int(key, v)
	case int64:
		attr = attribute.Int64(key, v)
	case float64:
		attr = attribute.Float64(key, v)
	case bool:
		attr = attribute.Bool(key, v)
	default:
		attr = attribute.String(key, fmt.Sprintf("%v", v))
	}

	tc.span.SetAttributes(attr)
}

// SetError records an error in the span
func (tc *TraceableContext) SetError(err error) {
	if err != nil {
		tc.span.RecordError(err)
		tc.span.SetStatus(codes.Error, err.Error())
	}
}

// AddEvent adds an event to the span
func (tc *TraceableContext) AddEvent(name string, attributes ...attribute.KeyValue) {
	tc.span.AddEvent(name, oteltrace.WithAttributes(attributes...))
}

// End finalizes the span
func (tc *TraceableContext) End() {
	tc.span.End()
}

// Child creates a child span
func (tc *TraceableContext) Child(operationName string) *TraceableContext {
	return NewTraceableContext(tc.ctx, tc.tracer, operationName)
}

// CorrelationID extracts or generates a correlation ID
func (tc *TraceableContext) CorrelationID() string {
	if tc.span.SpanContext().HasTraceID() {
		return tc.span.SpanContext().TraceID().String()
	}
	return "unknown"
}

// SpanID returns the current span ID
func (tc *TraceableContext) SpanID() string {
	if tc.span.SpanContext().HasSpanID() {
		return tc.span.SpanContext().SpanID().String()
	}
	return "unknown"
}

// TraceableDispatcher wraps dispatcher with tracing
type TraceableDispatcher struct {
	dispatcher interface{} // Original dispatcher
	tracer     oteltrace.Tracer
	logger     *logrus.Logger
}

// NewTraceableDispatcher creates a traceable dispatcher wrapper
func NewTraceableDispatcher(dispatcher interface{}, tracer oteltrace.Tracer, logger *logrus.Logger) *TraceableDispatcher {
	return &TraceableDispatcher{
		dispatcher: dispatcher,
		tracer:     tracer,
		logger:     logger,
	}
}

// TraceableLogEntry represents a log entry with tracing information
type TraceableLogEntry struct {
	TraceID    string                 `json:"trace_id"`
	SpanID     string                 `json:"span_id"`
	ParentSpanID string               `json:"parent_span_id,omitempty"`
	Timestamp  time.Time              `json:"timestamp"`
	Message    string                 `json:"message"`
	Level      string                 `json:"level"`
	Source     TraceableSource        `json:"source"`
	Labels     map[string]string      `json:"labels"`
	Fields     map[string]interface{} `json:"fields"`
	Processing ProcessingTrace        `json:"processing"`
}

// TraceableSource represents the source of a log with tracing
type TraceableSource struct {
	Type   string `json:"type"`
	ID     string `json:"id"`
	TraceID string `json:"trace_id,omitempty"`
}

// ProcessingTrace tracks processing steps
type ProcessingTrace struct {
	Steps     []ProcessingStep `json:"steps"`
	StartTime time.Time        `json:"start_time"`
	EndTime   time.Time        `json:"end_time"`
	Duration  time.Duration    `json:"duration"`
}

// ProcessingStep represents a step in log processing
type ProcessingStep struct {
	Name      string        `json:"name"`
	StartTime time.Time     `json:"start_time"`
	EndTime   time.Time     `json:"end_time"`
	Duration  time.Duration `json:"duration"`
	Error     string        `json:"error,omitempty"`
	TraceID   string        `json:"trace_id"`
	SpanID    string        `json:"span_id"`
}

// InstrumentedFunction represents a function wrapped with tracing
type InstrumentedFunction struct {
	tracer oteltrace.Tracer
	name   string
}

// NewInstrumentedFunction creates a new instrumented function
func NewInstrumentedFunction(tracer oteltrace.Tracer, name string) *InstrumentedFunction {
	return &InstrumentedFunction{
		tracer: tracer,
		name:   name,
	}
}

// Execute executes a function with tracing
func (fn *InstrumentedFunction) Execute(ctx context.Context, f func(*TraceableContext) error) error {
	tc := NewTraceableContext(ctx, fn.tracer, fn.name)
	defer tc.End()

	start := time.Now()
	tc.SetAttribute("start_time", start.Format(time.RFC3339))

	err := f(tc)

	duration := time.Since(start)
	tc.SetAttribute("duration_ms", duration.Milliseconds())

	if err != nil {
		tc.SetError(err)
		return err
	}

	tc.span.SetStatus(codes.Ok, "completed")
	return nil
}

// TraceHandler is a middleware for HTTP tracing
func TraceHandler(tracer oteltrace.Tracer, operationName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract context from headers
			ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))

			// Create span
			ctx, span := tracer.Start(ctx, operationName)
			defer span.End()

			// Add request attributes
			span.SetAttributes(
				semconv.HTTPMethod(r.Method),
				semconv.HTTPTarget(r.URL.Path),
				semconv.HTTPScheme(r.URL.Scheme),
				semconv.UserAgentOriginal(r.UserAgent()),
				semconv.ClientAddress(r.RemoteAddr),
			)

			// Inject trace context into response headers
			otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(w.Header()))

			// Call next handler
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ExtractTraceInfo extracts trace information from context
func ExtractTraceInfo(ctx context.Context) (traceID, spanID string) {
	span := oteltrace.SpanFromContext(ctx)
	if span.SpanContext().IsValid() {
		traceID = span.SpanContext().TraceID().String()
		spanID = span.SpanContext().SpanID().String()
	}
	return
}

// InjectTraceToLogEntry injects trace information into log entry
func InjectTraceToLogEntry(ctx context.Context, entry map[string]interface{}) {
	traceID, spanID := ExtractTraceInfo(ctx)
	if traceID != "" {
		entry["trace_id"] = traceID
	}
	if spanID != "" {
		entry["span_id"] = spanID
	}
}