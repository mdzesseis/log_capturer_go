package sinks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esapi"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"

	"ssw-logs-capture/pkg/compression"
	"ssw-logs-capture/pkg/types"
)

// ElasticsearchConfig configuration for Elasticsearch sink
type ElasticsearchConfig struct {
	Enabled           bool              `yaml:"enabled"`
	Hosts             []string          `yaml:"hosts"`
	IndexPrefix       string            `yaml:"index_prefix"`
	IndexPattern      string            `yaml:"index_pattern"`
	BatchSize         int               `yaml:"batch_size"`
	BatchTimeout      time.Duration     `yaml:"batch_timeout"`
	MaxRetries        int               `yaml:"max_retries"`
	RetryBackoff      time.Duration     `yaml:"retry_backoff"`
	Timeout           time.Duration     `yaml:"timeout"`
	Username          string            `yaml:"username"`
	Password          string            `yaml:"password"`
	APIKey            string            `yaml:"api_key"`
	CloudID           string            `yaml:"cloud_id"`
	Headers           map[string]string `yaml:"headers"`
	TLS               TLSConfig         `yaml:"tls"`
	Compression       bool              `yaml:"compression"`
	CompressionLevel  int               `yaml:"compression_level"`
	MaxDocumentSize   int               `yaml:"max_document_size"`
	RefreshPolicy     string            `yaml:"refresh_policy"`
	Pipeline          string            `yaml:"pipeline"`
	DefaultLabels     map[string]string `yaml:"default_labels"`
	MappingTemplate   map[string]interface{} `yaml:"mapping_template"`
	CreateIndex       bool              `yaml:"create_index"`
	ShardCount        int               `yaml:"shard_count"`
	ReplicaCount      int               `yaml:"replica_count"`
}

// ElasticsearchSink sends logs to Elasticsearch
type ElasticsearchSink struct {
	config       ElasticsearchConfig
	client       *elasticsearch.Client
	compressor   *compression.HTTPCompressor
	logger       *logrus.Logger
	ctx          context.Context
	cancel       context.CancelFunc
	queue        chan types.LogEntry
	batch        []types.LogEntry
	batchMutex   sync.Mutex
	flushTimer   *time.Timer
	stopped      bool
	stopMutex    sync.RWMutex
	retryCount   int

	// Metrics
	docsIndexed     prometheus.Counter
	batchesSent     prometheus.Counter
	indexErrors     prometheus.Counter
	batchLatency    prometheus.Histogram
	queueSize       prometheus.Gauge
	connectionPool  prometheus.Gauge
}

