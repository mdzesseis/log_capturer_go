package sinks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ssw-logs-capture/internal/metrics"
	"ssw-logs-capture/pkg/batching"
	"ssw-logs-capture/pkg/circuit"
	"ssw-logs-capture/pkg/compression"
	"ssw-logs-capture/pkg/dlq"
	"ssw-logs-capture/pkg/types"

	"github.com/sirupsen/logrus"
)

// LokiDataError representa um erro de dados do cliente que não deve acionar circuit breaker
// Exemplos: timestamp fora de ordem, timestamp muito antigo/futuro, formato inválido
type LokiDataError struct {
	StatusCode int
	Message    string
	IsTimestampError bool
}

func (e *LokiDataError) Error() string {
	return fmt.Sprintf("loki data error (%d): %s", e.StatusCode, e.Message)
}

// LokiSink implementa sink para Grafana Loki
type LokiSink struct {
	config       types.LokiConfig
	logger       *logrus.Logger
	httpClient   *http.Client
	breaker      *circuit.Breaker
	compressor   *compression.HTTPCompressor
	deadLetterQueue *dlq.DeadLetterQueue
	enhancedMetrics *metrics.EnhancedMetrics // Advanced metrics collection

	queue        chan *types.LogEntry
	batch        []*types.LogEntry
	batchMutex   sync.Mutex
	lastSent     time.Time

	// Adaptive batcher (se habilitado)
	adaptiveBatcher *batching.AdaptiveBatcher
	useAdaptiveBatching bool

	// Task 5: Timestamp learning and validation
	timestampLearner TimestampLearner
	name             string // Sink name for metrics

	ctx          context.Context
	cancel       context.CancelFunc
	isRunning    bool
	mutex        sync.RWMutex

	// C6: Goroutine Leak Fix - Track goroutines for proper shutdown
	loopWg sync.WaitGroup // Tracks main loop goroutines (processLoop, flushLoop, adaptiveBatchLoop)
	sendWg sync.WaitGroup // Tracks sendBatch goroutines

	// C11: HTTP Client Timeout - Request-specific timeout for cancellable requests
	requestTimeout time.Duration

	// Semaphore to limit concurrent sendBatch goroutines (prevents goroutine accumulation)
	sendSemaphore chan struct{}
	maxConcurrentSends int

	// Worker pool for batch processing (architectural fix for goroutine leak)
	batchQueue   chan []*types.LogEntry // Queue of batches to process
	workerCount  int                     // Number of fixed worker goroutines
	workersWg    sync.WaitGroup          // Track worker goroutines

	// Métricas de backpressure
	backpressureCount int64
	droppedCount      int64
}

// LokiPayload estrutura do payload para Loki
type LokiPayload struct {
	Streams []LokiStream `json:"streams"`
}

// LokiStream representa um stream no Loki
type LokiStream struct {
	Stream map[string]string `json:"stream"`
	Values [][]string        `json:"values"`
}

// LokiErrorType classifies Loki errors for retry decision
type LokiErrorType int

const (
	LokiErrorTemporary LokiErrorType = iota // Network/transient error - RETRY
	LokiErrorPermanent                       // Client data error - NO RETRY → DLQ
	LokiErrorRateLimit                       // Rate limit - RETRY with backoff
	LokiErrorServer                          // Server error (5xx) - RETRY
)

// classifyLokiError classifies error type for retry decision
//
// Returns:
//   - LokiErrorPermanent: 400 errors (bad request, timestamp issues) → NO RETRY
//   - LokiErrorRateLimit: 429 errors → RETRY with backoff
//   - LokiErrorServer: 5xx errors → RETRY
//   - LokiErrorTemporary: Network errors, 0 status code → RETRY
func classifyLokiError(statusCode int, errorMsg string) LokiErrorType {
	switch statusCode {
	case 400:
		// 400 Bad Request - usually permanent client errors
		// Common patterns:
		//   - "timestamp too old"
		//   - "timestamp too new"
		//   - "out of order"
		//   - "invalid labels"
		return LokiErrorPermanent // NO RETRY!

	case 429:
		// Rate limit - retry with backoff
		return LokiErrorRateLimit // RETRY with backoff

	case 500, 502, 503, 504:
		// Server errors - transient issues
		return LokiErrorServer // RETRY

	case 0:
		// Network error (no HTTP response)
		return LokiErrorTemporary // RETRY

	default:
		// Other errors - treat as permanent if 4xx, temporary otherwise
		if statusCode >= 400 && statusCode < 500 {
			return LokiErrorPermanent // NO RETRY
		}
		return LokiErrorTemporary // RETRY
	}
}

// errorTypeToString converts LokiErrorType to string for metrics
func errorTypeToString(errorType LokiErrorType) string {
	switch errorType {
	case LokiErrorPermanent:
		return "permanent"
	case LokiErrorRateLimit:
		return "rate_limit"
	case LokiErrorServer:
		return "server"
	case LokiErrorTemporary:
		return "temporary"
	default:
		return "unknown"
	}
}

