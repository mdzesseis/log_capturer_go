// Package types - Statistics and monitoring data structures
package types

import (
	"time"
)

// MonitorStatus represents the current operational status of a monitoring component.
//
// This structure provides detailed status information for health checks and monitoring
// systems to assess the operational state of file monitors, container monitors, and
// other input sources.
type MonitorStatus struct {
	// Component identification and basic status
	Name      string    `json:"name"`       // Monitor component name (e.g., "file_monitor", "container_monitor")
	Status    string    `json:"status"`     // Current status: "healthy", "degraded", "failed"
	LastCheck time.Time `json:"last_check"` // Timestamp of last health check
	IsRunning bool      `json:"is_running"` // Whether the monitor is currently running
	IsHealthy bool      `json:"is_healthy"` // Whether the monitor is healthy

	// Operational metrics
	FilesWatched   int64 `json:"files_watched,omitempty"`   // Number of files currently being monitored
	EntriesRead    int64 `json:"entries_read"`              // Total log entries read since start
	ErrorCount     int64 `json:"error_count"`               // Number of errors encountered
	LastError      string `json:"last_error,omitempty"`     // Description of most recent error
	LastErrorTime  time.Time `json:"last_error_time,omitempty"` // Timestamp of most recent error
}

// DispatcherStats provides comprehensive statistics about dispatcher operations.
//
// These statistics are used for monitoring, alerting, and performance analysis
// of the core log processing pipeline.
//
// Thread-safety note: int64 counter fields (Processed, Failed, etc.) should be
// accessed using sync/atomic operations for lock-free performance. Complex fields
// (maps, strings, timestamps) still require mutex protection.
type DispatcherStats struct {
	// Processing volume metrics - use atomic operations for these counters
	Processed           int64            `json:"processed"`           // Total entries processed successfully
	TotalProcessed      int64            `json:"total_processed"`     // Alias for Processed
	Failed              int64            `json:"failed"`              // Total entries that failed processing
	ErrorCount          int64            `json:"error_count"`         // Alias for Failed
	Retries             int64            `json:"retries"`             // Total retry attempts made
	Throttled           int64            `json:"throttled"`           // Entries rejected due to rate limiting
	DuplicatesDetected  int64            `json:"duplicates_detected"` // Number of duplicate entries detected
	SinkDistribution    map[string]int64 `json:"sink_distribution"`   // Entries sent to each sink by name (requires mutex)
	LastProcessedTime   time.Time        `json:"last_processed_time"` // Timestamp of last processed entry (requires mutex)

	// Performance metrics
	ProcessingRate   float64       `json:"processing_rate"`   // Entries processed per second
	AverageLatency   time.Duration `json:"average_latency"`   // Average processing latency
	QueueSize        int           `json:"queue_size"`        // Current queue utilization
	QueueCapacity    int           `json:"queue_capacity"`    // Maximum queue capacity

	// Error tracking
	LastError     string    `json:"last_error,omitempty"`      // Most recent error message (requires mutex)
	LastErrorTime time.Time `json:"last_error_time,omitempty"` // Timestamp of most recent error (requires mutex)
	ErrorRate     float64   `json:"error_rate"`                // Errors per second over recent window

	// Advanced metrics (enterprise features)
	DeduplicationRate float64 `json:"deduplication_rate,omitempty"` // Percentage of duplicates filtered
	BackpressureLevel string  `json:"backpressure_level,omitempty"` // Current backpressure level (requires mutex)
	DLQSize           int64   `json:"dlq_size,omitempty"`           // Dead letter queue size
}

// HealthStatus represents the overall health of the application and its components.
//
// This comprehensive health structure is used by load balancers, monitoring systems,
// and health check endpoints to assess application readiness and operational status.
type HealthStatus struct {
	// Overall application status
	Status    string    `json:"status"`    // "healthy", "degraded", "failed"
	Timestamp time.Time `json:"timestamp"` // Health check timestamp
	Version   string    `json:"version"`   // Application version
	Uptime    time.Duration `json:"uptime"` // Time since application start

	// Component health details
	Components map[string]ComponentHealth `json:"components"` // Health of individual components

	// System resource status
	Resources ResourceStatus `json:"resources,omitempty"` // System resource utilization

	// Enterprise features status
	Enterprise EnterpriseStatus `json:"enterprise,omitempty"` // Enterprise features health
}

