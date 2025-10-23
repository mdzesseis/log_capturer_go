// Package types - Enterprise feature configuration and data structures
package types

import (
	"context"
	"time"
)

// SecurityConfig contains enterprise security feature settings.
type SecurityConfig struct {
	Enabled        bool                    `yaml:"enabled"`         // Enable security features
	Authentication AuthenticationConfig   `yaml:"authentication"`  // Authentication configuration
	Authorization  AuthorizationConfig    `yaml:"authorization"`   // Authorization configuration
	Audit          AuditConfig            `yaml:"audit"`           // Audit logging configuration
	RateLimit      SecurityRateLimitConfig `yaml:"rate_limit"`     // Security-specific rate limiting
}

// AuthenticationConfig contains user authentication settings.
type AuthenticationConfig struct {
	Enabled        bool              `yaml:"enabled"`         // Enable authentication
	Method         string            `yaml:"method"`          // Authentication method (basic, jwt, oauth)
	SessionTimeout string            `yaml:"session_timeout"` // Session timeout duration
	MaxAttempts    int               `yaml:"max_attempts"`    // Maximum login attempts
	LockoutTime    string            `yaml:"lockout_time"`    // Account lockout duration
	Users          map[string]User   `yaml:"users"`           // Static user definitions
	JWT            JWTConfig         `yaml:"jwt"`             // JWT-specific configuration
	OAuth          OAuthConfig       `yaml:"oauth"`           // OAuth-specific configuration
}

// AuthorizationConfig contains access control settings.
type AuthorizationConfig struct {
	Enabled     bool                `yaml:"enabled"`      // Enable authorization
	DefaultRole string              `yaml:"default_role"` // Default role for authenticated users
	Roles       map[string]Role     `yaml:"roles"`        // Role definitions
	Resources   map[string]Resource `yaml:"resources"`    // Protected resource definitions
}

// AuditConfig contains security audit logging settings.
type AuditConfig struct {
	Enabled     bool   `yaml:"enabled"`      // Enable audit logging
	LogFile     string `yaml:"log_file"`     // Audit log file path
	MaxFileSize string `yaml:"max_file_size"` // Maximum audit log file size
	MaxFiles    int    `yaml:"max_files"`    // Maximum number of audit log files
	Format      string `yaml:"format"`       // Audit log format (json, text)
}

// SecurityRateLimitConfig contains security-specific rate limiting.
type SecurityRateLimitConfig struct {
	Enabled           bool   `yaml:"enabled"`             // Enable security rate limiting
	LoginAttempts     int    `yaml:"login_attempts"`      // Max login attempts per window
	LoginWindow       string `yaml:"login_window"`        // Login attempt window duration
	APIRequests       int    `yaml:"api_requests"`        // Max API requests per window
	APIWindow         string `yaml:"api_window"`          // API request window duration
	BlockDuration     string `yaml:"block_duration"`      // Duration to block violating IPs
}

// User represents a user account for authentication.
type User struct {
	Username     string    `yaml:"username"`      // Username for login
	PasswordHash string    `yaml:"password_hash"` // Hashed password
	Roles        []string  `yaml:"roles"`         // Assigned roles
	Enabled      bool      `yaml:"enabled"`       // Account enabled status
	CreatedAt    time.Time `yaml:"created_at"`    // Account creation timestamp
	LastLogin    time.Time `yaml:"last_login"`    // Last successful login
	LoginCount   int64     `yaml:"login_count"`   // Total successful logins
}

// Role represents a role with associated permissions.
type Role struct {
	Name        string       `yaml:"name"`        // Role name
	Description string       `yaml:"description"` // Role description
	Permissions []Permission `yaml:"permissions"` // Granted permissions
}

// Permission represents an access permission for a resource and action.
type Permission struct {
	Resource string `yaml:"resource"` // Resource identifier (endpoint, data type, etc.)
	Action   string `yaml:"action"`   // Action allowed (read, write, delete, etc.)
}

// Resource represents a protected resource in the system.
type Resource struct {
	Name        string   `yaml:"name"`        // Resource name
	Description string   `yaml:"description"` // Resource description
	Actions     []string `yaml:"actions"`     // Available actions for this resource
}

// JWTConfig contains JWT-specific authentication settings.
type JWTConfig struct {
	Secret         string `yaml:"secret"`          // JWT signing secret
	ExpirationTime string `yaml:"expiration_time"` // Token expiration time
	Issuer         string `yaml:"issuer"`          // JWT issuer
	Audience       string `yaml:"audience"`        // JWT audience
}

