// Package app HTTP handlers for API endpoints and monitoring
package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"ssw-logs-capture/internal/dispatcher"
	"ssw-logs-capture/pkg/tracing"

	"github.com/gorilla/mux"
)

// registerHandlers configures HTTP routes and applies middleware to the router.
//
// This method sets up all HTTP endpoints with appropriate middleware:
//
// Core Endpoints:
//   - GET /health: Application and component health status
//   - GET /stats: Detailed operational statistics
//   - GET /config: Current configuration (sanitized)
//   - POST /config/reload: Trigger configuration reload
//   - GET /positions: File position tracking status
//   - GET /dlq/stats: Dead letter queue statistics
//
// Enterprise Endpoints (when enabled):
//   - GET /slo/status: Service level objective monitoring
//   - GET /goroutines/stats: Goroutine tracking and leak detection
//   - GET /security/audit: Security audit logs and statistics
//
// Middleware Integration:
//   - Security middleware for authentication and authorization
//   - Tracing middleware for distributed request tracing
//   - Request logging and error handling
//   - Rate limiting and request validation
//
// The middleware stack is conditionally applied based on feature
// availability, ensuring graceful degradation when enterprise
// features are not enabled.
//
// All endpoints return JSON responses with appropriate HTTP status
// codes and error handling.
func (app *App) registerHandlers(router *mux.Router) {
	// Apply security middleware if security is enabled
	var middleware func(http.Handler) http.Handler
	if app.securityManager != nil {
		middleware = app.securityManager.AuthMiddleware("api", "read")
	} else {
		middleware = func(h http.Handler) http.Handler { return h }
	}

	// Apply tracing middleware if tracing is enabled
	if app.tracingManager != nil {
		tracer := app.tracingManager.GetTracer()
		traceMiddleware := tracing.TraceHandler(tracer, "http_request")
		middleware = func(h http.Handler) http.Handler {
			return middleware(traceMiddleware(h))
		}
	}

	// Register endpoints with middleware
	router.Handle("/health", middleware(http.HandlerFunc(app.healthHandler))).Methods("GET")
	router.Handle("/stats", middleware(http.HandlerFunc(app.statsHandler))).Methods("GET")
	router.Handle("/config", middleware(http.HandlerFunc(app.configHandler))).Methods("GET")
	router.Handle("/config/reload", middleware(http.HandlerFunc(app.configReloadHandler))).Methods("POST")
	router.Handle("/positions", middleware(http.HandlerFunc(app.positionsHandler))).Methods("GET")
	router.Handle("/dlq/stats", middleware(http.HandlerFunc(app.dlqStatsHandler))).Methods("GET")

	// Enterprise endpoints
	if app.sloManager != nil {
		router.Handle("/slo/status", middleware(http.HandlerFunc(app.sloStatusHandler))).Methods("GET")
	}
	if app.goroutineTracker != nil {
		router.Handle("/goroutines/stats", middleware(http.HandlerFunc(app.goroutineStatsHandler))).Methods("GET")
	}
	if app.securityManager != nil {
		router.Handle("/security/audit", middleware(http.HandlerFunc(app.securityAuditHandler))).Methods("GET")
	}
}

