package types

import (
	"context"
	"time"
)

// LogEntry representa uma entrada de log com metadados
type LogEntry struct {
	Timestamp   time.Time              `json:"timestamp"`
	Message     string                 `json:"message"`
	SourceType  string                 `json:"source_type"`  // "file" ou "docker"
	SourceID    string                 `json:"source_id"`    // hash do arquivo ou container ID
	Level       string                 `json:"level"`        // log level (info, warn, error)
	Labels      map[string]string      `json:"labels"`
	Fields      map[string]interface{} `json:"fields"`       // additional structured fields
	ProcessedAt time.Time              `json:"processed_at"`
}

// Monitor interface para monitores de logs
type Monitor interface {
	Start(ctx context.Context) error
	Stop() error
	IsHealthy() bool
	GetStatus() MonitorStatus
}

// Sink interface para destinos de logs
type Sink interface {
	Send(ctx context.Context, entries []LogEntry) error
	Start(ctx context.Context) error
	Stop() error
	IsHealthy() bool
	GetQueueUtilization() float64
}

// Dispatcher interface para roteamento de logs
type Dispatcher interface {
	Handle(ctx context.Context, sourceType, sourceID, message string, labels map[string]string) error
	AddSink(sink Sink)
	Start(ctx context.Context) error
	Stop() error
	GetStats() DispatcherStats
}

// Processor interface para processamento de logs
type Processor interface {
	Process(ctx context.Context, entry *LogEntry) (*LogEntry, error)
	GetPipelineName() string
}

// MonitorStatus representa o status de um monitor
type MonitorStatus struct {
	Name          string    `json:"name"`
	IsRunning     bool      `json:"is_running"`
	IsHealthy     bool      `json:"is_healthy"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
	ErrorCount    int64     `json:"error_count"`
	ProcessedLogs int64     `json:"processed_logs"`
}

// DispatcherStats representa estatísticas do dispatcher
type DispatcherStats struct {
	TotalProcessed     int64             `json:"total_processed"`
	ErrorCount         int64             `json:"error_count"`
	QueueSize          int               `json:"queue_size"`
	SinkDistribution   map[string]int64  `json:"sink_distribution"`
	LastProcessedTime  time.Time         `json:"last_processed_time"`
	DuplicatesDetected int64             `json:"duplicates_detected"`
	Throttled          int64             `json:"throttled"`           // Logs descartados por throttling
}

// HealthStatus representa o status de saúde do sistema
type HealthStatus struct {
	Status     string                 `json:"status"`     // "healthy", "degraded", "unhealthy"
	Components map[string]interface{} `json:"components"`
	Issues     []string               `json:"issues"`
	CheckTime  time.Time              `json:"check_time"`
}

// TaskManager interface para gerenciamento de tarefas
type TaskManager interface {
	StartTask(ctx context.Context, taskID string, fn func(context.Context) error) error
	StopTask(taskID string) error
	Heartbeat(taskID string) error
	GetTaskStatus(taskID string) TaskStatus
	GetAllTasks() map[string]TaskStatus
	Cleanup() error
}

// TaskStatus representa o status de uma tarefa
type TaskStatus struct {
	ID            string    `json:"id"`
	State         string    `json:"state"`         // "running", "stopped", "failed"
	StartedAt     time.Time `json:"started_at"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
	ErrorCount    int64     `json:"error_count"`
	LastError     string    `json:"last_error,omitempty"`
}

// FilePipelineConfig estrutura do arquivo file_pipeline.yml
type FilePipelineConfig struct {
	Version     string                   `yaml:"version"`
	Files       []FilePipelineFileEntry  `yaml:"files"`
	Directories []FilePipelineDirEntry   `yaml:"directories"`
	Monitoring  FilePipelineMonitoring   `yaml:"monitoring"`
}

// FilePipelineFileEntry entrada de arquivo específico
type FilePipelineFileEntry struct {
	Path    string            `yaml:"path"`
	Labels  map[string]string `yaml:"labels"`
	Enabled bool              `yaml:"enabled"`
}

// FilePipelineDirEntry entrada de diretório
type FilePipelineDirEntry struct {
	Path              string            `yaml:"path"`
	Patterns          []string          `yaml:"patterns"`
	ExcludePatterns   []string          `yaml:"exclude_patterns"`
	ExcludeDirectories []string         `yaml:"exclude_directories"`
	Recursive         bool              `yaml:"recursive"`
	DefaultLabels     map[string]string `yaml:"default_labels"`
	Enabled           bool              `yaml:"enabled"`
}

// FilePipelineMonitoring configurações de monitoramento no pipeline
type FilePipelineMonitoring struct {
	DirectoryScanInterval int    `yaml:"directory_scan_interval"`
	MaxFiles              int    `yaml:"max_files"`
	ReadBufferSize        int    `yaml:"read_buffer_size"`
	PollInterval          int    `yaml:"poll_interval"`
	FollowSymlinks        bool   `yaml:"follow_symlinks"`
	IncludeHidden         bool   `yaml:"include_hidden"`
	MaxFileSize           int64  `yaml:"max_file_size"`
	RotationAction        string `yaml:"rotation_action"`
}

