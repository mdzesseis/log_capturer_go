package metrics

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

var (
	// Counter para logs processados
	LogsProcessedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "log_capturer_logs_processed_total",
			Help: "Total number of logs processed",
		},
		[]string{"source_type", "source_id", "pipeline"},
	)

	// Gauge para logs por segundo
	LogsPerSecond = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "log_capturer_logs_per_second",
			Help: "Current logs per second throughput",
		},
		[]string{"component"},
	)

	// Gauge para utilização da fila do dispatcher
	DispatcherQueueUtilization = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "log_capturer_dispatcher_queue_utilization",
		Help: "Current utilization of the dispatcher queue (0.0 to 1.0)",
	})

	// Histograma para duração de steps de processamento
	ProcessingStepDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "log_capturer_processing_step_duration_seconds",
			Help:    "Time spent in each processing step",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"pipeline", "step"},
	)

	// Counter para logs enviados para sinks
	LogsSentTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "log_capturer_logs_sent_total",
			Help: "Total number of logs sent to sinks",
		},
		[]string{"sink_type", "status"},
	)

	// Counter para erros
	ErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "log_capturer_errors_total",
			Help: "Total number of errors",
		},
		[]string{"component", "error_type"},
	)

	// Gauge para arquivos monitorados
	FilesMonitored = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "log_capturer_files_monitored",
			Help: "Number of files being monitored",
		},
		[]string{"filepath", "source_type"},
	)

	// Gauge para containers monitorados
	ContainersMonitored = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "log_capturer_containers_monitored",
			Help: "Number of containers being monitored",
		},
		[]string{"container_id", "container_name", "image"},
	)

	// Gauge para utilização de filas dos sinks
	SinkQueueUtilization = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "log_capturer_sink_queue_utilization",
			Help: "Queue utilization of sinks (0.0 to 1.0)",
		},
		[]string{"sink_type"},
	)

	// Gauge para status de saúde dos componentes
	ComponentHealth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "log_capturer_component_health",
			Help: "Health status of components (1 = healthy, 0 = unhealthy)",
		},
		[]string{"component_type", "component_name"},
	)

	// Histogram para latência de processamento
	ProcessingDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "log_capturer_processing_duration_seconds",
			Help:    "Time spent processing logs",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0},
		},
		[]string{"component", "operation"},
	)

	// Histogram para latência de envio para sinks
	SinkSendDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "log_capturer_sink_send_duration_seconds",
			Help:    "Time spent sending logs to sinks",
			Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0},
		},
		[]string{"sink_type"},
	)

	// Gauge para tamanho das filas
	QueueSize = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "log_capturer_queue_size",
			Help: "Current size of queues",
		},
		[]string{"component", "queue_type"},
	)

	// Counter para heartbeats de tarefas
	TaskHeartbeats = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "log_capturer_task_heartbeats_total",
			Help: "Total number of task heartbeats",
		},
		[]string{"task_id", "task_type"},
	)

	// Gauge para tarefas ativas
	ActiveTasks = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "log_capturer_active_tasks",
			Help: "Number of active tasks",
		},
		[]string{"task_type", "state"},
	)

	// NOTE: Circuit breaker metrics removed as the package was deleted

	// Gauge para uso de memória
	MemoryUsage = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "log_capturer_memory_usage_bytes",
			Help: "Memory usage in bytes",
		},
		[]string{"type"},
	)

	// Gauge para uso de CPU
	CPUUsage = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "log_capturer_cpu_usage_percent",
			Help: "CPU usage percentage",
		},
	)

	// Counter para garbage collection
	GCRuns = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "log_capturer_gc_runs_total",
			Help: "Total number of garbage collection runs",
		},
	)

	// Gauge para número de goroutines
	Goroutines = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "log_capturer_goroutines",
			Help: "Number of goroutines",
		},
	)

	// Gauge para file descriptors abertos
	FileDescriptors = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "log_capturer_file_descriptors_open",
			Help: "Number of open file descriptors",
		},
	)

	// Histogram para pausas de GC
	GCPauseDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "log_capturer_gc_pause_duration_seconds",
			Help:    "GC pause duration in seconds",
			Buckets: []float64{0.00001, 0.00005, 0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0},
		},
	)

	// Gauge para total de arquivos monitorados (agregado)
	TotalFilesMonitored = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "log_capturer_total_files_monitored",
			Help: "Total number of files being monitored across all sources",
		},
	)

	// Gauge para total de containers monitorados (agregado)
	TotalContainersMonitored = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "log_capturer_total_containers_monitored",
			Help: "Total number of containers being monitored",
		},
	)

	// Enhanced metrics - Advanced monitoring metrics
	DiskUsageBytes = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "log_capturer_disk_usage_bytes",
			Help: "Disk usage in bytes by mount point",
		},
		[]string{"mount_point", "device"},
	)

	ResponseTimeSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "log_capturer_response_time_seconds",
			Help:    "Response time in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"endpoint", "method"},
	)

	ConnectionPoolStats = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "log_capturer_connection_pool_stats",
			Help: "Connection pool statistics",
		},
		[]string{"pool_name", "stat_type"},
	)

	CompressionRatio = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "log_capturer_compression_ratio",
			Help: "Compression ratio for different components",
		},
		[]string{"component", "algorithm"},
	)

	BatchingStats = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "log_capturer_batching_stats",
			Help: "Batching statistics",
		},
		[]string{"component", "stat_type"},
	)

	LeakDetection = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "log_capturer_leak_detection",
			Help: "Resource leak detection metrics",
		},
		[]string{"resource_type", "component"},
	)

	// =============================================================================
	// KAFKA SINK METRICS
	// =============================================================================

	// Kafka messages produced total
	KafkaMessagesProducedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kafka_messages_produced_total",
			Help: "Total number of messages produced to Kafka",
		},
		[]string{"topic", "status"},
	)

	// Kafka producer errors
	KafkaProducerErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kafka_producer_errors_total",
			Help: "Total number of Kafka producer errors",
		},
		[]string{"topic", "error_type"},
	)

	// Kafka batch size (messages per batch sent)
	KafkaBatchSize = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kafka_batch_size_messages",
			Help:    "Number of messages in each Kafka batch",
			Buckets: []float64{1, 10, 50, 100, 250, 500, 1000, 2500, 5000, 10000},
		},
		[]string{"topic"},
	)

	// Kafka batch send duration
	KafkaBatchSendDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kafka_batch_send_duration_seconds",
			Help:    "Time spent sending a batch to Kafka",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0},
		},
		[]string{"topic"},
	)

	// Kafka queue size (internal queue before producing)
	KafkaQueueSize = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_queue_size",
			Help: "Current size of Kafka internal queue",
		},
		[]string{"sink_name"},
	)

	// Kafka queue utilization (0.0 to 1.0)
	KafkaQueueUtilization = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_queue_utilization",
			Help: "Kafka queue utilization percentage (0.0 to 1.0)",
		},
		[]string{"sink_name"},
	)

	// Kafka partition distribution
	KafkaPartitionMessages = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kafka_partition_messages_total",
			Help: "Total messages sent to each Kafka partition",
		},
		[]string{"topic", "partition"},
	)

	// Kafka compression ratio
	KafkaCompressionRatio = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_compression_ratio",
			Help: "Kafka message compression ratio (compressed/uncompressed)",
		},
		[]string{"topic", "compression_type"},
	)

	// Kafka backpressure events
	KafkaBackpressureTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kafka_backpressure_events_total",
			Help: "Total number of backpressure events (queue full, etc)",
		},
		[]string{"sink_name", "threshold_level"},
	)

	// Kafka circuit breaker state
	KafkaCircuitBreakerState = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_circuit_breaker_state",
			Help: "Kafka circuit breaker state (0=closed, 1=half-open, 2=open)",
		},
		[]string{"sink_name"},
	)

	// Kafka message size
	KafkaMessageSizeBytes = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kafka_message_size_bytes",
			Help:    "Size of Kafka messages in bytes",
			Buckets: []float64{100, 500, 1024, 5120, 10240, 51200, 102400, 512000, 1048576},
		},
		[]string{"topic"},
	)

	// Kafka DLQ messages
	KafkaDLQMessagesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kafka_dlq_messages_total",
			Help: "Total number of messages sent to Kafka DLQ",
		},
		[]string{"topic", "reason"},
	)

	// Kafka connection status
	KafkaConnectionStatus = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_connection_status",
			Help: "Kafka connection status (1=connected, 0=disconnected)",
		},
		[]string{"broker", "sink_name"},
	)

	// =============================================================================
	// CONTAINER MONITOR STREAM METRICS
	// =============================================================================

	// Active container log streams
	ActiveContainerStreams = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "log_capturer_container_streams_active",
			Help: "Number of active container log streams",
		},
	)

	// Stream rotations total
	StreamRotationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "log_capturer_container_stream_rotations_total",
			Help: "Total number of stream rotations",
		},
		[]string{"container_id", "container_name"},
	)

	// Stream age when rotated
	StreamAgeSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "log_capturer_container_stream_age_seconds",
			Help:    "Age of container streams when rotated",
			Buckets: []float64{60, 120, 180, 240, 300, 360, 420, 480, 540, 600},
		},
		[]string{"container_id"},
	)

	// Stream errors by type
	StreamErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "log_capturer_container_stream_errors_total",
			Help: "Total stream errors by type",
		},
		[]string{"error_type", "container_id"},
	)

	// Stream pool utilization
	StreamPoolUtilization = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "log_capturer_container_stream_pool_utilization",
			Help: "Stream pool utilization (0.0 to 1.0)",
		},
	)
)