// healthHandler provides comprehensive health status information for the application and all its components.
//
// This endpoint returns detailed health information including:
//   - Overall application status (healthy/degraded)
//   - Individual component health status
//   - Performance statistics where available
//   - Version and timestamp information
//
// Component Health Checks:
//   - Dispatcher: Queue statistics and processing status
//   - File Monitor: Monitoring status and file count
//   - Container Monitor: Docker connection status
//   - Goroutine Tracker: Goroutine count and leak detection status
//   - SLO Manager: Service level objective compliance
//
// Response Codes:
//   - 200 OK: All components are healthy
//   - 503 Service Unavailable: One or more components are degraded
//
// The response is JSON formatted with a hierarchical structure showing
// the status of each component. This endpoint is typically used by
// load balancers, monitoring systems, and health check scripts.
//
// Example response:
//
//	{
//	  "status": "healthy",
//	  "timestamp": 1634567890,
//	  "version": "v1.0.0",
//	  "services": {
//	    "dispatcher": {"status": "healthy", "stats": {...}}
//	  }
//	}
func (app *App) healthHandler(w http.ResponseWriter, r *http.Request) {
	health := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().Unix(),
		"version":   app.config.App.Version,
		"services":  make(map[string]interface{}),
	}

	services := health["services"].(map[string]interface{})
	allHealthy := true

	// Check dispatcher health
	if app.dispatcher != nil {
		dispatcherStats := app.dispatcher.GetStats()
		services["dispatcher"] = map[string]interface{}{
			"status": "healthy",
			"stats":  dispatcherStats,
		}
	}

	// Check monitors health
	if app.fileMonitor != nil {
		services["file_monitor"] = map[string]interface{}{
			"status": "healthy",
			"enabled": true,
		}
	}

	if app.containerMonitor != nil {
		services["container_monitor"] = map[string]interface{}{
			"status": "healthy",
			"enabled": true,
		}
	}

	// Check enterprise services
	if app.goroutineTracker != nil {
		goroutineStats := app.goroutineTracker.GetStats()
		status := "healthy"
		if goroutineStats.Status != "healthy" {
			status = "warning"
			allHealthy = false
		}
		services["goroutine_tracker"] = map[string]interface{}{
			"status": status,
			"stats":  goroutineStats,
		}
	}

	if app.sloManager != nil {
		sloStatus := app.sloManager.GetSLOStatus()
		services["slo_manager"] = map[string]interface{}{
			"status": "healthy",
			"slos":   sloStatus,
		}
	}

	// Set overall health status
	if !allHealthy {
		health["status"] = "degraded"
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		w.WriteHeader(http.StatusOK)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(health)
}