// parseTimestampLearningConfig converts types.TimestampLearningConfig to sinks.TimestampLearnerConfig
func parseTimestampLearningConfig(config types.TimestampLearningConfig) TimestampLearnerConfig {
	result := TimestampLearnerConfig{
		Enabled:           true,             // Default: enabled
		DefaultMaxAge:     24 * time.Hour,   // Default: 24 hours
		ClampEnabled:      false,            // Default: disabled (don't modify timestamps)
		LearnFromErrors:   true,             // Default: enabled
		MinLearningWindow: 5 * time.Minute,  // Default: 5 minutes
	}

	// Apply user configuration
	if !config.Enabled {
		result.Enabled = false
	}

	if config.DefaultMaxAge != "" {
		if d, err := time.ParseDuration(config.DefaultMaxAge); err == nil {
			result.DefaultMaxAge = d
		}
	}

	if config.ClampEnabled {
		result.ClampEnabled = true
	}

	if !config.LearnFromErrors {
		result.LearnFromErrors = false
	}

	if config.MinLearningWindow != "" {
		if d, err := time.ParseDuration(config.MinLearningWindow); err == nil {
			result.MinLearningWindow = d
		}
	}

	return result
}

// NewLokiSink cria um novo sink para Loki
func NewLokiSink(config types.LokiConfig, logger *logrus.Logger, deadLetterQueue *dlq.DeadLetterQueue, enhancedMetrics *metrics.EnhancedMetrics) *LokiSink {
	ctx, cancel := context.WithCancel(context.Background())

	// Parse timeout from string
	timeout := 30 * time.Second
	if config.Timeout != "" {
		if t, err := time.ParseDuration(config.Timeout); err == nil {
			timeout = t
		}
	}

	// Configurar HTTP client with proper connection limits to prevent goroutine leaks
	// Each HTTP connection spawns 2 goroutines (readLoop + writeLoop)
	// Without MaxConnsPerHost, connections accumulate indefinitely
	httpClient := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			MaxIdleConns:          100, // Global idle connection pool
			MaxIdleConnsPerHost:   10,  // Max idle connections per host
			MaxConnsPerHost:       50,  // CRITICAL: Limit total connections per host (prevents unlimited growth)
			IdleConnTimeout:       90 * time.Second, // How long idle connections stay open
			TLSHandshakeTimeout:   10 * time.Second, // Timeout for TLS handshake
			ExpectContinueTimeout: 1 * time.Second,  // Timeout for Expect: 100-continue
			ResponseHeaderTimeout: timeout,          // Timeout waiting for response headers
			DisableKeepAlives:     true,             // INTERIM FIX: Disable pooling to stop goroutine leak (trades 10-20% perf for stability)
			ForceAttemptHTTP2:     false,            // Stick to HTTP/1.1 for simplicity
		},
	}

	// Configurar circuit breaker
	breaker := circuit.NewBreaker(circuit.BreakerConfig{
		Name:             "loki_sink",
		FailureThreshold: 20,      // Aumentado de 5 para 20 - menos sensível
		SuccessThreshold: 3,
		Timeout:          60 * time.Second,  // Aumentado de 30s para 60s
		HalfOpenMaxCalls: 10,
	}, logger)

	// Configurar compressor HTTP
	compressionConfig := compression.Config{
		DefaultAlgorithm: compression.AlgorithmGzip,
		AdaptiveEnabled:  true,
		MinBytes:         1024,
		Level:            6,
		PoolSize:         10,
		PerSink: map[string]compression.SinkCompressionConfig{
			"loki": {
				Algorithm: compression.AlgorithmGzip,
				Enabled:   true, // Always enable compression for Loki
				Level:     6,
			},
		},
	}
	compressor := compression.NewHTTPCompressor(compressionConfig, logger)

	// Use configured queue size, default to 20000 if not set
	queueSize := config.QueueSize
	if queueSize <= 0 {
		queueSize = 20000
	}

	ls := &LokiSink{
		config:             config,
		logger:             logger,
		httpClient:         httpClient,
		breaker:            breaker,
		compressor:         compressor,
		deadLetterQueue:    deadLetterQueue,
		enhancedMetrics:    enhancedMetrics,
		queue:              make(chan *types.LogEntry, queueSize),
		batch:              make([]*types.LogEntry, 0, config.BatchSize),
		ctx:                ctx,
		cancel:             cancel,
		requestTimeout:     timeout, // C11: Store timeout for request-specific contexts
		sendSemaphore:      make(chan struct{}, 15), // Limit to 15 concurrent sendBatch goroutines
		maxConcurrentSends: 15,
		batchQueue:         make(chan []*types.LogEntry, 100), // Queue for worker pool (100 batches buffer)
		workerCount:        10,                                // Fixed pool of 10 workers
		name:               "loki",                            // Sink name for metrics
	}

	// Task 5: Initialize timestamp learner
	tsConfig := parseTimestampLearningConfig(config.TimestampLearning)
	ls.timestampLearner = NewTimestampLearner(tsConfig, logger)

	// Report initial threshold to metrics
	if tsConfig.Enabled {
		metrics.UpdateTimestampMaxAge("loki", tsConfig.DefaultMaxAge.Seconds())
	}

	// Configurar adaptive batcher se habilitado
	if config.AdaptiveBatching.Enabled {
		adaptiveConfig := batching.AdaptiveBatchConfig{
			MinBatchSize:       config.AdaptiveBatching.MinBatchSize,
			MaxBatchSize:       config.AdaptiveBatching.MaxBatchSize,
			InitialBatchSize:   config.AdaptiveBatching.InitialBatchSize,
			ThroughputTarget:   config.AdaptiveBatching.ThroughputTarget,
			BufferSize:         config.AdaptiveBatching.BufferSize,
		}

		// Parse durations with fallbacks
		if d, err := time.ParseDuration(config.AdaptiveBatching.MinFlushDelay); err == nil {
			adaptiveConfig.MinFlushDelay = d
		} else {
			adaptiveConfig.MinFlushDelay = 50 * time.Millisecond
		}

		if d, err := time.ParseDuration(config.AdaptiveBatching.MaxFlushDelay); err == nil {
			adaptiveConfig.MaxFlushDelay = d
		} else {
			adaptiveConfig.MaxFlushDelay = 10 * time.Second
		}

		if d, err := time.ParseDuration(config.AdaptiveBatching.InitialFlushDelay); err == nil {
			adaptiveConfig.InitialFlushDelay = d
		} else {
			adaptiveConfig.InitialFlushDelay = 1 * time.Second
		}

		if d, err := time.ParseDuration(config.AdaptiveBatching.AdaptationInterval); err == nil {
			adaptiveConfig.AdaptationInterval = d
		} else {
			adaptiveConfig.AdaptationInterval = 30 * time.Second
		}

		if d, err := time.ParseDuration(config.AdaptiveBatching.LatencyThreshold); err == nil {
			adaptiveConfig.LatencyThreshold = d
		} else {
			adaptiveConfig.LatencyThreshold = 500 * time.Millisecond
		}

		ls.adaptiveBatcher = batching.NewAdaptiveBatcher(adaptiveConfig, logger)
		ls.useAdaptiveBatching = true
		logger.Info("Adaptive batching enabled for Loki sink")
	}

	return ls
}