// Config representa a configuração da aplicação
type Config struct {
	App                  AppConfig                `yaml:"app"`
	Server               ServerConfig             `yaml:"server"`
	Metrics              MetricsConfig            `yaml:"metrics"`
	FilesConfig          FilesConfig              `yaml:"files_config"`
	FileMonitorService   FileMonitorServiceConfig `yaml:"file_monitor_service"`
	FileMonitor          FileMonitorConfig        `yaml:"file_monitor"` // Legacy
	ContainerMonitor     ContainerMonitorConfig   `yaml:"container_monitor"`
	Dispatcher           DispatcherConfig         `yaml:"dispatcher"`
	Sinks                SinksConfig              `yaml:"sinks"`
	Processing           ProcessingConfig         `yaml:"processing"`
	TimestampValidation  TimestampValidationConfig `yaml:"timestamp_validation"`
	ServiceDiscovery     ServiceDiscoveryConfig   `yaml:"service_discovery"`
	HotReload            HotReloadConfig          `yaml:"hot_reload"`
	AnomalyDetection     AnomalyDetectionConfig   `yaml:"anomaly_detection"`
	MultiTenant          MultiTenantConfig        `yaml:"multi_tenant"`
	Positions            PositionConfig           `yaml:"positions"`
	Cleanup              CleanupConfig            `yaml:"cleanup"`
	LeakDetection        LeakDetectionConfig      `yaml:"leak_detection"`
	DiskBuffer           DiskBufferConfig         `yaml:"disk_buffer"`

	// Legacy fields for backward compatibility
	API      APIConfig      `yaml:"api,omitempty"`
	Docker   DockerConfig   `yaml:"docker,omitempty"`
	File     FileConfig     `yaml:"file,omitempty"`
	Logging  LoggingConfig  `yaml:"logging,omitempty"`
	Pipeline PipelineConfig `yaml:"pipeline,omitempty"`
}

// AppConfig configuração geral da aplicação
type AppConfig struct {
	Name               string `yaml:"name"`
	Version            string `yaml:"version"`
	Environment        string `yaml:"environment"`
	LogLevel           string `yaml:"log_level"`
	LogFormat          string `yaml:"log_format"`
	LogFile            string `yaml:"log_file"`
	OperationTimeout   string `yaml:"operation_timeout"`
}

// ServerConfig configuração do servidor HTTP
type ServerConfig struct {
	Port           int    `yaml:"port"`
	Host           string `yaml:"host"`
	ReadTimeout    string `yaml:"read_timeout"`
	WriteTimeout   string `yaml:"write_timeout"`
	IdleTimeout    string `yaml:"idle_timeout"`
	MaxHeaderBytes int    `yaml:"max_header_bytes"`
}

// FilesConfig configuração de padrões de arquivos (defaults)
type FilesConfig struct {
	WatchDirectories   []string `yaml:"watch_directories"`
	IncludePatterns    []string `yaml:"include_patterns"`
	ExcludePatterns    []string `yaml:"exclude_patterns"`
	ExcludeDirectories []string `yaml:"exclude_directories"`
}

// FileMonitorServiceConfig configuração do serviço de monitoramento de arquivos
type FileMonitorServiceConfig struct {
	Enabled         bool   `yaml:"enabled"`
	PipelineFile    string `yaml:"pipeline_file"`
	PollInterval    string `yaml:"poll_interval"`
	ReadBufferSize  int    `yaml:"read_buffer_size"`
	ReadInterval    string `yaml:"read_interval"`
	Recursive       bool   `yaml:"recursive"`
	FollowSymlinks  bool   `yaml:"follow_symlinks"`
}

// FileMonitorConfig configuração legada (compatibilidade)
type FileMonitorConfig struct {
	Enabled            bool     `yaml:"enabled"`
	WatchDirectories   []string `yaml:"watch_directories"`
	IncludePatterns    []string `yaml:"include_patterns"`
	ExcludePatterns    []string `yaml:"exclude_patterns"`
	ExcludeDirectories []string `yaml:"exclude_directories"`
	PollInterval       string   `yaml:"poll_interval"`
	ReadBufferSize     int      `yaml:"read_buffer_size"`
	ReadInterval       string   `yaml:"read_interval"`
	Recursive          bool     `yaml:"recursive"`
	FollowSymlinks     bool     `yaml:"follow_symlinks"`
}

