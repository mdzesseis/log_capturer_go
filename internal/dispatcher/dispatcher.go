// Package dispatcher provides the core log entry orchestration and delivery system.
//
// The dispatcher is the central component responsible for:
//   - Receiving log entries from various input sources (file monitors, container monitors)
//   - Processing and transforming log entries through configured pipelines
//   - Routing log entries to appropriate output sinks (Loki, local files, etc.)
//   - Managing delivery reliability with batching, retries, and dead letter queues
//   - Implementing advanced features like deduplication, rate limiting, and backpressure
//
// Key Features:
//   - Multi-worker parallel processing for high throughput
//   - Configurable batching for optimized sink delivery
//   - Retry logic with exponential backoff for transient failures
//   - Dead letter queue for permanently failed entries
//   - Deduplication to prevent duplicate log processing
//   - Adaptive rate limiting and backpressure management
//   - Graceful degradation during high load or sink failures
//   - Comprehensive metrics and monitoring integration
//
// The dispatcher integrates with enterprise features including:
//   - Anomaly detection for unusual log patterns
//   - Distributed tracing for request correlation
//   - Resource monitoring and leak detection
//   - Security audit logging and compliance
//
// Example usage:
//
//	config := DispatcherConfig{
//		QueueSize:    10000,
//		Workers:      4,
//		BatchSize:    100,
//		BatchTimeout: 5*time.Second,
//	}
//	dispatcher := NewDispatcher(config, processor, logger, anomalyDetector)
//	dispatcher.AddSink(lokiSink)
//	dispatcher.Start(ctx)
package dispatcher

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"ssw-logs-capture/internal/metrics"
	"ssw-logs-capture/internal/processing"
	"ssw-logs-capture/pkg/anomaly"
	"ssw-logs-capture/pkg/backpressure"
	"ssw-logs-capture/pkg/deduplication"
	"ssw-logs-capture/pkg/degradation"
	"ssw-logs-capture/pkg/dlq"
	"ssw-logs-capture/pkg/ratelimit"
	"ssw-logs-capture/pkg/tracing"
	"ssw-logs-capture/pkg/types"

	"github.com/sirupsen/logrus"
)

// Dispatcher orchestrates log entry processing and delivery to configured output sinks.
//
// The Dispatcher is the core component that manages the flow of log entries from
// input sources to output destinations. It provides:
//
// Core Functionality:
//   - Multi-worker parallel processing for high throughput
//   - Configurable batching and timeout management
//   - Retry logic with exponential backoff
//   - Comprehensive error handling and recovery
//
// Advanced Features:
//   - Deduplication to prevent duplicate processing
//   - Dead letter queue for permanently failed entries
//   - Backpressure management during high load
//   - Graceful degradation with adaptive behavior
//   - Rate limiting for sink protection
//   - Anomaly detection integration
//
// Performance Optimization:
//   - Lock-free queue implementation for high concurrency
//   - Batch processing to reduce sink API calls
//   - Worker pool for parallel processing
//   - Memory-efficient buffering strategies
//
// Reliability Features:
//   - Persistent position tracking for resumable processing
//   - Health monitoring and automatic recovery
//   - Graceful shutdown with queue draining
//   - Comprehensive metrics and observability
//
// The Dispatcher maintains detailed statistics about processing rates,
// error rates, and performance metrics for monitoring and alerting.
type Dispatcher struct {
	// Core configuration and logging
	config    DispatcherConfig         // Dispatcher configuration parameters
	logger    *logrus.Logger           // Structured logger for dispatcher events
	processor *processing.LogProcessor // Log transformation and filtering pipeline

	// Advanced feature managers (conditionally enabled)
	deduplicationManager *deduplication.DeduplicationManager // Prevents duplicate log processing
	deadLetterQueue      *dlq.DeadLetterQueue                // Handles permanently failed entries
	backpressureManager  *backpressure.Manager               // Manages load-based throttling
	degradationManager   *degradation.Manager                // Implements graceful degradation
	rateLimiter          *ratelimit.AdaptiveRateLimiter      // Adaptive rate limiting for sink protection
	anomalyDetector      *anomaly.AnomalyDetector            // Detects unusual log patterns and anomalies
	enhancedMetrics      *metrics.EnhancedMetrics            // Advanced metrics collection and reporting
	tracingManager       *tracing.EnhancedTracingManager     // Distributed tracing with hybrid mode support

	// PHASE 2 REFACTORING: Modular components for dispatcher functionality
	batchProcessor *BatchProcessor // Handles batch collection and processing
	retryManager   *RetryManager   // Manages retry logic and DLQ integration
	retryManagerV2 *RetryManagerV2 // New retry manager with centralized queue
	statsCollector *StatsCollector // Collects and reports statistics

	// Core operational components
	sinks      []types.Sink          // Collection of configured output destinations
	queue      chan dispatchItem     // Internal queue for log entry processing
	stats      types.DispatcherStats // Real-time performance and operational statistics
	statsMutex sync.RWMutex          // Mutex for thread-safe statistics access

	// Lifecycle management
	ctx       context.Context    // Context for coordinated shutdown and cancellation
	cancel    context.CancelFunc // Cancel function for graceful shutdown
	isRunning bool               // Running state flag for lifecycle management
	mutex     sync.RWMutex       // Mutex for thread-safe state management
	wg        sync.WaitGroup     // WaitGroup for tracking goroutines lifecycle

	// Note: retrySemaphore and maxConcurrentRetries removed - RetryManagerV2 handles this internally
}