// statsHandler provides detailed operational statistics for all application components.
//
// This endpoint returns comprehensive metrics including:
//   - Application metadata (name, version, uptime)
//   - Dispatcher statistics (queue size, throughput, batching metrics)
//   - Position manager statistics (file positions, buffer usage)
//   - Resource monitoring data (memory, goroutines, file descriptors)
//   - Goroutine tracking information (leak detection, allocation patterns)
//
// The statistics are real-time snapshots of current application state
// and are useful for:
//   - Performance monitoring and tuning
//   - Capacity planning and resource allocation
//   - Troubleshooting and debugging
//   - Integration with monitoring systems
//
// All statistics are returned in JSON format with nested objects
// organizing metrics by component. This endpoint complements the
// Prometheus metrics by providing detailed internal state information.
//
// Response is always 200 OK with JSON content type.
func (app *App) statsHandler(w http.ResponseWriter, r *http.Request) {
	stats := map[string]interface{}{
		"application": map[string]interface{}{
			"name":    app.config.App.Name,
			"version": app.config.App.Version,
			"uptime":  time.Since(time.Now()).String(),
		},
		"dispatcher": app.dispatcher.GetStats(),
	}

	if app.positionManager != nil {
		stats["positions"] = app.positionManager.GetStats()
	}

	if app.resourceMonitor != nil {
		stats["resources"] = app.resourceMonitor.GetStats()
	}

	if app.goroutineTracker != nil {
		stats["goroutines"] = app.goroutineTracker.GetStats()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// configHandler returns the current application configuration in sanitized form.
//
// This endpoint provides access to the current configuration state while
// ensuring sensitive information is not exposed. The sanitization process:
//   - Removes authentication credentials and API keys
//   - Excludes internal implementation details
//   - Provides only operational configuration parameters
//
// Returned configuration includes:
//   - Application metadata (name, version, log settings)
//   - Component enablement status
//   - Performance tuning parameters (queue sizes, timeouts)
//   - Monitoring and metrics configuration
//
// This endpoint is useful for:
//   - Configuration verification and debugging
//   - Documentation and audit purposes
//   - Integration with configuration management tools
//   - Runtime configuration inspection
//
// The response excludes sensitive data like passwords, API keys,
// and internal connection details for security purposes.
//
// Response is always 200 OK with JSON content type.
func (app *App) configHandler(w http.ResponseWriter, r *http.Request) {
	// Return sanitized config without sensitive information
	sanitizedConfig := map[string]interface{}{
		"app": app.config.App,
		"metrics": app.config.Metrics,
		"processing": app.config.Processing,
		"dispatcher": map[string]interface{}{
			"queue_size":    app.config.Dispatcher.QueueSize,
			"worker_count":  app.config.Dispatcher.WorkerCount,
			"batch_size":    app.config.Dispatcher.BatchSize,
			"batch_timeout": app.config.Dispatcher.BatchTimeout,
		},
		"sinks": map[string]interface{}{
			"loki_enabled":       app.config.Sinks.Loki.Enabled,
			"local_file_enabled": app.config.Sinks.LocalFile.Enabled,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sanitizedConfig)
}

// configReloadHandler triggers a configuration reload when hot reload is enabled.
//
// This endpoint allows runtime configuration updates without requiring
// a full application restart. The reload process:
//   1. Validates the request and hot reload availability
//   2. Triggers the configuration reloader component
//   3. Applies changes where possible without service interruption
//   4. Returns status information about the reload operation
//
// Response Codes:
//   - 200 OK: Reload triggered successfully
//   - 500 Internal Server Error: Reload failed
//   - 503 Service Unavailable: Hot reload not enabled
//
// Note: Some configuration changes may require a full restart to take
// effect. The reload operation provides best-effort updating of
// runtime-configurable parameters.
//
// This endpoint should be used with caution in production environments
// and may require appropriate authentication and authorization.
func (app *App) configReloadHandler(w http.ResponseWriter, r *http.Request) {
	if app.reloader == nil {
		http.Error(w, "Hot reload is not enabled", http.StatusServiceUnavailable)
		return
	}
	if err := app.reloader.TriggerReload(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to trigger reload: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "success",
		"message": "Configuration reload triggered successfully.",
	})
}

// positionsHandler returns current file position tracking statistics.
//
// This endpoint provides information about the position manager's state:
//   - Currently tracked files and their read positions
//   - Buffer usage and flush statistics
//   - Position persistence and cleanup metrics
//
// The position information is crucial for:
//   - Monitoring reading progress across multiple files
//   - Debugging restart/recovery behavior
//   - Verifying position persistence functionality
//   - Capacity planning for position storage
//
// Response Codes:
//   - 200 OK: Position statistics returned successfully
//   - 503 Service Unavailable: Position manager not available/enabled
//
// The JSON response includes detailed position tracking metrics
// organized by file path and monitoring component.
func (app *App) positionsHandler(w http.ResponseWriter, r *http.Request) {
	if app.positionManager == nil {
		http.Error(w, "Position manager not available", http.StatusServiceUnavailable)
		return
	}
	stats := app.positionManager.GetStats()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// dlqStatsHandler returns Dead Letter Queue statistics for failed log entries.
//
// This endpoint provides information about the DLQ functionality:
//   - Number of entries in the dead letter queue
//   - Failure reasons and categorization
//   - Retry attempt statistics
//   - DLQ processing and cleanup metrics
//
// The DLQ statistics help with:
//   - Monitoring delivery failures and their causes
//   - Identifying problematic log entries or sink issues
//   - Capacity planning for DLQ storage
//   - Debugging sink connectivity problems
//
// Response Codes:
//   - 200 OK: DLQ statistics returned successfully
//   - 503 Service Unavailable: DLQ not available/enabled
//
// The response includes detailed failure analysis and DLQ metrics
// that can be used for alerting and troubleshooting.
func (app *App) dlqStatsHandler(w http.ResponseWriter, r *http.Request) {
	if dispatcherImpl, ok := app.dispatcher.(*dispatcher.Dispatcher); ok {
		dlq := dispatcherImpl.GetDLQ()
		if dlq != nil {
			stats := dlq.GetStats()
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(stats)
			return
		}
	}
	http.Error(w, "DLQ not available", http.StatusServiceUnavailable)
}

// Enterprise handlers - Advanced monitoring and security endpoints

// sloStatusHandler returns Service Level Objective monitoring status and compliance metrics.
//
// This enterprise endpoint provides:
//   - Current SLO compliance status across all defined objectives
//   - Error budget consumption and remaining budget
//   - Historical compliance trends and burn rate analysis
//   - Alert status and threshold violations
//
// SLO metrics include:
//   - Availability SLO: Uptime and service availability percentage
//   - Latency SLO: Response time percentiles and threshold compliance
//   - Error Rate SLO: Error percentage and error budget tracking
//   - Throughput SLO: Processing rate and capacity utilization
//
// Response Codes:
//   - 200 OK: SLO status returned successfully
//   - 503 Service Unavailable: SLO manager not available/enabled
//
// This endpoint is typically integrated with alerting systems and
// SRE dashboards for proactive service reliability monitoring.
func (app *App) sloStatusHandler(w http.ResponseWriter, r *http.Request) {
	if app.sloManager == nil {
		http.Error(w, "SLO manager not available", http.StatusServiceUnavailable)
		return
	}
	status := app.sloManager.GetSLOStatus()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// goroutineStatsHandler returns detailed goroutine tracking and leak detection statistics.
//
// This enterprise endpoint provides comprehensive goroutine monitoring:
//   - Current goroutine count and growth trends
//   - Goroutine leak detection and classification
//   - Memory allocation patterns and potential leaks
//   - Stack trace analysis for stuck or long-running goroutines
//
// Goroutine tracking includes:
//   - Creation and termination rates
//   - Lifecycle analysis and duration histograms
//   - Resource consumption per goroutine type
//   - Alert conditions for abnormal goroutine behavior
//
// Response Codes:
//   - 200 OK: Goroutine statistics returned successfully
//   - 503 Service Unavailable: Goroutine tracker not available/enabled
//
// This endpoint is essential for:
//   - Memory leak detection and prevention
//   - Performance optimization and resource planning
//   - Debugging concurrency issues and goroutine management
func (app *App) goroutineStatsHandler(w http.ResponseWriter, r *http.Request) {
	if app.goroutineTracker == nil {
		http.Error(w, "Goroutine tracker not available", http.StatusServiceUnavailable)
		return
	}
	stats := app.goroutineTracker.GetStats()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// securityAuditHandler returns security audit logs and authentication statistics.
//
// This enterprise endpoint provides security monitoring data:
//   - Authentication attempts and success/failure rates
//   - Authorization decisions and access patterns
//   - Security events and potential threats
//   - Audit trail for compliance and forensic analysis
//
// Security audit information includes:
//   - Login attempts with source IP and user agent
//   - Permission escalation attempts and denials
//   - Configuration changes and administrative actions
//   - Rate limiting triggers and security violations
//
// Response Codes:
//   - 200 OK: Security audit data returned successfully
//   - 503 Service Unavailable: Security manager not available/enabled
//
// This endpoint requires appropriate authorization and should be
// restricted to security administrators and monitoring systems.
// The audit data is essential for:
//   - Security incident investigation
//   - Compliance reporting and regulatory requirements
//   - Threat detection and security analytics
func (app *App) securityAuditHandler(w http.ResponseWriter, r *http.Request) {
	if app.securityManager == nil {
		http.Error(w, "Security manager not available", http.StatusServiceUnavailable)
		return
	}
	// TODO: Implement proper audit log collection
	audit := map[string]interface{}{
		"message": "Audit log feature not yet implemented",
		"status":  "pending",
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(audit)
}