// ContainerMonitorConfig configuração do monitoramento de containers
type ContainerMonitorConfig struct {
	Enabled           bool              `yaml:"enabled"`
	SocketPath        string            `yaml:"socket_path"`
	HealthCheckDelay  string            `yaml:"health_check_delay"`
	ReconnectInterval string            `yaml:"reconnect_interval"`
	MaxConcurrent     int               `yaml:"max_concurrent"`
	IncludeLabels     map[string]string `yaml:"include_labels,omitempty"`
	ExcludeLabels     map[string]string `yaml:"exclude_labels,omitempty"`
	IncludeNames      []string          `yaml:"include_names,omitempty"`
	ExcludeNames      []string          `yaml:"exclude_names,omitempty"`
	IncludeStdout     bool              `yaml:"include_stdout"`
	IncludeStderr     bool              `yaml:"include_stderr"`
	TailLines         int               `yaml:"tail_lines"`
	Follow            bool              `yaml:"follow"`
}

// DispatcherConfig configuração do dispatcher
type DispatcherConfig struct {
	QueueSize       int    `yaml:"queue_size"`
	WorkerCount     int    `yaml:"worker_count"`
	SendTimeout     string `yaml:"send_timeout"`
	BatchSize       int    `yaml:"batch_size"`
	BatchTimeout    string `yaml:"batch_timeout"`
	MaxRetries      int    `yaml:"max_retries"`
	RetryBaseDelay  string `yaml:"retry_base_delay"`
	RetryMultiplier int    `yaml:"retry_multiplier"`
	RetryMaxDelay   string `yaml:"retry_max_delay"`

	// Configuração de deduplicação
	DeduplicationEnabled bool                 `yaml:"deduplication_enabled"`
	DeduplicationConfig  map[string]interface{} `yaml:"deduplication_config"`

	// Configuração de Dead Letter Queue
	DLQEnabled bool                 `yaml:"dlq_enabled"`
	DLQConfig  map[string]interface{} `yaml:"dlq_config"`

	// Configuração de Backpressure
	BackpressureEnabled bool                 `yaml:"backpressure_enabled"`
	BackpressureConfig  map[string]interface{} `yaml:"backpressure_config"`

	// Configuração de Degradation
	DegradationEnabled bool                 `yaml:"degradation_enabled"`
	DegradationConfig  map[string]interface{} `yaml:"degradation_config"`

	// Configuração de Rate Limiting
	RateLimitEnabled bool                 `yaml:"rate_limit_enabled"`
	RateLimitConfig  map[string]interface{} `yaml:"rate_limit_config"`
}

// ProcessingConfig configuração do processamento
type ProcessingConfig struct {
	Enabled           bool   `yaml:"enabled"`
	PipelinesFile     string `yaml:"pipelines_file"`
	WorkerCount       int    `yaml:"worker_count"`
	QueueSize         int    `yaml:"queue_size"`
	ProcessingTimeout string `yaml:"processing_timeout"`
	SkipFailedLogs    bool   `yaml:"skip_failed_logs"`
	EnrichLogs        bool   `yaml:"enrich_logs"`
}

// TimestampValidationConfig configuração da validação de timestamp
type TimestampValidationConfig struct {
	Enabled             bool     `yaml:"enabled"`
	MaxPastAgeSeconds   int      `yaml:"max_past_age_seconds"`
	MaxFutureAgeSeconds int      `yaml:"max_future_age_seconds"`
	ClampEnabled        bool     `yaml:"clamp_enabled"`
	ClampDLQ            bool     `yaml:"clamp_dlq"`
	InvalidAction       string   `yaml:"invalid_action"`
	DefaultTimezone     string   `yaml:"default_timezone"`
	AcceptedFormats     []string `yaml:"accepted_formats"`
}

// ServiceDiscoveryConfig configuração do service discovery
type ServiceDiscoveryConfig struct {
	Enabled           bool                          `yaml:"enabled"`
	UpdateInterval    string                        `yaml:"update_interval"`
	DockerEnabled     bool                          `yaml:"docker_enabled"`
	FileEnabled       bool                          `yaml:"file_enabled"`
	KubernetesEnabled bool                          `yaml:"kubernetes_enabled"`
	Docker            DockerDiscoveryConfig         `yaml:"docker"`
	File              FileDiscoveryConfig           `yaml:"file"`
	Kubernetes        KubernetesDiscoveryConfig     `yaml:"kubernetes"`
}

// DockerDiscoveryConfig configuração para descoberta Docker
type DockerDiscoveryConfig struct {
	SocketPath      string            `yaml:"socket_path"`
	RequiredLabels  map[string]string `yaml:"required_labels"`
	ExcludeLabels   map[string]string `yaml:"exclude_labels"`
	RequireLabel    string            `yaml:"require_label"`
	PipelineLabel   string            `yaml:"pipeline_label"`
	ComponentLabel  string            `yaml:"component_label"`
	TenantLabel     string            `yaml:"tenant_label"`
}

// FileDiscoveryConfig configuração para descoberta de arquivos
type FileDiscoveryConfig struct {
	WatchPaths      []string          `yaml:"watch_paths"`
	ConfigFiles     []string          `yaml:"config_files"`
	RequiredLabels  map[string]string `yaml:"required_labels"`
	AutoDetectLogs  bool              `yaml:"auto_detect_logs"`
}

