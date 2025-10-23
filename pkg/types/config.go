// Package types - Configuration data structures
package types

import (
	"time"
)

// Config represents the complete application configuration structure.
//
// This is the root configuration object that contains all settings for
// the application components, features, and operational parameters.
type Config struct {
	// Core application settings
	App                 AppConfig                 `yaml:"app"`
	Server              ServerConfig              `yaml:"server"`
	Metrics             MetricsConfig             `yaml:"metrics"`

	// Processing pipeline configuration
	Processing          ProcessingConfig          `yaml:"processing"`
	Pipeline            ProcessingConfig          `yaml:"pipeline"`  // Alias for Processing
	Dispatcher          DispatcherConfig          `yaml:"dispatcher"`

	// Input source configurations
	FileMonitorService  FileMonitorServiceConfig  `yaml:"file_monitor_service"`
	FileMonitor         FileMonitorServiceConfig  `yaml:"file_monitor"`
	ContainerMonitor    ContainerMonitorConfig    `yaml:"container_monitor"`
	FilesConfig         FilesConfig               `yaml:"files_config"`

	// Output destination configurations
	Sinks               SinksConfig               `yaml:"sinks"`

	// Storage and persistence
	Positions           PositionsConfig           `yaml:"positions"`
	DiskBuffer          DiskBufferConfig          `yaml:"disk_buffer"`
	DiskCleanup         DiskCleanupConfig         `yaml:"disk_cleanup"`

	// Enterprise features
	Security            SecurityConfig            `yaml:"security"`
	Tracing             TracingConfig             `yaml:"tracing"`
	SLO                 SLOConfig                 `yaml:"slo"`
	GoroutineTracking   GoroutineTrackingConfig   `yaml:"goroutine_tracking"`
	ResourceMonitoring  ResourceMonitoringConfig  `yaml:"resource_monitoring"`
	AnomalyDetection    AnomalyDetectionConfig    `yaml:"anomaly_detection"`
	ServiceDiscovery    ServiceDiscoveryConfig    `yaml:"service_discovery"`
	HotReload           HotReloadConfig           `yaml:"hot_reload"`

	// Validation and enrichment
	TimestampValidation TimestampValidationConfig `yaml:"timestamp_validation"`

	// File pipeline configuration
	File                FilePipelineConfig        `yaml:"file"`

	// Logging configuration
	Logging             AppConfig                 `yaml:"logging"`  // Alias for App logging config
}

// AppConfig contains core application settings.
type AppConfig struct {
	Name        string `yaml:"name"`        // Application name for identification
	Version     string `yaml:"version"`     // Application version
	Environment string `yaml:"environment"` // Deployment environment (dev, staging, prod)
	LogLevel    string `yaml:"log_level"`   // Logging level (trace, debug, info, warn, error)
	LogFormat   string `yaml:"log_format"`  // Log output format (json, text)
	Level       string `yaml:"level"`       // Alias for LogLevel
	Format      string `yaml:"format"`      // Alias for LogFormat
	DataDir     string `yaml:"data_dir"`    // Base directory for application data
	ConfigDir   string `yaml:"config_dir"`  // Configuration files directory
}

// ServerConfig contains HTTP server settings.
type ServerConfig struct {
	Enabled      bool   `yaml:"enabled"`       // Enable HTTP server
	Host         string `yaml:"host"`          // Server bind host
	Port         int    `yaml:"port"`          // Server bind port
	ReadTimeout  string `yaml:"read_timeout"`  // HTTP read timeout
	WriteTimeout string `yaml:"write_timeout"` // HTTP write timeout
	TLSEnabled   bool   `yaml:"tls_enabled"`   // Enable TLS/HTTPS
	TLSCertFile  string `yaml:"tls_cert_file"` // TLS certificate file path
	TLSKeyFile   string `yaml:"tls_key_file"`  // TLS private key file path
}

// MetricsConfig contains Prometheus metrics settings.
type MetricsConfig struct {
	Enabled    bool   `yaml:"enabled"`     // Enable metrics collection
	Port       int    `yaml:"port"`        // Metrics server port
	Path       string `yaml:"path"`        // Metrics endpoint path
	Namespace  string `yaml:"namespace"`   // Metrics namespace prefix
}

