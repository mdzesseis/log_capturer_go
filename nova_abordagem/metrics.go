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
	"github.com/shirou/gopsutil/v3/cpu"
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

	// Gauge para profundidade da fila do dispatcher (número de itens)
	DispatcherQueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "log_capturer_dispatcher_queue_depth",
		Help: "Current number of entries in dispatcher queue",
	})

	// Gauge para utilização da fila do dispatcher
	DispatcherQueueUtilization = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "log_capturer_dispatcher_queue_utilization",
		Help: "Current utilization of the dispatcher queue (0.0 to 1.0)",
	})

	// NOVO: Gauge para tamanho da fila de retry centralizada
	DispatcherRetryQueueSize = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "log_capturer_dispatcher_retry_queue_size",
		Help: "Current number of items waiting in the retry queue",
	})

	// NOVO: Counter para itens descartados da fila de retry (overflow)
	DispatcherRetryDropsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "log_capturer_dispatcher_retry_drops_total",
		Help: "Total number of items dropped from retry queue due to overflow",
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

	// Deduplication metrics
	LogsDeduplicated = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "log_capturer_logs_deduplicated_total",
			Help: "Total logs deduplicated",
		},
		[]string{"source_type", "source_id"},
	)

	DeduplicationCacheSize = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "log_capturer_deduplication_cache_size",
			Help: "Current size of deduplication cache",
		},
	)

	DeduplicationCacheHitRate = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "log_capturer_deduplication_hit_rate",
			Help: "Deduplication cache hit rate (0.0 to 1.0)",
		},
	)

	DeduplicationDuplicateRate = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "log_capturer_deduplication_duplicate_rate",
			Help: "Duplicate log rate (0.0 to 1.0)",
		},
	)

	DeduplicationCacheEvictions = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "log_capturer_deduplication_cache_evictions_total",
			Help: "Total cache evictions (LRU or TTL expiration)",
		},
	)

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

	// Task 2: File monitor new features metrics
	FileMonitorOldLogsIgnored = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "log_capturer_file_monitor_old_logs_ignored_total",
			Help: "Total number of old logs ignored by file monitor (timestamp before start)",
		},
		[]string{"component", "file_path"},
	)

	FileMonitorOffsetRestored = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "log_capturer_file_monitor_offset_restored_total",
			Help: "Total number of times offset was restored from persistence",
		},
		[]string{"component", "file_path"},
	)

	FileMonitorRetryQueueSize = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "log_capturer_file_monitor_retry_queue_size",
			Help: "Current size of the file monitor retry queue",
		},
		[]string{"component"},
	)

	FileMonitorDropsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "log_capturer_file_monitor_drops_total",
			Help: "Total number of entries dropped from retry queue",
		},
		[]string{"component", "reason"},
	)

	FileMonitorRetryQueued = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "log_capturer_file_monitor_retry_queued_total",
			Help: "Total number of entries added to retry queue",
		},
		[]string{"component"},
	)

	FileMonitorRetrySuccess = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "log_capturer_file_monitor_retry_success_total",
			Help: "Total number of successful retries",
		},
		[]string{"component"},
	)

	FileMonitorRetryFailed = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "log_capturer_file_monitor_retry_failed_total",
			Help: "Total number of failed retries",
		},
		[]string{"component"},
	)

	FileMonitorRetryGiveUp = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "log_capturer_file_monitor_retry_giveup_total",
			Help: "Total number of retries given up (max attempts exceeded)",
		},
		[]string{"component"},
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

	// KAFKA SINK METRICS
	KafkaMessagesProducedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kafka_messages_produced_total",
			Help: "Total number of messages produced to Kafka",
		},
		[]string{"topic", "status"},
	)

	KafkaProducerErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kafka_producer_errors_total",
			Help: "Total number of Kafka producer errors",
		},
		[]string{"topic", "error_type"},
	)

	KafkaBatchSize = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kafka_batch_size_messages",
			Help:    "Number of messages in each Kafka batch",
			Buckets: []float64{1, 10, 50, 100, 250, 500, 1000, 2500, 5000, 10000},
		},
		[]string{"topic"},
	)

	KafkaBatchSendDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kafka_batch_send_duration_seconds",
			Help:    "Time spent sending a batch to Kafka",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0},
		},
		[]string{"topic"},
	)

	KafkaQueueSize = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_queue_size",
			Help: "Current size of Kafka internal queue",
		},
		[]string{"sink_name"},
	)

	KafkaQueueUtilization = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_queue_utilization",
			Help: "Kafka queue utilization percentage (0.0 to 1.0)",
		},
		[]string{"sink_name"},
	)

	KafkaPartitionMessages = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kafka_partition_messages_total",
			Help: "Total messages sent to each Kafka partition",
		},
		[]string{"topic", "partition"},
	)

	KafkaCompressionRatio = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_compression_ratio",
			Help: "Kafka message compression ratio (compressed/uncompressed)",
		},
		[]string{"topic", "compression_type"},
	)

	KafkaBackpressureTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kafka_backpressure_events_total",
			Help: "Total number of backpressure events (queue full, etc)",
		},
		[]string{"sink_name", "threshold_level"},
	)

	KafkaCircuitBreakerState = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_circuit_breaker_state",
			Help: "Kafka circuit breaker state (0=closed, 1=half-open, 2=open)",
		},
		[]string{"sink_name"},
	)

	KafkaMessageSizeBytes = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kafka_message_size_bytes",
			Help:    "Size of Kafka messages in bytes",
			Buckets: []float64{100, 500, 1024, 5120, 10240, 51200, 102400, 512000, 1048576},
		},
		[]string{"topic"},
	)

	KafkaDLQMessagesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kafka_dlq_messages_total",
			Help: "Total number of messages sent to Kafka DLQ",
		},
		[]string{"topic", "reason"},
	)

	KafkaConnectionStatus = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_connection_status",
			Help: "Kafka connection status (1=connected, 0=disconnected)",
		},
		[]string{"broker", "sink_name"},
	)

	// CONTAINER MONITOR STREAM METRICS
	LogsCollected = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "log_capturer_logs_collected_total",
			Help: "Total number of log lines collected from containers",
		},
		[]string{"stream", "container"},
	)

	ContainerEvents = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "log_capturer_container_events_total",
			Help: "Total number of container lifecycle events",
		},
		[]string{"event_type", "container"},
	)

	ActiveContainerStreams = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "log_capturer_container_streams_active",
			Help: "Number of active container log streams",
		},
	)

	StreamRotationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "log_capturer_container_stream_rotations_total",
			Help: "Total number of stream rotations",
		},
		[]string{"container_id", "container_name"},
	)

	StreamAgeSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "log_capturer_container_stream_age_seconds",
			Help:    "Age of container streams when rotated",
			Buckets: []float64{60, 120, 180, 240, 300, 360, 420, 480, 540, 600},
		},
		[]string{"container_id"},
	)

	StreamErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "log_capturer_container_stream_errors_total",
			Help: "Total stream errors by type",
		},
		[]string{"error_type", "container_id"},
	)

	StreamPoolUtilization = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "log_capturer_container_stream_pool_utilization",
			Help: "Stream pool utilization (0.0 to 1.0)",
		},
	)

	// DLQ METRICS
	DLQStoredEntries = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "log_capturer_dlq_stored_total",
			Help: "Total entries stored in DLQ",
		},
		[]string{"sink", "reason"},
	)

	DLQEntriesTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "log_capturer_dlq_entries_total",
			Help: "Total number of entries in DLQ",
		},
		[]string{"sink"},
	)

	DLQSizeBytes = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "log_capturer_dlq_size_bytes",
			Help: "Total size of DLQ in bytes",
		},
		[]string{"sink"},
	)

	DLQReprocessAttempts = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "log_capturer_dlq_reprocess_attempts_total",
			Help: "Total DLQ reprocessing attempts",
		},
		[]string{"sink", "result"},
	)

	// TIMESTAMP LEARNING METRICS
	TimestampRejectionTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "log_capturer_timestamp_rejection_total",
			Help: "Total timestamp rejections by reason",
		},
		[]string{"sink", "reason"},
	)

	TimestampClampedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "log_capturer_timestamp_clamped_total",
			Help: "Total timestamps clamped to acceptable range",
		},
		[]string{"sink"},
	)

	TimestampMaxAcceptableAge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "log_capturer_timestamp_max_acceptable_age_seconds",
			Help: "Current learned max acceptable age for timestamps",
		},
		[]string{"sink"},
	)

	LokiErrorTypeTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "log_capturer_loki_error_type_total",
			Help: "Loki errors by classified type",
		},
		[]string{"sink", "error_type"},
	)

	TimestampLearningEventsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "log_capturer_timestamp_learning_events_total",
			Help: "Total timestamp learning events from Loki errors",
		},
		[]string{"sink"},
	)

	// POSITION SYSTEM METRICS (Phase 1)
	PositionRotationDetected = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "log_capturer_position_rotation_detected_total",
			Help: "File rotations detected via inode change",
		},
		[]string{"file_path"},
	)

	PositionTruncationDetected = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "log_capturer_position_truncation_detected_total",
			Help: "File truncations detected (offset > size)",
		},
		[]string{"file_path"},
	)

	PositionSaveSuccess = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "log_capturer_position_save_success_total",
			Help: "Successful position saves to disk",
		},
	)

	PositionSaveFailed = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "log_capturer_position_save_failed_total",
			Help: "Failed position saves to disk",
		},
		[]string{"error_type"},
	)

	PositionLagSeconds = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "log_capturer_position_lag_seconds",
			Help: "Seconds since last successful position save",
		},
		[]string{"manager_type"},
	)

	PositionFlushTrigger = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "log_capturer_position_flush_trigger_total",
			Help: "Position flushes by trigger type",
		},
		[]string{"trigger_type"},
	)

	PositionOffsetReset = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "log_capturer_position_offset_reset_total",
			Help: "Position offset resets due to truncation or corruption",
		},
		[]string{"file_path", "reason"},
	)

	// POSITION SYSTEM METRICS (Phase 2 - Health Monitoring)
	PositionActiveByStatus = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "log_capturer_position_active_by_status",
			Help: "Active positions grouped by status",
		},
		[]string{"status"},
	)

	PositionUpdateRate = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "log_capturer_position_update_rate_per_second",
			Help: "Rate of position updates per second",
		},
		[]string{"manager_type"},
	)

	PositionFileSize = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "log_capturer_position_file_size_bytes",
			Help: "Size of position tracking files",
		},
		[]string{"file_type"},
	)

	PositionLagDistribution = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "log_capturer_position_lag_seconds_histogram",
			Help:    "Distribution of position lag times",
			Buckets: prometheus.ExponentialBuckets(0.1, 2, 10),
		},
		[]string{"manager_type"},
	)

	PositionMemoryUsage = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "log_capturer_position_memory_bytes",
			Help: "Memory used by position tracking structures",
		},
	)

	CheckpointHealth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "log_capturer_checkpoint_health",
			Help: "Checkpoint system health (1=healthy, 0=unhealthy)",
		},
		[]string{"component"},
	)

	PositionBackpressure = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "log_capturer_position_backpressure",
			Help: "Position system backpressure indicator (0-1)",
		},
		[]string{"manager_type"},
	)

	PositionCorruptionDetected = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "log_capturer_position_corruption_detected_total",
			Help: "Position file corruption detections",
		},
		[]string{"file_type", "recovery_action"},
	)

	// CHECKPOINT MANAGER METRICS (Phase 2)
	PositionCheckpointCreatedTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "log_capturer_position_checkpoint_created_total",
			Help: "Total checkpoints created",
		},
	)

	PositionCheckpointSizeBytes = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "log_capturer_position_checkpoint_size_bytes",
			Help: "Size of last checkpoint in bytes",
		},
	)

	PositionCheckpointAgeSeconds = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "log_capturer_position_checkpoint_age_seconds",
			Help: "Age of last checkpoint in seconds",
		},
	)

	PositionCheckpointRestoreAttemptsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "log_capturer_position_checkpoint_restore_attempts_total",
			Help: "Total checkpoint restore attempts",
		},
		[]string{"result"},
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
		safeRegister(DispatcherQueueDepth)
		safeRegister(DispatcherRetryQueueSize)  // NOVO
		safeRegister(DispatcherRetryDropsTotal) // NOVO
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
		safeRegister(LogsDeduplicated)
		safeRegister(DeduplicationCacheSize)
		safeRegister(DeduplicationCacheHitRate)
		safeRegister(DeduplicationDuplicateRate)
		safeRegister(DeduplicationCacheEvictions)
		safeRegister(MemoryUsage)
		safeRegister(CPUUsage)
		safeRegister(GCRuns)
		safeRegister(Goroutines)
		safeRegister(FileDescriptors)
		safeRegister(GCPauseDuration)
		safeRegister(TotalFilesMonitored)
		safeRegister(TotalContainersMonitored)
		safeRegister(FileMonitorOldLogsIgnored)
		safeRegister(FileMonitorOffsetRestored)
		safeRegister(FileMonitorRetryQueueSize)
		safeRegister(FileMonitorDropsTotal)
		safeRegister(FileMonitorRetryQueued)
		safeRegister(FileMonitorRetrySuccess)
		safeRegister(FileMonitorRetryFailed)
		safeRegister(FileMonitorRetryGiveUp)
		safeRegister(DiskUsageBytes)
		safeRegister(ResponseTimeSeconds)
		safeRegister(ConnectionPoolStats)
		safeRegister(CompressionRatio)
		safeRegister(BatchingStats)
		safeRegister(LeakDetection)
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
		safeRegister(LogsCollected)
		safeRegister(ContainerEvents)
		safeRegister(ActiveContainerStreams)
		safeRegister(StreamRotationsTotal)
		safeRegister(StreamAgeSeconds)
		safeRegister(StreamErrorsTotal)
		safeRegister(StreamPoolUtilization)
		safeRegister(DLQStoredEntries)
		safeRegister(DLQEntriesTotal)
		safeRegister(DLQSizeBytes)
		safeRegister(DLQReprocessAttempts)
		safeRegister(TimestampRejectionTotal)
		safeRegister(TimestampClampedTotal)
		safeRegister(TimestampMaxAcceptableAge)
		safeRegister(LokiErrorTypeTotal)
		safeRegister(TimestampLearningEventsTotal)
		safeRegister(PositionRotationDetected)
		safeRegister(PositionTruncationDetected)
		safeRegister(PositionSaveSuccess)
		safeRegister(PositionSaveFailed)
		safeRegister(PositionLagSeconds)
		safeRegister(PositionFlushTrigger)
		safeRegister(PositionOffsetReset)
		safeRegister(PositionActiveByStatus)
		safeRegister(PositionUpdateRate)
		safeRegister(PositionFileSize)
		safeRegister(PositionLagDistribution)
		safeRegister(PositionMemoryUsage)
		safeRegister(CheckpointHealth)
		safeRegister(PositionBackpressure)
		safeRegister(PositionCorruptionDetected)
		safeRegister(PositionCheckpointCreatedTotal)
		safeRegister(PositionCheckpointSizeBytes)
		safeRegister(PositionCheckpointAgeSeconds)
		safeRegister(PositionCheckpointRestoreAttemptsTotal)
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