// KubernetesDiscoveryConfig configuração para descoberta Kubernetes
type KubernetesDiscoveryConfig struct {
	Namespace           string            `yaml:"namespace"`
	RequiredAnnotations map[string]string `yaml:"required_annotations"`
	RequiredLabels      map[string]string `yaml:"required_labels"`
	ServiceAccount      string            `yaml:"service_account"`
}

// HotReloadConfig configuração do hot reload
type HotReloadConfig struct {
	Enabled          bool     `yaml:"enabled"`
	WatchInterval    string   `yaml:"watch_interval"`
	DebounceInterval string   `yaml:"debounce_interval"`
	ValidateOnReload bool     `yaml:"validate_on_reload"`
	BackupOnReload   bool     `yaml:"backup_on_reload"`
	BackupDirectory  string   `yaml:"backup_directory"`
	MaxBackups       int      `yaml:"max_backups"`
	FailsafeMode     bool     `yaml:"failsafe_mode"`
	WatchFiles       []string `yaml:"watch_files"`
}

// APIConfig configuração da API HTTP
type APIConfig struct {
	Port    int    `yaml:"port"`
	Host    string `yaml:"host"`
	Enabled bool   `yaml:"enabled"`
}

// DockerConfig configuração do monitor Docker
type DockerConfig struct {
	Enabled           bool                 `yaml:"enabled"`
	SocketPath        string               `yaml:"socket_path"`
	MaxConcurrent     int                  `yaml:"max_concurrent"`
	ReconnectInterval time.Duration        `yaml:"reconnect_interval"`
	HealthCheckDelay  time.Duration        `yaml:"health_check_delay"`
	IncludeLabels     map[string]string    `yaml:"include_labels"`
	ExcludeLabels     map[string]string    `yaml:"exclude_labels"`
	IncludeNames      []string             `yaml:"include_names"`
	ExcludeNames      []string             `yaml:"exclude_names"`
}

// FileConfig configuração do monitor de arquivos
type FileConfig struct {
	Enabled            bool                `yaml:"enabled"`
	PollInterval       time.Duration       `yaml:"poll_interval"`
	MaxOpenFiles       int                 `yaml:"max_open_files"`
	BufferSize         int                 `yaml:"buffer_size"`
	PositionsPath      string              `yaml:"positions_path"`
	WatchDirectories   []string            `yaml:"watch_directories"`
	IncludePatterns    []string            `yaml:"include_patterns"`
	ExcludePatterns    []string            `yaml:"exclude_patterns"`
	ExcludeDirectories []string            `yaml:"exclude_directories"`
	ReadInterval       time.Duration       `yaml:"read_interval"`
	Recursive          bool                `yaml:"recursive"`
	FollowSymlinks     bool                `yaml:"follow_symlinks"`
	PipelineConfig     *FilePipelineConfig // Configuração do pipeline (se existir)
}

// SinksConfig configuração dos sinks
type SinksConfig struct {
	Loki        LokiConfig        `yaml:"loki"`
	LocalFile   LocalFileConfig   `yaml:"local_file"`
	Elasticsearch ElasticsearchConfig `yaml:"elasticsearch"`
	Splunk      SplunkConfig      `yaml:"splunk"`
}

// LokiConfig configuração do sink Loki
type LokiConfig struct {
	Enabled       bool              `yaml:"enabled"`
	URL           string            `yaml:"url"`
	PushEndpoint  string            `yaml:"push_endpoint"`
	TenantID      string            `yaml:"tenant_id,omitempty"`
	Timeout       string            `yaml:"timeout"`
	BatchSize     int               `yaml:"batch_size"`
	BatchTimeout  string            `yaml:"batch_timeout"`
	MaxRequestSize int              `yaml:"max_request_size"`
	QueueSize     int               `yaml:"queue_size"`
	DefaultLabels map[string]string `yaml:"default_labels,omitempty"`
	Headers       map[string]string `yaml:"headers,omitempty"`
	Auth          LokiAuthConfig    `yaml:"auth,omitempty"`
	TLS           LokiTLSConfig     `yaml:"tls,omitempty"`
	AdaptiveBatching AdaptiveBatchingConfig `yaml:"adaptive_batching,omitempty"`
}

// AdaptiveBatchingConfig configuração para batching adaptativo
type AdaptiveBatchingConfig struct {
	Enabled            bool   `yaml:"enabled"`
	MinBatchSize       int    `yaml:"min_batch_size"`
	MaxBatchSize       int    `yaml:"max_batch_size"`
	InitialBatchSize   int    `yaml:"initial_batch_size"`
	MinFlushDelay      string `yaml:"min_flush_delay"`
	MaxFlushDelay      string `yaml:"max_flush_delay"`
	InitialFlushDelay  string `yaml:"initial_flush_delay"`
	AdaptationInterval string `yaml:"adaptation_interval"`
	LatencyThreshold   string `yaml:"latency_threshold"`
	ThroughputTarget   int    `yaml:"throughput_target"`
	BufferSize         int    `yaml:"buffer_size"`
}

