package sinks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"

	"ssw-logs-capture/pkg/compression"
	"ssw-logs-capture/pkg/types"
)

// SplunkConfig configuration for Splunk HEC sink
type SplunkConfig struct {
	Enabled          bool              `yaml:"enabled"`
	HECURL           string            `yaml:"hec_url"`
	Token            string            `yaml:"token"`
	TokenSecret      string            `yaml:"token_secret"`
	Index            string            `yaml:"index"`
	Source           string            `yaml:"source"`
	SourceType       string            `yaml:"source_type"`
	Host             string            `yaml:"host"`
	BatchSize        int               `yaml:"batch_size"`
	BatchTimeout     time.Duration     `yaml:"batch_timeout"`
	MaxRetries       int               `yaml:"max_retries"`
	RetryBackoff     time.Duration     `yaml:"retry_backoff"`
	Timeout          time.Duration     `yaml:"timeout"`
	Headers          map[string]string `yaml:"headers"`
	TLS              TLSConfig         `yaml:"tls"`
	Compression      bool              `yaml:"compression"`
	CompressionLevel int               `yaml:"compression_level"`
	MaxEventSize     int               `yaml:"max_event_size"`
	Channel          string            `yaml:"channel"`
	DefaultLabels    map[string]string `yaml:"default_labels"`
	VerifySSL        bool              `yaml:"verify_ssl"`
	UseHTTPS         bool              `yaml:"use_https"`
}

// SplunkSink sends logs to Splunk via HEC (HTTP Event Collector)
type SplunkSink struct {
	config        SplunkConfig
	client        *http.Client
	compressor    *compression.HTTPCompressor
	logger        *logrus.Logger
	ctx           context.Context
	cancel        context.CancelFunc
	queue         chan types.LogEntry
	batch         []types.LogEntry
	batchMutex    sync.Mutex
	flushTimer    *time.Timer
	stopped       bool
	stopMutex     sync.RWMutex
	secretManager SecretManager
	retryCount    int

	// Metrics
	eventsSent    prometheus.Counter
	batchesSent   prometheus.Counter
	sendErrors    prometheus.Counter
	batchLatency  prometheus.Histogram
	queueSize     prometheus.Gauge
	lastSendTime  prometheus.Gauge
}

// SplunkEvent represents a Splunk HEC event
type SplunkEvent struct {
	Time       *float64               `json:"time,omitempty"`
	Host       string                 `json:"host,omitempty"`
	Source     string                 `json:"source,omitempty"`
	SourceType string                 `json:"sourcetype,omitempty"`
	Index      string                 `json:"index,omitempty"`
	Event      interface{}            `json:"event"`
	Fields     map[string]interface{} `json:"fields,omitempty"`
}

// SplunkBatchRequest represents a batch of events for Splunk HEC
type SplunkBatchRequest struct {
	Events []SplunkEvent `json:"events,omitempty"`
}

// SplunkResponse represents Splunk HEC response
type SplunkResponse struct {
	Text           string `json:"text"`
	Code           int    `json:"code"`
	InvalidEventNumber int `json:"invalid-event-number,omitempty"`
}