// startWorkers initializes the worker pool for batch processing
func (ls *LokiSink) startWorkers() {
	ls.logger.WithField("worker_count", ls.workerCount).Info("Starting Loki sink worker pool")

	for i := 0; i < ls.workerCount; i++ {
		ls.workersWg.Add(1)
		go ls.worker(i)
	}
}

// worker processes batches from the queue
func (ls *LokiSink) worker(id int) {
	defer ls.workersWg.Done()

	ls.logger.WithField("worker_id", id).Debug("Loki sink worker started")

	for {
		select {
		case <-ls.ctx.Done():
			ls.logger.WithField("worker_id", id).Debug("Loki sink worker shutting down")
			return

		case batch := <-ls.batchQueue:
			// Process batch using existing sendBatch method
			// Note: sendBatch already handles WaitGroup tracking via sendWg
			ls.sendWg.Add(1)
			ls.sendBatch(batch)
		}
	}
}

// Start inicia o sink
func (ls *LokiSink) Start(ctx context.Context) error {
	if !ls.config.Enabled {
		ls.logger.Info("Loki sink disabled")
		return nil
	}

	ls.mutex.Lock()
	defer ls.mutex.Unlock()

	if ls.isRunning {
		return fmt.Errorf("loki sink already running")
	}

	ls.isRunning = true
	ls.logger.WithField("url", ls.config.URL).Info("Starting Loki sink")

	// Start worker pool for batch processing
	ls.startWorkers()

	// Definir como healthy no início
	metrics.SetComponentHealth("sink", "loki", true)

	// C6: Track loop goroutines for proper shutdown
	// Iniciar adaptive batcher se habilitado
	if ls.useAdaptiveBatching && ls.adaptiveBatcher != nil {
		if err := ls.adaptiveBatcher.Start(); err != nil {
			return fmt.Errorf("failed to start adaptive batcher: %w", err)
		}
		ls.loopWg.Add(1)
		go ls.adaptiveBatchLoop()
	} else {
		// Usar batching tradicional
		ls.loopWg.Add(2) // processLoop + flushLoop
		go ls.processLoop()
		go ls.flushLoop()
	}

	return nil
}

// Stop para o sink
func (ls *LokiSink) Stop() error {
	ls.mutex.Lock()
	if !ls.isRunning {
		ls.mutex.Unlock()
		return nil
	}

	ls.logger.Info("Stopping Loki sink")
	ls.isRunning = false
	ls.mutex.Unlock() // Unlock early to allow goroutines to finish

	// Definir como unhealthy ao parar
	metrics.SetComponentHealth("sink", "loki", false)

	// C6: Cancel context to signal all goroutines to stop
	ls.cancel()

	// C6: Wait for main loop goroutines to finish
	loopDone := make(chan struct{})
	go func() {
		ls.loopWg.Wait()
		close(loopDone)
	}()

	select {
	case <-loopDone:
		ls.logger.Info("All loop goroutines stopped")
	case <-time.After(5 * time.Second):
		ls.logger.Warn("Timeout waiting for loop goroutines to stop")
	}

	// Parar adaptive batcher se habilitado
	if ls.useAdaptiveBatching && ls.adaptiveBatcher != nil {
		if err := ls.adaptiveBatcher.Stop(); err != nil {
			ls.logger.WithError(err).Error("Failed to stop adaptive batcher")
		}
	}

	// Flush final batch
	ls.flushBatch()

	// C6: Wait for sendBatch goroutines to finish
	sendDone := make(chan struct{})
	go func() {
		ls.sendWg.Wait()
		close(sendDone)
	}()

	select {
	case <-sendDone:
		ls.logger.Info("All sendBatch goroutines stopped")
	case <-time.After(10 * time.Second):
		ls.logger.Warn("Timeout waiting for sendBatch goroutines to stop")
	}

	ls.logger.Info("Loki sink stopped")
	return nil
}

