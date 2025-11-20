package sinks

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ssw-logs-capture/internal/metrics"
	"ssw-logs-capture/pkg/circuit"
	"ssw-logs-capture/pkg/compression"
	"ssw-logs-capture/pkg/dlq"
	"ssw-logs-capture/pkg/types"

	"github.com/IBM/sarama"
	"github.com/sirupsen/logrus"
)

// KafkaSink implementa sink para Apache Kafka
type KafkaSink struct {
	config          types.KafkaSinkConfig
	logger          *logrus.Logger
	producer        sarama.AsyncProducer
	breaker         *circuit.Breaker
	compressor      *compression.HTTPCompressor
	deadLetterQueue *dlq.DeadLetterQueue
	enhancedMetrics *metrics.EnhancedMetrics

	queue      chan *types.LogEntry
	batch      []*types.LogEntry
	batchMutex sync.Mutex
	lastSent   time.Time

	ctx       context.Context
	cancel    context.CancelFunc
	isRunning bool
	mutex     sync.RWMutex

	// Goroutine lifecycle management
	loopWg sync.WaitGroup // Tracks main loop goroutines
	sendWg sync.WaitGroup // Tracks send goroutines

	// Métricas de backpressure
	backpressureCount int64
	droppedCount      int64
	sentCount         int64
	errorCount        int64

	// Partitioner strategy
	partitioner sarama.Partitioner
}

// NewKafkaSink cria um novo sink para Kafka
func NewKafkaSink(config types.KafkaSinkConfig, logger *logrus.Logger, deadLetterQueue *dlq.DeadLetterQueue, enhancedMetrics *metrics.EnhancedMetrics) (*KafkaSink, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Validar configuração
	if len(config.Brokers) == 0 {
		cancel()
		return nil, fmt.Errorf("kafka sink: no brokers configured")
	}
	if config.Topic == "" {
		cancel()
		return nil, fmt.Errorf("kafka sink: no topic configured")
	}

	// Configurar Sarama
	saramaConfig := sarama.NewConfig()
	saramaConfig.Producer.Return.Successes = true
	saramaConfig.Producer.Return.Errors = true
	saramaConfig.Producer.RequiredAcks = sarama.RequiredAcks(config.RequiredAcks)

	// Configurar compressão
	switch strings.ToLower(config.Compression) {
	case "gzip":
		saramaConfig.Producer.Compression = sarama.CompressionGZIP
	case "snappy":
		saramaConfig.Producer.Compression = sarama.CompressionSnappy
	case "lz4":
		saramaConfig.Producer.Compression = sarama.CompressionLZ4
	case "zstd":
		saramaConfig.Producer.Compression = sarama.CompressionZSTD
	default:
		saramaConfig.Producer.Compression = sarama.CompressionNone
	}

	// Configurar batching
	if config.BatchSize > 0 {
		saramaConfig.Producer.Flush.Messages = config.BatchSize
	}
	if config.BatchTimeout != "" {
		if timeout, err := time.ParseDuration(config.BatchTimeout); err == nil {
			saramaConfig.Producer.Flush.Frequency = timeout
		}
	}

	// Configurar message size
	if config.MaxMessageBytes > 0 {
		saramaConfig.Producer.MaxMessageBytes = config.MaxMessageBytes
	}

	// Configurar retry
	if config.RetryMax > 0 {
		saramaConfig.Producer.Retry.Max = config.RetryMax
	}

	// Configurar timeout
	if config.Timeout != "" {
		if timeout, err := time.ParseDuration(config.Timeout); err == nil {
			saramaConfig.Net.DialTimeout = timeout
			saramaConfig.Net.ReadTimeout = timeout
			saramaConfig.Net.WriteTimeout = timeout
		}
	}

	// Configurar autenticação SASL
	if config.Auth.Enabled {
		saramaConfig.Net.SASL.Enable = true
		saramaConfig.Net.SASL.User = config.Auth.Username
		saramaConfig.Net.SASL.Password = config.Auth.Password

		switch strings.ToUpper(config.Auth.Mechanism) {
		case "PLAIN":
			saramaConfig.Net.SASL.Mechanism = sarama.SASLTypePlaintext
		case "SCRAM-SHA-256":
			saramaConfig.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA256
			saramaConfig.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient {
				return &XDGSCRAMClient{HashGeneratorFcn: SHA256}
			}
		case "SCRAM-SHA-512":
			saramaConfig.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA512
			saramaConfig.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient {
				return &XDGSCRAMClient{HashGeneratorFcn: SHA512}
			}
		}
	}

	// Configurar TLS
	if config.TLS.Enabled {
		saramaConfig.Net.TLS.Enable = true
		// TODO: Load TLS certificates from config.TLS.CAFile, CertFile, KeyFile
	}

	// Configurar partitioner
	switch strings.ToLower(config.Partitioning.Strategy) {
	case "hash":
		saramaConfig.Producer.Partitioner = sarama.NewHashPartitioner
	case "round-robin":
		saramaConfig.Producer.Partitioner = sarama.NewRoundRobinPartitioner
	case "random":
		saramaConfig.Producer.Partitioner = sarama.NewRandomPartitioner
	default:
		saramaConfig.Producer.Partitioner = sarama.NewHashPartitioner
	}

	// Criar producer
	producer, err := sarama.NewAsyncProducer(config.Brokers, saramaConfig)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("kafka sink: failed to create producer: %w", err)
	}

	logger.WithFields(logrus.Fields{
		"brokers":     config.Brokers,
		"topic":       config.Topic,
		"compression": config.Compression,
		"batch_size":  config.BatchSize,
		"queue_size":  config.QueueSize,
	}).Info("Kafka sink initialized")

	// Configurar HTTP compressor (para internal use)
	compressionConfig := compression.Config{
		DefaultAlgorithm: compression.AlgorithmSnappy,
		AdaptiveEnabled:  false,
		MinBytes:         512,
		Level:            6,
		PoolSize:         5,
	}
	compressor := compression.NewHTTPCompressor(compressionConfig, logger)

	// Configurar circuit breaker
	breaker := circuit.NewBreaker(circuit.BreakerConfig{
		Name:             "kafka_sink",
		FailureThreshold: 10,
		SuccessThreshold: 2,
		Timeout:          60 * time.Second,
	}, logger)

	// Queue size
	queueSize := config.QueueSize
	if queueSize <= 0 {
		queueSize = 25000 // Default
	}

	sink := &KafkaSink{
		config:          config,
		logger:          logger,
		producer:        producer,
		breaker:         breaker,
		compressor:      compressor,
		deadLetterQueue: deadLetterQueue,
		enhancedMetrics: enhancedMetrics,
		queue:           make(chan *types.LogEntry, queueSize),
		ctx:             ctx,
		cancel:          cancel,
	}

	return sink, nil
}