// LokiAuthConfig configuração de autenticação do Loki
type LokiAuthConfig struct {
	Type     string `yaml:"type"`     // none, basic, bearer
	Username string `yaml:"username,omitempty"`
	Password string `yaml:"password,omitempty"`
	Token    string `yaml:"token,omitempty"`
}

// LokiTLSConfig configuração TLS do Loki
type LokiTLSConfig struct {
	Enabled           bool   `yaml:"enabled"`
	VerifyCertificate bool   `yaml:"verify_certificate"`
	CAFile            string `yaml:"ca_file,omitempty"`
	CertFile          string `yaml:"cert_file,omitempty"`
	KeyFile           string `yaml:"key_file,omitempty"`
}

// LocalFileConfig configuração do sink de arquivo local
type LocalFileConfig struct {
	Enabled         bool                    `yaml:"enabled"`
	Directory       string                  `yaml:"directory"`
	FilenamePattern string                  `yaml:"filename_pattern"`
	OutputFormat    string                  `yaml:"output_format"`
	TextFormat      LocalFileTextFormat     `yaml:"text_format,omitempty"`
	Rotation        LocalFileRotation       `yaml:"rotation,omitempty"`
	AutoSync        bool                    `yaml:"auto_sync"`
	SyncInterval    string                  `yaml:"sync_interval"`
	FilePermissions string                  `yaml:"file_permissions"`
	DirPermissions  string                  `yaml:"dir_permissions"`
	QueueSize       int                     `yaml:"queue_size"`

	// Proteções contra disco cheio
	MaxTotalDiskGB         float64 `yaml:"max_total_disk_gb"`
	DiskCheckInterval      string  `yaml:"disk_check_interval"`
	EmergencyCleanupEnabled bool    `yaml:"emergency_cleanup_enabled"`
	CleanupThresholdPercent float64 `yaml:"cleanup_threshold_percent"`
}

// LocalFileTextFormat configuração do formato de texto
type LocalFileTextFormat struct {
	IncludeTimestamp bool   `yaml:"include_timestamp"`
	TimestampFormat  string `yaml:"timestamp_format"`
	IncludeLabels    bool   `yaml:"include_labels"`
	FieldSeparator   string `yaml:"field_separator"`
}

// LocalFileRotation configuração de rotação de arquivos
type LocalFileRotation struct {
	Enabled        bool `yaml:"enabled"`
	MaxSizeMB      int  `yaml:"max_size_mb"`
	MaxFiles       int  `yaml:"max_files"`
	RetentionDays  int  `yaml:"retention_days"`
	Compress       bool `yaml:"compress"`
}

// ElasticsearchConfig configuração do sink Elasticsearch
type ElasticsearchConfig struct {
	Enabled   bool     `yaml:"enabled"`
	URLs      []string `yaml:"urls"`
	Index     string   `yaml:"index"`
	BatchSize int      `yaml:"batch_size"`
	Username  string   `yaml:"username,omitempty"`
	Password  string   `yaml:"password,omitempty"`
}

// SplunkConfig configuração do sink Splunk
type SplunkConfig struct {
	Enabled bool   `yaml:"enabled"`
	URL     string `yaml:"url"`
	Token   string `yaml:"token"`
	Index   string `yaml:"index"`
}

// MetricsConfig configuração das métricas
type MetricsConfig struct {
	Enabled bool   `yaml:"enabled"`
	Port    int    `yaml:"port"`
	Path    string `yaml:"path"`
}

// LoggingConfig configuração do logging
type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"` // "json" ou "text"
}

// PipelineConfig configuração dos pipelines de processamento
type PipelineConfig struct {
	Enabled bool   `yaml:"enabled"`
	File    string `yaml:"file"`
}

// Circuit Breaker states
const (
	CircuitBreakerClosed   = "closed"
	CircuitBreakerOpen     = "open"
	CircuitBreakerHalfOpen = "half_open"
)

// CircuitBreaker interface para implementação do padrão circuit breaker
type CircuitBreaker interface {
	Execute(fn func() error) error
	State() string
	IsOpen() bool
	Reset()
	GetStats() CircuitBreakerStats
}

// CircuitBreakerStats estatísticas do circuit breaker
type CircuitBreakerStats struct {
	State         string    `json:"state"`
	Failures      int64     `json:"failures"`
	Successes     int64     `json:"successes"`
	Requests      int64     `json:"requests"`
	LastFailure   time.Time `json:"last_failure,omitempty"`
	LastSuccess   time.Time `json:"last_success,omitempty"`
	NextRetryTime time.Time `json:"next_retry_time,omitempty"`
}

// PositionConfig configuração do sistema de posições
type PositionConfig struct {
	Enabled            bool   `yaml:"enabled"`
	Directory          string `yaml:"directory"`
	FlushInterval      string `yaml:"flush_interval"`
	MaxMemoryBuffer    int    `yaml:"max_memory_buffer"`
	MaxMemoryPositions int    `yaml:"max_memory_positions"`
	ForceFlushOnExit   bool   `yaml:"force_flush_on_exit"`
	CleanupInterval    string `yaml:"cleanup_interval"`
	MaxPositionAge     string `yaml:"max_position_age"`
}

