package monitoring

import (
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/sirupsen/logrus"
)

// EnhancedMetrics provides comprehensive monitoring and metrics collection
type EnhancedMetrics struct {
	logger *logrus.Logger

	// Application metrics
	logEntriesProcessed    *prometheus.CounterVec
	logProcessingDuration  *prometheus.HistogramVec
	queueSizes            *prometheus.GaugeVec
	errorRates            *prometheus.CounterVec
	throughputGauge       *prometheus.GaugeVec

	// System metrics
	goroutineCount        prometheus.Gauge
	fileDescriptorCount   prometheus.Gauge
	memoryUsage          *prometheus.GaugeVec
	cpuUsage             prometheus.Gauge
	diskUsage            *prometheus.GaugeVec

	// Performance metrics
	responseTime         *prometheus.HistogramVec
	connectionPoolStats  *prometheus.GaugeVec
	compressionRatio     *prometheus.GaugeVec
	batchingStats       *prometheus.GaugeVec

	// Business metrics
	containerCount       prometheus.Gauge
	fileCount           prometheus.Gauge
	sinkHealth          *prometheus.GaugeVec
	leakDetection       *prometheus.GaugeVec

	// Custom metrics registry
	customMetrics map[string]prometheus.Metric
	customMutex   sync.RWMutex

	// Internal state
	isRunning bool
	startTime time.Time
	mutex     sync.RWMutex
}

// NewEnhancedMetrics creates a new enhanced metrics collector
func NewEnhancedMetrics(logger *logrus.Logger) *EnhancedMetrics {
	em := &EnhancedMetrics{
		logger:        logger,
		customMetrics: make(map[string]prometheus.Metric),
		startTime:     time.Now(),
	}

	em.initializeMetrics()
	return em
}

// initializeMetrics initializes all Prometheus metrics
func (em *EnhancedMetrics) initializeMetrics() {
	// Application metrics
	em.logEntriesProcessed = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ssw_log_entries_processed_total",
			Help: "Total number of log entries processed",
		},
		[]string{"source_type", "sink", "status"},
	)

	em.logProcessingDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "ssw_log_processing_duration_seconds",
			Help:    "Time spent processing log entries",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0},
		},
		[]string{"pipeline", "stage"},
	)

	em.queueSizes = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ssw_queue_size",
			Help: "Current size of internal queues",
		},
		[]string{"queue_type", "component"},
	)

	em.errorRates = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ssw_errors_total",
			Help: "Total number of errors by component and type",
		},
		[]string{"component", "error_type", "severity"},
	)

	em.throughputGauge = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ssw_throughput_logs_per_second",
			Help: "Current throughput in logs per second",
		},
		[]string{"component", "measurement_window"},
	)

	// System metrics
	em.goroutineCount = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "ssw_goroutines_count",
			Help: "Current number of goroutines",
		},
	)

	em.fileDescriptorCount = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "ssw_file_descriptors_count",
			Help: "Current number of open file descriptors",
		},
	)

	em.memoryUsage = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ssw_memory_usage_bytes",
			Help: "Current memory usage in bytes",
		},
		[]string{"type"},
	)

	em.cpuUsage = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "ssw_cpu_usage_percent",
			Help: "Current CPU usage percentage",
		},
	)

	em.diskUsage = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ssw_disk_usage_bytes",
			Help: "Disk usage in bytes",
		},
		[]string{"path", "type"},
	)

	// Performance metrics
	em.responseTime = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "ssw_response_time_seconds",
			Help:    "Response time for various operations",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0},
		},
		[]string{"operation", "component"},
	)

	em.connectionPoolStats = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ssw_connection_pool_stats",
			Help: "Connection pool statistics",
		},
		[]string{"pool_type", "metric"},
	)

	em.compressionRatio = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ssw_compression_ratio",
			Help: "Compression ratio achieved",
		},
		[]string{"algorithm", "sink"},
	)

	em.batchingStats = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ssw_batching_stats",
			Help: "Batching system statistics",
		},
		[]string{"component", "metric"},
	)

	// Business metrics
	em.containerCount = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "ssw_monitored_containers_count",
			Help: "Number of containers currently being monitored",
		},
	)

	em.fileCount = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "ssw_monitored_files_count",
			Help: "Number of files currently being monitored",
		},
	)

	em.sinkHealth = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ssw_sink_health",
			Help: "Health status of sinks (1 = healthy, 0 = unhealthy)",
		},
		[]string{"sink_name", "sink_type"},
	)

	em.leakDetection = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ssw_resource_leak_detection",
			Help: "Resource leak detection metrics",
		},
		[]string{"resource_type", "metric"},
	)
}