// Start inicia o sink Kafka
func (ks *KafkaSink) Start(ctx context.Context) error {
	if !ks.config.Enabled {
		ks.logger.Info("Kafka sink disabled")
		return nil
	}

	ks.mutex.Lock()
	if ks.isRunning {
		ks.mutex.Unlock()
		return fmt.Errorf("kafka sink already running")
	}
	ks.isRunning = true
	ks.lastSent = time.Now()
	ks.mutex.Unlock()

	ks.logger.Info("Starting Kafka sink")

	// Start goroutine para processar fila
	ks.loopWg.Add(1)
	go ks.processLoop()

	// Start goroutine para flush periódico
	ks.loopWg.Add(1)
	go ks.flushLoop()

	// Start goroutine para lidar com producer responses
	ks.loopWg.Add(1)
	go ks.handleProducerResponses()

	ks.logger.Info("Kafka sink started successfully")
	return nil
}

// Stop para o sink Kafka
func (ks *KafkaSink) Stop() error {
	ks.mutex.Lock()
	if !ks.isRunning {
		ks.mutex.Unlock()
		return nil
	}
	ks.isRunning = false
	ks.mutex.Unlock()

	ks.logger.Info("Stopping Kafka sink")

	// Cancel context para parar goroutines
	ks.cancel()

	// Aguarda goroutines pararem
	ks.loopWg.Wait()

	// Flush batch final
	ks.flushBatch()

	// Aguarda sends pendentes
	ks.sendWg.Wait()

	// Close producer
	if err := ks.producer.Close(); err != nil {
		ks.logger.WithError(err).Error("Error closing Kafka producer")
	}

	ks.logger.WithFields(logrus.Fields{
		"sent":    atomic.LoadInt64(&ks.sentCount),
		"errors":  atomic.LoadInt64(&ks.errorCount),
		"dropped": atomic.LoadInt64(&ks.droppedCount),
	}).Info("Kafka sink stopped")

	return nil
}