// EnhancedMetrics provides comprehensive monitoring and metrics collection
type EnhancedMetrics struct {
	logger *logrus.Logger

	// Custom metrics registry
	customMetrics map[string]prometheus.Metric
	customMutex   sync.RWMutex

	// Internal state
	isRunning bool
	startTime time.Time

	// CPU tracking for percentage calculation
	lastCPUTimes cpu.TimesStat
	lastCPUCheck time.Time

	// Logs per second tracking
	lastLogsProcessed int64
	lastLogsCheck     time.Time

	// Dispatcher stats getter (set via SetDispatcherStatsGetter)
	getDispatcherStats func() int64
}

// NewEnhancedMetrics creates a new enhanced metrics instance
func NewEnhancedMetrics(logger *logrus.Logger) *EnhancedMetrics {
	em := &EnhancedMetrics{
		logger:        logger,
		customMetrics: make(map[string]prometheus.Metric),
		startTime:     time.Now(),
		lastCPUCheck:  time.Now(),
		lastLogsCheck: time.Now(),
	}

	// Note: Advanced metrics (diskUsage, responseTime, etc.) are now global variables
	// registered in NewMetricsServer, so we don't need to initialize them here

	return em
}

// SetDispatcherStatsGetter sets a function to retrieve the current logs processed count
// This allows EnhancedMetrics to calculate logs per second rate
func (em *EnhancedMetrics) SetDispatcherStatsGetter(getter func() int64) {
	em.getDispatcherStats = getter
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

	// Update CPU usage percentage
	times, err := cpu.Times(false)
	if err == nil && len(times) > 0 {
		// Calculate CPU percentage between this call and the last
		if !em.lastCPUCheck.IsZero() {
			total := times[0].Total() - em.lastCPUTimes.Total()
			idle := times[0].Idle - em.lastCPUTimes.Idle
			if total > 0 {
				cpuPercent := 100.0 * (total - idle) / total
				CPUUsage.Set(cpuPercent)
			}
		}
		em.lastCPUTimes = times[0]
		em.lastCPUCheck = time.Now()
	}

	// Update logs per second rate
	if em.getDispatcherStats != nil {
		currentLogs := em.getDispatcherStats()
		elapsed := time.Since(em.lastLogsCheck).Seconds()
		if elapsed > 0 {
			rate := float64(currentLogs-em.lastLogsProcessed) / elapsed
			if rate < 0 {
				rate = 0 // Handle counter reset
			}
			LogsPerSecond.WithLabelValues("dispatcher").Set(rate)
		}
		em.lastLogsProcessed = currentLogs
		em.lastLogsCheck = time.Now()
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

// CONTAINER MONITOR STREAM HELPER FUNCTIONS
func RecordStreamRotation(containerID, containerName string, ageSeconds float64) {
	StreamRotationsTotal.WithLabelValues(containerID, containerName).Inc()
	StreamAgeSeconds.WithLabelValues(containerID).Observe(ageSeconds)
}

func RecordStreamError(errorType, containerID string) {
	StreamErrorsTotal.WithLabelValues(errorType, containerID).Inc()
}

func UpdateActiveStreams(count int) {
	ActiveContainerStreams.Set(float64(count))
}

func UpdateStreamPoolUtilization(current, max int) {
	if max > 0 {
		StreamPoolUtilization.Set(float64(current) / float64(max))
	} else {
		StreamPoolUtilization.Set(0)
	}
}

// TASK 2: FILE MONITOR NEW FEATURES METRICS
func RecordOldLogIgnored(component, filePath string) {
	FileMonitorOldLogsIgnored.WithLabelValues(component, filePath).Inc()
}

func RecordOffsetRestored(component, filePath string) {
	FileMonitorOffsetRestored.WithLabelValues(component, filePath).Inc()
}

func RecordRetryQueueSize(component string, size int) {
	FileMonitorRetryQueueSize.WithLabelValues(component).Set(float64(size))
}

func RecordDrop(component, reason string) {
	FileMonitorDropsTotal.WithLabelValues(component, reason).Inc()
}

func RecordRetryQueued(component string) {
	FileMonitorRetryQueued.WithLabelValues(component).Inc()
}

func RecordRetrySuccess(component string) {
	FileMonitorRetrySuccess.WithLabelValues(component).Inc()
}

func RecordRetryFailed(component string) {
	FileMonitorRetryFailed.WithLabelValues(component).Inc()
}

func RecordRetryGiveUp(component string) {
	FileMonitorRetryGiveUp.WithLabelValues(component).Inc()
}

// DLQ METRICS HELPER FUNCTIONS
func RecordDLQStore(sink, reason string) {
	DLQStoredEntries.WithLabelValues(sink, reason).Inc()
}

func RecordDLQReprocess(sink, result string) {
	DLQReprocessAttempts.WithLabelValues(sink, result).Inc()
}

func UpdateDLQStats(sink string, entryCount int, sizeBytes int64) {
	DLQEntriesTotal.WithLabelValues(sink).Set(float64(entryCount))
	DLQSizeBytes.WithLabelValues(sink).Set(float64(sizeBytes))
}

// TIMESTAMP LEARNING METRICS HELPERS
func RecordTimestampRejection(sink, reason string) {
	TimestampRejectionTotal.WithLabelValues(sink, reason).Inc()
}

func RecordTimestampClamped(sink string) {
	TimestampClampedTotal.WithLabelValues(sink).Inc()
}

func UpdateTimestampMaxAge(sink string, ageSeconds float64) {
	TimestampMaxAcceptableAge.WithLabelValues(sink).Set(ageSeconds)
}

func RecordLokiErrorType(sink, errorType string) {
	LokiErrorTypeTotal.WithLabelValues(sink, errorType).Inc()
}

func RecordTimestampLearningEvent(sink string) {
	TimestampLearningEventsTotal.WithLabelValues(sink).Inc()
}

func RecordLokiRateLimit(sink string) {
	RecordLokiErrorType(sink, "rate_limit")
}

// POSITION SYSTEM METRICS HELPERS (Phase 1)
func RecordPositionRotation(filePath string) {
	PositionRotationDetected.WithLabelValues(filePath).Inc()
}

func RecordPositionTruncation(filePath string) {
	PositionTruncationDetected.WithLabelValues(filePath).Inc()
}

func RecordPositionSaveSuccess() {
	PositionSaveSuccess.Inc()
}

func RecordPositionSaveFailed(errorType string) {
	PositionSaveFailed.WithLabelValues(errorType).Inc()
}

func UpdatePositionLag(managerType string, lagSeconds float64) {
	PositionLagSeconds.WithLabelValues(managerType).Set(lagSeconds)
}

func RecordPositionFlushTrigger(triggerType string) {
	PositionFlushTrigger.WithLabelValues(triggerType).Inc()
}

func RecordPositionOffsetReset(filePath, reason string) {
	PositionOffsetReset.WithLabelValues(filePath, reason).Inc()
}

// POSITION SYSTEM METRICS HELPERS (Phase 2)
func UpdatePositionActiveByStatus(status string, count int) {
	PositionActiveByStatus.WithLabelValues(status).Set(float64(count))
}

func UpdatePositionUpdateRate(managerType string, ratePerSecond float64) {
	PositionUpdateRate.WithLabelValues(managerType).Set(ratePerSecond)
}

func UpdatePositionFileSize(fileType string, sizeBytes int64) {
	PositionFileSize.WithLabelValues(fileType).Set(float64(sizeBytes))
}

func RecordPositionLagDistribution(managerType string, lagSeconds float64) {
	PositionLagDistribution.WithLabelValues(managerType).Observe(lagSeconds)
}

func UpdatePositionMemoryUsage(bytes int64) {
	PositionMemoryUsage.Set(float64(bytes))
}

func UpdateCheckpointHealth(component string, healthy bool) {
	var value float64
	if healthy {
		value = 1
	}
	CheckpointHealth.WithLabelValues(component).Set(value)
}

func UpdatePositionBackpressure(managerType string, backpressure float64) {
	PositionBackpressure.WithLabelValues(managerType).Set(backpressure)
}

func RecordPositionCorruption(fileType, recoveryAction string) {
	PositionCorruptionDetected.WithLabelValues(fileType, recoveryAction).Inc()
}
