// Package types defines core data structures and interfaces used throughout the log capture system.
//
// This package provides:
//   - LogEntry: The primary data structure representing a log entry with metadata
//   - Interface definitions for pluggable components (Sink, Dispatcher, etc.)
//   - Configuration structures for all system components
//   - Statistics and monitoring data structures
//   - Enterprise feature data types for security, tracing, and SLO monitoring
//
// The types in this package are designed to support:
//   - High-performance log processing with minimal allocations
//   - Distributed tracing and correlation across services
//   - Enterprise security and compliance requirements
//   - Comprehensive observability and monitoring
//   - Pluggable architecture for extensibility
//
// Key Concepts:
//   - LogEntry: Enriched log data with tracing, metadata, and compliance information
//   - Sink: Output destination interface for delivering processed logs
//   - Dispatcher: Core orchestration interface for log processing pipeline
//   - TaskManager: Background task coordination and lifecycle management
//   - Configuration: Hierarchical configuration structures for all components
package types

import (
	"sync"
	"time"
)

// LogEntry represents a comprehensive log entry with full metadata, tracing, and compliance information.
//
// LogEntry is the central data structure that flows through the entire log processing
// pipeline. It supports:
//
// Distributed Tracing:
//   - OpenTelemetry-compatible trace and span IDs for request correlation
//   - Parent-child span relationships for distributed system visibility
//   - Integration with tracing backends (Jaeger, Zipkin, etc.)
//
// Performance Monitoring:
//   - High-resolution timestamps for accurate timing analysis
//   - Processing duration tracking for performance optimization
//   - Pipeline stage timing for bottleneck identification
//
// Content and Context:
//   - Raw log message with structured field extraction
//   - Standardized log levels following industry conventions
//   - Rich metadata for filtering, routing, and analysis
//
// Source Identification:
//   - Source type classification (file, container, API, internal)
//   - Unique source identifiers for origin tracking
//   - Container and file metadata integration
//
// Processing Pipeline:
//   - Transformation step tracking for audit and debugging
//   - Pipeline identification for routing and processing logic
//   - Configurable processing stage metadata
//
// Enterprise Features:
//   - Data classification for compliance and security
//   - Retention policy enforcement and lifecycle management
//   - Field sanitization tracking for privacy compliance
//   - SLO and metrics integration for observability
//
// The LogEntry structure is optimized for:
//   - High-throughput processing with minimal memory allocations
//   - JSON serialization for API and storage compatibility
//   - Thread-safe access patterns across concurrent workers
//   - Efficient copying and transformation operations
type LogEntry struct {
	// Distributed tracing - OpenTelemetry compatible identifiers
	TraceID      string `json:"trace_id"`      // Unique trace identifier for request correlation across services
	SpanID       string `json:"span_id"`       // Unique span identifier for this log entry's operation
	ParentSpanID string `json:"parent_span_id,omitempty"` // Parent span ID for hierarchical tracing

	// Timing and performance metrics
	Timestamp   time.Time     `json:"timestamp"`    // Original log entry timestamp from source
	Duration    time.Duration `json:"duration,omitempty"` // Operation duration if available
	ProcessedAt time.Time     `json:"processed_at"` // When this entry was processed by the system

	// Content and context information
	Message string `json:"message"` // Raw log message content
	Level   string `json:"level"`   // Standardized log level (trace, debug, info, warn, error, fatal, panic)

	// Source identification and origin tracking
	SourceType string `json:"source_type"` // Source type: "file", "docker", "api", "internal"
	SourceID   string `json:"source_id"`   // Unique source identifier (file hash, container ID, etc.)

	// Categorization and routing metadata
	Tags   []string              `json:"tags,omitempty"` // Classification tags for filtering and routing
	Labels map[string]string     `json:"labels"`        // Key-value labels for Prometheus-style querying
	Fields map[string]interface{} `json:"fields"`        // Additional structured fields extracted from log content

	// Processing pipeline metadata
	ProcessingSteps []ProcessingStep `json:"processing_steps,omitempty"` // Detailed processing step history for audit
	Pipeline        string           `json:"pipeline,omitempty"`        // Processing pipeline identifier

	// Enterprise compliance and data governance
	DataClassification string   `json:"data_classification,omitempty"` // Data sensitivity: "public", "internal", "confidential", "restricted"
	RetentionPolicy    string   `json:"retention_policy,omitempty"`    // Data retention policy identifier
	SanitizedFields    []string `json:"sanitized_fields,omitempty"`    // Fields that have been sanitized for privacy

	// Enterprise observability and SLO tracking
	Metrics map[string]float64 `json:"metrics,omitempty"` // Custom metrics associated with this log entry
	SLOs    map[string]float64 `json:"slos,omitempty"`    // SLO measurements and error budget tracking

	// Thread-safety for concurrent access to maps
	// This mutex protects: Labels, Fields, Metrics, SLOs
	mu sync.RWMutex `json:"-"`
}

