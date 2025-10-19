package dispatcher

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"ssw-logs-capture/internal/metrics"
	"ssw-logs-capture/internal/processing"
	"ssw-logs-capture/pkg/backpressure"
	"ssw-logs-capture/pkg/deduplication"
	"ssw-logs-capture/pkg/degradation"
	"ssw-logs-capture/pkg/dlq"
	"ssw-logs-capture/pkg/ratelimit"
	"ssw-logs-capture/pkg/types"

	"github.com/sirupsen/logrus"
)

// Dispatcher gerencia roteamento de logs para sinks
type Dispatcher struct {
	config               DispatcherConfig
	logger               *logrus.Logger
	processor            *processing.LogProcessor
	deduplicationManager *deduplication.DeduplicationManager
	deadLetterQueue      *dlq.DeadLetterQueue
	backpressureManager  *backpressure.Manager
	degradationManager   *degradation.Manager
	rateLimiter          *ratelimit.AdaptiveRateLimiter

	sinks       []types.Sink
	queue       chan dispatchItem
	stats       types.DispatcherStats
	statsMutex  sync.RWMutex

	ctx       context.Context
	cancel    context.CancelFunc
	isRunning bool
	mutex     sync.RWMutex
}

// DispatcherConfig configuração do dispatcher
type DispatcherConfig struct {
	QueueSize          int           `yaml:"queue_size"`
	Workers            int           `yaml:"workers"`
	BatchSize          int           `yaml:"batch_size"`
	BatchTimeout       time.Duration `yaml:"batch_timeout"`
	MaxRetries         int           `yaml:"max_retries"`
	RetryDelay         time.Duration `yaml:"retry_delay"`
	TimestampTolerance time.Duration `yaml:"timestamp_tolerance"`

	// Configuração de deduplicação
	DeduplicationEnabled bool                      `yaml:"deduplication_enabled"`
	DeduplicationConfig  deduplication.Config     `yaml:"deduplication_config"`

	// Configuração de Dead Letter Queue
	DLQEnabled bool        `yaml:"dlq_enabled"`
	DLQConfig  dlq.Config  `yaml:"dlq_config"`

	// Configuração de Backpressure
	BackpressureEnabled bool               `yaml:"backpressure_enabled"`
	BackpressureConfig  backpressure.Config `yaml:"backpressure_config"`

	// Configuração de Degradation
	DegradationEnabled bool               `yaml:"degradation_enabled"`
	DegradationConfig  degradation.Config `yaml:"degradation_config"`

	// Configuração de Rate Limiting
	RateLimitEnabled bool              `yaml:"rate_limit_enabled"`
	RateLimitConfig  ratelimit.Config  `yaml:"rate_limit_config"`
}

// dispatchItem item na fila de dispatch
type dispatchItem struct {
	Entry     types.LogEntry
	Timestamp time.Time
	Retries   int
}

// NewDispatcher cria um novo dispatcher
func NewDispatcher(config DispatcherConfig, processor *processing.LogProcessor, logger *logrus.Logger) *Dispatcher {
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

	return &Dispatcher{
		config:               config,
		logger:               logger,
		processor:            processor,
		deduplicationManager: deduplicationManager,
		deadLetterQueue:      deadLetterQueue,
		backpressureManager:  backpressureManager,
		degradationManager:   degradationManager,
		rateLimiter:          rateLimiter,
		sinks:                make([]types.Sink, 0),
		queue:                make(chan dispatchItem, config.QueueSize),
		stats: types.DispatcherStats{
			SinkDistribution: make(map[string]int64),
		},
		ctx:    ctx,
		cancel: cancel,
	}
}

// AddSink adiciona um sink ao dispatcher
func (d *Dispatcher) AddSink(sink types.Sink) {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	d.sinks = append(d.sinks, sink)
	d.logger.WithField("sink_count", len(d.sinks)).Info("Sink added to dispatcher")
}