// ProcessingConfig contains log processing pipeline settings.
type ProcessingConfig struct {
	Enabled       bool   `yaml:"enabled"`        // Enable log processing pipelines
	PipelinesFile string `yaml:"pipelines_file"` // Path to processing pipelines configuration
	File          string `yaml:"file"`           // Processing file path
}

// DispatcherConfig contains core dispatcher settings.
type DispatcherConfig struct {
	QueueSize        int    `yaml:"queue_size"`        // Internal queue capacity
	WorkerCount      int    `yaml:"worker_count"`      // Number of worker goroutines
	BatchSize        int    `yaml:"batch_size"`        // Maximum entries per batch
	BatchTimeout     string `yaml:"batch_timeout"`     // Maximum time to wait for batch
	MaxRetries       int    `yaml:"max_retries"`       // Maximum retry attempts
	RetryBaseDelay   string `yaml:"retry_base_delay"`  // Base delay between retries
	DLQEnabled       bool   `yaml:"dlq_enabled"`       // Enable dead letter queue
}

// FileMonitorServiceConfig contains file monitoring settings.
type FileMonitorServiceConfig struct {
	Enabled           bool     `yaml:"enabled"`             // Enable file monitoring
	PipelineFile      string   `yaml:"pipeline_file"`       // Path to file pipeline configuration
	WatchDirectories  []string `yaml:"watch_directories"`   // Directories to watch
	IncludePatterns   []string `yaml:"include_patterns"`    // File patterns to include
	PollInterval      string   `yaml:"poll_interval"`       // File system polling interval
	ReadInterval      string   `yaml:"read_interval"`       // File reading interval
	ReadBufferSize    int      `yaml:"read_buffer_size"`    // File read buffer size
	Recursive         bool     `yaml:"recursive"`           // Enable recursive directory monitoring
	FollowSymlinks    bool     `yaml:"follow_symlinks"`     // Follow symbolic links
}

// ContainerMonitorConfig contains Docker container monitoring settings.
type ContainerMonitorConfig struct {
	Enabled           bool              `yaml:"enabled"`             // Enable container monitoring
	SocketPath        string            `yaml:"socket_path"`         // Docker socket path
	MaxConcurrent     int               `yaml:"max_concurrent"`      // Maximum concurrent container connections
	ReconnectInterval string            `yaml:"reconnect_interval"`  // Docker API reconnection interval
	HealthCheckDelay  string            `yaml:"health_check_delay"`  // Container health check delay
	IncludeLabels     map[string]string `yaml:"include_labels"`      // Container labels to include
	ExcludeLabels     map[string]string `yaml:"exclude_labels"`      // Container labels to exclude
	IncludeNames      []string          `yaml:"include_names"`       // Container names to include
	ExcludeNames      []string          `yaml:"exclude_names"`       // Container names to exclude
	IncludeStdout     bool              `yaml:"include_stdout"`      // Include stdout logs
	IncludeStderr     bool              `yaml:"include_stderr"`      // Include stderr logs
	Follow            bool              `yaml:"follow"`              // Follow log stream
}

// FilesConfig contains file selection and filtering settings.
type FilesConfig struct {
	WatchDirectories   []string `yaml:"watch_directories"`   // Directories to monitor
	IncludePatterns    []string `yaml:"include_patterns"`    // File patterns to include
	ExcludePatterns    []string `yaml:"exclude_patterns"`    // File patterns to exclude
	ExcludeDirectories []string `yaml:"exclude_directories"` // Directories to exclude
}

// SinksConfig contains output destination configurations.
type SinksConfig struct {
	Loki          LokiSinkConfig          `yaml:"loki"`          // Grafana Loki sink configuration
	LocalFile     LocalFileSinkConfig     `yaml:"local_file"`     // Local file sink configuration
	Elasticsearch ElasticsearchSinkConfig `yaml:"elasticsearch"`  // Elasticsearch sink configuration
	Splunk        SplunkSinkConfig        `yaml:"splunk"`        // Splunk sink configuration
}

// AuthConfig represents authentication configuration.
type AuthConfig struct {
	Type     string `yaml:"type"`     // Authentication type (basic, bearer, etc.)
	Username string `yaml:"username"` // Username for basic auth
	Password string `yaml:"password"` // Password for basic auth
	Token    string `yaml:"token"`    // Token for bearer auth
}