// DeepCopy creates a deep copy of the LogEntry for safe concurrent access.
//
// This method performs a complete deep copy of all fields including:
//   - All primitive fields (strings, timestamps, etc.)
//   - Map fields with full key-value duplication
//   - Slice fields with element-by-element copying
//   - Nested structures like ProcessingStep
//
// The deep copy ensures:
//   - Thread-safe modification of the copied entry
//   - No shared references between original and copy
//   - Preservation of all metadata and tracing information
//   - Safe concurrent processing across multiple workers
//
// This method is used extensively in:
//   - Parallel processing pipelines
//   - Retry mechanisms that need to preserve original data
//   - Transformation stages that modify entries
//   - Fan-out delivery to multiple sinks
//
// Returns:
//   - *LogEntry: A complete deep copy of the original log entry
func (e *LogEntry) DeepCopy() *LogEntry {
	// Protect read access to maps during copy
	e.mu.RLock()
	defer e.mu.RUnlock()

	newEntry := *e
	// Reset mutex for the new entry (don't copy lock state)
	newEntry.mu = sync.RWMutex{}

	// Deep copy slices
	if e.Tags != nil {
		newEntry.Tags = make([]string, len(e.Tags))
		copy(newEntry.Tags, e.Tags)
	}

	if e.SanitizedFields != nil {
		newEntry.SanitizedFields = make([]string, len(e.SanitizedFields))
		copy(newEntry.SanitizedFields, e.SanitizedFields)
	}

	if e.ProcessingSteps != nil {
		newEntry.ProcessingSteps = make([]ProcessingStep, len(e.ProcessingSteps))
		copy(newEntry.ProcessingSteps, e.ProcessingSteps)
	}

	// Deep copy maps (protected by RLock above)
	if e.Labels != nil {
		newEntry.Labels = make(map[string]string, len(e.Labels))
		for k, v := range e.Labels {
			newEntry.Labels[k] = v
		}
	}

	if e.Fields != nil {
		newEntry.Fields = make(map[string]interface{}, len(e.Fields))
		for k, v := range e.Fields {
			newEntry.Fields[k] = v
		}
	}

	if e.Metrics != nil {
		newEntry.Metrics = make(map[string]float64, len(e.Metrics))
		for k, v := range e.Metrics {
			newEntry.Metrics[k] = v
		}
	}

	if e.SLOs != nil {
		newEntry.SLOs = make(map[string]float64, len(e.SLOs))
		for k, v := range e.SLOs {
			newEntry.SLOs[k] = v
		}
	}

	return &newEntry
}

// Thread-safe methods for Labels access
//
// These methods provide safe concurrent access to the Labels map.
// Direct access to entry.Labels should be avoided in concurrent contexts.

// GetLabel retrieves a label value safely.
//
// Parameters:
//   - key: The label key to retrieve
//
// Returns:
//   - value: The label value if found
//   - ok: true if the key exists, false otherwise
//
// Thread-safety: Safe for concurrent reads
func (e *LogEntry) GetLabel(key string) (string, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	val, ok := e.Labels[key]
	return val, ok
}

// SetLabel sets a label value safely.
//
// Parameters:
//   - key: The label key to set
//   - value: The label value
//
// Thread-safety: Safe for concurrent writes
func (e *LogEntry) SetLabel(key, value string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.Labels == nil {
		e.Labels = make(map[string]string)
	}
	e.Labels[key] = value
}

// CopyLabels returns a thread-safe copy of all labels.
//
// This method creates a new map with all label key-value pairs,
// protecting against concurrent modification during iteration.
//
// Returns:
//   - A new map containing all labels
//
// Thread-safety: Safe for concurrent access
//
// Usage:
//   labelsCopy := entry.CopyLabels()
//   for k, v := range labelsCopy {
//       // Safe to iterate over copy
//   }
func (e *LogEntry) CopyLabels() map[string]string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.Labels == nil {
		return make(map[string]string)
	}

	copy := make(map[string]string, len(e.Labels))
	for k, v := range e.Labels {
		copy[k] = v
	}
	return copy
}

// Thread-safe methods for Fields access

// GetField retrieves a field value safely.
func (e *LogEntry) GetField(key string) (interface{}, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	val, ok := e.Fields[key]
	return val, ok
}

// SetField sets a field value safely.
func (e *LogEntry) SetField(key string, value interface{}) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.Fields == nil {
		e.Fields = make(map[string]interface{})
	}
	e.Fields[key] = value
}

// CopyFields returns a thread-safe copy of all fields.
func (e *LogEntry) CopyFields() map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.Fields == nil {
		return make(map[string]interface{})
	}

	copy := make(map[string]interface{}, len(e.Fields))
	for k, v := range e.Fields {
		copy[k] = v
	}
	return copy
}

// Thread-safe methods for Metrics access

// GetMetric retrieves a metric value safely.
func (e *LogEntry) GetMetric(key string) (float64, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	val, ok := e.Metrics[key]
	return val, ok
}

// SetMetric sets a metric value safely.
func (e *LogEntry) SetMetric(key string, value float64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.Metrics == nil {
		e.Metrics = make(map[string]float64)
	}
	e.Metrics[key] = value
}

// ProcessingStep represents a single step in the log processing pipeline.
//
// ProcessingSteps provide a detailed audit trail of all transformations
// applied to a log entry, enabling:
//   - Debugging of processing pipeline issues
//   - Performance analysis of individual processing stages
//   - Compliance auditing for data transformation
//   - Pipeline optimization based on step timing
type ProcessingStep struct {
	Name        string                 `json:"name"`                  // Processing step name
	Timestamp   time.Time              `json:"timestamp"`             // When this step was executed
	Duration    time.Duration          `json:"duration"`              // How long this step took
	Success     bool                   `json:"success"`               // Whether the step completed successfully
	Error       string                 `json:"error,omitempty"`       // Error message if step failed
	Input       map[string]interface{} `json:"input,omitempty"`       // Input parameters for this step
	Output      map[string]interface{} `json:"output,omitempty"`      // Output/changes from this step
	Metadata    map[string]interface{} `json:"metadata,omitempty"`    // Additional step-specific metadata
}