// OAuthConfig contains OAuth-specific authentication settings.
type OAuthConfig struct {
	Provider     string            `yaml:"provider"`      // OAuth provider (google, github, etc.)
	ClientID     string            `yaml:"client_id"`     // OAuth client ID
	ClientSecret string            `yaml:"client_secret"` // OAuth client secret
	RedirectURL  string            `yaml:"redirect_url"`  // OAuth redirect URL
	Scopes       []string          `yaml:"scopes"`        // Required OAuth scopes
	Endpoints    map[string]string `yaml:"endpoints"`     // OAuth endpoint URLs
}

// TracingConfig contains distributed tracing settings.
type TracingConfig struct {
	Enabled        bool              `yaml:"enabled"`         // Enable distributed tracing
	ServiceName    string            `yaml:"service_name"`    // Service name for tracing
	ServiceVersion string            `yaml:"service_version"` // Service version for tracing
	Exporter       string            `yaml:"exporter"`        // Trace exporter (jaeger, zipkin, otlp)
	Endpoint       string            `yaml:"endpoint"`        // Trace collector endpoint
	SampleRate     float64           `yaml:"sample_rate"`     // Trace sampling rate (0.0 to 1.0)
	Headers        map[string]string `yaml:"headers"`         // Additional headers for trace export
	Compression    bool              `yaml:"compression"`     // Enable trace compression
	BatchTimeout   string            `yaml:"batch_timeout"`   // Trace batch timeout
	BatchSize      int               `yaml:"batch_size"`      // Trace batch size
}

// SLOConfig contains Service Level Objective monitoring settings.
type SLOConfig struct {
	Enabled       bool              `yaml:"enabled"`        // Enable SLO monitoring
	PrometheusURL string            `yaml:"prometheus_url"` // Prometheus server URL
	QueryInterval string            `yaml:"query_interval"` // SLO query interval
	Objectives    map[string]SLO    `yaml:"objectives"`     // SLO definitions
	Alerting      SLOAlertConfig    `yaml:"alerting"`       // SLO alerting configuration
}

// SLO represents a Service Level Objective definition.
type SLO struct {
	Name           string  `yaml:"name"`            // SLO name
	Description    string  `yaml:"description"`     // SLO description
	Target         float64 `yaml:"target"`          // Target percentage (0.0 to 1.0)
	Window         string  `yaml:"window"`          // Evaluation window (24h, 7d, 30d)
	Query          string  `yaml:"query"`           // Prometheus query for SLO metric
	ErrorBudget    float64 `yaml:"error_budget"`    // Error budget percentage
	BurnRateAlert  float64 `yaml:"burn_rate_alert"` // Burn rate alert threshold
}

// SLOAlertConfig contains SLO alerting settings.
type SLOAlertConfig struct {
	Enabled     bool   `yaml:"enabled"`      // Enable SLO alerting
	WebhookURL  string `yaml:"webhook_url"`  // Alert webhook URL
	SlackToken  string `yaml:"slack_token"`  // Slack bot token
	SlackChannel string `yaml:"slack_channel"` // Slack channel for alerts
	EmailSMTP   string `yaml:"email_smtp"`   // SMTP server for email alerts
	EmailFrom   string `yaml:"email_from"`   // From email address
	EmailTo     []string `yaml:"email_to"`   // Alert email recipients
}

// GoroutineTrackingConfig contains goroutine monitoring settings.
type GoroutineTrackingConfig struct {
	Enabled           bool   `yaml:"enabled"`             // Enable goroutine tracking
	CheckInterval     string `yaml:"check_interval"`      // Monitoring check interval
	LeakThreshold     int    `yaml:"leak_threshold"`      // Goroutine count threshold for leak detection
	GrowthThreshold   float64 `yaml:"growth_threshold"`   // Growth rate threshold for alerts
	AlertWebhook      string `yaml:"alert_webhook"`       // Webhook URL for leak alerts
	DetailedProfiling bool   `yaml:"detailed_profiling"`  // Enable detailed profiling
	StackTraceDepth   int    `yaml:"stack_trace_depth"`   // Stack trace depth for profiling
}

// ResourceMonitoringConfig contains system resource monitoring settings.
type ResourceMonitoringConfig struct {
	Enabled             bool   `yaml:"enabled"`               // Enable resource monitoring
	CheckInterval       string `yaml:"check_interval"`        // Resource check interval
	GoroutineThreshold  int    `yaml:"goroutine_threshold"`   // Goroutine count alert threshold
	MemoryThresholdMB   int64  `yaml:"memory_threshold_mb"`   // Memory usage alert threshold
	FDThreshold         int    `yaml:"fd_threshold"`          // File descriptor alert threshold
	AlertOnThreshold    bool   `yaml:"alert_on_threshold"`    // Enable threshold-based alerting
	AlertWebhookURL     string `yaml:"alert_webhook_url"`     // Webhook URL for resource alerts
	CPUThreshold        float64 `yaml:"cpu_threshold"`        // CPU usage alert threshold
	DiskThreshold       float64 `yaml:"disk_threshold"`       // Disk usage alert threshold
}