// LokiSinkConfig contains Grafana Loki output settings.
type LokiSinkConfig struct {
	Enabled          bool                   `yaml:"enabled"`           // Enable Loki sink
	URL              string                 `yaml:"url"`               // Loki push API URL
	PushEndpoint     string                 `yaml:"push_endpoint"`     // Loki push endpoint
	Username         string                 `yaml:"username"`          // Basic auth username
	Password         string                 `yaml:"password"`          // Basic auth password
	TenantID         string                 `yaml:"tenant_id"`         // Multi-tenant ID
	Labels           map[string]string      `yaml:"labels"`            // Static labels to add
	DefaultLabels    map[string]string      `yaml:"default_labels"`    // Default labels to add
	BatchSize        int                    `yaml:"batch_size"`        // Batch size for push requests
	BatchTimeout     string                 `yaml:"batch_timeout"`     // Batch timeout duration
	Timeout          string                 `yaml:"timeout"`           // Request timeout
	Compression      bool                   `yaml:"compression"`       // Enable request compression
	QueueSize        int                    `yaml:"queue_size"`        // Internal queue size
	Headers          map[string]string      `yaml:"headers"`           // Additional HTTP headers
	Auth             AuthConfig             `yaml:"auth"`              // Authentication configuration
	AdaptiveBatching AdaptiveBatchingConfig `yaml:"adaptive_batching"` // Adaptive batching configuration
}

// LocalFileSinkConfig contains local file output settings.
type LocalFileSinkConfig struct {
	Enabled      bool   `yaml:"enabled"`       // Enable local file sink
	Directory    string `yaml:"directory"`     // Output directory
	Filename     string `yaml:"filename"`      // Output filename pattern
	MaxFileSize  string `yaml:"max_file_size"`  // Maximum file size before rotation
	MaxFiles     int    `yaml:"max_files"`     // Maximum number of rotated files
	Compress     bool   `yaml:"compress"`      // Compress rotated files
	OutputFormat string `yaml:"output_format"` // Output format (json, text, csv)
}

// PositionsConfig contains file position tracking settings.
type PositionsConfig struct {
	Enabled          bool   `yaml:"enabled"`           // Enable position tracking
	Directory        string `yaml:"directory"`         // Position files directory
	FlushInterval    string `yaml:"flush_interval"`    // Position flush interval
	MaxMemoryBuffer  int    `yaml:"max_memory_buffer"` // Maximum in-memory position entries
	ForceFlushOnExit bool   `yaml:"force_flush_on_exit"` // Force flush on application exit
	CleanupInterval  string `yaml:"cleanup_interval"`  // Cleanup interval for stale positions
	MaxPositionAge   string `yaml:"max_position_age"`  // Maximum age for position entries
}

// DiskBufferConfig contains persistent buffering settings.
type DiskBufferConfig struct {
	Enabled           bool   `yaml:"enabled"`            // Enable disk buffering
	Directory         string `yaml:"directory"`          // Buffer files directory
	MaxFileSize       int64  `yaml:"max_file_size"`      // Maximum buffer file size
	MaxTotalSize      int64  `yaml:"max_total_size"`     // Maximum total buffer size
	FlushInterval     string `yaml:"flush_interval"`     // Buffer flush interval
	SyncInterval      string `yaml:"sync_interval"`      // Disk sync interval
	CompressionEnabled bool  `yaml:"compression_enabled"` // Enable buffer compression
	EncryptionEnabled bool   `yaml:"encryption_enabled"` // Enable buffer encryption
	RetentionPeriod   string `yaml:"retention_period"`   // Buffer file retention period
}

// DiskCleanupConfig contains automated cleanup settings.
type DiskCleanupConfig struct {
	Enabled         bool   `yaml:"enabled"`          // Enable disk cleanup
	CheckInterval   string `yaml:"check_interval"`   // Cleanup check interval
	MaxSizeGB       float64 `yaml:"max_size_gb"`     // Maximum disk usage in GB
	WarningPercent  float64 `yaml:"warning_percent"` // Warning threshold percentage
	CleanupPercent  float64 `yaml:"cleanup_percent"` // Cleanup trigger percentage
	RetentionPeriod string `yaml:"retention_period"` // File retention period
	AutoCleanup     bool   `yaml:"auto_cleanup"`     // Enable automatic cleanup
	CleanupStrategy string `yaml:"cleanup_strategy"` // Cleanup strategy (oldest, largest, etc.)
}