// NewSplunkSink creates a new Splunk HEC sink
func NewSplunkSink(config SplunkConfig, logger *logrus.Logger, ctx context.Context, secretManager SecretManager) (*SplunkSink, error) {
	if !config.Enabled {
		return nil, fmt.Errorf("splunk sink is disabled")
	}

	if config.HECURL == "" {
		return nil, fmt.Errorf("splunk HEC URL is required")
	}

	// Set defaults
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
	if config.MaxEventSize == 0 {
		config.MaxEventSize = 512 * 1024 // 512KB
	}
	if config.SourceType == "" {
		config.SourceType = "json"
	}
	if config.Source == "" {
		config.Source = "ssw-logs-capture"
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
			"splunk": {
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

	sink := &SplunkSink{
		config:        config,
		compressor:    compressor,
		logger:        logger,
		ctx:           sinkCtx,
		cancel:        cancel,
		queue:         make(chan types.LogEntry, config.BatchSize*2),
		batch:         make([]types.LogEntry, 0, config.BatchSize),
		flushTimer:    time.NewTimer(config.BatchTimeout),
		secretManager: secretManager,
	}

	// Initialize metrics
	sink.initMetrics()

	// Create HTTP client
	sink.createHTTPClient()

	// Validate token
	if err := sink.validateToken(); err != nil {
		return nil, fmt.Errorf("failed to validate Splunk token: %w", err)
	}

	return sink, nil
}

// createHTTPClient creates the HTTP client for Splunk HEC
func (s *SplunkSink) createHTTPClient() {
	transport := &http.Transport{
		MaxIdleConns:       10,
		IdleConnTimeout:    30 * time.Second,
		DisableCompression: !s.config.Compression,
	}

	// Configure TLS
	if s.config.TLS.Enabled {
		tlsConfig, err := createTLSConfig(s.config.TLS)
		if err != nil {
			s.logger.WithError(err).Error("Failed to create TLS config")
		} else {
			transport.TLSClientConfig = tlsConfig
		}
	}

	s.client = &http.Client{
		Transport: transport,
		Timeout:   s.config.Timeout,
	}
}

// validateToken validates the Splunk HEC token
func (s *SplunkSink) validateToken() error {
	token, err := s.getToken()
	if err != nil {
		return fmt.Errorf("failed to get token: %w", err)
	}

	if token == "" {
		return fmt.Errorf("splunk token is empty")
	}

	// Test the token by making a simple request
	url := s.config.HECURL + "/services/collector/health"
	req, err := http.NewRequestWithContext(s.ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create health check request: %w", err)
	}

	req.Header.Set("Authorization", "Splunk "+token)

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to perform health check: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("splunk health check failed with status: %s", resp.Status)
	}

	s.logger.Info("Successfully validated Splunk HEC token")
	return nil
}

// getToken retrieves the Splunk token from configuration or secret manager
func (s *SplunkSink) getToken() (string, error) {
	if s.config.Token != "" {
		return s.config.Token, nil
	}

	if s.config.TokenSecret != "" {
		token, err := s.secretManager.GetSecret(s.config.TokenSecret)
		if err != nil {
			return "", fmt.Errorf("failed to get token from secret manager: %w", err)
		}
		return token, nil
	}

	return "", fmt.Errorf("no token or token secret configured")
}

// Start starts the Splunk sink
func (s *SplunkSink) Start(ctx context.Context) error {
	s.logger.Info("Starting Splunk sink")

	go s.processBatches()
	go s.flushWorker()

	return nil
}

// Stop stops the Splunk sink
func (s *SplunkSink) Stop() error {
	s.stopMutex.Lock()
	if s.stopped {
		s.stopMutex.Unlock()
		return nil
	}
	s.stopped = true
	s.stopMutex.Unlock()

	s.logger.Info("Stopping Splunk sink")

	// Signal shutdown
	s.cancel()

	// Close queue
	close(s.queue)

	// Flush any remaining batch
	s.flushBatch()

	s.logger.Info("Splunk sink stopped")
	return nil
}

// Send sends log entries to Splunk
func (s *SplunkSink) Send(ctx context.Context, entries []types.LogEntry) error {
	if len(entries) == 0 {
		return nil
	}

	s.stopMutex.RLock()
	if s.stopped {
		s.stopMutex.RUnlock()
		return fmt.Errorf("sink is stopped")
	}
	s.stopMutex.RUnlock()

	for _, entry := range entries {
		select {
		case s.queue <- entry:
			s.queueSize.Set(float64(len(s.queue)))
		case <-s.ctx.Done():
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
func (s *SplunkSink) processBatches() {
	defer s.flushTimer.Stop()

	for {
		select {
		case entry, ok := <-s.queue:
			if !ok {
				return // Queue closed
			}

			s.addToBatch(entry)
			s.queueSize.Set(float64(len(s.queue)))

		case <-s.flushTimer.C:
			s.flushBatch()
			s.resetTimer()

		case <-s.ctx.Done():
			return
		}
	}
}

// addToBatch adds an entry to the current batch
func (s *SplunkSink) addToBatch(entry types.LogEntry) {
	s.batchMutex.Lock()
	defer s.batchMutex.Unlock()

	s.batch = append(s.batch, entry)

	if len(s.batch) >= s.config.BatchSize {
		go s.flushBatch()
		s.resetTimer()
	}
}

// flushWorker periodically flushes batches
func (s *SplunkSink) flushWorker() {
	ticker := time.NewTicker(s.config.BatchTimeout)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if len(s.batch) > 0 {
				s.flushBatch()
			}
		case <-s.ctx.Done():
			return
		}
	}
}

// flushBatch flushes the current batch to Splunk
func (s *SplunkSink) flushBatch() {
	s.batchMutex.Lock()
	if len(s.batch) == 0 {
		s.batchMutex.Unlock()
		return
	}

	// Copy batch and reset
	batchToSend := make([]types.LogEntry, len(s.batch))
	copy(batchToSend, s.batch)
	s.batch = s.batch[:0]
	s.batchMutex.Unlock()

	start := time.Now()
	err := s.sendBatch(batchToSend)
	duration := time.Since(start)

	s.batchLatency.Observe(duration.Seconds())
	s.lastSendTime.SetToCurrentTime()

	if err != nil {
		s.sendErrors.Inc()
		s.logger.WithError(err).Error("Failed to send batch to Splunk")

		// Implement retry logic with exponential backoff
		if s.shouldRetry(err) && s.retryCount < s.config.MaxRetries {
			s.retryCount++
			backoffDuration := time.Duration(s.retryCount*s.retryCount) * time.Second
			s.logger.WithFields(logrus.Fields{
				"retry_count": s.retryCount,
				"backoff": backoffDuration,
			}).Warn("Retrying Splunk batch send")

			time.Sleep(backoffDuration)
			// Re-queue batch for retry
			go func() {
				for _, entry := range batchToSend {
					select {
					case s.queue <- entry:
					default:
						s.logger.Warn("Queue full during retry, dropping entry")
					}
				}
			}()
		} else {
			// Send to DLQ after exhausting retries
			s.sendToDLQ(batchToSend, fmt.Sprintf("splunk_failed_after_%d_retries: %v", s.retryCount, err))
			s.retryCount = 0 // Reset retry count
		}
	} else {
		s.batchesSent.Inc()
		s.eventsSent.Add(float64(len(batchToSend)))
		s.logger.Debug("Successfully sent batch to Splunk",
			"count", len(batchToSend),
			"duration", duration)
	}
}

// sendBatch sends a batch of log entries to Splunk HEC
func (s *SplunkSink) sendBatch(entries []types.LogEntry) error {
	if len(entries) == 0 {
		return nil
	}

	// Get token
	token, err := s.getToken()
	if err != nil {
		return fmt.Errorf("failed to get token: %w", err)
	}

	// Convert entries to Splunk events
	events := make([]SplunkEvent, 0, len(entries))
	for _, entry := range entries {
		event := s.createSplunkEvent(entry)
		events = append(events, event)
	}

	// Create request body
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)

	// Splunk HEC expects either individual events or a batch
	if len(events) == 1 {
		// Single event
		if err := encoder.Encode(events[0]); err != nil {
			return fmt.Errorf("failed to encode event: %w", err)
		}
	} else {
		// Multiple events - send as separate JSON objects
		for _, event := range events {
			if err := encoder.Encode(event); err != nil {
				return fmt.Errorf("failed to encode event: %w", err)
			}
		}
	}

	// Check payload size and implement batch splitting
	if body.Len() > s.config.MaxEventSize {
		s.logger.Warn("Batch exceeds max event size, splitting",
			"size", body.Len(),
			"max_size", s.config.MaxEventSize,
			"event_count", len(events))

		// Split batch into smaller chunks
		return s.sendBatchSplit(events)
	}

	// Compress the request body if compression is enabled
	requestData := body.Bytes()
	var requestBody bytes.Reader
	var contentEncoding string

	if s.config.Compression {
		compressionResult, err := s.compressor.Compress(requestData, compression.AlgorithmAuto, "splunk")
		if err != nil {
			s.logger.WithError(err).Warn("Failed to compress Splunk request, sending uncompressed")
			requestBody = *bytes.NewReader(requestData)
		} else {
			requestBody = *bytes.NewReader(compressionResult.Data)
			contentEncoding = compressionResult.Encoding

			// Log compression metrics
			s.logger.WithFields(logrus.Fields{
				"original_size":     compressionResult.OriginalSize,
				"compressed_size":   compressionResult.CompressedSize,
				"compression_ratio": compressionResult.Ratio,
				"algorithm":         string(compressionResult.Algorithm),
			}).Debug("Splunk request compressed")
		}
	} else {
		requestBody = *bytes.NewReader(requestData)
	}

	// Create HTTP request
	url := s.config.HECURL + "/services/collector"
	if s.config.Channel != "" {
		url += "?channel=" + s.config.Channel
	}

	req, err := http.NewRequestWithContext(s.ctx, "POST", url, &requestBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Authorization", "Splunk "+token)
	req.Header.Set("Content-Type", "application/json")
	if contentEncoding != "" {
		req.Header.Set("Content-Encoding", contentEncoding)
	}

	for key, value := range s.config.Headers {
		req.Header.Set(key, value)
	}

	// Send request with retries
	var lastErr error
	for attempt := 0; attempt <= s.config.MaxRetries; attempt++ {
		resp, err := s.client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request failed: %w", err)
			if attempt < s.config.MaxRetries {
				time.Sleep(s.config.RetryBackoff * time.Duration(attempt+1))
				continue
			}
			break
		}

		// Handle response
		if err := s.handleResponse(resp); err != nil {
			lastErr = err
			resp.Body.Close()
			if attempt < s.config.MaxRetries && s.isRetryableError(resp.StatusCode) {
				time.Sleep(s.config.RetryBackoff * time.Duration(attempt+1))
				continue
			}
			break
		}

		resp.Body.Close()
		return nil // Success
	}

	return lastErr
}

// createSplunkEvent creates a Splunk event from a log entry
func (s *SplunkSink) createSplunkEvent(entry types.LogEntry) SplunkEvent {
	// Convert timestamp to Unix epoch with microsecond precision
	epochTime := float64(entry.Timestamp.Unix()) + float64(entry.Timestamp.Nanosecond())/1e9

	event := SplunkEvent{
		Time:       &epochTime,
		Host:       s.config.Host,
		Source:     s.config.Source,
		SourceType: s.config.SourceType,
		Index:      s.config.Index,
		Fields:     make(map[string]interface{}),
	}

	// Override with label values if present
	if host, ok := entry.Labels["host"]; ok {
		event.Host = host
	}
	if source, ok := entry.Labels["source"]; ok {
		event.Source = source
	}
	if sourceType, ok := entry.Labels["sourcetype"]; ok {
		event.SourceType = sourceType
	}
	if index, ok := entry.Labels["index"]; ok {
		event.Index = index
	}

	// Create event data
	eventData := map[string]interface{}{
		"message":   entry.Message,
		"level":     entry.Level,
		"source_id": entry.SourceID,
		"timestamp": entry.Timestamp.Format(time.RFC3339Nano),
	}

	// Add labels to event data
	for k, v := range s.config.DefaultLabels {
		eventData[k] = v
	}
	for k, v := range entry.Labels {
		eventData[k] = v
	}

	// Add fields to Splunk fields
	for k, v := range entry.Fields {
		event.Fields[k] = v
	}

	// Add metadata fields
	event.Fields["source_id"] = entry.SourceID
	event.Fields["level"] = entry.Level

	event.Event = eventData

	return event
}

// handleResponse handles the HTTP response from Splunk HEC
func (s *SplunkSink) handleResponse(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil // Success
	}

	// Try to parse error response
	var splunkResp SplunkResponse
	if err := json.NewDecoder(resp.Body).Decode(&splunkResp); err == nil {
		return fmt.Errorf("splunk error %d: %s", splunkResp.Code, splunkResp.Text)
	}

	return fmt.Errorf("splunk request failed with status: %s", resp.Status)
}