// validateAndFilterTimestamps validates timestamps and filters invalid entries
//
// Task 5: Timestamp validation layer - prevents retry storm from permanent errors
//
// Process:
//   1. Validate each entry's timestamp
//   2. If invalid: Send to DLQ immediately (NO RETRY)
//   3. If valid: Pass through for sending
//   4. Optional: Clamp old timestamps if configured
//
// Returns: Slice of valid entries ready for sending
func (ls *LokiSink) validateAndFilterTimestamps(entries []types.LogEntry) []*types.LogEntry {
	if ls.timestampLearner == nil {
		// Learner disabled, convert all entries to pointers
		validEntries := make([]*types.LogEntry, len(entries))
		for i := range entries {
			validEntries[i] = &entries[i]
		}
		return validEntries
	}

	validEntries := make([]*types.LogEntry, 0, len(entries))

	for i := range entries {
		entry := &entries[i]
		// Optional: Try to clamp timestamp first (if configured)
		if ls.timestampLearner.ClampTimestamp(entry) {
			metrics.RecordTimestampClamped(ls.name)
			ls.logger.WithFields(logrus.Fields{
				"original_age_hours": entry.Labels["_original_age_hours"],
				"clamped_timestamp":  entry.Timestamp.Format(time.RFC3339),
			}).Debug("Timestamp clamped to acceptable range")
		}

		// Validate timestamp
		if err := ls.timestampLearner.ValidateTimestamp(entry); err != nil {
			// Timestamp invalid - send to DLQ without retry
			reason := "validation_failed"
			if errors.Is(err, ErrTimestampTooOld) {
				reason = "too_old"
			} else if errors.Is(err, ErrTimestampTooNew) {
				reason = "too_new"
			}

			metrics.RecordTimestampRejection(ls.name, reason)

			ls.logger.WithFields(logrus.Fields{
				"timestamp":  entry.Timestamp.Format(time.RFC3339),
				"age_hours":  time.Since(entry.Timestamp).Hours(),
				"reason":     reason,
				"error":      err.Error(),
				"source_id":  entry.SourceID,
			}).Warn("Timestamp validation failed, sending to DLQ")

			// Send to DLQ immediately (NO RETRY)
			if ls.deadLetterQueue != nil {
				ls.deadLetterQueue.AddEntry(entry, err.Error(), "timestamp_"+reason, ls.name, 0, map[string]string{
					"validation_error": err.Error(),
					"timestamp":        entry.Timestamp.Format(time.RFC3339),
					"age_hours":        fmt.Sprintf("%.1f", time.Since(entry.Timestamp).Hours()),
				})
			}

			continue // Skip this entry
		}

		// Timestamp valid - add to valid entries
		validEntries = append(validEntries, entry)
	}

	return validEntries
}

// Send envia logs para o sink com backpressure inteligente
func (ls *LokiSink) Send(ctx context.Context, entries []types.LogEntry) error {
	if !ls.config.Enabled {
		return nil
	}

	// Task 5: Validate timestamps BEFORE sending to prevent permanent errors
	validEntries := ls.validateAndFilterTimestamps(entries)
	if len(validEntries) == 0 {
		return nil // All entries rejected by timestamp validation
	}

	for i := range validEntries {
		entry := validEntries[i]
		if ls.useAdaptiveBatching && ls.adaptiveBatcher != nil {
			// Usar adaptive batcher (entry já é ponteiro)
			if err := ls.adaptiveBatcher.Add(entry); err != nil {
				ls.sendToDLQ(entry, "adaptive_batcher_error", err.Error(), "loki", 0)
				atomic.AddInt64(&ls.droppedCount, 1)
				metrics.RecordError("loki_sink", "adaptive_batcher_error")
			}
		} else {
			// Usar fila tradicional com backpressure
			queueUtilization := ls.GetQueueUtilization()

			// Se a fila estiver acima de 95%, tentar enviar para DLQ ao invés de bloquear
			if queueUtilization > 0.95 {
				ls.sendToDLQ(entry, "loki_queue_full", "backpressure", "loki", 0)
				atomic.AddInt64(&ls.droppedCount, 1)
				metrics.RecordError("loki_sink", "queue_full")
				continue
			}

			// Backpressure escalonado baseado na utilização da fila
			var timeout time.Duration
			if queueUtilization > 0.9 {
				timeout = 1 * time.Second // Timeout curto quando quase cheio
			} else if queueUtilization > 0.75 {
				timeout = 3 * time.Second // Timeout médio
			} else {
				timeout = 10 * time.Second // Timeout normal
			}

			select {
			case ls.queue <- entry:
				// Enviado para fila com sucesso
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(timeout):
				// Timeout atingido - enviar para DLQ
				ls.logger.WithFields(logrus.Fields{
					"queue_utilization": queueUtilization,
					"timeout":           timeout,
				}).Warn("Loki sink timeout - sending to DLQ")

				ls.sendToDLQ(entry, "loki_timeout", "backpressure", "loki", 0)
				atomic.AddInt64(&ls.backpressureCount, 1)
				metrics.RecordError("loki_sink", "backpressure_timeout")
			}
		}
	}

	return nil
}

// IsHealthy verifica se o sink está saudável
func (ls *LokiSink) IsHealthy() bool {
	ls.mutex.RLock()
	defer ls.mutex.RUnlock()
	// Não verificar breaker aqui - deixar o breaker gerenciar estado internamente
	// Isso permite que o breaker tente half-open após o timeout
	return ls.isRunning
}

// GetQueueUtilization retorna a utilização da fila
func (ls *LokiSink) GetQueueUtilization() float64 {
	return float64(len(ls.queue)) / float64(cap(ls.queue))
}

// processLoop loop principal de processamento
func (ls *LokiSink) processLoop() {
	defer ls.loopWg.Done() // C6: Signal completion when loop exits
	for {
		select {
		case <-ls.ctx.Done():
			return
		case entry := <-ls.queue:
			ls.addToBatch(entry)
		}
	}
}