// DispatcherConfig contains all configuration parameters for the Dispatcher.
//
// This configuration structure defines:
//   - Core processing parameters (queue size, workers, batching)
//   - Retry and timeout behaviors
//   - Feature enablement flags for advanced capabilities
//   - Component-specific configurations for integrated features
//
// The configuration supports both basic log forwarding and advanced
// enterprise features like deduplication, rate limiting, and anomaly detection.
//
// All duration fields should be specified in Go duration format
// (e.g., "5s", "1m", "24h"). Zero values will be replaced with
// sensible defaults during dispatcher initialization.
type DispatcherConfig struct {
	// Core processing configuration
	QueueSize          int           `yaml:"queue_size"`          // Internal queue capacity for buffering log entries
	Workers            int           `yaml:"workers"`             // Number of worker goroutines for parallel processing
	BatchSize          int           `yaml:"batch_size"`          // Maximum entries per batch sent to sinks
	BatchTimeout       time.Duration `yaml:"batch_timeout"`       // Maximum time to wait before sending partial batch
	MaxRetries         int           `yaml:"max_retries"`         // Maximum retry attempts for failed deliveries
	RetryDelay         time.Duration `yaml:"retry_delay"`         // Base delay between retry attempts (with exponential backoff)
	TimestampTolerance time.Duration `yaml:"timestamp_tolerance"` // Acceptable timestamp drift for log entries

	// Deduplication feature configuration
	DeduplicationEnabled bool                 `yaml:"deduplication_enabled"` // Enable duplicate log entry detection and filtering
	DeduplicationConfig  deduplication.Config `yaml:"deduplication_config"`  // Deduplication algorithm and storage configuration

	// Dead Letter Queue configuration for permanently failed entries
	DLQEnabled bool       `yaml:"dlq_enabled"` // Enable dead letter queue for failed log entries
	DLQConfig  dlq.Config `yaml:"dlq_config"`  // DLQ storage and processing configuration

	// Backpressure management configuration
	BackpressureEnabled bool                `yaml:"backpressure_enabled"` // Enable adaptive backpressure management
	BackpressureConfig  backpressure.Config `yaml:"backpressure_config"`  // Backpressure thresholds and behavior configuration

	// Graceful degradation configuration
	DegradationEnabled bool               `yaml:"degradation_enabled"` // Enable graceful degradation during overload
	DegradationConfig  degradation.Config `yaml:"degradation_config"`  // Degradation levels and behavior configuration

	// Rate limiting configuration for sink protection
	RateLimitEnabled bool             `yaml:"rate_limit_enabled"` // Enable adaptive rate limiting
	RateLimitConfig  ratelimit.Config `yaml:"rate_limit_config"`  // Rate limiting algorithms and thresholds
}

// dispatchItem represents a log entry in the dispatcher's internal processing queue.
//
// Each dispatch item contains:
//   - The original log entry with metadata
//   - Processing timestamp for timeout and aging calculations
//   - Retry counter for failure handling and DLQ decisions
//
// Dispatch items flow through the processing pipeline and are used
// for batching, retry logic, and delivery tracking.
type dispatchItem struct {
	Entry     *types.LogEntry // The log entry to be processed and delivered (pointer to avoid copying mutex)
	Timestamp time.Time       // When this item was queued for processing
	Retries   int             // Number of delivery attempts for this entry
}

// NewDispatcher creates a new Dispatcher instance with the specified configuration and dependencies.
//
// This function performs complete dispatcher initialization including:
//   - Configuration validation and default value assignment
//   - Component initialization based on feature enablement
//   - Internal queue and statistics setup
//   - Integration with processing pipeline and anomaly detection
//
// The function applies sensible defaults for missing configuration values:
//   - QueueSize: 50000 entries
//   - Workers: 4 goroutines
//   - BatchSize: 100 entries
//   - BatchTimeout: 5 seconds
//   - MaxRetries: 3 attempts
//   - RetryDelay: 1 second
//   - TimestampTolerance: 24 hours
//
// Advanced features are conditionally initialized:
//   - Deduplication Manager: Prevents duplicate log processing
//   - Dead Letter Queue: Handles permanently failed entries
//   - Backpressure Manager: Adaptive load management
//   - Degradation Manager: Graceful performance degradation
//   - Rate Limiter: Adaptive rate limiting for sink protection
//
// Parameters:
//   - config: Dispatcher configuration with processing and feature settings
//   - processor: Log transformation and filtering pipeline
//   - logger: Structured logger for dispatcher events
//   - anomalyDetector: Anomaly detection integration (can be nil)
//
// Returns:
//   - *Dispatcher: Fully configured dispatcher ready to start
func NewDispatcher(config DispatcherConfig, processor *processing.LogProcessor, logger *logrus.Logger, enhancedMetrics *metrics.EnhancedMetrics, tracingMgr *tracing.EnhancedTracingManager) *Dispatcher {
	// Valores padrão
	if config.QueueSize == 0 {
		config.QueueSize = 50000
	}
	if config.Workers == 0 {
		config.Workers = 4
	}
	if config.BatchSize == 0 {
		config.BatchSize = 100
	}
	if config.BatchTimeout == 0 {
		config.BatchTimeout = 5 * time.Second
	}
	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}
	if config.RetryDelay == 0 {
		config.RetryDelay = 1 * time.Second
	}
	if config.TimestampTolerance == 0 {
		config.TimestampTolerance = 24 * time.Hour
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Configurar deduplication manager se habilitado
	var deduplicationManager *deduplication.DeduplicationManager
	if config.DeduplicationEnabled {
		deduplicationManager = deduplication.NewDeduplicationManager(config.DeduplicationConfig, logger)
	}

	// Configurar Dead Letter Queue se habilitado
	var deadLetterQueue *dlq.DeadLetterQueue
	if config.DLQEnabled {
		deadLetterQueue = dlq.NewDeadLetterQueue(config.DLQConfig, logger)
	}

	// Configurar Backpressure Manager se habilitado
	var backpressureManager *backpressure.Manager
	if config.BackpressureEnabled {
		backpressureManager = backpressure.NewManager(config.BackpressureConfig, logger)
	}

	// Configurar Degradation Manager se habilitado
	var degradationManager *degradation.Manager
	if config.DegradationEnabled {
		degradationManager = degradation.NewManager(config.DegradationConfig, logger)
	}

	// Configurar Rate Limiter se habilitado

	var rateLimiter *ratelimit.AdaptiveRateLimiter

	if config.RateLimitEnabled {

		rateLimiter = ratelimit.NewAdaptiveRateLimiter(config.RateLimitConfig, logger)

	}

	// Create queue
	queue := make(chan dispatchItem, config.QueueSize)

	// Create dispatcher with zero-value mutex and WaitGroup (avoid copying)
	d := &Dispatcher{
		config:               config,
		logger:               logger,
		processor:            processor,
		deduplicationManager: deduplicationManager,
		deadLetterQueue:      deadLetterQueue,
		backpressureManager:  backpressureManager,
		degradationManager:   degradationManager,
		rateLimiter:          rateLimiter,
		// anomalyDetector:      anomalyDetector, // Temporarily disabled
		enhancedMetrics: enhancedMetrics,
		tracingManager:  tracingMgr,

		sinks:  make([]types.Sink, 0),
		queue:  queue,
		ctx:    ctx,
		cancel: cancel,
		// statsMutex and wg use zero values - no copying
	}

	// Initialize stats structure (after struct creation to avoid copying mutex)
	d.stats = types.DispatcherStats{
		SinkDistribution: make(map[string]int64),
		QueueSize:        0, // Will be updated by RunStatsUpdater
		QueueCapacity:    config.QueueSize,
	}

	// PHASE 2 REFACTORING: Initialize modular components
	batchProcessor := NewBatchProcessor(config, logger, enhancedMetrics)
	// Note: RetryManager is kept for stats collection compatibility, but retry logic uses RetryManagerV2
	maxConcurrentRetries := config.Workers * 25
	if maxConcurrentRetries < 100 {
		maxConcurrentRetries = 100
	}
	retryManager := NewRetryManager(config, logger, deadLetterQueue, ctx, &d.wg, maxConcurrentRetries)
	statsCollector := NewStatsCollector(&d.stats, &d.statsMutex, config, logger, queue)

	d.batchProcessor = batchProcessor
	d.retryManager = retryManager
	d.statsCollector = statsCollector

	// Initialize RetryManagerV2 with centralized queue approach
	d.retryManagerV2 = NewRetryManagerV2(config, logger, deadLetterQueue)

	return d
}