// MetricsServer servidor HTTP para métricas Prometheus
type MetricsServer struct {
	server *http.Server
	logger *logrus.Logger
}

var (
	metricsRegisteredOnce sync.Once
)

// safeRegister safely registers metrics, ignoring already registered ones
func safeRegister(collector prometheus.Collector) {
	defer func() {
		if r := recover(); r != nil {
			// Ignore "duplicate metrics collector registration attempted" panics
			if _, ok := r.(error); ok {
				// Silently ignore registration errors
			}
		}
	}()
	prometheus.MustRegister(collector)
}

// NewMetricsServer cria um novo servidor de métricas
func NewMetricsServer(addr string, logger *logrus.Logger) *MetricsServer {
	// Registrar todas as métricas de forma segura (apenas uma vez)
	metricsRegisteredOnce.Do(func() {
		// Register metrics safely, ignoring conflicts
		safeRegister(LogsProcessedTotal)
		safeRegister(LogsPerSecond)
		safeRegister(DispatcherQueueUtilization)
		safeRegister(ProcessingStepDuration)
		safeRegister(LogsSentTotal)
		safeRegister(ErrorsTotal)
		safeRegister(FilesMonitored)
		safeRegister(ContainersMonitored)
		safeRegister(SinkQueueUtilization)
		safeRegister(ComponentHealth)
		safeRegister(ProcessingDuration)
		safeRegister(SinkSendDuration)
		safeRegister(QueueSize)
		safeRegister(TaskHeartbeats)
		safeRegister(ActiveTasks)
		// CircuitBreakerState and CircuitBreakerEvents removed (package deleted)
		safeRegister(MemoryUsage)
		safeRegister(CPUUsage)
		safeRegister(GCRuns)
		safeRegister(Goroutines)
		safeRegister(FileDescriptors)
		safeRegister(GCPauseDuration)
		safeRegister(TotalFilesMonitored)
		safeRegister(TotalContainersMonitored)
		// Enhanced metrics
		safeRegister(DiskUsageBytes)
		safeRegister(ResponseTimeSeconds)
		safeRegister(ConnectionPoolStats)
		safeRegister(CompressionRatio)
		safeRegister(BatchingStats)
		safeRegister(LeakDetection)
		// Kafka sink metrics
		safeRegister(KafkaMessagesProducedTotal)
		safeRegister(KafkaProducerErrorsTotal)
		safeRegister(KafkaBatchSize)
		safeRegister(KafkaBatchSendDuration)
		safeRegister(KafkaQueueSize)
		safeRegister(KafkaQueueUtilization)
		safeRegister(KafkaPartitionMessages)
		safeRegister(KafkaCompressionRatio)
		safeRegister(KafkaBackpressureTotal)
		safeRegister(KafkaCircuitBreakerState)
		safeRegister(KafkaMessageSizeBytes)
		safeRegister(KafkaDLQMessagesTotal)
		safeRegister(KafkaConnectionStatus)
		// Container monitor stream metrics
		safeRegister(ActiveContainerStreams)
		safeRegister(StreamRotationsTotal)
		safeRegister(StreamAgeSeconds)
		safeRegister(StreamErrorsTotal)
		safeRegister(StreamPoolUtilization)
	})

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	return &MetricsServer{
		server: &http.Server{
			Addr:    addr,
			Handler: mux,
		},
		logger: logger,
	}
}