// flushLoop flush por tempo
func (ls *LokiSink) flushLoop() {
	defer ls.loopWg.Done() // C6: Signal completion when loop exits

	batchTimeout := 10 * time.Second
	if ls.config.BatchTimeout != "" {
		if t, err := time.ParseDuration(ls.config.BatchTimeout); err == nil {
			batchTimeout = t
		}
	}
	ticker := time.NewTicker(batchTimeout)
	defer ticker.Stop()

	for {
		select {
		case <-ls.ctx.Done():
			return
		case <-ticker.C:
			ls.flushBatch()
		}
	}
}

// addToBatch adiciona entrada ao batch
func (ls *LokiSink) addToBatch(entry *types.LogEntry) {
	ls.batchMutex.Lock()
	defer ls.batchMutex.Unlock()

	ls.batch = append(ls.batch, entry)

	// Flush se batch estiver cheio
	if len(ls.batch) >= ls.config.BatchSize {
		ls.flushBatchUnsafe()
	}
}

// flushBatch faz flush do batch atual
func (ls *LokiSink) flushBatch() {
	ls.batchMutex.Lock()
	defer ls.batchMutex.Unlock()
	ls.flushBatchUnsafe()
}

// flushBatchUnsafe faz flush sem lock (deve ser chamado com lock)
func (ls *LokiSink) flushBatchUnsafe() {
	if len(ls.batch) == 0 {
		return
	}

	// Criar cópia do batch
	entries := make([]*types.LogEntry, len(ls.batch))
	copy(entries, ls.batch)

	// Limpar batch
	ls.batch = ls.batch[:0]

	// Enqueue batch for worker pool processing (non-blocking with timeout)
	select {
	case ls.batchQueue <- entries:
		// Successfully enqueued
	case <-time.After(100 * time.Millisecond):
		// Queue full, log warning and drop batch
		ls.logger.WithField("batch_size", len(entries)).Warn("Batch queue full, dropping batch")
		atomic.AddInt64(&ls.droppedCount, int64(len(entries)))
	}
}

// sendBatch envia um batch para o Loki
func (ls *LokiSink) sendBatch(entries []*types.LogEntry) {
	defer ls.sendWg.Done() // C6: Signal completion when sendBatch finishes

	startTime := time.Now()

	// Capture data errors separately to avoid triggering circuit breaker
	var dataErr *LokiDataError

	err := ls.breaker.Execute(func() error {
		err := ls.sendToLoki(entries)
		// Check if it's a data error (shouldn't trigger circuit breaker)
		if errors.As(err, &dataErr) {
			return nil // Don't trigger circuit breaker for client data errors
		}
		return err
	})

	// If we have a data error that didn't trigger the breaker, use it as the error
	if dataErr != nil {
		err = dataErr
	}

	duration := time.Since(startTime)
	metrics.RecordSinkSendDuration("loki", duration)

	if err != nil {
		// Task 5: Classify error and decide retry strategy
		var statusCode int
		var errorMsg string

		// Extract status code from error
		if dataErr != nil {
			statusCode = dataErr.StatusCode
			errorMsg = dataErr.Message
		} else {
			// Network or other error
			statusCode = 0
			errorMsg = err.Error()
		}

		// Classify error type for retry decision
		errorType := classifyLokiError(statusCode, errorMsg)
		metrics.RecordLokiErrorType(ls.name, errorTypeToString(errorType))

		// Handle based on error classification
		switch errorType {
		case LokiErrorPermanent:
			// Permanent error (400) - NO RETRY → DLQ immediately
			ls.logger.WithFields(logrus.Fields{
				"error":       errorMsg,
				"status_code": statusCode,
				"entries":     len(entries),
			}).Warn("Loki permanent error (400), sending to DLQ without retry")

			// Learn from timestamp errors
			if dataErr != nil && dataErr.IsTimestampError && ls.timestampLearner != nil {
				for i := range entries {
					ls.timestampLearner.LearnFromRejection(errorMsg, entries[i])
				}
				metrics.RecordTimestampLearningEvent(ls.name)
				// Update metrics with new threshold
				newThreshold := ls.timestampLearner.GetMaxAcceptableAge()
				metrics.UpdateTimestampMaxAge(ls.name, newThreshold.Seconds())
			}

			// Send to DLQ with retryCount=0 (permanent failure)
			for i := range entries {
				ls.sendToDLQ(entries[i], errorMsg, "loki_permanent_error", "loki", 0)
			}

			metrics.RecordLogSent("loki", "permanent_error")
			metrics.RecordError("loki_sink", "permanent_error")

		case LokiErrorRateLimit:
			// Rate limit (429) - will retry via normal mechanism
			ls.logger.WithField("entries", len(entries)).Warn("Loki rate limit, will retry")
			metrics.RecordLogSent("loki", "rate_limit")
			metrics.RecordError("loki_sink", "rate_limit")

			// Send to DLQ with retryCount=1 (will retry)
			for i := range entries {
				ls.sendToDLQ(entries[i], errorMsg, "loki_rate_limit", "loki", 1)
			}

		case LokiErrorServer, LokiErrorTemporary:
			// Server/network errors - will retry
			ls.logger.WithError(err).WithField("entries", len(entries)).Error("Failed to send batch to Loki, will retry")
			metrics.RecordLogSent("loki", "error")
			metrics.RecordError("loki_sink", "send_error")

			// Send to DLQ with retryCount=1 (will retry)
			for i := range entries {
				ls.sendToDLQ(entries[i], errorMsg, "send_failed", "loki", 1)
			}
		}
	} else {
		ls.logger.WithField("entries", len(entries)).Debug("Batch sent to Loki successfully")
		metrics.RecordLogSent("loki", "success")

		// Update lastSent with mutex protection (concurrent sendBatch goroutines)
		ls.batchMutex.Lock()
		ls.lastSent = time.Now()
		ls.batchMutex.Unlock()
	}

	// Atualizar métricas de utilização da fila
	metrics.SetSinkQueueUtilization("loki", ls.GetQueueUtilization())
}