// AddSink adds an output sink to the dispatcher's delivery destinations.
//
// This method registers a new sink that will receive processed log entries
// from the dispatcher. Sinks implement the types.Sink interface and can
// include:
//   - Loki sinks for Grafana Loki integration
//   - Local file sinks for filesystem output
//   - Custom sinks for specialized integrations
//
// The sink is added to the internal collection and will receive log entries
// through the batching and delivery mechanism. All configured sinks receive
// the same log entries (fan-out delivery pattern).
//
// This method is thread-safe and can be called during dispatcher operation,
// though it's typically called during application initialization.
//
// Parameters:
//   - sink: Output sink implementing the types.Sink interface
func (d *Dispatcher) AddSink(sink types.Sink) {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	d.sinks = append(d.sinks, sink)
	d.logger.WithField("sink_count", len(d.sinks)).Info("Sink added to dispatcher")
}

// Start begins the dispatcher operation and initializes all worker goroutines.
//
// This method performs the complete dispatcher startup sequence:
//  1. Validates that the dispatcher is not already running
//  2. Starts advanced feature managers (deduplication, DLQ, backpressure)
//  3. Configures component integration and callbacks
//  4. Launches worker goroutines for parallel processing
//  5. Starts statistics collection and monitoring
//
// Advanced Feature Startup:
//   - Deduplication Manager: Initializes duplicate detection algorithms
//   - Dead Letter Queue: Sets up failed entry storage and reprocessing
//   - Backpressure Manager: Begins load monitoring and throttling
//   - Rate Limiter: Activates adaptive rate limiting algorithms
//
// Worker Pool:
//   - Creates configured number of worker goroutines
//   - Each worker processes batches of log entries
//   - Workers handle retry logic and error management
//   - Statistics updater tracks performance metrics
//
// The dispatcher integrates with the provided context for coordinated
// shutdown and cancellation across the application.
//
// Parameters:
//   - ctx: Application context for shutdown coordination
//
// Returns:
//   - error: Startup failure from any component initialization
func (d *Dispatcher) Start(ctx context.Context) error {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	if d.isRunning {
		return fmt.Errorf("dispatcher already running")
	}

	d.isRunning = true
	d.logger.WithFields(logrus.Fields{
		"workers":               d.config.Workers,
		"queue_size":            d.config.QueueSize,
		"batch_size":            d.config.BatchSize,
		"deduplication_enabled": d.config.DeduplicationEnabled,
		"dlq_enabled":           d.config.DLQEnabled,
	}).Info("Starting dispatcher")

	// Iniciar deduplication manager se habilitado
	if d.config.DeduplicationEnabled && d.deduplicationManager != nil {
		if err := d.deduplicationManager.Start(); err != nil {
			return fmt.Errorf("failed to start deduplication manager: %w", err)
		}
	}

	// Iniciar Dead Letter Queue se habilitado
	if d.config.DLQEnabled && d.deadLetterQueue != nil {
		if err := d.deadLetterQueue.Start(); err != nil {
			return fmt.Errorf("failed to start dead letter queue: %w", err)
		}
		// Configurar reprocessamento da DLQ
		d.setupDLQReprocessing()
	}

	// Iniciar Backpressure Manager se habilitado
	if d.config.BackpressureEnabled && d.backpressureManager != nil {
		// Conectar degradation manager ao backpressure
		if d.config.DegradationEnabled && d.degradationManager != nil {
			d.backpressureManager.SetLevelChangeCallback(func(oldLevel, newLevel backpressure.Level, factor float64) {
				d.degradationManager.UpdateLevel(newLevel)
			})
		}

		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			if err := d.backpressureManager.Start(d.ctx); err != nil {
				d.logger.WithError(err).Error("Backpressure manager stopped with error")
			}
		}()
	}

	// Iniciar Rate Limiter se habilitado
	if d.config.RateLimitEnabled && d.rateLimiter != nil {
		d.logger.Info("Rate limiter enabled")
	}

	// Start RetryManagerV2 and connect to dispatcher queue
	if d.retryManagerV2 != nil {
		d.retryManagerV2.SetOutputQueue(d.queue)
		d.retryManagerV2.Start()
	}

	// Iniciar workers
	for i := 0; i < d.config.Workers; i++ {
		d.wg.Add(1)
		go func(workerID int) {
			defer d.wg.Done()
			d.worker(workerID)
		}(i)
	}

	// Iniciar stats updater
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		d.statsUpdater()
	}()

	return nil
}