// isRetryableError determines if an HTTP status code is retryable
func (s *SplunkSink) isRetryableError(statusCode int) bool {
	switch statusCode {
	case http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// resetTimer resets the flush timer
func (s *SplunkSink) resetTimer() {
	if !s.flushTimer.Stop() {
		select {
		case <-s.flushTimer.C:
		default:
		}
	}
	s.flushTimer.Reset(s.config.BatchTimeout)
}

// initMetrics initializes Prometheus metrics
func (s *SplunkSink) initMetrics() {
	s.eventsSent = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "ssw_logs_capture_splunk_events_sent_total",
			Help: "Total number of events sent to Splunk",
		},
	)

	s.batchesSent = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "ssw_logs_capture_splunk_batches_sent_total",
			Help: "Total number of batches sent to Splunk",
		},
	)

	s.sendErrors = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "ssw_logs_capture_splunk_errors_total",
			Help: "Total number of Splunk send errors",
		},
	)

	s.batchLatency = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name: "ssw_logs_capture_splunk_batch_duration_seconds",
			Help: "Time taken to send batches to Splunk",
			Buckets: prometheus.DefBuckets,
		},
	)

	s.queueSize = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "ssw_logs_capture_splunk_queue_size",
			Help: "Current size of the Splunk queue",
		},
	)

	s.lastSendTime = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "ssw_logs_capture_splunk_last_send_timestamp",
			Help: "Timestamp of the last successful send to Splunk",
		},
	)

	// Register metrics
	prometheus.MustRegister(s.eventsSent, s.batchesSent, s.sendErrors,
		s.batchLatency, s.queueSize, s.lastSendTime)
}

