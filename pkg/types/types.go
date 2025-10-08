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
	Positions            PositionConfig           `yaml:"positions"`

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