// Send envia uma log entry para o Kafka
func (ks *KafkaSink) Send(ctx context.Context, entries []types.LogEntry) error {
	if !ks.config.Enabled {
		return nil
	}

	for i := range entries {
		entry := &entries[i]
		select {
		case ks.queue <- entry:
			// Successfully queued
		case <-ctx.Done():
			return ctx.Err()
		default:
			// Queue full - backpressure
			atomic.AddInt64(&ks.backpressureCount, 1)
			metrics.KafkaBackpressureTotal.WithLabelValues("kafka_sink", "warning").Inc()

			// Check backpressure thresholds
			queueUsage := float64(len(ks.queue)) / float64(cap(ks.queue))

			if queueUsage >= ks.config.BackpressureConfig.QueueEmergencyThreshold {
				// Emergency - send to DLQ
				atomic.AddInt64(&ks.droppedCount, 1)
				metrics.KafkaBackpressureTotal.WithLabelValues("kafka_sink", "emergency").Inc()
				if ks.deadLetterQueue != nil && ks.config.DLQConfig.SendOnError {
					ks.deadLetterQueue.AddEntry(entry, "kafka_queue_full", "backpressure", "kafka_sink", 0, nil)
					metrics.KafkaDLQMessagesTotal.WithLabelValues(ks.config.Topic, "queue_full").Inc()
				}
				ks.logger.Warn("Kafka sink queue full - dropping entry to DLQ")
			} else {
				// Try again with timeout
				select {
				case ks.queue <- entry:
					// Successfully queued after retry
				case <-time.After(100 * time.Millisecond):
					// Timeout - send to DLQ
					atomic.AddInt64(&ks.droppedCount, 1)
					if ks.deadLetterQueue != nil && ks.config.DLQConfig.SendOnError {
						ks.deadLetterQueue.AddEntry(entry, "kafka_queue_timeout", "timeout", "kafka_sink", 0, nil)
						metrics.KafkaDLQMessagesTotal.WithLabelValues(ks.config.Topic, "queue_timeout").Inc()
					}
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
	}

	// Update queue metrics
	metrics.KafkaQueueSize.WithLabelValues("kafka_sink").Set(float64(len(ks.queue)))
	metrics.KafkaQueueUtilization.WithLabelValues("kafka_sink").Set(float64(len(ks.queue)) / float64(cap(ks.queue)))

	return nil
}

// processLoop processa entries da fila
func (ks *KafkaSink) processLoop() {
	defer ks.loopWg.Done()

	batchTimeout := 5 * time.Second
	if ks.config.BatchTimeout != "" {
		if timeout, err := time.ParseDuration(ks.config.BatchTimeout); err == nil {
			batchTimeout = timeout
		}
	}

	ticker := time.NewTicker(batchTimeout)
	defer ticker.Stop()

	for {
		select {
		case <-ks.ctx.Done():
			return

		case entry := <-ks.queue:
			ks.batchMutex.Lock()
			ks.batch = append(ks.batch, entry)
			shouldFlush := len(ks.batch) >= ks.config.BatchSize
			ks.batchMutex.Unlock()

			if shouldFlush {
				ks.flushBatch()
			}

		case <-ticker.C:
			ks.flushBatch()
		}
	}
}

// flushLoop garante flush periódico
func (ks *KafkaSink) flushLoop() {
	defer ks.loopWg.Done()

	flushInterval := 10 * time.Second
	if ks.config.BatchTimeout != "" {
		if interval, err := time.ParseDuration(ks.config.BatchTimeout); err == nil {
			flushInterval = interval * 2
		}
	}

	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ks.ctx.Done():
			return
		case <-ticker.C:
			ks.batchMutex.Lock()
			shouldFlush := len(ks.batch) > 0 && time.Since(ks.lastSent) > flushInterval
			ks.batchMutex.Unlock()

			if shouldFlush {
				ks.flushBatch()
			}
		}
	}
}

// flushBatch envia batch atual para Kafka
func (ks *KafkaSink) flushBatch() {
	ks.batchMutex.Lock()
	if len(ks.batch) == 0 {
		ks.batchMutex.Unlock()
		return
	}

	batch := ks.batch
	ks.batch = make([]*types.LogEntry, 0, ks.config.BatchSize)
	ks.lastSent = time.Now()
	ks.batchMutex.Unlock()

	// Send batch via circuit breaker
	err := ks.breaker.Execute(func() error {
		return ks.sendBatch(batch)
	})

	if err != nil {
		ks.logger.WithError(err).WithField("batch_size", len(batch)).Error("Failed to send batch to Kafka")
		atomic.AddInt64(&ks.errorCount, int64(len(batch)))

		// Send to DLQ if configured
		if ks.deadLetterQueue != nil && ks.config.DLQConfig.SendOnError {
			for i := range batch {
				ks.deadLetterQueue.AddEntry(batch[i], fmt.Sprintf("kafka_send_error: %v", err), "send_error", "kafka_sink", 0, nil)
			}
		}
	}
}

// sendBatch envia batch de entries para Kafka
func (ks *KafkaSink) sendBatch(entries []*types.LogEntry) error {
	if len(entries) == 0 {
		return nil
	}

	startTime := time.Now()
	successCount := 0
	errorCount := 0
	totalMessageSize := 0

	// Send each entry to Kafka producer
	for i := range entries {
		entry := entries[i]

		// Determine topic (pode ser customizado baseado em labels)
		topic := ks.determineTopic(entry)

		// Determine partition key for consistent partitioning
		partitionKey := ks.determinePartitionKey(entry)

		// Serialize entry to JSON
		value, err := json.Marshal(entry)
		if err != nil {
			ks.logger.WithError(err).Error("Failed to marshal entry to JSON")
			errorCount++
			metrics.KafkaProducerErrorsTotal.WithLabelValues(topic, "marshal_error").Inc()
			if ks.deadLetterQueue != nil {
				ks.deadLetterQueue.AddEntry(entry, fmt.Sprintf("marshal_error: %v", err), "marshal_error", "kafka_sink", 0, nil)
				metrics.KafkaDLQMessagesTotal.WithLabelValues(topic, "marshal_error").Inc()
			}
			continue
		}

		// Track message size
		messageSize := len(value)
		totalMessageSize += messageSize
		metrics.KafkaMessageSizeBytes.WithLabelValues(topic).Observe(float64(messageSize))

		// Create Kafka message
		msg := &sarama.ProducerMessage{
			Topic: topic,
			Key:   sarama.StringEncoder(partitionKey),
			Value: sarama.ByteEncoder(value),
		}

		// Send to producer (async)
		ks.producer.Input() <- msg
		successCount++
		metrics.KafkaMessagesProducedTotal.WithLabelValues(topic, "sent").Inc()
	}

	duration := time.Since(startTime)

	// Update metrics
	atomic.AddInt64(&ks.sentCount, int64(successCount))
	atomic.AddInt64(&ks.errorCount, int64(errorCount))

	// Update Kafka-specific batch metrics
	metrics.KafkaBatchSize.WithLabelValues(ks.config.Topic).Observe(float64(len(entries)))
	metrics.KafkaBatchSendDuration.WithLabelValues(ks.config.Topic).Observe(duration.Seconds())

	// Update queue metrics after send
	metrics.KafkaQueueSize.WithLabelValues("kafka_sink").Set(float64(len(ks.queue)))
	metrics.KafkaQueueUtilization.WithLabelValues("kafka_sink").Set(float64(len(ks.queue)) / float64(cap(ks.queue)))

	// Update circuit breaker state metric
	cbState := 0.0
	switch ks.breaker.State() {
	case "closed":
		cbState = 0.0
	case "half-open":
		cbState = 1.0
	case "open":
		cbState = 2.0
	}
	metrics.KafkaCircuitBreakerState.WithLabelValues("kafka_sink").Set(cbState)

	// TODO: Implement EnhancedMetrics methods (RecordLogsSent, RecordBatchDuration) in Phase 7
	// if ks.enhancedMetrics != nil {
	// 	ks.enhancedMetrics.RecordLogsSent("kafka", "success", float64(successCount))
	// 	if errorCount > 0 {
	// 		ks.enhancedMetrics.RecordLogsSent("kafka", "error", float64(errorCount))
	// 	}
	// 	ks.enhancedMetrics.RecordBatchDuration("kafka", duration.Seconds())
	// }

	metrics.LogsSentTotal.WithLabelValues("kafka", "success").Add(float64(successCount))
	if errorCount > 0 {
		metrics.LogsSentTotal.WithLabelValues("kafka", "error").Add(float64(errorCount))
		metrics.KafkaProducerErrorsTotal.WithLabelValues(ks.config.Topic, "batch_error").Add(float64(errorCount))
	}

	ks.logger.WithFields(logrus.Fields{
		"batch_size":  len(entries),
		"success":     successCount,
		"errors":      errorCount,
		"duration_ms": duration.Milliseconds(),
	}).Debug("Kafka batch sent")

	if errorCount > 0 {
		return fmt.Errorf("kafka sink: %d/%d entries failed", errorCount, len(entries))
	}

	return nil
}

// handleProducerResponses lida com successes e errors do producer
func (ks *KafkaSink) handleProducerResponses() {
	defer ks.loopWg.Done()

	for {
		select {
		case <-ks.ctx.Done():
			return

		case success := <-ks.producer.Successes():
			if success != nil {
				ks.logger.WithFields(logrus.Fields{
					"topic":     success.Topic,
					"partition": success.Partition,
					"offset":    success.Offset,
				}).Trace("Message delivered to Kafka")

				// Track successful message delivery and partition distribution
				metrics.KafkaMessagesProducedTotal.WithLabelValues(success.Topic, "delivered").Inc()
				metrics.KafkaPartitionMessages.WithLabelValues(success.Topic, fmt.Sprintf("%d", success.Partition)).Inc()
			}

		case err := <-ks.producer.Errors():
			if err != nil {
				ks.logger.WithError(err.Err).WithFields(logrus.Fields{
					"topic": err.Msg.Topic,
				}).Error("Failed to produce message to Kafka")

				atomic.AddInt64(&ks.errorCount, 1)

				// Track producer errors with topic and error type
				metrics.KafkaMessagesProducedTotal.WithLabelValues(err.Msg.Topic, "failed").Inc()
				metrics.KafkaProducerErrorsTotal.WithLabelValues(err.Msg.Topic, "produce_error").Inc()

				// TODO: Implement EnhancedMetrics.RecordLogsSent in Phase 7
				// if ks.enhancedMetrics != nil {
				// 	ks.enhancedMetrics.RecordLogsSent("kafka", "error", 1)
				// }
				metrics.ErrorsTotal.WithLabelValues("kafka_sink", "produce_error").Inc()
			}
		}
	}
}

// determineTopic determina o tópico Kafka baseado em entry labels
func (ks *KafkaSink) determineTopic(entry *types.LogEntry) string {
	// Check for priority-based routing
	if level, ok := entry.Labels["level"]; ok {
		switch strings.ToLower(level) {
		case "error", "fatal", "critical":
			return "logs-high-priority"
		case "debug", "trace":
			return "logs-low-priority"
		}
	}

	// Check for custom topic label
	if customTopic, ok := entry.Labels["kafka_topic"]; ok {
		return customTopic
	}

	// Default topic
	return ks.config.Topic
}

// determinePartitionKey determina a chave de particionamento
func (ks *KafkaSink) determinePartitionKey(entry *types.LogEntry) string {
	if !ks.config.Partitioning.Enabled {
		return ""
	}

	// Use configured key field
	keyField := ks.config.Partitioning.KeyField
	if keyField == "" {
		keyField = "tenant"
	}

	// Try to get partition key from labels
	if key, ok := entry.Labels[keyField]; ok {
		return key
	}

	// Fallback to source_id
	return entry.SourceID
}

// GetStats retorna estatísticas do sink
func (ks *KafkaSink) GetStats() map[string]interface{} {
	ks.mutex.RLock()
	defer ks.mutex.RUnlock()

	return map[string]interface{}{
		"enabled":            ks.config.Enabled,
		"running":            ks.isRunning,
		"queue_size":         len(ks.queue),
		"queue_capacity":     cap(ks.queue),
		"queue_utilization":  float64(len(ks.queue)) / float64(cap(ks.queue)),
		"sent_total":         atomic.LoadInt64(&ks.sentCount),
		"error_total":        atomic.LoadInt64(&ks.errorCount),
		"dropped_total":      atomic.LoadInt64(&ks.droppedCount),
		"backpressure_count": atomic.LoadInt64(&ks.backpressureCount),
		"circuit_breaker":    ks.breaker.State(),
	}
}

// IsHealthy retorna o status de saúde do Kafka sink
func (ks *KafkaSink) IsHealthy() bool {
	// Check if context is cancelled
	select {
	case <-ks.ctx.Done():
		return false
	default:
	}

	// Check circuit breaker state
	if ks.breaker.State() == "open" {
		return false
	}

	// Check if producer is still active (not nil)
	if ks.producer == nil {
		return false
	}

	// Check queue utilization - if queue is critically full, consider unhealthy
	queueUsage := float64(len(ks.queue)) / float64(cap(ks.queue))
	if queueUsage >= ks.config.BackpressureConfig.QueueEmergencyThreshold {
		return false
	}

	// If we get here, the sink is healthy
	return true
}