// Start inicia o dispatcher
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

		go func() {
			if err := d.backpressureManager.Start(d.ctx); err != nil {
				d.logger.WithError(err).Error("Backpressure manager stopped with error")
			}
		}()
	}

	// Iniciar Rate Limiter se habilitado
	if d.config.RateLimitEnabled && d.rateLimiter != nil {
		d.logger.Info("Rate limiter enabled")
	}


	// Iniciar workers
	for i := 0; i < d.config.Workers; i++ {
		go d.worker(i)
	}

	// Iniciar stats updater
	go d.statsUpdater()

	return nil
}

// Stop para o dispatcher
func (d *Dispatcher) Stop() error {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	if !d.isRunning {
		return nil
	}

	d.logger.Info("Stopping dispatcher")
	d.isRunning = false

	// Parar deduplication manager se habilitado
	if d.config.DeduplicationEnabled && d.deduplicationManager != nil {
		d.deduplicationManager.Stop()
	}

	// Parar Dead Letter Queue se habilitado
	if d.config.DLQEnabled && d.deadLetterQueue != nil {
		d.deadLetterQueue.Stop()
	}

	// Cancelar contexto
	d.cancel()

	// Processar itens restantes na fila
	d.drainQueue()

	return nil
}

// Handle processa uma entrada de log
func (d *Dispatcher) Handle(ctx context.Context, sourceType, sourceID, message string, labels map[string]string) error {
	if !d.isRunning {
		return fmt.Errorf("dispatcher not running")
	}

	// Aplicar rate limiting se habilitado
	if d.config.RateLimitEnabled && d.rateLimiter != nil {
		if !d.rateLimiter.Allow() {
			d.statsMutex.Lock()
			d.stats.Throttled++
			d.statsMutex.Unlock()
			return fmt.Errorf("rate limit exceeded")
		}
	}

	// Verificar backpressure e aplicar controle de fluxo
	if d.config.BackpressureEnabled && d.backpressureManager != nil {
		// Atualizar métricas do sistema para backpressure
		d.updateBackpressureMetrics()

		// Verificar se deve rejeitar por sobrecarga crítica
		if d.backpressureManager.ShouldReject() {
			d.statsMutex.Lock()
			d.stats.Throttled++
			d.statsMutex.Unlock()
			return fmt.Errorf("system overloaded - rejecting log entry")
		}

		// Verificar se deve aplicar throttling
		if d.backpressureManager.ShouldThrottle() {
			factor := d.backpressureManager.GetFactor()
			// Aplicar throttling probabilístico baseado no fator
			if float64(time.Now().UnixNano()%1000)/1000.0 > factor {
				d.statsMutex.Lock()
				d.stats.Throttled++
				d.statsMutex.Unlock()

				// Em vez de descartar, colocar numa fila de baixa prioridade
				// Para manter integridade dos dados
				return d.handleLowPriorityEntry(ctx, sourceType, sourceID, message, labels)
			}
		}
	}

	// Criar entrada de log com cópia segura dos labels
	labelsCopy := make(map[string]string, len(labels))
	for k, v := range labels {
		labelsCopy[k] = v
	}

	entry := types.LogEntry{
		Timestamp:   time.Now(),
		Message:     message,
		SourceType:  sourceType,
		SourceID:    sourceID,
		Labels:      labelsCopy,
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
				d.statsMutex.Lock()
				d.stats.DuplicatesDetected++
				d.statsMutex.Unlock()

				metrics.RecordLogProcessed(sourceType, sourceID, "duplicate_filtered")
				return nil // Não processar log duplicado
			}
		}
	}

	// Validar timestamp (detectar timestamps muito antigos)
	now := time.Now()
	if entry.Timestamp.Before(now.Add(-d.config.TimestampTolerance)) {
		d.logger.WithFields(logrus.Fields{
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
					"source_type": sourceType,
					"source_id":   sourceID,
				}).Error("Failed to process log entry")
				metrics.RecordError("dispatcher", "processing_error")
				return err
			}
			if processedEntry != nil {
				entry = *processedEntry
			}
		}
	}

	// Adicionar à fila
	item := dispatchItem{
		Entry:     entry,
		Timestamp: time.Now(),
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
func (d *Dispatcher) GetStats() types.DispatcherStats {
	d.statsMutex.RLock()
	defer d.statsMutex.RUnlock()

	// Criar cópia das estatísticas - copia tudo dentro do lock para evitar race condition
	stats := d.stats
	// Criar nova instância do map para evitar referência compartilhada
	stats.SinkDistribution = make(map[string]int64, len(d.stats.SinkDistribution))
	for k, v := range d.stats.SinkDistribution {
		stats.SinkDistribution[k] = v
	}

	return stats
}

// sendToDLQ envia entrada para Dead Letter Queue
func (d *Dispatcher) sendToDLQ(entry types.LogEntry, errorMsg, errorType, failedSink string, retryCount int) {
	if d.config.DLQEnabled && d.deadLetterQueue != nil {
		context := map[string]string{
			"worker_id": "dispatcher",
			"timestamp": time.Now().Format(time.RFC3339),
		}
		d.deadLetterQueue.AddEntry(entry, errorMsg, errorType, failedSink, retryCount, context)

		d.logger.WithFields(logrus.Fields{
			"source_type":  entry.SourceType,
			"source_id":    entry.SourceID,
			"failed_sink":  failedSink,
			"error_type":   errorType,
			"retry_count":  retryCount,
		}).Debug("Entry sent to DLQ")
	}
}

// worker processa itens da fila
func (d *Dispatcher) worker(workerID int) {
	logger := d.logger.WithField("worker_id", workerID)
	logger.Info("Dispatcher worker started")

	batch := make([]dispatchItem, 0, d.config.BatchSize)
	timer := time.NewTimer(d.config.BatchTimeout)
	timer.Stop()

	defer func() {
		if !timer.Stop() {
			<-timer.C
		}
		// Processar batch final se houver
		if len(batch) > 0 {
			d.processBatch(batch, logger)
		}
		logger.Info("Dispatcher worker stopped")
	}()

	for {
		select {
		case <-d.ctx.Done():
			return

		case item := <-d.queue:
			batch = append(batch, item)

			// Se é o primeiro item do batch, iniciar timer
			if len(batch) == 1 {
				timer.Reset(d.config.BatchTimeout)
			}

			// Processar batch se estiver cheio
			if len(batch) >= d.config.BatchSize {
				if !timer.Stop() {
					<-timer.C
				}
				d.processBatch(batch, logger)
				batch = batch[:0]
			}

		case <-timer.C:
			// Timeout do batch
			if len(batch) > 0 {
				d.processBatch(batch, logger)
				batch = batch[:0]
			}
		}
	}
}

// processBatch processa um batch de itens
func (d *Dispatcher) processBatch(batch []dispatchItem, logger *logrus.Entry) {
	if len(batch) == 0 {
		return
	}

	startTime := time.Now()

	// Converter para slice de LogEntry
	entries := make([]types.LogEntry, len(batch))
	for i, item := range batch {
		entries[i] = item.Entry
	}

	// Enviar para todos os sinks
	successCount := 0
	for _, sink := range d.sinks {
		if !sink.IsHealthy() {
			logger.Warn("Skipping unhealthy sink")
			continue
		}

		// Fazer cópia profunda dos entries para evitar race conditions entre sinks
		entriesCopy := make([]types.LogEntry, len(entries))
		for i, entry := range entries {
			entriesCopy[i] = types.LogEntry{
				Timestamp:   entry.Timestamp,
				Message:     entry.Message,
				SourceType:  entry.SourceType,
				SourceID:    entry.SourceID,
				Level:       entry.Level,
				Labels:      make(map[string]string, len(entry.Labels)),
				Fields:      make(map[string]interface{}, len(entry.Fields)),
				ProcessedAt: entry.ProcessedAt,
			}

			// Copiar labels (já são seguros por serem cópias desde a criação)
			for k, v := range entry.Labels {
				entriesCopy[i].Labels[k] = v
			}

			// Copiar fields
			for k, v := range entry.Fields {
				entriesCopy[i].Fields[k] = v
			}
		}

		ctx, cancel := context.WithTimeout(d.ctx, 30*time.Second)
		err := sink.Send(ctx, entriesCopy)
		cancel()

		if err != nil {
			logger.WithError(err).Error("Failed to send batch to sink")
			d.updateStats(func(stats *types.DispatcherStats) {
				stats.ErrorCount++
			})

			// Verificar se deve tentar novamente
			d.handleFailedBatch(batch, err)
		} else {
			successCount++
			// Atualizar distribuição por sink
			sinkType := d.getSinkType(sink)
			d.updateStats(func(stats *types.DispatcherStats) {
				stats.SinkDistribution[sinkType] += int64(len(entries))
			})
		}
	}

	duration := time.Since(startTime)
	metrics.RecordProcessingDuration("dispatcher", "batch_processing", duration)

	logger.WithFields(logrus.Fields{
		"batch_size":     len(batch),
		"success_count":  successCount,
		"duration_ms":    duration.Milliseconds(),
	}).Debug("Batch processed")
}

// handleFailedBatch trata batch que falhou
func (d *Dispatcher) handleFailedBatch(batch []dispatchItem, err error) {
	for _, item := range batch {
		if item.Retries < d.config.MaxRetries {
			// Reagendar item
			item.Retries++
			go func(item dispatchItem, ctx context.Context) {
				// Usar select com context para permitir cancelamento
				select {
				case <-time.After(d.config.RetryDelay * time.Duration(item.Retries)):
					// Tentar reagendar com context
					select {
					case d.queue <- item:
						// Reagendado com sucesso
					case <-ctx.Done():
						// Context cancelado
						return
					default:
						// Fila cheia, descartar
						d.logger.Warn("Failed to reschedule failed item, queue full")
					}
				case <-ctx.Done():
					// Context cancelado durante espera
					return
				}
			}(item, d.ctx)
		} else {
			// Max retries reached, enviar para DLQ
			d.logger.WithFields(logrus.Fields{
				"source_type": item.Entry.SourceType,
				"source_id":   item.Entry.SourceID,
				"retries":     item.Retries,
				"error":       err,
			}).Error("Max retries reached for log entry, sending to DLQ")

			// Enviar para Dead Letter Queue
			d.sendToDLQ(item.Entry, err.Error(), "max_retries_exceeded", "all_sinks", item.Retries)
		}
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
func (d *Dispatcher) statsUpdater() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.updateStats(func(stats *types.DispatcherStats) {
				stats.QueueSize = len(d.queue)
			})

			// Atualizar métricas
			stats := d.GetStats()
			metrics.SetQueueSize("dispatcher", "main", stats.QueueSize)
		}
	}
}