// AnomalyDetectionConfig contains anomaly detection settings.
type AnomalyDetectionConfig struct {
	Enabled         bool              `yaml:"enabled"`          // Enable anomaly detection
	Algorithm       string            `yaml:"algorithm"`        // Detection algorithm (statistical, ml, hybrid)
	SensitivityLevel string           `yaml:"sensitivity_level"` // Detection sensitivity (low, medium, high)
	WindowSize      string            `yaml:"window_size"`      // Analysis window size
	MinSamples      int               `yaml:"min_samples"`      // Minimum samples for detection
	Thresholds      AnomalyThresholds `yaml:"thresholds"`       // Detection thresholds
	Actions         AnomalyActions    `yaml:"actions"`          // Actions to take on anomaly detection
	ModelPath       string            `yaml:"model_path"`       // Path to ML model files
	TrainingEnabled bool              `yaml:"training_enabled"` // Enable online model training
}

// AnomalyThresholds contains anomaly detection threshold settings.
type AnomalyThresholds struct {
	VolumeChange    float64 `yaml:"volume_change"`    // Log volume change threshold
	PatternDeviation float64 `yaml:"pattern_deviation"` // Log pattern deviation threshold
	ErrorRateSpike  float64 `yaml:"error_rate_spike"` // Error rate spike threshold
	LatencyIncrease float64 `yaml:"latency_increase"` // Processing latency increase threshold
}

// AnomalyActions contains actions to take when anomalies are detected.
type AnomalyActions struct {
	AlertEnabled    bool   `yaml:"alert_enabled"`     // Enable anomaly alerts
	WebhookURL      string `yaml:"webhook_url"`       // Alert webhook URL
	AutoScale       bool   `yaml:"auto_scale"`        // Enable automatic scaling
	CircuitBreaker  bool   `yaml:"circuit_breaker"`   // Enable circuit breaker
	LogLevel        string `yaml:"log_level"`         // Log level for anomaly events
	MetricsEnabled  bool   `yaml:"metrics_enabled"`   // Export anomaly metrics
}

// HotReloadConfig contains configuration hot-reload settings.
type HotReloadConfig struct {
	Enabled      bool     `yaml:"enabled"`       // Enable configuration hot reload
	WatchFiles   []string `yaml:"watch_files"`   // Configuration files to watch
	CheckInterval string  `yaml:"check_interval"` // File change check interval
	GracePeriod  string   `yaml:"grace_period"`  // Grace period before applying changes
	BackupConfig bool     `yaml:"backup_config"` // Backup configuration before reload
	ValidateOnly bool     `yaml:"validate_only"` // Only validate, don't apply changes
}

// TaskManager represents a task coordination interface.
type TaskManager interface {
	// StartTask inicia uma nova tarefa
	StartTask(ctx context.Context, taskID string, fn func(context.Context) error) error
	// StopTask para uma tarefa
	StopTask(taskID string) error
	// Heartbeat atualiza o heartbeat de uma tarefa
	Heartbeat(taskID string) error
	// GetTaskStatus retorna o status de uma tarefa
	GetTaskStatus(taskID string) TaskStatus
	// GetAllTasks retorna o status de todas as tarefas
	GetAllTasks() map[string]TaskStatus
	// Cleanup limpa todos os recursos
	Cleanup()
}

// TaskStatus represents the status of a task
type TaskStatus struct {
	ID            string    `json:"id"`
	State         string    `json:"state"`
	StartedAt     time.Time `json:"started_at"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
	ErrorCount    int64     `json:"error_count"`
	LastError     string    `json:"last_error,omitempty"`
}

const (
	TaskStatePending   = "pending"
	TaskStateRunning   = "running"
	TaskStateCompleted = "completed"
	TaskStateFailed    = "failed"
)

// CircuitBreakerState represents the state of a circuit breaker
type CircuitBreakerState string

const (
	CircuitBreakerClosed   CircuitBreakerState = "closed"
	CircuitBreakerOpen     CircuitBreakerState = "open"
	CircuitBreakerHalfOpen CircuitBreakerState = "half_open"
)

// CircuitBreakerStats represents circuit breaker statistics
type CircuitBreakerStats struct {
	State         CircuitBreakerState `json:"state"`
	FailureCount  int64               `json:"failure_count"`
	SuccessCount  int64               `json:"success_count"`
	Failures      int64               `json:"failures"`      // Alias for FailureCount
	Successes     int64               `json:"successes"`     // Alias for SuccessCount
	Requests      int64               `json:"requests"`      // Total requests
	LastFailure   time.Time           `json:"last_failure"`
	LastSuccess   time.Time           `json:"last_success"`
	OpenTimestamp time.Time           `json:"open_timestamp"`
	NextRetryTime time.Time           `json:"next_retry_time"` // When next retry will be attempted
}