// Start inicia o servidor de métricas
func (ms *MetricsServer) Start() error {
	ms.logger.WithField("addr", ms.server.Addr).Info("Starting metrics server")

	go func() {
		if err := ms.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			ms.logger.WithError(err).Error("Metrics server error")
		}
	}()

	return nil
}

// Stop para o servidor de métricas
func (ms *MetricsServer) Stop() error {
	ms.logger.Info("Stopping metrics server")
	return ms.server.Close()
}

// Funções auxiliares para métricas comuns

// RecordLogProcessed registra um log processado
func RecordLogProcessed(sourceType, sourceID, pipeline string) {
	LogsProcessedTotal.WithLabelValues(sourceType, sourceID, pipeline).Inc()
}

// RecordLogSent registra um log enviado para sink
func RecordLogSent(sinkType, status string) {
	LogsSentTotal.WithLabelValues(sinkType, status).Inc()
}

// RecordError registra um erro
func RecordError(component, errorType string) {
	ErrorsTotal.WithLabelValues(component, errorType).Inc()
}

// SetFileMonitored define se um arquivo está sendo monitorado
func SetFileMonitored(filepath, sourceType string, monitored bool) {
	var value float64
	if monitored {
		value = 1
	}
	FilesMonitored.WithLabelValues(filepath, sourceType).Set(value)
}