// sendToLoki envia dados para o Loki
func (ls *LokiSink) sendToLoki(entries []*types.LogEntry) error {
	// C11: Check if context is already cancelled before starting HTTP request
	select {
	case <-ls.ctx.Done():
		return fmt.Errorf("sink context cancelled, aborting request: %w", ls.ctx.Err())
	default:
		// Continue with request
	}

	// Agrupar entradas por stream (combinação de labels)
	streams := ls.groupByStream(entries)

	// Criar payload
	payload := LokiPayload{
		Streams: streams,
	}

	// Serializar JSON
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Log debug do payload para análise de erros 400
	ls.logger.WithFields(logrus.Fields{
		"streams_count": len(payload.Streams),
		"payload_size":  len(data),
		"json_preview":  func() string {
			if len(data) > 500 {
				return string(data[:500]) + "..."
			}
			return string(data)
		}(), // Preview dos primeiros 500 chars
		"first_stream_labels": func() map[string]string {
			if len(payload.Streams) > 0 {
				return payload.Streams[0].Stream
			}
			return nil
		}(),
		"first_entry_count": func() int {
			if len(payload.Streams) > 0 {
				return len(payload.Streams[0].Values)
			}
			return 0
		}(),
	}).Debug("Sending payload to Loki")

	// Comprimir usando o HTTP compressor
	compressionResult, err := ls.compressor.Compress(data, compression.AlgorithmAuto, "loki")
	if err != nil {
		return fmt.Errorf("failed to compress data: %w", err)
	}

	// Record compression ratio metrics
	if ls.enhancedMetrics != nil {
		ls.enhancedMetrics.RecordCompressionRatio("loki_sink", string(compressionResult.Algorithm), compressionResult.Ratio)
	}

	// Construir URL com push endpoint
	url := ls.config.URL
	if ls.config.PushEndpoint != "" {
		url += ls.config.PushEndpoint
	} else {
		url += "/loki/api/v1/push"
	}

	// C11: Create request-specific context with timeout
	// This ensures the request respects both the sink's context AND has a timeout
	reqCtx, reqCancel := context.WithTimeout(ls.ctx, ls.requestTimeout)
	defer reqCancel()

	body := bytes.NewReader(compressionResult.Data)
	req, err := http.NewRequestWithContext(reqCtx, "POST", url, body)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Headers padrão
	req.Header.Set("Content-Type", compressionResult.ContentType)
	if compressionResult.Encoding != "" {
		req.Header.Set("Content-Encoding", compressionResult.Encoding)
	}

	// Headers customizados da configuração
	for key, value := range ls.config.Headers {
		req.Header.Set(key, value)
	}

	// Tenant ID para Loki multi-tenant
	if ls.config.TenantID != "" {
		req.Header.Set("X-Scope-OrgID", ls.config.TenantID)
	}

	// Autenticação
	if ls.config.Auth.Type == "basic" && ls.config.Auth.Username != "" && ls.config.Auth.Password != "" {
		req.SetBasicAuth(ls.config.Auth.Username, ls.config.Auth.Password)
	} else if ls.config.Auth.Type == "bearer" && ls.config.Auth.Token != "" {
		req.Header.Set("Authorization", "Bearer "+ls.config.Auth.Token)
	}

	// Log compression metrics
	ls.logger.WithFields(logrus.Fields{
		"original_size":   compressionResult.OriginalSize,
		"compressed_size": compressionResult.CompressedSize,
		"compression_ratio": compressionResult.Ratio,
		"algorithm":       string(compressionResult.Algorithm),
	}).Debug("Loki payload compressed")

	// DIAGNOSTIC: Log detailed request information to diagnose failure rate
	ls.logger.WithFields(logrus.Fields{
		"url":              url,
		"entries":          len(entries),
		"payload_size":     compressionResult.CompressedSize,
		"content_type":     compressionResult.ContentType,
		"content_encoding": compressionResult.Encoding,
		"tenant_id":        ls.config.TenantID,
		"timeout":          ls.requestTimeout.String(),
	}).Info("Sending Loki request")

	// Enviar request
	resp, err := ls.httpClient.Do(req)
	if err != nil {
		// DIAGNOSTIC: Enhanced error logging to diagnose connection issues
		ls.logger.WithFields(logrus.Fields{
			"url":     url,
			"error":   err.Error(),
			"entries": len(entries),
			"timeout": ls.requestTimeout.String(),
		}).Error("Loki request failed - HTTP client error")

		// C11: Differentiate timeout from cancellation errors
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("request timeout after %v: %w", ls.requestTimeout, err)
		}
		if errors.Is(err, context.Canceled) {
			return fmt.Errorf("request cancelled (sink shutting down): %w", err)
		}
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// CRITICAL FIX: Must FULLY READ response body to enable HTTP connection reuse
	// HTTP/1.1 connection pooling requires reading the entire response body
	// Without this, connections accumulate and leak (each spawns 2 goroutines)
	// See: https://pkg.go.dev/net/http#Response - "It is the caller's responsibility to close Body"
	// AND: https://github.com/golang/go/issues/23427

	// Read response body based on status
	var errorBody string
	if resp.StatusCode >= 300 {
		// Error response - read body for error details (limited to 64KB)
		bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		if readErr != nil {
			// Drain any remaining data before returning
			io.Copy(io.Discard, resp.Body)
			return fmt.Errorf("loki returned status %d (failed to read error details: %v)", resp.StatusCode, readErr)
		}
		errorBody = string(bodyBytes)
	} else {
		// Success response (2xx) - body is typically empty for Loki
		// Must drain it completely to enable connection reuse
		// Use io.Copy instead of ReadAll to handle empty bodies correctly
		bytesRead, copyErr := io.Copy(io.Discard, resp.Body)
		if copyErr != nil {
			ls.logger.WithError(copyErr).Warn("Failed to drain response body (may cause connection leak)")
		}
		// DIAGNOSTIC: Log successful requests to track success rate
		ls.logger.WithFields(logrus.Fields{
			"status":      resp.StatusCode,
			"bytes_read":  bytesRead,
			"entries":     len(entries),
			"url":         url,
		}).Info("Loki request successful")
	}

	// Process error responses
	if resp.StatusCode >= 300 {
		ls.logger.WithFields(logrus.Fields{
			"status_code": resp.StatusCode,
			"error_body":  errorBody,
			"entries":     len(entries),
		}).Error("Loki request failed with detailed error")

		// Retornar erro mais detalhado
		if resp.StatusCode == 400 {
			// Verificar se é erro de timestamp (não deve acionar circuit breaker)
			errorBodyLower := strings.ToLower(errorBody)
			isTimestampError := strings.Contains(errorBodyLower, "out of order") ||
				strings.Contains(errorBodyLower, "too old") ||
				strings.Contains(errorBodyLower, "too far in the future") ||
				strings.Contains(errorBodyLower, "timestamp") ||
				strings.Contains(errorBodyLower, "entry with ts")

			if isTimestampError {
				return &LokiDataError{
					StatusCode: 400,
					Message: errorBody,
					IsTimestampError: true,
				}
			}
			return fmt.Errorf("loki bad request (400): %s", errorBody)
		} else if resp.StatusCode == 401 {
			return fmt.Errorf("loki unauthorized (401): %s", errorBody)
		} else if resp.StatusCode == 403 {
			return fmt.Errorf("loki forbidden (403): %s", errorBody)
		} else if resp.StatusCode >= 500 {
			return fmt.Errorf("loki server error (%d): %s", resp.StatusCode, errorBody)
		} else {
			return fmt.Errorf("loki returned status %d: %s", resp.StatusCode, errorBody)
		}
	}

	// SUCCESS (2xx) - Body has been drained by deferred function above
	return nil
}