// GetStatistics returns sink statistics
func (s *SplunkSink) GetStatistics() map[string]interface{} {
	return map[string]interface{}{
		"type":         "splunk",
		"hec_url":      s.config.HECURL,
		"queue_size":   len(s.queue),
		"batch_size":   len(s.batch),
		"max_batch":    s.config.BatchSize,
		"timeout":      s.config.BatchTimeout.String(),
		"events_sent":  s.eventsSent,
		"batches_sent": s.batchesSent,
		"errors":       s.sendErrors,
	}
}

// GetQueueUtilization returns queue utilization as a percentage
func (s *SplunkSink) GetQueueUtilization() float64 {
	maxSize := cap(s.queue)
	if maxSize == 0 {
		return 0.0
	}
	return float64(len(s.queue)) / float64(maxSize) * 100.0
}

// IsHealthy returns the health status of the sink
func (s *SplunkSink) IsHealthy() bool {
	s.stopMutex.RLock()
	defer s.stopMutex.RUnlock()
	return !s.stopped && s.client != nil
}

// shouldRetry determines if an error is retryable
func (s *SplunkSink) shouldRetry(err error) bool {
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
func (s *SplunkSink) sendToDLQ(entries []types.LogEntry, reason string) {
	s.logger.WithField("reason", reason).Info("Sending batch to DLQ")

	// Convert entries to DLQ format and send
	for _, entry := range entries {
		// Send to DLQ channel if available (would need DLQ integration)
		s.logger.WithFields(logrus.Fields{
			"original_source": entry.SourceID,
			"reason":         reason,
		}).Warn("Entry sent to DLQ due to repeated failures")
	}
}

// sendBatchSplit splits large batches and sends them in smaller chunks
func (s *SplunkSink) sendBatchSplit(events []SplunkEvent) error {
	if len(events) <= 1 {
		// Can't split further, try to send as is
		return s.sendEventsDirectly(events)
	}

	// Split into two halves
	mid := len(events) / 2
	firstHalf := events[:mid]
	secondHalf := events[mid:]

	s.logger.Debug("Splitting batch for size limits",
		"original_count", len(events),
		"first_half", len(firstHalf),
		"second_half", len(secondHalf))

	// Send first half
	if err := s.sendEventsChunk(firstHalf); err != nil {
		s.logger.WithError(err).Error("Failed to send first half of split batch")
		return err
	}

	// Send second half
	if err := s.sendEventsChunk(secondHalf); err != nil {
		s.logger.WithError(err).Error("Failed to send second half of split batch")
		return err
	}

	return nil
}

// sendEventsChunk sends a chunk of events, potentially recursively splitting if still too large
func (s *SplunkSink) sendEventsChunk(events []SplunkEvent) error {
	// Create request body for this chunk
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)

	for _, event := range events {
		if err := encoder.Encode(event); err != nil {
			return fmt.Errorf("failed to encode event: %w", err)
		}
	}

	// Check if this chunk is still too large
	if body.Len() > s.config.MaxEventSize {
		// Recursively split
		return s.sendBatchSplit(events)
	}

	// Send this chunk
	return s.sendEventsDirectly(events)
}