// CleanupConfig configuração do gerenciamento de espaço em disco
type CleanupConfig struct {
	Enabled                 bool                     `yaml:"enabled"`
	CheckInterval           string                   `yaml:"check_interval"`
	CriticalSpaceThreshold  float64                  `yaml:"critical_space_threshold"`
	WarningSpaceThreshold   float64                  `yaml:"warning_space_threshold"`
	Directories             []CleanupDirectoryConfig `yaml:"directories"`
}

// CleanupDirectoryConfig configuração de diretório para cleanup
type CleanupDirectoryConfig struct {
	Path               string   `yaml:"path"`
	MaxSizeMB          int64    `yaml:"max_size_mb"`
	RetentionDays      int      `yaml:"retention_days"`
	FilePatterns       []string `yaml:"file_patterns"`
	MaxFiles           int      `yaml:"max_files"`
	CleanupAgeSeconds  int      `yaml:"cleanup_age_seconds"`
}

// LeakDetectionConfig configuração do monitoramento de vazamentos de recursos
type LeakDetectionConfig struct {
	Enabled                 bool   `yaml:"enabled"`
	MonitoringInterval      string `yaml:"monitoring_interval"`
	FDLeakThreshold         int64  `yaml:"fd_leak_threshold"`
	GoroutineLeakThreshold  int64  `yaml:"goroutine_leak_threshold"`
	MemoryLeakThreshold     int64  `yaml:"memory_leak_threshold"`
	AlertCooldown           string `yaml:"alert_cooldown"`
	EnableMemoryProfiling   bool   `yaml:"enable_memory_profiling"`
	EnableGCOptimization    bool   `yaml:"enable_gc_optimization"`
	MaxAlertHistory         int    `yaml:"max_alert_history"`
}

// DiskBufferConfig configuração do buffer de disco
type DiskBufferConfig struct {
	Enabled            bool   `yaml:"enabled"`
	BaseDir            string `yaml:"base_dir"`
	MaxFileSize        int64  `yaml:"max_file_size"`        // bytes
	MaxTotalSize       int64  `yaml:"max_total_size"`       // bytes
	MaxFiles           int    `yaml:"max_files"`
	CompressionEnabled bool   `yaml:"compression_enabled"`
	SyncInterval       string `yaml:"sync_interval"`
	CleanupInterval    string `yaml:"cleanup_interval"`
	RetentionPeriod    string `yaml:"retention_period"`
	FilePermissions    string `yaml:"file_permissions"`
	DirPermissions     string `yaml:"dir_permissions"`
}

// AnomalyDetectionConfig configuração da detecção de anomalias baseada em ML
type AnomalyDetectionConfig struct {
	Enabled            bool                        `yaml:"enabled"`
	ModelType          string                      `yaml:"model_type"`          // "isolation_forest", "statistical", "neural_network", "ensemble"
	TrainingInterval   string                      `yaml:"training_interval"`   // Intervalo para re-treinar o modelo
	PredictionInterval string                      `yaml:"prediction_interval"` // Intervalo para executar predições
	BufferSize         int                         `yaml:"buffer_size"`         // Tamanho do buffer de dados de treinamento
	ThresholdConfig    AnomalyThresholdConfig      `yaml:"threshold_config"`
	ModelConfig        AnomalyModelConfig          `yaml:"model_config"`
	FeatureExtraction  AnomalyFeatureConfig        `yaml:"feature_extraction"`
	AlertConfig        AnomalyAlertConfig          `yaml:"alert_config"`
	PatternConfig      AnomalyPatternConfig        `yaml:"pattern_config"`
	OutputConfig       AnomalyOutputConfig         `yaml:"output_config"`
}

// AnomalyThresholdConfig configuração dos limiares de anomalia
type AnomalyThresholdConfig struct {
	LowThreshold    float64 `yaml:"low_threshold"`    // 0.3 - anomalias leves
	MediumThreshold float64 `yaml:"medium_threshold"` // 0.6 - anomalias médias
	HighThreshold   float64 `yaml:"high_threshold"`   // 0.8 - anomalias altas
	CriticalThreshold float64 `yaml:"critical_threshold"` // 0.9 - anomalias críticas
}

// AnomalyModelConfig configuração específica do modelo
type AnomalyModelConfig struct {
	IsolationForest AnomalyIsolationForestConfig `yaml:"isolation_forest"`
	Statistical     AnomalyStatisticalConfig     `yaml:"statistical"`
	NeuralNetwork   AnomalyNeuralNetworkConfig   `yaml:"neural_network"`
	Ensemble        AnomalyEnsembleConfig        `yaml:"ensemble"`
}