// ElasticsearchSinkConfig contains Elasticsearch output settings.
type ElasticsearchSinkConfig struct {
	Enabled     bool     `yaml:"enabled"`      // Enable Elasticsearch sink
	URLs        []string `yaml:"urls"`         // Elasticsearch cluster URLs
	Index       string   `yaml:"index"`        // Index name pattern
	Username    string   `yaml:"username"`     // Basic auth username
	Password    string   `yaml:"password"`     // Basic auth password
	BatchSize   int      `yaml:"batch_size"`   // Batch size for bulk requests
	BatchTimeout string  `yaml:"batch_timeout"` // Batch timeout duration
	Timeout     string   `yaml:"timeout"`      // Request timeout
	Compression bool     `yaml:"compression"`  // Enable request compression
}

// SplunkSinkConfig contains Splunk output settings.
type SplunkSinkConfig struct {
	Enabled     bool   `yaml:"enabled"`      // Enable Splunk sink
	URL         string `yaml:"url"`          // Splunk HEC URL
	Token       string `yaml:"token"`        // HEC token
	Index       string `yaml:"index"`        // Index name
	Source      string `yaml:"source"`       // Source identifier
	SourceType  string `yaml:"source_type"`  // Source type
	BatchSize   int    `yaml:"batch_size"`   // Batch size for requests
	BatchTimeout string `yaml:"batch_timeout"` // Batch timeout duration
	Timeout     string `yaml:"timeout"`      // Request timeout
	Compression bool   `yaml:"compression"`  // Enable request compression
}

// TimestampValidationConfig contains timestamp validation settings.
type TimestampValidationConfig struct {
	Enabled              bool     `yaml:"enabled"`                // Enable timestamp validation
	MaxDrift             string   `yaml:"max_drift"`              // Maximum allowed timestamp drift
	DefaultTimezone      string   `yaml:"default_timezone"`       // Default timezone for parsing
	Formats              []string `yaml:"formats"`                // Supported timestamp formats
	FallbackToCurrent    bool     `yaml:"fallback_to_current"`    // Use current time if parsing fails
	MaxPastAgeSeconds    int      `yaml:"max_past_age_seconds"`   // Maximum age in past allowed
	MaxFutureAgeSeconds  int      `yaml:"max_future_age_seconds"` // Maximum age in future allowed
	ClampEnabled         bool     `yaml:"clamp_enabled"`          // Enable timestamp clamping
	ClampDLQ             bool     `yaml:"clamp_dlq"`              // Send clamped entries to DLQ
	InvalidAction        string   `yaml:"invalid_action"`         // Action for invalid timestamps
	AcceptedFormats      []string `yaml:"accepted_formats"`       // List of accepted timestamp formats
}

// FilePipelineConfig contains file pipeline configuration settings.
type FilePipelineConfig struct {
	Enabled        bool                   `yaml:"enabled"`         // Enable file pipeline
	Files          map[string]interface{} `yaml:"files"`           // File monitoring configurations
	PipelineConfig map[string]interface{} `yaml:"pipeline_config"` // Pipeline configuration
	Version        string                 `yaml:"version"`         // Configuration version
	Directories    []string               `yaml:"directories"`     // Directories to monitor
}

// FilePipelineDirEntry represents a directory entry in file pipeline configuration.
type FilePipelineDirEntry struct {
	Path                string            `yaml:"path"`                // Directory path to monitor
	IncludePatterns     []string          `yaml:"include_patterns"`    // File patterns to include
	ExcludePatterns     []string          `yaml:"exclude_patterns"`    // File patterns to exclude
	ExcludeDirectories  []string          `yaml:"exclude_directories"` // Directories to exclude
	Recursive           bool              `yaml:"recursive"`           // Monitor recursively
	FollowSymlinks      bool              `yaml:"follow_symlinks"`     // Follow symbolic links
	Patterns            []string          `yaml:"patterns"`            // File patterns
	DefaultLabels       map[string]string `yaml:"default_labels"`      // Default labels for entries
}

// Legacy configuration structures for backward compatibility