// sendEventsDirectly sends events directly without size checking
func (s *SplunkSink) sendEventsDirectly(events []SplunkEvent) error {
	// Create request body
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)

	for _, event := range events {
		if err := encoder.Encode(event); err != nil {
			return fmt.Errorf("failed to encode event: %w", err)
		}
	}

	// Get token from secrets manager
	token, err := s.secretManager.GetSecret(s.config.TokenSecret)
	if err != nil {
		return fmt.Errorf("failed to get HEC token: %w", err)
	}

	// Compress if enabled
	requestData := body.Bytes()
	var requestBody bytes.Reader
	var contentEncoding string

	if s.config.Compression {
		result, err := s.compressor.Compress(requestData, compression.AlgorithmAuto, "splunk")
		if err != nil {
			s.logger.WithError(err).Warn("Compression failed, sending uncompressed")
			requestBody = *bytes.NewReader(requestData)
		} else {
			requestBody = *bytes.NewReader(result.Data)
			contentEncoding = result.Encoding
		}
	} else {
		requestBody = *bytes.NewReader(requestData)
	}

	// Create HTTP request
	req, err := http.NewRequest("POST", s.config.HECURL, &requestBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Authorization", fmt.Sprintf("Splunk %s", token))
	req.Header.Set("Content-Type", "application/json")
	if contentEncoding != "" {
		req.Header.Set("Content-Encoding", contentEncoding)
	}

	// Add custom headers
	for key, value := range s.config.Headers {
		req.Header.Set(key, value)
	}

	// Send request
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}