// AnomalyIsolationForestConfig configuração do Isolation Forest
type AnomalyIsolationForestConfig struct {
	NumTrees   int `yaml:"num_trees"`    // Número de árvores na floresta
	MaxSamples int `yaml:"max_samples"`  // Amostras máximas por árvore
	MaxDepth   int `yaml:"max_depth"`    // Profundidade máxima das árvores
}

// AnomalyStatisticalConfig configuração do modelo estatístico
type AnomalyStatisticalConfig struct {
	ZScoreThreshold    float64  `yaml:"zscore_threshold"`    // Limite z-score
	PercentileMode     bool     `yaml:"percentile_mode"`     // Usar percentis em vez de z-score
	PercentileLimits   []float64 `yaml:"percentile_limits"`   // [1, 5, 95, 99] percentis para detecção
	WindowSize         int      `yaml:"window_size"`         // Tamanho da janela móvel
}

// AnomalyNeuralNetworkConfig configuração da rede neural
type AnomalyNeuralNetworkConfig struct {
	HiddenSize     int     `yaml:"hidden_size"`     // Neurônios na camada oculta
	LearningRate   float64 `yaml:"learning_rate"`   // Taxa de aprendizado
	Epochs         int     `yaml:"epochs"`          // Épocas de treinamento
	BatchSize      int     `yaml:"batch_size"`      // Tamanho do lote
}

// AnomalyEnsembleConfig configuração do modelo ensemble
type AnomalyEnsembleConfig struct {
	Models        []string             `yaml:"models"`         // Lista dos modelos a combinar
	VotingMethod  string               `yaml:"voting_method"`  // "average", "weighted", "majority"
	ModelWeights  map[string]float64   `yaml:"model_weights"`  // Pesos dos modelos
}

// AnomalyFeatureConfig configuração da extração de features
type AnomalyFeatureConfig struct {
	TextFeatures       bool `yaml:"text_features"`       // Extrair features de texto
	StatisticalFeatures bool `yaml:"statistical_features"` // Extrair features estatísticas
	TemporalFeatures   bool `yaml:"temporal_features"`   // Extrair features temporais
	PatternFeatures    bool `yaml:"pattern_features"`    // Extrair features de padrões
	CustomFeatures     bool `yaml:"custom_features"`     // Features customizadas
}

// AnomalyAlertConfig configuração de alertas de anomalia
type AnomalyAlertConfig struct {
	Enabled         bool   `yaml:"enabled"`
	WebhookURL      string `yaml:"webhook_url"`      // URL para enviar alertas
	EmailTo         string `yaml:"email_to"`         // Email para alertas
	SlackChannel    string `yaml:"slack_channel"`    // Canal Slack para alertas
	CooldownPeriod  string `yaml:"cooldown_period"`  // Período de cooldown entre alertas
	IncludeContext  bool   `yaml:"include_context"`  // Incluir contexto nos alertas
	MaxAlertsPerHour int   `yaml:"max_alerts_per_hour"` // Limite de alertas por hora
}

// AnomalyPatternConfig configuração de padrões conhecidos
type AnomalyPatternConfig struct {
	Enabled        bool     `yaml:"enabled"`
	WhitelistEnabled bool   `yaml:"whitelist_enabled"` // Habilitar whitelist de padrões normais
	BlacklistEnabled bool   `yaml:"blacklist_enabled"` // Habilitar blacklist de padrões anômalos
	WhitelistPatterns []string `yaml:"whitelist_patterns"` // Padrões regex normais
	BlacklistPatterns []string `yaml:"blacklist_patterns"` // Padrões regex anômalos
	UpdateInterval   string   `yaml:"update_interval"`    // Intervalo para atualizar padrões
}

// AnomalyOutputConfig configuração de saída dos resultados
type AnomalyOutputConfig struct {
	Enabled       bool   `yaml:"enabled"`
	LogResults    bool   `yaml:"log_results"`    // Log dos resultados de anomalia
	MetricsEnabled bool  `yaml:"metrics_enabled"` // Habilitar métricas Prometheus
	DLQEnabled    bool   `yaml:"dlq_enabled"`    // Enviar anomalias para DLQ
	FileOutput    string `yaml:"file_output"`    // Arquivo para salvar anomalias
	IncludeFeatures bool `yaml:"include_features"` // Incluir features nos resultados
}

// MultiTenantConfig configuração da arquitetura multi-tenant
type MultiTenantConfig struct {
	Enabled           bool                          `yaml:"enabled"`
	DefaultTenant     string                        `yaml:"default_tenant"`
	IsolationMode     string                        `yaml:"isolation_mode"`     // "soft", "hard"
	TenantDiscovery   MultiTenantDiscoveryConfig    `yaml:"tenant_discovery"`
	ResourceIsolation MultiTenantResourceConfig     `yaml:"resource_isolation"`
	SecurityIsolation MultiTenantSecurityConfig     `yaml:"security_isolation"`
	MetricsIsolation  MultiTenantMetricsConfig      `yaml:"metrics_isolation"`
	TenantRouting     MultiTenantRoutingConfig      `yaml:"tenant_routing"`
}