// SetContainerMonitored define se um container está sendo monitorado
func SetContainerMonitored(containerID, containerName, image string, monitored bool) {
	var value float64
	if monitored {
		value = 1
	}
	ContainersMonitored.WithLabelValues(containerID, containerName, image).Set(value)
}

// RecordContainerEvent registra eventos de containers Docker
func RecordContainerEvent(event, containerID string) {
	ErrorsTotal.WithLabelValues("container_monitor", event).Inc()
}

// SetSinkQueueUtilization define a utilização da fila de um sink
func SetSinkQueueUtilization(sinkType string, utilization float64) {
	SinkQueueUtilization.WithLabelValues(sinkType).Set(utilization)
}

// SetComponentHealth define o status de saúde de um componente
func SetComponentHealth(componentType, componentName string, healthy bool) {
	var value float64
	if healthy {
		value = 1
	}
	ComponentHealth.WithLabelValues(componentType, componentName).Set(value)
}

// RecordProcessingDuration registra a duração de processamento
func RecordProcessingDuration(component, operation string, duration time.Duration) {
	ProcessingDuration.WithLabelValues(component, operation).Observe(duration.Seconds())
}

// RecordSinkSendDuration registra a duração de envio para sink
func RecordSinkSendDuration(sinkType string, duration time.Duration) {
	SinkSendDuration.WithLabelValues(sinkType).Observe(duration.Seconds())
}

// SetQueueSize define o tamanho de uma fila
func SetQueueSize(component, queueType string, size int) {
	QueueSize.WithLabelValues(component, queueType).Set(float64(size))
}

// RecordTaskHeartbeat registra um heartbeat de tarefa
func RecordTaskHeartbeat(taskID, taskType string) {
	TaskHeartbeats.WithLabelValues(taskID, taskType).Inc()
}

// SetActiveTasks define o número de tarefas ativas
func SetActiveTasks(taskType, state string, count int) {
	ActiveTasks.WithLabelValues(taskType, state).Set(float64(count))
}

// Circuit breaker functions removed (package deleted)

// EnhancedMetrics provides comprehensive monitoring and metrics collection
type EnhancedMetrics struct {
	logger *logrus.Logger

	// Custom metrics registry
	customMetrics map[string]prometheus.Metric
	customMutex   sync.RWMutex

	// Internal state
	isRunning bool
	startTime time.Time
}

// NewEnhancedMetrics creates a new enhanced metrics instance
func NewEnhancedMetrics(logger *logrus.Logger) *EnhancedMetrics {
	em := &EnhancedMetrics{
		logger:        logger,
		customMetrics: make(map[string]prometheus.Metric),
		startTime:     time.Now(),
	}

	// Note: Advanced metrics (diskUsage, responseTime, etc.) are now global variables
	// registered in NewMetricsServer, so we don't need to initialize them here

	return em
}

// UpdateSystemMetrics updates system-level metrics
func (em *EnhancedMetrics) UpdateSystemMetrics() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// Update memory metrics
	MemoryUsage.WithLabelValues("heap_alloc").Set(float64(m.HeapAlloc))
	MemoryUsage.WithLabelValues("heap_sys").Set(float64(m.HeapSys))
	MemoryUsage.WithLabelValues("heap_idle").Set(float64(m.HeapIdle))
	MemoryUsage.WithLabelValues("heap_inuse").Set(float64(m.HeapInuse))

	// Update goroutine count
	Goroutines.Set(float64(runtime.NumGoroutine()))

	// Update GC metrics
	GCRuns.Add(float64(m.NumGC))

	// Update GC pause duration (last pause in nanoseconds converted to seconds)
	if m.NumGC > 0 {
		// Get the most recent GC pause time
		lastPauseNs := m.PauseNs[(m.NumGC+255)%256]
		GCPauseDuration.Observe(float64(lastPauseNs) / 1e9)
	}

	// Update file descriptors (attempt to read from /proc/self/fd on Linux)
	if fds := getOpenFileDescriptors(); fds >= 0 {
		FileDescriptors.Set(float64(fds))
	}
}

