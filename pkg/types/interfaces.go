// Package types - Interface definitions for pluggable components
package types

import (
	"context"
)

// Monitor defines the interface for log input sources that monitor and capture log entries.
//
// Monitors are responsible for detecting new log entries from various sources and feeding
// them into the processing pipeline. Implementations include file monitors, container
// monitors, and custom integrations.
type Monitor interface {
	// Start begins monitoring operations and should block until context is cancelled
	Start(ctx context.Context) error
	// Stop gracefully shuts down the monitor and releases resources
	Stop() error
}

// Sink defines the interface for log output destinations.
//
// Sinks receive processed log entries and deliver them to their configured
// destinations. Implementations include Loki sinks, local file sinks, and
// custom integrations.
type Sink interface {
	// Start initializes the sink and prepares it for receiving log entries
	Start(ctx context.Context) error
	// Send delivers a batch of log entries to the sink destination
	Send(ctx context.Context, entries []LogEntry) error
	// Stop gracefully shuts down the sink and flushes any buffered data
	Stop() error
	// IsHealthy checks if the sink is operational
	IsHealthy() bool
}

// Dispatcher defines the interface for the core log processing orchestrator.
//
// The dispatcher coordinates the flow of log entries from input sources to
// output destinations, managing batching, retries, and error handling.
type Dispatcher interface {
	// AddSink registers a new output destination
	AddSink(sink Sink)
	// Handle processes a single log entry through the pipeline
	Handle(ctx context.Context, sourceType, sourceID, message string, labels map[string]string) error
	// Start begins dispatcher operations
	Start(ctx context.Context) error
	// Stop gracefully shuts down the dispatcher
	Stop() error
	// GetStats returns current dispatcher statistics
	GetStats() DispatcherStats
}

// Processor defines the interface for log entry transformation and filtering.
//
// Processors apply configured pipelines to modify, enrich, or filter log entries
// before they are sent to output destinations.
type Processor interface {
	// Process transforms a log entry according to configured rules
	Process(entry *LogEntry) (*LogEntry, error)
}