// updateStats atualiza estatísticas de forma thread-safe
func (d *Dispatcher) updateStats(fn func(*types.DispatcherStats)) {
	d.statsMutex.Lock()
	defer d.statsMutex.Unlock()
	fn(&d.stats)
}

// updateTimestampWarnings incrementa contador de warnings de timestamp
func (d *Dispatcher) updateTimestampWarnings() {
	// Implementar contador thread-safe se necessário
	metrics.RecordError("dispatcher", "timestamp_drift")
}

// updateBackpressureMetrics atualiza métricas para o sistema de backpressure
func (d *Dispatcher) updateBackpressureMetrics() {
	if d.backpressureManager == nil {
		return
	}

	// Calcular utilização da fila
	queueUtilization := float64(len(d.queue)) / float64(cap(d.queue))

	// Coletar métricas do sistema
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// Calcular utilização de memória (assumindo 512MB como limite padrão)
	memoryUtilization := float64(memStats.Alloc) / (512 * 1024 * 1024)
	if memoryUtilization > 1.0 {
		memoryUtilization = 1.0
	}

	// Para CPU e IO, usamos valores simplificados baseados na carga da fila
	cpuUtilization := queueUtilization * 0.8    // Estimativa baseada na fila
	ioUtilization := queueUtilization * 0.6     // Estimativa baseada na fila

	// Taxa de erro baseada nas estatísticas
	d.statsMutex.RLock()
	totalProcessed := d.stats.TotalProcessed
	errorCount := d.stats.ErrorCount
	d.statsMutex.RUnlock()

	var errorRate float64
	if totalProcessed > 0 {
		errorRate = float64(errorCount) / float64(totalProcessed)
	}

	// Atualizar métricas no manager
	d.backpressureManager.UpdateMetrics(backpressure.Metrics{
		QueueUtilization:  queueUtilization,
		MemoryUtilization: memoryUtilization,
		CPUUtilization:    cpuUtilization,
		IOUtilization:     ioUtilization,
		ErrorRate:         errorRate,
	})
}