// MultiTenantDiscoveryConfig configuração da descoberta automática de tenants
type MultiTenantDiscoveryConfig struct {
	Enabled               bool     `yaml:"enabled"`
	UpdateInterval        string   `yaml:"update_interval"`
	ConfigPaths           []string `yaml:"config_paths"`
	AutoCreateTenants     bool     `yaml:"auto_create_tenants"`
	AutoUpdateTenants     bool     `yaml:"auto_update_tenants"`
	AutoDeleteTenants     bool     `yaml:"auto_delete_tenants"`
	DefaultTenantTemplate string   `yaml:"default_tenant_template"`
	FileFormats           []string `yaml:"file_formats"`
	ValidationEnabled     bool     `yaml:"validation_enabled"`
}

// MultiTenantResourceConfig configuração de isolamento de recursos
type MultiTenantResourceConfig struct {
	Enabled             bool                              `yaml:"enabled"`
	CPUIsolation        bool                              `yaml:"cpu_isolation"`
	MemoryIsolation     bool                              `yaml:"memory_isolation"`
	DiskIsolation       bool                              `yaml:"disk_isolation"`
	NetworkIsolation    bool                              `yaml:"network_isolation"`
	DefaultLimits       MultiTenantResourceLimitsConfig   `yaml:"default_limits"`
	MonitoringInterval  string                            `yaml:"monitoring_interval"`
	EnforcementMode     string                            `yaml:"enforcement_mode"` // "warn", "throttle", "block"
}

// MultiTenantResourceLimitsConfig limites padrão de recursos para tenants
type MultiTenantResourceLimitsConfig struct {
	MaxMemoryMB        int64   `yaml:"max_memory_mb"`
	MaxCPUPercent      float64 `yaml:"max_cpu_percent"`
	MaxDiskMB          int64   `yaml:"max_disk_mb"`
	MaxConnections     int     `yaml:"max_connections"`
	MaxEventsPerSec    int     `yaml:"max_events_per_sec"`
	MaxFileDescriptors int     `yaml:"max_file_descriptors"`
	MaxGoroutines      int     `yaml:"max_goroutines"`
}

// MultiTenantSecurityConfig configuração de isolamento de segurança
type MultiTenantSecurityConfig struct {
	Enabled                bool     `yaml:"enabled"`
	TenantAuthentication   bool     `yaml:"tenant_authentication"`
	APIKeyRequired         bool     `yaml:"api_key_required"`
	EncryptionPerTenant    bool     `yaml:"encryption_per_tenant"`
	AuditLoggingEnabled    bool     `yaml:"audit_logging_enabled"`
	CrossTenantAccessDenied bool    `yaml:"cross_tenant_access_denied"`
	AllowedTenantSources   []string `yaml:"allowed_tenant_sources"`
	SecurityHeaders        map[string]string `yaml:"security_headers"`
}

// MultiTenantMetricsConfig configuração de isolamento de métricas
type MultiTenantMetricsConfig struct {
	Enabled           bool              `yaml:"enabled"`
	PerTenantMetrics  bool              `yaml:"per_tenant_metrics"`
	MetricsPrefix     string            `yaml:"metrics_prefix"`
	TenantLabels      []string          `yaml:"tenant_labels"`
	CustomLabels      map[string]string `yaml:"custom_labels"`
	AggregationLevel  string            `yaml:"aggregation_level"` // "tenant", "global", "both"
}

// MultiTenantRoutingConfig configuração de roteamento por tenant
type MultiTenantRoutingConfig struct {
	Enabled          bool                           `yaml:"enabled"`
	RoutingStrategy  string                         `yaml:"routing_strategy"`  // "label", "header", "source", "pattern"
	TenantHeader     string                         `yaml:"tenant_header"`     // Nome do header HTTP para tenant
	TenantLabel      string                         `yaml:"tenant_label"`      // Nome do label para tenant
	RoutingRules     []MultiTenantRoutingRule       `yaml:"routing_rules"`
	FallbackTenant   string                         `yaml:"fallback_tenant"`
	LoadBalancing    MultiTenantLoadBalancingConfig `yaml:"load_balancing"`
}

// MultiTenantRoutingRule regra de roteamento para tenants
type MultiTenantRoutingRule struct {
	Name        string            `yaml:"name"`
	Priority    int               `yaml:"priority"`
	Conditions  map[string]string `yaml:"conditions"`  // field -> pattern
	TenantID    string            `yaml:"tenant_id"`
	Enabled     bool              `yaml:"enabled"`
}

// MultiTenantLoadBalancingConfig configuração de balanceamento de carga entre tenants
type MultiTenantLoadBalancingConfig struct {
	Enabled    bool   `yaml:"enabled"`
	Strategy   string `yaml:"strategy"`   // "round_robin", "weighted", "least_connections"
	HealthCheck bool  `yaml:"health_check"`
	WeightDistribution map[string]int `yaml:"weight_distribution"` // tenant_id -> weight
}