// Start starts the enhanced metrics collector
func (em *EnhancedMetrics) Start() error {
	em.mutex.Lock()
	defer em.mutex.Unlock()

	if em.isRunning {
		return fmt.Errorf("enhanced metrics already running")
	}

	em.isRunning = true

	// Start system metrics collection
	go em.collectSystemMetrics()

	em.logger.Info("Enhanced metrics collector started")
	return nil
}

// Stop stops the enhanced metrics collector
func (em *EnhancedMetrics) Stop() error {
	em.mutex.Lock()
	defer em.mutex.Unlock()

	em.isRunning = false
	em.logger.Info("Enhanced metrics collector stopped")
	return nil
}

// collectSystemMetrics collects system-level metrics
func (em *EnhancedMetrics) collectSystemMetrics() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if !em.isRunning {
			break
		}

		em.updateSystemMetrics()
	}
}

// updateSystemMetrics updates system metrics
func (em *EnhancedMetrics) updateSystemMetrics() {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// Update goroutine count
	em.goroutineCount.Set(float64(runtime.NumGoroutine()))

	// Update memory metrics
	em.memoryUsage.WithLabelValues("heap_alloc").Set(float64(memStats.HeapAlloc))
	em.memoryUsage.WithLabelValues("heap_sys").Set(float64(memStats.HeapSys))
	em.memoryUsage.WithLabelValues("heap_inuse").Set(float64(memStats.HeapInuse))
	em.memoryUsage.WithLabelValues("stack_inuse").Set(float64(memStats.StackInuse))
	em.memoryUsage.WithLabelValues("sys").Set(float64(memStats.Sys))

	// Update GC metrics
	em.memoryUsage.WithLabelValues("gc_next").Set(float64(memStats.NextGC))
	em.memoryUsage.WithLabelValues("gc_pause_total").Set(float64(memStats.PauseTotalNs))

	// File descriptor count (simplified - in production use platform-specific methods)
	em.fileDescriptorCount.Set(float64(em.getFileDescriptorCount()))
}

// getFileDescriptorCount gets current FD count (simplified implementation)
func (em *EnhancedMetrics) getFileDescriptorCount() int {
	// In a real implementation, use platform-specific methods
	// For now, return a placeholder
	return 50
}

// RecordLogProcessed records a processed log entry
func (em *EnhancedMetrics) RecordLogProcessed(sourceType, sink, status string) {
	em.logEntriesProcessed.WithLabelValues(sourceType, sink, status).Inc()
}

// RecordProcessingDuration records log processing duration
func (em *EnhancedMetrics) RecordProcessingDuration(pipeline, stage string, duration time.Duration) {
	em.logProcessingDuration.WithLabelValues(pipeline, stage).Observe(duration.Seconds())
}

// UpdateQueueSize updates queue size metrics
func (em *EnhancedMetrics) UpdateQueueSize(queueType, component string, size int) {
	em.queueSizes.WithLabelValues(queueType, component).Set(float64(size))
}

// RecordError records an error occurrence
func (em *EnhancedMetrics) RecordError(component, errorType, severity string) {
	em.errorRates.WithLabelValues(component, errorType, severity).Inc()
}

// UpdateThroughput updates throughput metrics
func (em *EnhancedMetrics) UpdateThroughput(component, window string, logsPerSecond float64) {
	em.throughputGauge.WithLabelValues(component, window).Set(logsPerSecond)
}

// RecordResponseTime records response time for operations
func (em *EnhancedMetrics) RecordResponseTime(operation, component string, duration time.Duration) {
	em.responseTime.WithLabelValues(operation, component).Observe(duration.Seconds())
}

// UpdateConnectionPoolStats updates connection pool metrics
func (em *EnhancedMetrics) UpdateConnectionPoolStats(poolType, metric string, value float64) {
	em.connectionPoolStats.WithLabelValues(poolType, metric).Set(value)
}

// RecordCompressionRatio records compression ratio achieved
func (em *EnhancedMetrics) RecordCompressionRatio(algorithm, sink string, ratio float64) {
	em.compressionRatio.WithLabelValues(algorithm, sink).Set(ratio)
}

// UpdateBatchingStats updates batching statistics
func (em *EnhancedMetrics) UpdateBatchingStats(component, metric string, value float64) {
	em.batchingStats.WithLabelValues(component, metric).Set(value)
}

// UpdateContainerCount updates monitored container count
func (em *EnhancedMetrics) UpdateContainerCount(count int) {
	em.containerCount.Set(float64(count))
}

// UpdateFileCount updates monitored file count
func (em *EnhancedMetrics) UpdateFileCount(count int) {
	em.fileCount.Set(float64(count))
}