// Stop performs graceful shutdown of the dispatcher and all its components.
//
// This method orchestrates the complete shutdown sequence:
//  1. Validates that the dispatcher is currently running
//  2. Stops advanced feature managers (deduplication, DLQ)
//  3. Cancels the dispatcher context to signal worker goroutines
//  4. Drains remaining log entries from the internal queue
//  5. Ensures all in-flight processing completes
//
// Shutdown Sequence:
//   - Deduplication Manager: Stops duplicate detection and flushes state
//   - Dead Letter Queue: Stops DLQ processing and persists failed entries
//   - Worker Goroutines: Gracefully terminate after processing current batches
//   - Queue Draining: Processes remaining entries to prevent data loss
//
// The method ensures that:
//   - No new log entries are accepted during shutdown
//   - Existing entries in the queue are processed or persisted
//   - All background goroutines terminate cleanly
//   - Component state is properly saved for restart
//
// This method is thread-safe and can be called multiple times without error.
// Subsequent calls after the first will return immediately without effect.
//
// Returns:
//   - error: Always returns nil; errors are logged but don't prevent shutdown
func (d *Dispatcher) Stop() error {
	d.mutex.Lock()
	if !d.isRunning {
		d.mutex.Unlock()
		return nil
	}

	d.logger.Info("Stopping dispatcher")
	d.isRunning = false
	d.mutex.Unlock() // Unlock early to allow goroutines to check state

	// Parar deduplication manager se habilitado
	if d.config.DeduplicationEnabled && d.deduplicationManager != nil {
		if err := d.deduplicationManager.Stop(); err != nil {
			d.logger.WithError(err).Warn("Failed to stop deduplication manager")
		}
	}

	// Parar Dead Letter Queue se habilitado
	if d.config.DLQEnabled && d.deadLetterQueue != nil {
		if err := d.deadLetterQueue.Stop(); err != nil {
			d.logger.WithError(err).Warn("Failed to stop dead letter queue")
		}
	}

	// Stop RetryManagerV2
	if d.retryManagerV2 != nil {
		d.retryManagerV2.Stop()
	}

	// Cancelar contexto para sinalizar todas as goroutines
	d.cancel()

	// Processar itens restantes na fila
	d.drainQueue()

	// Aguardar todas as goroutines terminarem com timeout
	done := make(chan struct{})
	go func() {
		d.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		d.logger.Info("All dispatcher goroutines stopped cleanly")
	case <-time.After(10 * time.Second):
		d.logger.Warn("Timeout waiting for dispatcher goroutines to stop")
	}

	return nil
}

// Handle processes a single log entry through the dispatcher pipeline.
//
// This is the main entry point for log entries into the dispatcher system.
// The method performs the complete processing workflow:
//  1. Validates dispatcher operational state
//  2. Applies rate limiting if enabled
//  3. Creates a structured log entry with metadata
//  4. Processes the entry through transformation pipelines
//  5. Applies deduplication if enabled
//  6. Queues the entry for batch delivery to sinks
//
// Processing Features:
//   - Rate Limiting: Protects against log floods and sink overload
//   - Transformation: Applies configured processing pipelines
//   - Deduplication: Prevents duplicate entries from being processed
//   - Anomaly Detection: Integrates with anomaly detection systems
//   - Backpressure: Handles queue overflow with adaptive behavior
//
// Error Handling:
//   - Rate limit exceeded: Returns error and updates throttling metrics
//   - Queue full: Applies backpressure or degradation strategies
//   - Processing errors: Logs errors and optionally sends to DLQ
//   - Context cancellation: Respects timeout and cancellation signals
//
// The method updates dispatcher statistics including entry counts,
// processing rates, and error rates for monitoring and alerting.
//
// Parameters:
//   - ctx: Processing context for timeout and cancellation
//   - sourceType: Type of log source ("file", "container", etc.)
//   - sourceID: Unique identifier for the log source
//   - message: Raw log message content
//   - labels: Additional metadata and labels for the log entry
//
// Returns:
//   - error: Processing error including rate limiting, queue full, or validation errors
func (d *Dispatcher) Handle(ctx context.Context, sourceType, sourceID, message string, labels map[string]string) error {
	// Create trace span for this log entry if tracing is enabled
	// The TracingManager will decide based on mode and sampling whether to actually trace
	if d.tracingManager != nil {
		entry := &types.LogEntry{
			Timestamp:  time.Now().UTC(),
			Message:    message,
			SourceType: sourceType,
			SourceID:   sourceID,
			Labels:     types.NewLabelsCOWFromMap(labels),
		}

		newCtx, span := d.tracingManager.CreateLogSpan(ctx, entry)
		if span != nil {
			ctx = newCtx
			defer span.End()
			// Note: trace_id and span_id are automatically added to entry.Labels by CreateLogSpan
		}
	}

	if !d.isRunning {
		return fmt.Errorf("dispatcher not running")
	}

	// Aplicar rate limiting se habilitado
	if d.config.RateLimitEnabled && d.rateLimiter != nil {
		if !d.rateLimiter.Allow() {
			atomic.AddInt64(&d.stats.Throttled, 1)
			return fmt.Errorf("rate limit exceeded")
		}
	}

	// Aplicar rate limiting se habilitado
	if d.rateLimiter != nil && !d.rateLimiter.Allow() {
		atomic.AddInt64(&d.stats.Throttled, 1)
		metrics.RecordError("dispatcher", "rate_limit_exceeded")
		return fmt.Errorf("rate limit exceeded")
	}

	// Verificar backpressure e aplicar controle de fluxo
	if d.config.BackpressureEnabled && d.backpressureManager != nil {
		// Atualizar métricas do sistema para backpressure
		d.updateBackpressureMetrics()

		// Verificar se deve rejeitar por sobrecarga crítica
		if d.backpressureManager.ShouldReject() {
			atomic.AddInt64(&d.stats.Throttled, 1)
			return fmt.Errorf("system overloaded - rejecting log entry")
		}

		// Verificar se deve aplicar throttling
		if d.backpressureManager.ShouldThrottle() {
			factor := d.backpressureManager.GetFactor()
			// Aplicar throttling probabilístico baseado no fator
			if float64(time.Now().UnixNano()%1000)/1000.0 > factor {
				atomic.AddInt64(&d.stats.Throttled, 1)

				// Em vez de descartar, colocar numa fila de baixa prioridade
				// Para manter integridade dos dados
				return d.handleLowPriorityEntry(ctx, sourceType, sourceID, message, labels)
			}
		}
	}

	// Criar entrada de log com cópia segura dos labels usando LabelsCOW
	entry := types.LogEntry{
		Timestamp:   time.Now().UTC(),
		Message:     message,
		SourceType:  sourceType,
		SourceID:    sourceID,
		Labels:      types.NewLabelsCOWFromMap(labels), // COW automatically copies the map
		ProcessedAt: time.Now(),
	}

	// Verificar duplicação se habilitado (respeitando degradação)
	if d.config.DeduplicationEnabled && d.deduplicationManager != nil {
		// Verificar se deduplicação está degradada
		skipDeduplication := false
		if d.config.DegradationEnabled && d.degradationManager != nil {
			skipDeduplication = !d.degradationManager.IsFeatureEnabled(degradation.FeatureDeduplication)
		}

		if !skipDeduplication {
			if d.deduplicationManager.IsDuplicate(sourceID, message, entry.Timestamp) {
				// Log duplicado detectado - incrementar estatística e retornar
				atomic.AddInt64(&d.stats.DuplicatesDetected, 1)

				metrics.RecordLogProcessed(sourceType, sourceID, "duplicate_filtered")
				return nil // Não processar log duplicado
			}
		}
	}

	// Validar timestamp (detectar timestamps muito antigos)
	now := time.Now()
	if entry.Timestamp.Before(now.Add(-d.config.TimestampTolerance)) {
		d.logger.WithFields(logrus.Fields{
			"trace_id":           entry.TraceID,
			"source_type":        sourceType,
			"source_id":          sourceID,
			"original_timestamp": entry.Timestamp,
			"drift_seconds":      now.Sub(entry.Timestamp).Seconds(),
		}).Warn("Timestamp muito antigo; ajustando para agora")

		entry.Timestamp = now
		d.updateTimestampWarnings()
	}

	// Processar entrada (respeitando degradação)
	if d.processor != nil {
		// Verificar se processamento está degradado
		skipProcessing := false
		if d.config.DegradationEnabled && d.degradationManager != nil {
			skipProcessing = !d.degradationManager.IsFeatureEnabled(degradation.FeatureProcessing)
		}

		if !skipProcessing {
			processedEntry, err := d.processor.Process(ctx, &entry)
			if err != nil {
				d.logger.WithError(err).WithFields(logrus.Fields{
					"trace_id":    entry.TraceID,
					"source_type": sourceType,
					"source_id":   sourceID,
				}).Error("Failed to process log entry")
				metrics.RecordError("dispatcher", "processing_error")
				return err
			}
			if processedEntry != nil {
				// Use DeepCopy to avoid copying mutex from processedEntry
				entry = *processedEntry.DeepCopy()
			}
		}
	}

	// Calculate current queue utilization for backpressure
	queueDepth := len(d.queue)
	queueSize := cap(d.queue)
	utilization := float64(queueDepth) / float64(queueSize) * 100

	// Update metrics
	metrics.DispatcherQueueDepth.Set(float64(queueDepth))
	metrics.DispatcherQueueUtilization.Set(utilization)

	// Apply backpressure at 95% threshold
	if utilization >= 95.0 {
		d.logger.Warn("dispatcher queue at critical capacity - applying backpressure",
			"queue_depth", queueDepth,
			"queue_size", queueSize,
			"utilization_percent", utilization,
		)
		metrics.RecordError("dispatcher", "queue_backpressure")
		return fmt.Errorf("dispatcher queue near full: %.1f%% utilization", utilization)
	}

	// C5: Race Condition Fix - Use deep copy to avoid sharing mutex
	// Deep copy ensures the queued item has independent maps and fresh mutex
	item := dispatchItem{
		Entry:     entry.DeepCopy(),
		Timestamp: time.Now().UTC(),
		Retries:   0,
	}

	select {
	case d.queue <- item:
		d.updateStats(func(stats *types.DispatcherStats) {
			stats.TotalProcessed++
			stats.QueueSize = len(d.queue)
			stats.LastProcessedTime = time.Now()
		})
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		metrics.RecordError("dispatcher", "queue_full")
		d.updateStats(func(stats *types.DispatcherStats) {
			stats.ErrorCount++
		})
		return fmt.Errorf("dispatcher queue full")
	}
}