// FileConfig represents legacy file monitoring configuration.
type FileConfig struct {
	Enabled            bool          `yaml:"enabled"`
	PollInterval       time.Duration `yaml:"poll_interval"`
	BufferSize         int           `yaml:"buffer_size"`
	PositionsPath      string        `yaml:"positions_path"`
	WatchDirectories   []string      `yaml:"watch_directories"`
	IncludePatterns    []string      `yaml:"include_patterns"`
	ExcludePatterns    []string      `yaml:"exclude_patterns"`
	ExcludeDirectories []string      `yaml:"exclude_directories"`
	ReadInterval       time.Duration `yaml:"read_interval"`
	Recursive          bool          `yaml:"recursive"`
	FollowSymlinks     bool          `yaml:"follow_symlinks"`
	PipelineConfig     map[string]interface{} `yaml:"pipeline_config"` // Pipeline configuration
}

// LokiConfig represents legacy Loki configuration (alias for LokiSinkConfig).
type LokiConfig = LokiSinkConfig

// CircuitBreakerConfig represents circuit breaker configuration.
type CircuitBreakerConfig struct {
	Enabled         bool   `yaml:"enabled"`          // Enable circuit breaker
	FailureThreshold int   `yaml:"failure_threshold"` // Number of failures before opening
	TimeoutDuration string `yaml:"timeout_duration"` // Timeout before retrying
	ResetTimeout    string `yaml:"reset_timeout"`    // Time before resetting circuit
	MaxRetries      int    `yaml:"max_retries"`      // Maximum retry attempts
}

// CircuitBreaker is an alias for backwards compatibility
type CircuitBreaker = CircuitBreakerConfig

// AdaptiveBatchingConfig represents adaptive batching configuration.
type AdaptiveBatchingConfig struct {
	Enabled             bool    `yaml:"enabled"`               // Enable adaptive batching
	MinBatchSize        int     `yaml:"min_batch_size"`        // Minimum batch size
	MaxBatchSize        int     `yaml:"max_batch_size"`        // Maximum batch size
	InitialBatchSize    int     `yaml:"initial_batch_size"`    // Initial batch size
	TargetLatency       string  `yaml:"target_latency"`        // Target latency for adaptation
	LatencyThreshold    string  `yaml:"latency_threshold"`     // Latency threshold for batch size adjustment
	ScalingFactor       float64 `yaml:"scaling_factor"`        // Scaling factor for batch size adjustment
	ThroughputTarget    int     `yaml:"throughput_target"`     // Target throughput for adaptation
	BufferSize          int     `yaml:"buffer_size"`           // Buffer size for batching
	MinFlushDelay       string  `yaml:"min_flush_delay"`       // Minimum flush delay
	MaxFlushDelay       string  `yaml:"max_flush_delay"`       // Maximum flush delay
	InitialFlushDelay   string  `yaml:"initial_flush_delay"`   // Initial flush delay
	AdaptationInterval  string  `yaml:"adaptation_interval"`   // Adaptation interval for adjustments
}

// TextFormatConfig represents text format configuration.
type TextFormatConfig struct {
	TimestampFormat   string `yaml:"timestamp_format"`   // Timestamp format for text output
	IncludeTimestamp  bool   `yaml:"include_timestamp"`  // Include timestamp in output
	IncludeLabels     bool   `yaml:"include_labels"`     // Include labels in output
	FieldSeparator    string `yaml:"field_separator"`    // Field separator for text format
}

// RotationConfig represents file rotation configuration.
type RotationConfig struct {
	Enabled     bool   `yaml:"enabled"`      // Enable file rotation
	MaxSize     string `yaml:"max_size"`     // Maximum file size before rotation
	MaxSizeMB   int    `yaml:"max_size_mb"`  // Maximum file size in MB before rotation
	MaxFiles    int    `yaml:"max_files"`    // Maximum number of rotated files
	MaxAge      string `yaml:"max_age"`      // Maximum age before rotation
	Compress    bool   `yaml:"compress"`     // Compress rotated files
}