// groupByStream agrupa entradas por stream
func (ls *LokiSink) groupByStream(entries []*types.LogEntry) []LokiStream {
	streamMap := make(map[string]*LokiStream)

	for i := range entries {
		entry := entries[i]
		// Criar chave do stream baseada nos labels
		streamKey := ls.createStreamKey(entry.Labels)

		// Obter ou criar stream
		stream, exists := streamMap[streamKey]
		if !exists {
			stream = &LokiStream{
				Stream: ls.prepareLokiLabels(entry.Labels),
				Values: make([][]string, 0),
			}
			streamMap[streamKey] = stream
		}

		// Adicionar valor
		timestamp := strconv.FormatInt(entry.Timestamp.UnixNano(), 10)
		stream.Values = append(stream.Values, []string{timestamp, entry.Message})
	}

	// Converter map para slice
	streams := make([]LokiStream, 0, len(streamMap))
	for _, stream := range streamMap {
		streams = append(streams, *stream)
	}

	return streams
}

// createStreamKey cria chave única para o stream
func (ls *LokiSink) createStreamKey(labels map[string]string) string {
	// C7: Unsafe JSON Marshal Fix - Use deterministic key generation
	// JSON marshal has undefined map key order, causing duplicate streams!

	if len(labels) == 0 {
		return "{}"
	}

	// Extract and sort keys for deterministic ordering
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Build key with sorted keys using strings.Builder for performance
	var sb strings.Builder
	sb.WriteString("{")

	for i, k := range keys {
		if i > 0 {
			sb.WriteString(",")
		}
		// Use JSON-like format for compatibility
		sb.WriteString(`"`)
		sb.WriteString(k)
		sb.WriteString(`":"`)
		sb.WriteString(labels[k])
		sb.WriteString(`"`)
	}

	sb.WriteString("}")
	return sb.String()
}

// prepareLokiLabels prepara labels para o Loki
func (ls *LokiSink) prepareLokiLabels(labels map[string]string) map[string]string {
	lokiLabels := make(map[string]string)

	// Adicionar labels padrão da configuração primeiro
	for key, value := range ls.config.DefaultLabels {
		sanitizedKey := ls.sanitizeLabelName(key)
		lokiLabels[sanitizedKey] = value
	}

	// Copiar labels do log, filtrando temporários e alta cardinalidade
	for key, value := range labels {
		// FILTRAR labels temporários e de alta cardinalidade
		if ls.shouldDropLabel(key) {
			continue
		}

		sanitizedKey := ls.sanitizeLabelName(key)
		lokiLabels[sanitizedKey] = value
	}

	// Garantir que existam labels obrigatórios
	if _, exists := lokiLabels["service"]; !exists {
		lokiLabels["service"] = "ssw-log-capturer"
	}

	return lokiLabels
}