// RecordDiskUsage records disk usage metrics
func (em *EnhancedMetrics) RecordDiskUsage(mountPoint, device string, usage int64) {
	DiskUsageBytes.WithLabelValues(mountPoint, device).Set(float64(usage))
}

// RecordResponseTime records HTTP response time
func (em *EnhancedMetrics) RecordResponseTime(endpoint, method string, duration time.Duration) {
	ResponseTimeSeconds.WithLabelValues(endpoint, method).Observe(duration.Seconds())
}

// RecordConnectionPoolStats records connection pool statistics
func (em *EnhancedMetrics) RecordConnectionPoolStats(poolName, statType string, value float64) {
	ConnectionPoolStats.WithLabelValues(poolName, statType).Set(value)
}

// RecordCompressionRatio records compression ratio
func (em *EnhancedMetrics) RecordCompressionRatio(component, algorithm string, ratio float64) {
	CompressionRatio.WithLabelValues(component, algorithm).Set(ratio)
}

// RecordBatchingStats records batching statistics
func (em *EnhancedMetrics) RecordBatchingStats(component, statType string, value float64) {
	BatchingStats.WithLabelValues(component, statType).Set(value)
}

// RecordLeakDetection records resource leak detection metrics
func (em *EnhancedMetrics) RecordLeakDetection(resourceType, component string, count float64) {
	LeakDetection.WithLabelValues(resourceType, component).Set(count)
}

// Start begins the enhanced metrics collection
func (em *EnhancedMetrics) Start() error {
	if em.isRunning {
		return fmt.Errorf("enhanced metrics already running")
	}

	em.isRunning = true
	em.logger.Info("Enhanced metrics collection started")

	// Start periodic system metrics update
	go em.systemMetricsLoop()

	return nil
}

// Stop stops the enhanced metrics collection
func (em *EnhancedMetrics) Stop() error {
	if !em.isRunning {
		return nil
	}

	em.isRunning = false
	em.logger.Info("Enhanced metrics collection stopped")

	return nil
}

// systemMetricsLoop periodically updates system metrics
func (em *EnhancedMetrics) systemMetricsLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for em.isRunning {
		select {
		case <-ticker.C:
			em.UpdateSystemMetrics()
		}
	}
}

// getOpenFileDescriptors counts the number of open file descriptors
// Works on Linux by reading /proc/self/fd directory
func getOpenFileDescriptors() int {
	files, err := ioutil.ReadDir("/proc/self/fd")
	if err != nil {
		// Not on Linux or unable to read, return -1 to skip metric update
		return -1
	}
	return len(files)
}

// UpdateTotalFilesMonitored updates the total count of monitored files
func UpdateTotalFilesMonitored(count int) {
	TotalFilesMonitored.Set(float64(count))
}

// UpdateTotalContainersMonitored updates the total count of monitored containers
func UpdateTotalContainersMonitored(count int) {
	TotalContainersMonitored.Set(float64(count))
}

// =============================================================================
// CONTAINER MONITOR STREAM HELPER FUNCTIONS
// =============================================================================

// RecordStreamRotation records a stream rotation event
func RecordStreamRotation(containerID, containerName string, ageSeconds float64) {
	StreamRotationsTotal.WithLabelValues(containerID, containerName).Inc()
	StreamAgeSeconds.WithLabelValues(containerID).Observe(ageSeconds)
}

// RecordStreamError records a stream error
func RecordStreamError(errorType, containerID string) {
	StreamErrorsTotal.WithLabelValues(errorType, containerID).Inc()
}

// UpdateActiveStreams updates the count of active streams
func UpdateActiveStreams(count int) {
	ActiveContainerStreams.Set(float64(count))
}

// UpdateStreamPoolUtilization updates stream pool utilization
func UpdateStreamPoolUtilization(current, max int) {
	if max > 0 {
		StreamPoolUtilization.Set(float64(current) / float64(max))
	} else {
		StreamPoolUtilization.Set(0)
	}
}