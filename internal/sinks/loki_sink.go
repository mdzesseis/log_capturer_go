package sinks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
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

// LokiSink implementa sink para Grafana Loki
type LokiSink struct {
	config       types.LokiConfig
	logger       *logrus.Logger
	httpClient   *http.Client
	breaker      *circuit.Breaker
	compressor   *compression.HTTPCompressor
	deadLetterQueue *dlq.DeadLetterQueue

	queue        chan types.LogEntry
	batch        []types.LogEntry
	batchMutex   sync.Mutex
	lastSent     time.Time

	// Adaptive batcher (se habilitado)
	adaptiveBatcher *batching.AdaptiveBatcher
	useAdaptiveBatching bool

	ctx          context.Context
	cancel       context.CancelFunc
	isRunning    bool
	mutex        sync.RWMutex

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

// NewLokiSink cria um novo sink para Loki
func NewLokiSink(config types.LokiConfig, logger *logrus.Logger, deadLetterQueue *dlq.DeadLetterQueue) *LokiSink {
	ctx, cancel := context.WithCancel(context.Background())

	// Parse timeout from string
	timeout := 30 * time.Second
	if config.Timeout != "" {
		if t, err := time.ParseDuration(config.Timeout); err == nil {
			timeout = t
		}
	}

	// Configurar HTTP client
	httpClient := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			MaxIdleConns:        10,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     30 * time.Second,
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
		config:          config,
		logger:          logger,
		httpClient:      httpClient,
		breaker:         breaker,
		compressor:      compressor,
		deadLetterQueue: deadLetterQueue,
		queue:           make(chan types.LogEntry, queueSize),
		batch:           make([]types.LogEntry, 0, config.BatchSize),
		ctx:             ctx,
		cancel:          cancel,
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

	// Iniciar adaptive batcher se habilitado
	if ls.useAdaptiveBatching && ls.adaptiveBatcher != nil {
		if err := ls.adaptiveBatcher.Start(); err != nil {
			return fmt.Errorf("failed to start adaptive batcher: %w", err)
		}
		go ls.adaptiveBatchLoop()
	} else {
		// Usar batching tradicional
		go ls.processLoop()
		go ls.flushLoop()
	}

	return nil
}

// Stop para o sink
func (ls *LokiSink) Stop() error {
	ls.mutex.Lock()
	defer ls.mutex.Unlock()

	if !ls.isRunning {
		return nil
	}

	ls.logger.Info("Stopping Loki sink")
	ls.isRunning = false

	// Cancelar contexto
	ls.cancel()

	// Parar adaptive batcher se habilitado
	if ls.useAdaptiveBatching && ls.adaptiveBatcher != nil {
		if err := ls.adaptiveBatcher.Stop(); err != nil {
			ls.logger.WithError(err).Error("Failed to stop adaptive batcher")
		}
	}

	// Flush final
	ls.flushBatch()

	return nil
}

// Send envia logs para o sink com backpressure inteligente
func (ls *LokiSink) Send(ctx context.Context, entries []types.LogEntry) error {
	if !ls.config.Enabled {
		return nil
	}

	for _, entry := range entries {
		if ls.useAdaptiveBatching && ls.adaptiveBatcher != nil {
			// Usar adaptive batcher
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
func (ls *LokiSink) addToBatch(entry types.LogEntry) {
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
	entries := make([]types.LogEntry, len(ls.batch))
	copy(entries, ls.batch)

	// Limpar batch
	ls.batch = ls.batch[:0]

	// Enviar de forma assíncrona
	go ls.sendBatch(entries)
}

// sendBatch envia um batch para o Loki
func (ls *LokiSink) sendBatch(entries []types.LogEntry) {
	startTime := time.Now()

	err := ls.breaker.Execute(func() error {
		return ls.sendToLoki(entries)
	})

	duration := time.Since(startTime)
	metrics.RecordSinkSendDuration("loki", duration)

	if err != nil {
		ls.logger.WithError(err).WithField("entries", len(entries)).Error("Failed to send batch to Loki")
		metrics.RecordLogSent("loki", "error")
		metrics.RecordError("loki_sink", "send_error")

		// Enviar batch inteiro para DLQ após falha
		for _, entry := range entries {
			ls.sendToDLQ(entry, err.Error(), "send_failed", "loki", 1)
		}
	} else {
		ls.logger.WithField("entries", len(entries)).Debug("Batch sent to Loki successfully")
		metrics.RecordLogSent("loki", "success")
		ls.lastSent = time.Now()
	}

	// Atualizar métricas de utilização da fila
	metrics.SetSinkQueueUtilization("loki", ls.GetQueueUtilization())
}

// sendToLoki envia dados para o Loki
func (ls *LokiSink) sendToLoki(entries []types.LogEntry) error {
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

	// Construir URL com push endpoint
	url := ls.config.URL
	if ls.config.PushEndpoint != "" {
		url += ls.config.PushEndpoint
	} else {
		url += "/loki/api/v1/push"
	}

	body := bytes.NewReader(compressionResult.Data)
	req, err := http.NewRequestWithContext(ls.ctx, "POST", url, body)
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

	// Enviar request
	resp, err := ls.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Verificar status e capturar detalhes do erro
	if resp.StatusCode >= 300 {
		// Ler o body da resposta para obter detalhes do erro
		bodyBytes, bodyErr := io.ReadAll(resp.Body)
		if bodyErr != nil {
			return fmt.Errorf("loki returned status %d (failed to read error details: %v)", resp.StatusCode, bodyErr)
		}

		errorBody := string(bodyBytes)
		ls.logger.WithFields(logrus.Fields{
			"status_code": resp.StatusCode,
			"error_body":  errorBody,
			"entries":     len(entries),
		}).Error("Loki request failed with detailed error")

		// Retornar erro mais detalhado
		if resp.StatusCode == 400 {
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

	return nil
}

// groupByStream agrupa entradas por stream
func (ls *LokiSink) groupByStream(entries []types.LogEntry) []LokiStream {
	streamMap := make(map[string]*LokiStream)

	for _, entry := range entries {
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
	// Fazer cópia do map para evitar concurrent access durante JSON marshal
	labelsCopy := make(map[string]string, len(labels))
	for k, v := range labels {
		labelsCopy[k] = v
	}

	// Usar JSON para criar chave determinística
	data, _ := json.Marshal(labelsCopy)
	return string(data)
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
func (ls *LokiSink) sendToDLQ(entry types.LogEntry, errorMsg, errorType, failedSink string, retryCount int) {
	if ls.deadLetterQueue != nil {
		context := map[string]string{
			"sink_type":        "loki",
			"queue_utilization": fmt.Sprintf("%.2f", ls.GetQueueUtilization()),
			"loki_url":         ls.config.URL,
		}

		ls.deadLetterQueue.AddEntry(entry, errorMsg, errorType, failedSink, retryCount, context)
		metrics.RecordError("loki_sink", "dlq_entry")

		ls.logger.WithFields(logrus.Fields{
			"error_type":    errorType,
			"failed_sink":   failedSink,
			"retry_count":   retryCount,
			"source_type":   entry.SourceType,
			"source_id":     entry.SourceID,
		}).Debug("Entry sent to DLQ")
	} else {
		// Se não tiver DLQ, pelo menos registrar o erro
		ls.logger.WithFields(logrus.Fields{
			"error_msg":     errorMsg,
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
				// Enviar batch
				go ls.sendBatch(batch)

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