// LocalFileConfig represents legacy local file configuration (alias for LocalFileSinkConfig).
type LocalFileConfig struct {
	Enabled                 bool              `yaml:"enabled"`                   // Enable local file sink
	Directory               string            `yaml:"directory"`                 // Output directory
	Filename                string            `yaml:"filename"`                  // Output filename pattern
	MaxFileSize             string            `yaml:"max_file_size"`             // Maximum file size before rotation
	MaxFiles                int               `yaml:"max_files"`                 // Maximum number of rotated files
	Compress                bool              `yaml:"compress"`                  // Compress rotated files
	OutputFormat            string            `yaml:"output_format"`             // Output format (json, text, csv)
	TextFormat              TextFormatConfig  `yaml:"text_format"`               // Text format configuration
	QueueSize               int               `yaml:"queue_size"`                // Internal queue size
	MaxTotalDiskGB          float64           `yaml:"max_total_disk_gb"`         // Maximum total disk usage in GB
	DiskCheckInterval       string            `yaml:"disk_check_interval"`       // Disk check interval
	CleanupThresholdPercent float64           `yaml:"cleanup_threshold_percent"` // Cleanup threshold percentage
	Rotation                RotationConfig    `yaml:"rotation"`                  // File rotation configuration
	EmergencyCleanupEnabled bool              `yaml:"emergency_cleanup_enabled"` // Enable emergency cleanup
}

// DockerConfig represents legacy Docker monitoring configuration.
type DockerConfig struct {
	Enabled           bool              `yaml:"enabled"`
	SocketPath        string            `yaml:"socket_path"`
	MaxConcurrent     int               `yaml:"max_concurrent"`
	ReconnectInterval time.Duration     `yaml:"reconnect_interval"`
	HealthCheckDelay  time.Duration     `yaml:"health_check_delay"`
	IncludeLabels     map[string]string `yaml:"include_labels"`
	ExcludeLabels     map[string]string `yaml:"exclude_labels"`
	IncludeNames      []string          `yaml:"include_names"`
	ExcludeNames      []string          `yaml:"exclude_names"`
}

// PipelineConfig represents processing pipeline configuration.
type PipelineConfig struct {
	Enabled bool   `yaml:"enabled"` // Enable processing pipelines
	File    string `yaml:"file"`    // Pipeline configuration file path
}

// ServiceDiscoveryConfig contains service discovery settings.
type ServiceDiscoveryConfig struct {
	Enabled         bool                      `yaml:"enabled"`          // Enable service discovery
	UpdateInterval  string                    `yaml:"update_interval"`  // Discovery update interval
	DockerEnabled   bool                      `yaml:"docker_enabled"`   // Enable Docker discovery
	FileEnabled     bool                      `yaml:"file_enabled"`     // Enable file discovery
	Docker          DockerDiscoveryConfig     `yaml:"docker"`           // Docker discovery config
	File            FileDiscoveryConfig       `yaml:"file"`             // File discovery config
	KubernetesEnabled bool                    `yaml:"kubernetes_enabled"` // Enable Kubernetes discovery (future)
	Kubernetes      KubernetesDiscoveryConfig `yaml:"kubernetes"`       // Kubernetes discovery config
}

// DockerDiscoveryConfig configuration for Docker service discovery.
type DockerDiscoveryConfig struct {
	SocketPath      string            `yaml:"socket_path"`      // Docker socket path
	RequiredLabels  map[string]string `yaml:"required_labels"`  // Required container labels
	ExcludeLabels   map[string]string `yaml:"exclude_labels"`   // Labels to exclude containers
	RequireLabel    string            `yaml:"require_label"`    // Single required label key
	PipelineLabel   string            `yaml:"pipeline_label"`   // Label for pipeline identification
	ComponentLabel  string            `yaml:"component_label"`  // Label for component identification
	TenantLabel     string            `yaml:"tenant_label"`     // Label for tenant identification
}

// FileDiscoveryConfig configuration for file-based service discovery.
type FileDiscoveryConfig struct {
	WatchPaths      []string          `yaml:"watch_paths"`      // Paths to watch for log files
	ConfigFiles     []string          `yaml:"config_files"`     // Configuration files to monitor
	RequiredLabels  map[string]string `yaml:"required_labels"`  // Required labels for file discovery
	AutoDetectLogs  bool              `yaml:"auto_detect_logs"` // Automatically detect log files
}

// KubernetesDiscoveryConfig configuration for Kubernetes service discovery.
type KubernetesDiscoveryConfig struct {
	Namespace           string            `yaml:"namespace"`             // Kubernetes namespace
	RequiredAnnotations map[string]string `yaml:"required_annotations"`  // Required pod annotations
	RequiredLabels      map[string]string `yaml:"required_labels"`       // Required pod labels
	ServiceAccount      string            `yaml:"service_account"`       // Service account for K8s API access
}