// GetStats retorna estatísticas do dispatcher
// PHASE 2 REFACTORING: Delegates to StatsCollector for thread-safe stats access
func (d *Dispatcher) GetStats() types.DispatcherStats {
	return d.statsCollector.GetStats()
}

// GetRetryQueueStats returns statistics about the retry queue
// PHASE 2 REFACTORING: Delegates to RetryManager
// This helps monitor goroutine leak prevention - shows how many retry slots are in use
func (d *Dispatcher) GetRetryQueueStats() map[string]interface{} {
	return d.retryManager.GetRetryStats()
}

// sendToDLQ envia entrada para Dead Letter Queue
func (d *Dispatcher) sendToDLQ(entry *types.LogEntry, errorMsg, errorType, failedSink string, retryCount int) {
	if d.config.DLQEnabled && d.deadLetterQueue != nil {
		context := map[string]string{
			"worker_id": "dispatcher",
			"timestamp": time.Now().Format(time.RFC3339),
		}

		if err := d.deadLetterQueue.AddEntry(entry, errorMsg, errorType, failedSink, retryCount, context); err != nil {
			d.logger.WithFields(logrus.Fields{
				"trace_id":    entry.TraceID,
				"source_type": entry.SourceType,
				"source_id":   entry.SourceID,
				"failed_sink": failedSink,
				"error_type":  errorType,
				"error":       errorMsg,
				"retry_count": retryCount,
				"dlq_error":   err.Error(),
			}).Error("Failed to send entry to DLQ")
			return
		}

		d.logger.WithFields(logrus.Fields{
			"trace_id":    entry.TraceID,
			"source_type": entry.SourceType,
			"source_id":   entry.SourceID,
			"failed_sink": failedSink,
			"error_type":  errorType,
			"error":       errorMsg,
			"retry_count": retryCount,
		}).Debug("Entry sent to DLQ")
	}
}

// worker processa itens da fila
// PHASE 2 REFACTORING: Simplified worker using BatchProcessor for batch collection
func (d *Dispatcher) worker(workerID int) {
	logger := d.logger.WithField("worker_id", workerID)
	logger.Info("Dispatcher worker started")
	defer logger.Info("Dispatcher worker stopped")

	for {
		select {
		case <-d.ctx.Done():
			return
		default:
			// Use BatchProcessor to collect batch from queue
			batch, timedOut := d.batchProcessor.CollectBatch(d.ctx, d.queue)

			if len(batch) > 0 {
				// Process batch using new modular components
				d.processBatchWrapper(batch, logger)
			}

			// Log timeout for debugging if needed
			if timedOut && len(batch) > 0 {
				logger.WithFields(logrus.Fields{
					"batch_size": len(batch),
					"timeout":    d.config.BatchTimeout,
				}).Debug("Batch processed due to timeout")
			}
		}
	}
}