// handleLowPriorityEntry processa uma entrada de baixa prioridade sem descartar
func (d *Dispatcher) handleLowPriorityEntry(ctx context.Context, sourceType, sourceID, message string, labels map[string]string) error {
	// Em vez de descartar, envia para Dead Letter Queue se disponível
	if d.config.DLQEnabled && d.deadLetterQueue != nil {
		entry := types.LogEntry{
			Timestamp:   time.Now(),
			Message:     message,
			SourceType:  sourceType,
			SourceID:    sourceID,
			Labels:      labels,
			ProcessedAt: time.Now(),
		}

		// Adicionar tag indicando que foi throttled
		if entry.Labels == nil {
			entry.Labels = make(map[string]string)
		}
		entry.Labels["throttle_reason"] = "backpressure_low_priority"

		d.deadLetterQueue.AddEntry(entry, "throttled due to backpressure", "backpressure", "dispatcher", 0, map[string]string{
			"throttle_level": d.backpressureManager.GetLevel().String(),
		})
		return nil
	}

	// Se não tiver DLQ, logar warning mas não descartar
	d.logger.WithFields(logrus.Fields{
		"source_type": sourceType,
		"source_id":   sourceID,
		"level":       d.backpressureManager.GetLevel().String(),
	}).Warn("Log entry throttled due to backpressure, but no DLQ available")

	// Ainda assim tenta processar, mas com delay
	time.Sleep(10 * time.Millisecond)
	return d.Handle(ctx, sourceType, sourceID, message, labels)
}