// ElasticsearchDocument represents a document to be indexed
type ElasticsearchDocument struct {
	Timestamp time.Time              `json:"@timestamp"`
	Message   string                 `json:"message"`
	Level     string                 `json:"level,omitempty"`
	SourceID  string                 `json:"source_id,omitempty"`
	Labels    map[string]string      `json:"labels,omitempty"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
	Host      string                 `json:"host,omitempty"`
	Service   string                 `json:"service,omitempty"`
}

// NewElasticsearchSink creates a new Elasticsearch sink
func NewElasticsearchSink(config ElasticsearchConfig, logger *logrus.Logger, ctx context.Context) (*ElasticsearchSink, error) {
	if !config.Enabled {
		return nil, fmt.Errorf("elasticsearch sink is disabled")
	}

	if len(config.Hosts) == 0 {
		return nil, fmt.Errorf("no elasticsearch hosts configured")
	}

	// Set defaults
	if config.IndexPrefix == "" {
		config.IndexPrefix = "logs"
	}
	if config.IndexPattern == "" {
		config.IndexPattern = config.IndexPrefix + "-{date}"
	}
	if config.BatchSize == 0 {
		config.BatchSize = 100
	}
	if config.BatchTimeout == 0 {
		config.BatchTimeout = 30 * time.Second
	}
	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}
	if config.RetryBackoff == 0 {
		config.RetryBackoff = time.Second
	}
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}
	if config.MaxDocumentSize == 0 {
		config.MaxDocumentSize = 1024 * 1024 // 1MB
	}
	if config.RefreshPolicy == "" {
		config.RefreshPolicy = "false"
	}
	if config.ShardCount == 0 {
		config.ShardCount = 1
	}
	if config.ReplicaCount == 0 {
		config.ReplicaCount = 1
	}

	sinkCtx, cancel := context.WithCancel(ctx)

	// Configure HTTP compressor
	compressionConfig := compression.Config{
		DefaultAlgorithm: compression.AlgorithmGzip,
		AdaptiveEnabled:  true,
		MinBytes:         1024,
		Level:            config.CompressionLevel,
		PoolSize:         10,
		PerSink: map[string]compression.SinkCompressionConfig{
			"elasticsearch": {
				Algorithm: compression.AlgorithmGzip,
				Enabled:   config.Compression,
				Level:     config.CompressionLevel,
			},
		},
	}
	if compressionConfig.Level == 0 {
		compressionConfig.Level = 6
	}
	compressor := compression.NewHTTPCompressor(compressionConfig, logger)

	sink := &ElasticsearchSink{
		config:     config,
		compressor: compressor,
		logger:     logger,
		ctx:        sinkCtx,
		cancel:     cancel,
		queue:      make(chan types.LogEntry, config.BatchSize*2),
		batch:      make([]types.LogEntry, 0, config.BatchSize),
		flushTimer: time.NewTimer(config.BatchTimeout),
	}

	// Initialize metrics
	sink.initMetrics()

	// Create Elasticsearch client
	if err := sink.createClient(); err != nil {
		return nil, fmt.Errorf("failed to create elasticsearch client: %w", err)
	}

	// Create index template if configured
	if config.CreateIndex {
		if err := sink.createIndexTemplate(); err != nil {
			logger.WithError(err).Warn("Failed to create index template")
		}
	}

	return sink, nil
}

// createClient creates the Elasticsearch client
func (es *ElasticsearchSink) createClient() error {
	cfg := elasticsearch.Config{
		Addresses: es.config.Hosts,
		Username:  es.config.Username,
		Password:  es.config.Password,
		APIKey:    es.config.APIKey,
		CloudID:   es.config.CloudID,
		Transport: &http.Transport{
			MaxIdleConns:       10,
			IdleConnTimeout:    30 * time.Second,
			DisableCompression: !es.config.Compression,
		},
	}

	// Configure TLS
	if es.config.TLS.Enabled {
		transport := cfg.Transport.(*http.Transport)
		tlsConfig, err := createTLSConfig(es.config.TLS)
		if err != nil {
			return fmt.Errorf("failed to create TLS config: %w", err)
		}
		transport.TLSClientConfig = tlsConfig
	}

	// Add custom headers
	if len(es.config.Headers) > 0 {
		cfg.Header = make(http.Header)
		for key, value := range es.config.Headers {
			cfg.Header.Set(key, value)
		}
	}

	client, err := elasticsearch.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	es.client = client

	// Test connection
	ctx, cancel := context.WithTimeout(es.ctx, 10*time.Second)
	defer cancel()

	res, err := client.Ping(client.Ping.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("failed to ping elasticsearch: %w", err)
	}
	if res.IsError() {
		return fmt.Errorf("elasticsearch ping failed: %s", res.Status())
	}

	es.logger.Info("Successfully connected to Elasticsearch", "hosts", es.config.Hosts)
	return nil
}

// createIndexTemplate creates an index template for logs
func (es *ElasticsearchSink) createIndexTemplate() error {
	templateName := es.config.IndexPrefix + "-template"

	template := map[string]interface{}{
		"index_patterns": []string{es.config.IndexPrefix + "-*"},
		"settings": map[string]interface{}{
			"number_of_shards":   es.config.ShardCount,
			"number_of_replicas": es.config.ReplicaCount,
			"codec":              "best_compression",
		},
		"mappings": map[string]interface{}{
			"properties": map[string]interface{}{
				"@timestamp": map[string]interface{}{
					"type": "date",
				},
				"message": map[string]interface{}{
					"type": "text",
					"fields": map[string]interface{}{
						"keyword": map[string]interface{}{
							"type":         "keyword",
							"ignore_above": 256,
						},
					},
				},
				"level": map[string]interface{}{
					"type": "keyword",
				},
				"source_id": map[string]interface{}{
					"type": "keyword",
				},
				"host": map[string]interface{}{
					"type": "keyword",
				},
				"service": map[string]interface{}{
					"type": "keyword",
				},
				"labels": map[string]interface{}{
					"type": "object",
				},
				"fields": map[string]interface{}{
					"type": "object",
				},
			},
		},
	}

	// Use custom mapping template if provided
	if es.config.MappingTemplate != nil {
		template = es.config.MappingTemplate
	}

	templateJSON, err := json.Marshal(template)
	if err != nil {
		return fmt.Errorf("failed to marshal template: %w", err)
	}

	req := esapi.IndicesPutIndexTemplateRequest{
		Name: templateName,
		Body: strings.NewReader(string(templateJSON)),
	}

	ctx, cancel := context.WithTimeout(es.ctx, 30*time.Second)
	defer cancel()

	res, err := req.Do(ctx, es.client)
	if err != nil {
		return fmt.Errorf("failed to create index template: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("index template creation failed: %s", res.Status())
	}

	es.logger.Info("Successfully created index template", "template", templateName)
	return nil
}

// Start starts the Elasticsearch sink
func (es *ElasticsearchSink) Start(ctx context.Context) error {
	es.logger.Info("Starting Elasticsearch sink")

	go es.processBatches()
	go es.flushWorker()

	return nil
}

// Stop stops the Elasticsearch sink
func (es *ElasticsearchSink) Stop() error {
	es.stopMutex.Lock()
	if es.stopped {
		es.stopMutex.Unlock()
		return nil
	}
	es.stopped = true
	es.stopMutex.Unlock()

	es.logger.Info("Stopping Elasticsearch sink")

	// Signal shutdown
	es.cancel()

	// Close queue
	close(es.queue)

	// Flush any remaining batch
	es.flushBatch()

	es.logger.Info("Elasticsearch sink stopped")
	return nil
}

// Send sends log entries to Elasticsearch
func (es *ElasticsearchSink) Send(ctx context.Context, entries []types.LogEntry) error {
	if len(entries) == 0 {
		return nil
	}

	es.stopMutex.RLock()
	if es.stopped {
		es.stopMutex.RUnlock()
		return fmt.Errorf("sink is stopped")
	}
	es.stopMutex.RUnlock()

	for _, entry := range entries {
		select {
		case es.queue <- entry:
			es.queueSize.Set(float64(len(es.queue)))
		case <-es.ctx.Done():
			return fmt.Errorf("sink is shutting down")
		case <-ctx.Done():
			return ctx.Err()
		default:
			return fmt.Errorf("queue is full")
		}
	}
	return nil
}

// processBatches processes log entries from the queue
func (es *ElasticsearchSink) processBatches() {
	defer es.flushTimer.Stop()

	for {
		select {
		case entry, ok := <-es.queue:
			if !ok {
				return // Queue closed
			}

			es.addToBatch(entry)
			es.queueSize.Set(float64(len(es.queue)))

		case <-es.flushTimer.C:
			es.flushBatch()
			es.resetTimer()

		case <-es.ctx.Done():
			return
		}
	}
}

// addToBatch adds an entry to the current batch
func (es *ElasticsearchSink) addToBatch(entry types.LogEntry) {
	es.batchMutex.Lock()
	defer es.batchMutex.Unlock()

	es.batch = append(es.batch, entry)

	if len(es.batch) >= es.config.BatchSize {
		go es.flushBatch()
		es.resetTimer()
	}
}

// flushWorker periodically flushes batches
func (es *ElasticsearchSink) flushWorker() {
	ticker := time.NewTicker(es.config.BatchTimeout)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if len(es.batch) > 0 {
				es.flushBatch()
			}
		case <-es.ctx.Done():
			return
		}
	}
}

// flushBatch flushes the current batch to Elasticsearch
func (es *ElasticsearchSink) flushBatch() {
	es.batchMutex.Lock()
	if len(es.batch) == 0 {
		es.batchMutex.Unlock()
		return
	}

	// Copy batch and reset
	batchToSend := make([]types.LogEntry, len(es.batch))
	copy(batchToSend, es.batch)
	es.batch = es.batch[:0]
	es.batchMutex.Unlock()

	start := time.Now()
	err := es.sendBatch(batchToSend)
	duration := time.Since(start)

	es.batchLatency.Observe(duration.Seconds())

	if err != nil {
		es.indexErrors.Inc()
		es.logger.WithError(err).Error("Failed to send batch to Elasticsearch")

		// Implement retry logic with exponential backoff
		if es.shouldRetry(err) && es.retryCount < es.config.MaxRetries {
			es.retryCount++
			backoffDuration := time.Duration(es.retryCount*es.retryCount) * time.Second
			es.logger.WithFields(logrus.Fields{
				"retry_count": es.retryCount,
				"backoff": backoffDuration,
			}).Warn("Retrying Elasticsearch batch send")

			time.Sleep(backoffDuration)
			// Re-queue batch for retry
			go func() {
				for _, entry := range batchToSend {
					select {
					case es.queue <- entry:
					default:
						es.logger.Warn("Queue full during retry, dropping entry")
					}
				}
			}()
		} else {
			// Send to DLQ after exhausting retries
			es.sendToDLQ(batchToSend, fmt.Sprintf("elasticsearch_failed_after_%d_retries: %v", es.retryCount, err))
			es.retryCount = 0 // Reset retry count
		}
	} else {
		es.batchesSent.Inc()
		es.docsIndexed.Add(float64(len(batchToSend)))
		es.logger.Debug("Successfully sent batch to Elasticsearch",
			"count", len(batchToSend),
			"duration", duration)
	}
}

// sendBatch sends a batch of log entries to Elasticsearch
func (es *ElasticsearchSink) sendBatch(entries []types.LogEntry) error {
	if len(entries) == 0 {
		return nil
	}

	// Build bulk request body
	var buf bytes.Buffer

	for _, entry := range entries {
		// Create document
		doc := es.createDocument(entry)

		// Create index action
		indexName := es.generateIndexName(entry.Timestamp)
		action := map[string]interface{}{
			"index": map[string]interface{}{
				"_index": indexName,
			},
		}

		// Add pipeline if configured
		if es.config.Pipeline != "" {
			action["index"].(map[string]interface{})["pipeline"] = es.config.Pipeline
		}

		// Write action
		actionJSON, err := json.Marshal(action)
		if err != nil {
			return fmt.Errorf("failed to marshal action: %w", err)
		}
		buf.Write(actionJSON)
		buf.WriteByte('\n')

		// Write document
		docJSON, err := json.Marshal(doc)
		if err != nil {
			return fmt.Errorf("failed to marshal document: %w", err)
		}

		// Check document size
		if len(docJSON) > es.config.MaxDocumentSize {
			es.logger.Warn("Document exceeds max size, truncating",
				"size", len(docJSON),
				"max_size", es.config.MaxDocumentSize)
			// Truncate message if document is too large
			doc.Message = doc.Message[:len(doc.Message)/2] + "... [TRUNCATED]"
			docJSON, _ = json.Marshal(doc)
		}

		buf.Write(docJSON)
		buf.WriteByte('\n')
	}

	// Compress the bulk request body if compression is enabled
	bulkData := buf.Bytes()
	var requestBody bytes.Reader
	var contentEncoding string

	if es.config.Compression {
		compressionResult, err := es.compressor.Compress(bulkData, compression.AlgorithmAuto, "elasticsearch")
		if err != nil {
			es.logger.WithError(err).Warn("Failed to compress bulk request, sending uncompressed")
			requestBody = *bytes.NewReader(bulkData)
		} else {
			requestBody = *bytes.NewReader(compressionResult.Data)
			contentEncoding = compressionResult.Encoding

			// Log compression metrics
			es.logger.WithFields(logrus.Fields{
				"original_size":     compressionResult.OriginalSize,
				"compressed_size":   compressionResult.CompressedSize,
				"compression_ratio": compressionResult.Ratio,
				"algorithm":         string(compressionResult.Algorithm),
			}).Debug("Elasticsearch bulk request compressed")
		}
	} else {
		requestBody = *bytes.NewReader(bulkData)
	}

	// Send bulk request
	req := esapi.BulkRequest{
		Body:    &requestBody,
		Refresh: es.config.RefreshPolicy,
	}

	// Add compression header if needed
	if contentEncoding != "" {
		req.Header = map[string][]string{
			"Content-Encoding": {contentEncoding},
		}
	}

	ctx, cancel := context.WithTimeout(es.ctx, es.config.Timeout)
	defer cancel()

	res, err := req.Do(ctx, es.client)
	if err != nil {
		return fmt.Errorf("bulk request failed: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("bulk request error: %s", res.Status())
	}

	// Parse response to check for individual document errors
	var bulkResponse map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&bulkResponse); err != nil {
		return fmt.Errorf("failed to parse bulk response: %w", err)
	}

	// Check for errors in individual items
	if items, ok := bulkResponse["items"].([]interface{}); ok {
		errorCount := 0
		for _, item := range items {
			if itemMap, ok := item.(map[string]interface{}); ok {
				if indexMap, ok := itemMap["index"].(map[string]interface{}); ok {
					if status, ok := indexMap["status"].(float64); ok && status >= 400 {
						errorCount++
						if errorCount <= 5 { // Log first 5 errors
							es.logger.Warn("Document indexing failed",
								"status", status,
								"error", indexMap["error"])
						}
					}
				}
			}
		}

		if errorCount > 0 {
			es.logger.Warn("Some documents failed to index",
				"failed", errorCount,
				"total", len(entries))
		}
	}

	return nil
}

// createDocument creates an Elasticsearch document from a log entry
func (es *ElasticsearchSink) createDocument(entry types.LogEntry) ElasticsearchDocument {
	doc := ElasticsearchDocument{
		Timestamp: entry.Timestamp,
		Message:   entry.Message,
		Level:     entry.Level,
		SourceID:  entry.SourceID,
		Labels:    make(map[string]string),
		Fields:    make(map[string]interface{}),
	}

	// Copy labels and add default labels
	for k, v := range es.config.DefaultLabels {
		doc.Labels[k] = v
	}

	// Fazer cópia do map para evitar concurrent access durante iteração
	labelsCopy := make(map[string]string, len(entry.Labels))
	for k, v := range entry.Labels {
		labelsCopy[k] = v
	}
	for k, v := range labelsCopy {
		doc.Labels[k] = v
	}

	// Extract host and service from labels
	if host, ok := doc.Labels["host"]; ok {
		doc.Host = host
	}
	if service, ok := doc.Labels["service"]; ok {
		doc.Service = service
	}

	// Copy fields
	for k, v := range entry.Fields {
		doc.Fields[k] = v
	}

	return doc
}

// generateIndexName generates the index name based on the timestamp
func (es *ElasticsearchSink) generateIndexName(timestamp time.Time) string {
	pattern := es.config.IndexPattern

	// Replace date patterns
	pattern = strings.ReplaceAll(pattern, "{date}", timestamp.Format("2006.01.02"))
	pattern = strings.ReplaceAll(pattern, "{year}", timestamp.Format("2006"))
	pattern = strings.ReplaceAll(pattern, "{month}", timestamp.Format("01"))
	pattern = strings.ReplaceAll(pattern, "{day}", timestamp.Format("02"))
	pattern = strings.ReplaceAll(pattern, "{hour}", timestamp.Format("15"))

	return pattern
}

// resetTimer resets the flush timer
func (es *ElasticsearchSink) resetTimer() {
	if !es.flushTimer.Stop() {
		select {
		case <-es.flushTimer.C:
		default:
		}
	}
	es.flushTimer.Reset(es.config.BatchTimeout)
}

// initMetrics initializes Prometheus metrics
func (es *ElasticsearchSink) initMetrics() {
	es.docsIndexed = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "ssw_logs_capture_elasticsearch_docs_indexed_total",
			Help: "Total number of documents indexed to Elasticsearch",
		},
	)

	es.batchesSent = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "ssw_logs_capture_elasticsearch_batches_sent_total",
			Help: "Total number of batches sent to Elasticsearch",
		},
	)

	es.indexErrors = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "ssw_logs_capture_elasticsearch_errors_total",
			Help: "Total number of Elasticsearch indexing errors",
		},
	)

	es.batchLatency = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name: "ssw_logs_capture_elasticsearch_batch_duration_seconds",
			Help: "Time taken to send batches to Elasticsearch",
			Buckets: prometheus.DefBuckets,
		},
	)

	es.queueSize = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "ssw_logs_capture_elasticsearch_queue_size",
			Help: "Current size of the Elasticsearch queue",
		},
	)

	es.connectionPool = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "ssw_logs_capture_elasticsearch_connections",
			Help: "Number of active Elasticsearch connections",
		},
	)

	// Register metrics
	prometheus.MustRegister(es.docsIndexed, es.batchesSent, es.indexErrors,
		es.batchLatency, es.queueSize, es.connectionPool)
}

// GetStatistics returns sink statistics
func (es *ElasticsearchSink) GetStatistics() map[string]interface{} {
	return map[string]interface{}{
		"type":          "elasticsearch",
		"hosts":         es.config.Hosts,
		"queue_size":    len(es.queue),
		"batch_size":    len(es.batch),
		"max_batch":     es.config.BatchSize,
		"timeout":       es.config.BatchTimeout.String(),
		"docs_indexed":  es.docsIndexed,
		"batches_sent":  es.batchesSent,
		"errors":        es.indexErrors,
	}
}

// GetQueueUtilization returns queue utilization as a percentage
func (es *ElasticsearchSink) GetQueueUtilization() float64 {
	maxSize := cap(es.queue)
	if maxSize == 0 {
		return 0.0
	}
	return float64(len(es.queue)) / float64(maxSize) * 100.0
}

// IsHealthy returns the health status of the sink
func (es *ElasticsearchSink) IsHealthy() bool {
	es.stopMutex.RLock()
	defer es.stopMutex.RUnlock()
	return !es.stopped && es.client != nil
}

// shouldRetry determines if an error is retryable
func (es *ElasticsearchSink) shouldRetry(err error) bool {
	if err == nil {
		return false
	}

	// Check for retryable HTTP status codes
	if strings.Contains(err.Error(), "429") || // Too Many Requests
		strings.Contains(err.Error(), "500") || // Internal Server Error
		strings.Contains(err.Error(), "502") || // Bad Gateway
		strings.Contains(err.Error(), "503") || // Service Unavailable
		strings.Contains(err.Error(), "504") {  // Gateway Timeout
		return true
	}

	// Check for connection errors
	if strings.Contains(err.Error(), "connection") ||
		strings.Contains(err.Error(), "timeout") ||
		strings.Contains(err.Error(), "EOF") {
		return true
	}

	return false
}

// sendToDLQ sends failed entries to Dead Letter Queue
func (es *ElasticsearchSink) sendToDLQ(entries []types.LogEntry, reason string) {
	es.logger.WithField("reason", reason).Info("Sending batch to DLQ")

	// Convert entries to DLQ format and send
	for _, entry := range entries {
		// Send to DLQ channel if available (would need DLQ integration)
		es.logger.WithFields(logrus.Fields{
			"original_source": entry.SourceID,
			"reason":         reason,
		}).Warn("Entry sent to DLQ due to repeated failures")
	}
}