// PHASE 2 REFACTORING: processBatchWrapper uses modular components for batch processing
// This is the new simplified interface that delegates to BatchProcessor and RetryManager
func (d *Dispatcher) processBatchWrapper(batch []dispatchItem, logger *logrus.Entry) {
	if len(batch) == 0 {
		return
	}

	// Use BatchProcessor to send batch to all sinks
	successCount, healthySinks, lastErr := d.batchProcessor.ProcessBatch(
		d.ctx,
		batch,
		d.sinks,
		d.anomalyDetector,
	)

	// Update statistics using StatsCollector
	for range batch {
		d.statsCollector.IncrementProcessed()
		// Update sink distribution
		for _, sink := range d.sinks {
			if sink.IsHealthy() {
				sinkType := d.getSinkType(sink)
				d.statsCollector.UpdateSinkDistribution(sinkType, 1)
			}
		}
	}

	// Handle failures with RetryManager
	if healthySinks > 0 && successCount == 0 {
		// Circuit breaker - all sinks failed, send directly to DLQ
		d.retryManager.HandleCircuitBreaker(batch, lastErr)
	} else if successCount < healthySinks && healthySinks > 0 {
		// Some sinks failed - retry with exponential backoff
		d.retryManager.HandleFailedBatch(batch, lastErr, d.queue)
	}
}

// processBatch processa um batch de itens
// PHASE 2 NOTE: This is the original implementation kept for compatibility
// New code should use processBatchWrapper() instead
func (d *Dispatcher) processBatch(batch []dispatchItem, logger *logrus.Entry) {
	if len(batch) == 0 {
		return
	}

	// Create system-level trace span for batch processing
	// This is always traced (regardless of log sampling mode) except in ModeOff
	ctx := context.Background()
	if d.tracingManager != nil {
		newCtx, span := d.tracingManager.CreateSystemSpan(ctx, "dispatcher.process_batch")
		if span != nil {
			ctx = newCtx
			defer span.End()
			// Add batch metadata to span (this will use otel attributes internally)
			logger = logger.WithFields(logrus.Fields{
				"batch.size":     len(batch),
				"queue.depth":    len(d.queue),
				"queue.capacity": d.config.QueueSize,
			})
		}
	}

	startTime := time.Now()

	// C5: Race Condition Fix - Use deep copy to avoid sharing mutexes
	// Each entry must have its own mutex and independent maps
	entries := make([]types.LogEntry, len(batch))
	for i, item := range batch {
		entries[i] = *item.Entry.DeepCopy() // Deep copy creates fresh mutex
	}

	// Detectar anomalias se o detector estiver habilitado
	// Re-enabled with timeout protection and error recovery
	// Process anomaly detection for limited samples to avoid goroutine explosion
	if d.anomalyDetector != nil {
		// Sample only a few entries per batch to avoid overwhelming the system
		maxAnomaliesToCheck := 5
		entriesProcessed := 0

		for i := range entries {
			if entriesProcessed >= maxAnomaliesToCheck {
				break
			}

			// Use a function with defer-recover to catch panics
			func(entry *types.LogEntry) {
				defer func() {
					if r := recover(); r != nil {
						logger.WithFields(logrus.Fields{
							"panic":     r,
							"source_id": entry.SourceID,
						}).Error("Panic in anomaly detection")
					}
				}()

				// Call DetectAnomaly without goroutine to avoid explosion
				result, err := d.anomalyDetector.DetectAnomaly(entry)
				if err != nil {
					// Log at debug level to reduce noise
					logger.WithError(err).WithField("source_id", entry.SourceID).Debug("Anomaly detection failed")
					return
				}

				if result != nil && result.IsAnomaly {
					// Thread-safe label updates
					entry.SetLabel("anomaly", "true")
					entry.SetLabel("anomaly_reason", result.Reason)
					entry.SetLabel("anomaly_score", fmt.Sprintf("%.2f", result.AnomalyScore))

					logger.WithFields(logrus.Fields{
						"source_id":     entry.SourceID,
						"anomaly_score": result.AnomalyScore,
						"severity":      result.Severity,
					}).Info("Anomaly detected in log entry")
				}
			}(&entries[i])

			entriesProcessed++
		}
	}

	// Enviar para todos os sinks
	successCount := 0
	healthySinks := 0
	var lastErr error

	for _, sink := range d.sinks {
		if !sink.IsHealthy() {
			logger.Warn("Skipping unhealthy sink")
			continue
		}

		healthySinks++

		// Fazer cópia profunda dos entries para evitar race conditions entre sinks
		entriesCopy := make([]types.LogEntry, len(entries))
		for i := range entries {
			entriesCopy[i] = *entries[i].DeepCopy()
		}

		ctx, cancel := context.WithTimeout(d.ctx, 120*time.Second)
		err := sink.Send(ctx, entriesCopy)
		cancel()

		if err != nil {
			logger.WithError(err).Error("Failed to send batch to sink")
			d.updateStats(func(stats *types.DispatcherStats) {
				stats.ErrorCount++
			})
			lastErr = err
		} else {
			successCount++
			// Atualizar distribuição por sink
			sinkType := d.getSinkType(sink)
			d.updateStats(func(stats *types.DispatcherStats) {
				stats.SinkDistribution[sinkType] += int64(len(entries))
			})
		}
	}

	// Circuit Breaker: If ALL sinks failed, send directly to DLQ instead of retrying
	// This prevents goroutine explosion when all sinks are down
	if healthySinks > 0 && successCount == 0 {
		logger.WithFields(logrus.Fields{
			"batch_size":    len(batch),
			"healthy_sinks": healthySinks,
			"success_count": 0,
		}).Warn("All healthy sinks failed - sending batch to DLQ without retry to prevent goroutine leak")

		// Send all items directly to DLQ
		for _, item := range batch {
			d.sendToDLQ(item.Entry, lastErr.Error(), "all_sinks_failed", "all_sinks", item.Retries)
		}
	} else if successCount < healthySinks && healthySinks > 0 {
		// Some sinks failed, some succeeded - handle normally with retry
		d.handleFailedBatch(batch, lastErr)
	}

	duration := time.Since(startTime)
	metrics.RecordProcessingDuration("dispatcher", "batch_processing", duration)

	// Record enhanced metrics for batching statistics
	if d.enhancedMetrics != nil {
		// Batch size
		d.enhancedMetrics.RecordBatchingStats("dispatcher", "batch_size", float64(len(batch)))

		// Batch fill rate (percentage of max batch size)
		fillRate := (float64(len(batch)) / float64(d.config.BatchSize)) * 100.0
		d.enhancedMetrics.RecordBatchingStats("dispatcher", "batch_fill_rate", fillRate)

		// Flush time in milliseconds
		d.enhancedMetrics.RecordBatchingStats("dispatcher", "flush_time", float64(duration.Milliseconds()))

		// Note: batches_per_second requires tracking across multiple batches
		// This is already tracked by the metrics collector loop
	}

	logger.WithFields(logrus.Fields{
		"batch_size":    len(batch),
		"success_count": successCount,
		"duration_ms":   duration.Milliseconds(),
	}).Debug("Batch processed")
}