// shouldDropLabel determina se um label deve ser descartado para evitar alta cardinalidade
func (ls *LokiSink) shouldDropLabel(key string) bool {
	// Lista de prefixos que indicam labels temporários
	temporaryPrefixes := []string{
		"_temp_",
		"label__temp_",
		"temp_",
	}

	// Labels de alta cardinalidade que devem ser removidos
	highCardinalityLabels := []string{
		"timestamp",
		"time",
		"trace_id",
		"request_id",
		"transaction_id",
		"container_id",  // Mantemos container_name mas removemos ID
		"file_path",     // Mantemos file_name mas removemos path completo
		"filepath",
		"pid",
		"thread_id",
		"line_number",
		"offset",
		"position",
		"instance",       // IP addresses - alta cardinalidade
		"instance_name",  // Container IDs - alta cardinalidade
		"image",          // Imagens com versões - média/alta cardinalidade
		"msg",            // Mensagem não deve ser label
	}

	// Verificar prefixos temporários
	for _, prefix := range temporaryPrefixes {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			return true
		}
	}

	// Verificar labels de alta cardinalidade
	for _, highCard := range highCardinalityLabels {
		if key == highCard {
			return true
		}
	}

	return false
}

// sanitizeLabelName sanitiza nome do label para o Loki
func (ls *LokiSink) sanitizeLabelName(name string) string {
	// Loki tem regras específicas para nomes de labels
	// Substituir caracteres inválidos
	sanitized := ""
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			sanitized += string(r)
		} else {
			sanitized += "_"
		}
	}

	// Garantir que comece com letra
	if len(sanitized) > 0 && !(sanitized[0] >= 'a' && sanitized[0] <= 'z') && !(sanitized[0] >= 'A' && sanitized[0] <= 'Z') {
		sanitized = "label_" + sanitized
	}

	if sanitized == "" {
		sanitized = "unknown"
	}

	return sanitized
}

// sendToDLQ envia entrada para Dead Letter Queue
func (ls *LokiSink) sendToDLQ(entry *types.LogEntry, errorMsg, errorType, failedSink string, retryCount int) {
	if ls.deadLetterQueue != nil {
		context := map[string]string{
			"sink_type":        "loki",
			"queue_utilization": fmt.Sprintf("%.2f", ls.GetQueueUtilization()),
			"loki_url":         ls.config.URL,
		}

		if err := ls.deadLetterQueue.AddEntry(entry, errorMsg, errorType, failedSink, retryCount, context); err != nil {
			ls.logger.WithFields(logrus.Fields{
				"error_type":    errorType,
				"error":         errorMsg,
				"failed_sink":   failedSink,
				"retry_count":   retryCount,
				"source_type":   entry.SourceType,
				"source_id":     entry.SourceID,
				"dlq_error":     err.Error(),
			}).Error("Failed to send entry to DLQ")
			metrics.RecordError("loki_sink", "dlq_write_failed")
			return
		}

		metrics.RecordError("loki_sink", "dlq_entry")

		ls.logger.WithFields(logrus.Fields{
			"error_type":    errorType,
			"error":         errorMsg,
			"failed_sink":   failedSink,
			"retry_count":   retryCount,
			"source_type":   entry.SourceType,
			"source_id":     entry.SourceID,
		}).Debug("Entry sent to DLQ")
	} else {
		// Se não tiver DLQ, pelo menos registrar o erro
		ls.logger.WithFields(logrus.Fields{
			"error":         errorMsg,
			"error_type":    errorType,
			"failed_sink":   failedSink,
			"retry_count":   retryCount,
		}).Error("Failed to send log entry and no DLQ available")
	}
}

// GetBackpressureStats retorna estatísticas de backpressure
func (ls *LokiSink) GetBackpressureStats() map[string]interface{} {
	return map[string]interface{}{
		"backpressure_count": atomic.LoadInt64(&ls.backpressureCount),
		"dropped_count":      atomic.LoadInt64(&ls.droppedCount),
		"queue_utilization":  ls.GetQueueUtilization(),
		"queue_size":         len(ls.queue),
		"queue_capacity":     cap(ls.queue),
	}
}

// adaptiveBatchLoop loop principal para adaptive batching
func (ls *LokiSink) adaptiveBatchLoop() {
	defer ls.loopWg.Done() // C6: Signal completion when loop exits

	for {
		select {
		case <-ls.ctx.Done():
			return
		default:
			// Obter próximo batch do adaptive batcher
			batch, err := ls.adaptiveBatcher.GetBatch(ls.ctx)
			if err != nil {
				if err == context.Canceled {
					return
				}
				ls.logger.WithError(err).Error("Error getting batch from adaptive batcher")
				continue
			}

			if len(batch) > 0 {
				// batch já é []*types.LogEntry (P1 FIX aplicado no adaptive_batcher)

				// Acquire semaphore slot (blocks if limit reached)
				ls.sendSemaphore <- struct{}{}

				// C6: Track sendBatch goroutine for proper shutdown
				ls.sendWg.Add(1)
				go func(entries []*types.LogEntry) {
					defer func() {
						<-ls.sendSemaphore // Release semaphore slot
					}()
					ls.sendBatch(entries)
				}(batch)

				// Log básico de métricas do adaptive batcher
				stats := ls.adaptiveBatcher.GetStats()
				ls.logger.WithFields(logrus.Fields{
					"batch_size":         stats.CurrentBatchSize,
					"flush_delay_ms":     stats.CurrentFlushDelay,
					"throughput_per_sec": stats.ThroughputPerSec,
					"adaptation_count":   stats.AdaptationCount,
				}).Debug("Adaptive batcher stats")
			}
		}
	}
}