// GetDLQ retorna a instância da Dead Letter Queue
func (d *Dispatcher) GetDLQ() *dlq.DeadLetterQueue {
	return d.deadLetterQueue
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
func (d *Dispatcher) reprocessLogEntry(entry types.LogEntry, originalSink string) error {
	d.logger.WithFields(logrus.Fields{
		"source_type":    entry.SourceType,
		"source_id":      entry.SourceID,
		"original_sink":  originalSink,
		"timestamp":      entry.Timestamp,
	}).Debug("Attempting to reprocess DLQ entry")

	// Criar contexto com timeout para reprocessamento
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
	if err := targetSink.Send(ctx, []types.LogEntry{entry}); err != nil {
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

	// Mapear nomes de sink conhecidos
	sinkNameMap := map[string]string{
		"loki":           "loki",
		"local_file":     "local_file",
		"elasticsearch":  "elasticsearch",
		"splunk":         "splunk",
	}

	// Se o nome não estiver no mapa, usar como está
	normalizedName := sinkNameMap[sinkName]
	if normalizedName == "" {
		normalizedName = sinkName
	}

	// Para simplificar, retornamos o primeiro sink healthy que encontramos
	// Em uma implementação mais sofisticada, poderíamos tag os sinks com nomes
	for _, sink := range d.sinks {
		if sink.IsHealthy() {
			return sink
		}
	}

	return nil
}

// reprocessToAnySink tenta reprocessar para qualquer sink healthy disponível
func (d *Dispatcher) reprocessToAnySink(ctx context.Context, entry types.LogEntry) error {
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
		if err := sink.Send(ctx, []types.LogEntry{entry}); err != nil {
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