// handleFailedBatch trata batch que falhou
func (d *Dispatcher) handleFailedBatch(batch []dispatchItem, err error) {
	// Use RetryManagerV2 if available (centralized queue approach)
	if d.retryManagerV2 != nil {
		// Prepare items for retry with incremented retry count
		retryItems := make([]dispatchItem, 0, len(batch))
		for _, item := range batch {
			if item.Retries < d.config.MaxRetries {
				item.Retries++
				retryItems = append(retryItems, item)
			} else {
				// Max retries reached, send to DLQ
				d.logger.WithFields(logrus.Fields{
					"trace_id":    item.Entry.TraceID,
					"source_type": item.Entry.SourceType,
					"source_id":   item.Entry.SourceID,
					"retries":     item.Retries,
					"error":       err,
				}).Error("Max retries reached for log entry, sending to DLQ")
				d.sendToDLQ(item.Entry, err.Error(), "max_retries_exceeded", "all_sinks", item.Retries)
			}
		}
		if len(retryItems) > 0 {
			d.retryManagerV2.ScheduleRetryBatch(retryItems)
		}
		return
	}

	// Note: Old fallback retry logic (goroutine-based) has been removed.
	// RetryManagerV2 should always be available. If for some reason it's not,
	// send items directly to DLQ to prevent data loss.
	d.logger.Error("RetryManagerV2 not available - sending failed batch to DLQ")
	for _, item := range batch {
		d.sendToDLQ(item.Entry, err.Error(), "retry_manager_unavailable", "all_sinks", item.Retries)
	}
}

// getSinkType determina o tipo de um sink
func (d *Dispatcher) getSinkType(sink types.Sink) string {
	// Usar reflection ou type assertion para determinar o tipo
	switch sink.(type) {
	default:
		return "unknown"
	}
}

// drainQueue processa itens restantes na fila
func (d *Dispatcher) drainQueue() {
	logger := d.logger.WithField("operation", "drain_queue")
	count := 0

	for {
		select {
		case item := <-d.queue:
			// Processar item individual
			d.processBatch([]dispatchItem{item}, logger)
			count++
		default:
			// Fila vazia
			if count > 0 {
				logger.WithField("drained_items", count).Info("Queue drained")
			}
			return
		}
	}
}

// statsUpdater atualiza estatísticas periodicamente
// PHASE 2 REFACTORING: Delegates to StatsCollector.RunStatsUpdater
func (d *Dispatcher) statsUpdater() {
	// Delegate to StatsCollector's RunStatsUpdater
	// Pass a callback to get retry stats from RetryManager
	d.statsCollector.RunStatsUpdater(d.ctx, d.retryManager.GetRetryStats)
}

// updateStats atualiza estatísticas de forma thread-safe
// PHASE 2 REFACTORING: Delegates to StatsCollector
func (d *Dispatcher) updateStats(fn func(*types.DispatcherStats)) {
	d.statsCollector.UpdateStats(fn)
}

// updateTimestampWarnings incrementa contador de warnings de timestamp
func (d *Dispatcher) updateTimestampWarnings() {
	// Implementar contador thread-safe se necessário
	metrics.RecordError("dispatcher", "timestamp_drift")
}

// updateBackpressureMetrics atualiza métricas para o sistema de backpressure
// PHASE 2 REFACTORING: Delegates to StatsCollector
func (d *Dispatcher) updateBackpressureMetrics() {
	if d.backpressureManager == nil {
		return
	}
	d.statsCollector.UpdateBackpressureMetrics(d.backpressureManager)
}