// ComponentHealth represents the health status of an individual application component.
type ComponentHealth struct {
	Status      string                 `json:"status"`                // "healthy", "degraded", "failed"
	LastCheck   time.Time              `json:"last_check"`            // Last health check timestamp
	ErrorCount  int64                  `json:"error_count"`           // Total errors since start
	LastError   string                 `json:"last_error,omitempty"`  // Most recent error message
	Metrics     map[string]interface{} `json:"metrics,omitempty"`     // Component-specific metrics
	Config      map[string]interface{} `json:"config,omitempty"`      // Sanitized configuration
}

// ResourceStatus provides system resource utilization information.
type ResourceStatus struct {
	// Memory utilization
	MemoryUsedMB    int64   `json:"memory_used_mb"`    // Current memory usage in MB
	MemoryLimitMB   int64   `json:"memory_limit_mb"`   // Memory limit in MB
	MemoryPercent   float64 `json:"memory_percent"`    // Memory utilization percentage

	// CPU utilization
	CPUPercent      float64 `json:"cpu_percent"`       // CPU utilization percentage
	LoadAverage     float64 `json:"load_average"`      // System load average

	// Goroutine tracking
	GoroutineCount  int     `json:"goroutine_count"`   // Current number of goroutines
	GoroutineLimit  int     `json:"goroutine_limit"`   // Goroutine threshold for alerts

	// File descriptor usage
	FDCount         int     `json:"fd_count"`          // Current file descriptor count
	FDLimit         int     `json:"fd_limit"`          // File descriptor limit

	// Disk usage
	DiskUsedGB      float64 `json:"disk_used_gb"`      // Disk space used in GB
	DiskAvailableGB float64 `json:"disk_available_gb"` // Available disk space in GB
	DiskPercent     float64 `json:"disk_percent"`      // Disk utilization percentage
}

// EnterpriseStatus provides status information for enterprise features.
type EnterpriseStatus struct {
	// Security features
	SecurityEnabled    bool      `json:"security_enabled"`              // Security manager status
	AuthenticationRate float64   `json:"authentication_rate,omitempty"` // Authentication requests per second
	FailedLoginCount   int64     `json:"failed_login_count,omitempty"`  // Failed authentication attempts
	LastSecurityEvent  time.Time `json:"last_security_event,omitempty"` // Most recent security event

	// Distributed tracing
	TracingEnabled     bool    `json:"tracing_enabled"`                // Tracing manager status
	TracesPerSecond    float64 `json:"traces_per_second,omitempty"`    // Trace generation rate
	TraceExportRate    float64 `json:"trace_export_rate,omitempty"`    // Trace export success rate

	// SLO monitoring
	SLOEnabled         bool                     `json:"slo_enabled"`                   // SLO manager status
	SLOCompliance      map[string]float64       `json:"slo_compliance,omitempty"`      // SLO compliance percentages
	ErrorBudgetStatus  map[string]string        `json:"error_budget_status,omitempty"` // Error budget status by SLO

	// Goroutine tracking
	GoroutineTracking  bool    `json:"goroutine_tracking"`             // Goroutine tracker status
	LeakDetectionCount int     `json:"leak_detection_count,omitempty"` // Number of potential leaks detected
}

// PerformanceMetrics provides detailed performance analysis data.
type PerformanceMetrics struct {
	// Throughput metrics
	EntriesPerSecond    float64 `json:"entries_per_second"`    // Log entries processed per second
	BytesPerSecond      float64 `json:"bytes_per_second"`      // Data throughput in bytes per second
	PeakThroughput      float64 `json:"peak_throughput"`       // Maximum observed throughput

	// Latency metrics
	P50Latency          time.Duration `json:"p50_latency"`          // 50th percentile latency
	P95Latency          time.Duration `json:"p95_latency"`          // 95th percentile latency
	P99Latency          time.Duration `json:"p99_latency"`          // 99th percentile latency
	AverageLatency      time.Duration `json:"average_latency"`      // Mean processing latency

	// Queue metrics
	QueueUtilization    float64 `json:"queue_utilization"`    // Queue usage percentage
	MaxQueueDepth       int     `json:"max_queue_depth"`      // Maximum observed queue depth
	QueueWaitTime       time.Duration `json:"queue_wait_time"` // Average time entries spend in queue

	// Error metrics
	ErrorRate           float64 `json:"error_rate"`           // Errors per second
	RetryRate           float64 `json:"retry_rate"`           // Retries per second
	SuccessRate         float64 `json:"success_rate"`         // Success percentage

	// Resource efficiency
	CPUEfficiency       float64 `json:"cpu_efficiency"`       // CPU utilization efficiency
	MemoryEfficiency    float64 `json:"memory_efficiency"`    // Memory usage efficiency
	NetworkUtilization  float64 `json:"network_utilization"`  // Network bandwidth usage
}