// UpdateSinkHealth updates sink health status
func (em *EnhancedMetrics) UpdateSinkHealth(sinkName, sinkType string, healthy bool) {
	healthValue := 0.0
	if healthy {
		healthValue = 1.0
	}
	em.sinkHealth.WithLabelValues(sinkName, sinkType).Set(healthValue)
}

// UpdateLeakDetection updates resource leak detection metrics
func (em *EnhancedMetrics) UpdateLeakDetection(resourceType, metric string, value float64) {
	em.leakDetection.WithLabelValues(resourceType, metric).Set(value)
}

// RegisterCustomMetric registers a custom metric
func (em *EnhancedMetrics) RegisterCustomMetric(name string, metric prometheus.Metric) error {
	em.customMutex.Lock()
	defer em.customMutex.Unlock()

	if _, exists := em.customMetrics[name]; exists {
		return fmt.Errorf("metric %s already registered", name)
	}

	em.customMetrics[name] = metric
	// Note: prometheus.Metric cannot be registered directly, would need prometheus.Collector
	return nil
}

// UnregisterCustomMetric unregisters a custom metric
func (em *EnhancedMetrics) UnregisterCustomMetric(name string) error {
	em.customMutex.Lock()
	defer em.customMutex.Unlock()

	_, exists := em.customMetrics[name]
	if !exists {
		return fmt.Errorf("metric %s not found", name)
	}

	delete(em.customMetrics, name)
	return nil
}

// GetCustomMetric returns a custom metric by name
func (em *EnhancedMetrics) GetCustomMetric(name string) (prometheus.Metric, bool) {
	em.customMutex.RLock()
	defer em.customMutex.RUnlock()

	metric, exists := em.customMetrics[name]
	return metric, exists
}

// CreateTimer creates a timer for measuring operation duration
func (em *EnhancedMetrics) CreateTimer(operation, component string) func() {
	start := time.Now()
	return func() {
		em.RecordResponseTime(operation, component, time.Since(start))
	}
}

// GetUptime returns application uptime
func (em *EnhancedMetrics) GetUptime() time.Duration {
	return time.Since(em.startTime)
}

// GetMetricSummary returns a summary of key metrics
func (em *EnhancedMetrics) GetMetricSummary() map[string]interface{} {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	return map[string]interface{}{
		"uptime_seconds":     em.GetUptime().Seconds(),
		"goroutines":         runtime.NumGoroutine(),
		"memory_alloc_mb":    float64(memStats.Alloc) / (1024 * 1024),
		"memory_sys_mb":      float64(memStats.Sys) / (1024 * 1024),
		"gc_cycles":          memStats.NumGC,
		"file_descriptors":   em.getFileDescriptorCount(),
		"custom_metrics":     len(em.customMetrics),
		"monitoring_active":  em.isRunning,
	}
}

// PerformanceProfiler provides performance profiling utilities
type PerformanceProfiler struct {
	metrics *EnhancedMetrics
	samples map[string][]time.Duration
	mutex   sync.RWMutex
}

// NewPerformanceProfiler creates a new performance profiler
func NewPerformanceProfiler(metrics *EnhancedMetrics) *PerformanceProfiler {
	return &PerformanceProfiler{
		metrics: metrics,
		samples: make(map[string][]time.Duration),
	}
}

// StartTrace starts a performance trace
func (pp *PerformanceProfiler) StartTrace(name string) func() {
	start := time.Now()
	return func() {
		duration := time.Since(start)
		pp.recordSample(name, duration)
		pp.metrics.RecordResponseTime(name, "profiler", duration)
	}
}

// recordSample records a performance sample
func (pp *PerformanceProfiler) recordSample(name string, duration time.Duration) {
	pp.mutex.Lock()
	defer pp.mutex.Unlock()

	samples := pp.samples[name]
	if len(samples) >= 100 { // Keep last 100 samples
		samples = samples[1:]
	}
	samples = append(samples, duration)
	pp.samples[name] = samples
}

// GetAverageLatency returns average latency for an operation
func (pp *PerformanceProfiler) GetAverageLatency(name string) time.Duration {
	pp.mutex.RLock()
	defer pp.mutex.RUnlock()

	samples := pp.samples[name]
	if len(samples) == 0 {
		return 0
	}

	var total time.Duration
	for _, sample := range samples {
		total += sample
	}

	return total / time.Duration(len(samples))
}

// GetPercentile returns the specified percentile for an operation
func (pp *PerformanceProfiler) GetPercentile(name string, percentile float64) time.Duration {
	pp.mutex.RLock()
	defer pp.mutex.RUnlock()

	samples := pp.samples[name]
	if len(samples) == 0 {
		return 0
	}

	// Simple percentile calculation (would use more sophisticated sorting in production)
	index := int(float64(len(samples)) * percentile / 100.0)
	if index >= len(samples) {
		index = len(samples) - 1
	}

	return samples[index]
}