// handleLowPriorityEntry processa uma entrada de baixa prioridade sem descartar
func (d *Dispatcher) handleLowPriorityEntry(ctx context.Context, sourceType, sourceID, message string, labels map[string]string) error {
	// Em vez de descartar, envia para Dead Letter Queue se disponível
	if d.config.DLQEnabled && d.deadLetterQueue != nil {
		// C1: Race Condition Fix - Create safe copy of labels map
		labelsCopy := make(map[string]string, len(labels))
		for k, v := range labels {
			labelsCopy[k] = v
		}

		entry := types.LogEntry{
			Timestamp:   time.Now().UTC(),
			Message:     message,
			SourceType:  sourceType,
			SourceID:    sourceID,
			Labels:      types.NewLabelsCOWFromMap(labelsCopy),
			ProcessedAt: time.Now(),
		}

		// Adicionar tag indicando que foi throttled (thread-safe)
		entry.SetLabel("throttle_reason", "backpressure_low_priority")

		if err := d.deadLetterQueue.AddEntry(&entry, "throttled due to backpressure", "backpressure", "dispatcher", 0, map[string]string{
			"throttle_level": d.backpressureManager.GetLevel().String(),
		}); err != nil {
			d.logger.WithError(err).Warn("Failed to add throttled entry to DLQ")
			return err
		}
		return nil
	}

	// Se não tiver DLQ, logar warning mas não descartar
	d.logger.WithFields(logrus.Fields{
		"source_type": sourceType,
		"source_id":   sourceID,
		"level":       d.backpressureManager.GetLevel().String(),
	}).Warn("Log entry throttled due to backpressure, but no DLQ available")

	// Em vez de recursão infinita, implementar delay e falha controlada
	select {
	case <-time.After(10 * time.Millisecond):
		// Retry apenas uma vez sem backpressure check para evitar recursão
		return d.handleWithoutBackpressure(ctx, sourceType, sourceID, message, labels)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// handleWithoutBackpressure processa uma entrada sem verificar backpressure (usado internamente)
func (d *Dispatcher) handleWithoutBackpressure(ctx context.Context, sourceType, sourceID, message string, labels map[string]string) error {
	if !d.isRunning {
		return fmt.Errorf("dispatcher not running")
	}

	// Criar entrada de log com cópia segura dos labels usando LabelsCOW
	entry := types.LogEntry{
		Timestamp:   time.Now().UTC(),
		Message:     message,
		SourceType:  sourceType,
		SourceID:    sourceID,
		Labels:      types.NewLabelsCOWFromMap(labels), // COW automatically copies the map
		ProcessedAt: time.Now(),
	}

	// Processar entrada
	if d.processor != nil {
		processedEntry, err := d.processor.Process(ctx, &entry)
		if err != nil {
			d.logger.WithError(err).WithFields(logrus.Fields{
				"trace_id":    entry.TraceID,
				"source_type": sourceType,
				"source_id":   sourceID,
			}).Error("Failed to process log entry in low priority path")
			return err
		}
		if processedEntry != nil {
			// Use DeepCopy to avoid copying mutex from processedEntry
			entry = *processedEntry.DeepCopy()
		}
	}

	// C5: Race Condition Fix - Use deep copy to avoid sharing mutex
	// Deep copy ensures the queued item has independent maps and fresh mutex
	item := dispatchItem{
		Entry:     entry.DeepCopy(),
		Timestamp: time.Now().UTC(),
		Retries:   0,
	}

	select {
	case d.queue <- item:
		d.updateStats(func(stats *types.DispatcherStats) {
			stats.TotalProcessed++
			stats.QueueSize = len(d.queue)
			stats.LastProcessedTime = time.Now()
		})
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(1 * time.Second):
		// Se não conseguir adicionar à fila em 1 segundo, descartar
		d.updateStats(func(stats *types.DispatcherStats) {
			stats.ErrorCount++
		})
		return fmt.Errorf("timeout adding to dispatcher queue")
	}
}

// GetDLQ retorna a instância da Dead Letter Queue
func (d *Dispatcher) GetDLQ() *dlq.DeadLetterQueue {
	return d.deadLetterQueue
}

// SetAnomalyDetector sets the anomaly detector for the dispatcher
func (d *Dispatcher) SetAnomalyDetector(detector *anomaly.AnomalyDetector) {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	d.anomalyDetector = detector
}

// setupDLQReprocessing configura o callback de reprocessamento da DLQ
func (d *Dispatcher) setupDLQReprocessing() {
	if d.config.DLQEnabled && d.deadLetterQueue != nil {
		// Definir callback para reprocessamento
		d.deadLetterQueue.SetReprocessCallback(d.reprocessLogEntry)
		d.logger.Info("DLQ reprocessing callback configured")
	}
}

// reprocessLogEntry tenta reprocessar uma entrada da DLQ
func (d *Dispatcher) reprocessLogEntry(entry *types.LogEntry, originalSink string) error {
	d.logger.WithFields(logrus.Fields{
		"source_type":   entry.SourceType,
		"source_id":     entry.SourceID,
		"original_sink": originalSink,
		"timestamp":     entry.Timestamp,
	}).Debug("Attempting to reprocess DLQ entry")

	// Criar contexto com timeout para reprocessamento
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Tentar encontrar o sink específico que falhou
	targetSink := d.findSinkByName(originalSink)
	if targetSink == nil {
		// Se sink específico não encontrado, tentar todos os sinks disponíveis
		return d.reprocessToAnySink(ctx, entry)
	}

	// Verificar se o sink está healthy
	if !targetSink.IsHealthy() {
		return fmt.Errorf("target sink %s is not healthy", originalSink)
	}

	// Tentar enviar para o sink original
	// Use DeepCopy to avoid copying mutex
	if err := targetSink.Send(ctx, []types.LogEntry{*entry.DeepCopy()}); err != nil {
		d.logger.WithError(err).WithFields(logrus.Fields{
			"source_type":   entry.SourceType,
			"source_id":     entry.SourceID,
			"original_sink": originalSink,
		}).Warn("Failed to reprocess entry to original sink")

		// Se falhou no sink original, tentar outros sinks
		return d.reprocessToAnySink(ctx, entry)
	}

	d.logger.WithFields(logrus.Fields{
		"source_type":   entry.SourceType,
		"source_id":     entry.SourceID,
		"original_sink": originalSink,
	}).Info("Successfully reprocessed DLQ entry to original sink")

	return nil
}

// findSinkByName encontra um sink pelo nome
func (d *Dispatcher) findSinkByName(sinkName string) types.Sink {
	d.mutex.RLock()
	defer d.mutex.RUnlock()

	// Para simplificar, retornamos o primeiro sink healthy que encontramos
	// Em uma implementação mais sofisticada, poderíamos tag os sinks com nomes
	// e fazer lookup por nome exato usando o parâmetro sinkName
	_ = sinkName // Unused for now, but kept for future implementation

	for _, sink := range d.sinks {
		if sink.IsHealthy() {
			return sink
		}
	}

	return nil
}

// reprocessToAnySink tenta reprocessar para qualquer sink healthy disponível
func (d *Dispatcher) reprocessToAnySink(ctx context.Context, entry *types.LogEntry) error {
	d.mutex.RLock()
	healthySinks := make([]types.Sink, 0)
	for _, sink := range d.sinks {
		if sink.IsHealthy() {
			healthySinks = append(healthySinks, sink)
		}
	}
	d.mutex.RUnlock()

	if len(healthySinks) == 0 {
		return fmt.Errorf("no healthy sinks available for reprocessing")
	}

	// Tentar enviar para cada sink healthy
	var lastError error
	for _, sink := range healthySinks {
		// Use DeepCopy to avoid copying mutex
		if err := sink.Send(ctx, []types.LogEntry{*entry.DeepCopy()}); err != nil {
			lastError = err
			d.logger.WithError(err).WithFields(logrus.Fields{
				"source_type": entry.SourceType,
				"source_id":   entry.SourceID,
			}).Debug("Failed to reprocess entry to alternative sink")
			continue
		}

		d.logger.WithFields(logrus.Fields{
			"source_type": entry.SourceType,
			"source_id":   entry.SourceID,
		}).Info("Successfully reprocessed DLQ entry to alternative sink")

		return nil
	}

	return fmt.Errorf("failed to reprocess to any sink, last error: %w", lastError)
}
