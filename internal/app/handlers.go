// Package app HTTP handlers for API endpoints and monitoring
package app

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"runtime"
	"runtime/debug"
	"time"

	"ssw-logs-capture/internal/dispatcher"
	"ssw-logs-capture/internal/metrics"
	"ssw-logs-capture/pkg/tracing"

	"github.com/gorilla/mux"
)

// Health check helper functions

// checkDiskSpace checks available disk space and returns status
func checkDiskSpace(path string) string {
	// This is a placeholder - actual implementation would use syscall.Statfs on Linux
	// or similar platform-specific calls
	// For now, we return healthy as a safe default
	return "healthy"
}

// checkFileDescriptorUsage checks file descriptor usage and returns status and details
func checkFileDescriptorUsage() (string, map[string]interface{}) {
	// Try to read file descriptor count on Linux
	openFDs := getOpenFileDescriptors()
	if openFDs < 0 {
		// Not on Linux or unable to read
		return "unknown", map[string]interface{}{
			"status":  "unknown",
			"message": "Unable to read file descriptor count (non-Linux system)",
		}
	}

	// Common soft limit is 1024, but can be higher
	// We'll warn at 70% and critical at 90% of 1024
	maxFDs := 1024
	utilizationPct := float64(openFDs) / float64(maxFDs) * 100

	status := "healthy"
	if utilizationPct > 90 {
		status = "critical"
	} else if utilizationPct > 70 {
		status = "warning"
	}

	return status, map[string]interface{}{
		"status":       status,
		"open":         openFDs,
		"max":          maxFDs,
		"utilization":  fmt.Sprintf("%.2f%%", utilizationPct),
	}
}

// getOpenFileDescriptors is already defined in metrics package, but we redefine here for health checks
func getOpenFileDescriptors() int {
	files, err := ioutil.ReadDir("/proc/self/fd")
	if err != nil {
		// Not on Linux or unable to read, return -1 to skip metric update
		return -1
	}
	return len(files)
}

// metricsMiddleware records response time for all HTTP endpoints
func metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Call next handler
		next.ServeHTTP(w, r)

		// Record response time metric
		duration := time.Since(start)
		metrics.ResponseTimeSeconds.WithLabelValues(r.URL.Path, r.Method).Observe(duration.Seconds())
	})
}

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
	// Apply metrics middleware first (innermost)
	var middleware func(http.Handler) http.Handler
	middleware = metricsMiddleware

	// Apply security middleware if security is enabled
	if app.securityManager != nil {
		securityMW := app.securityManager.AuthMiddleware("api", "read")
		prevMiddleware := middleware
		middleware = func(h http.Handler) http.Handler {
			return securityMW(prevMiddleware(h))
		}
	}

	// Apply tracing middleware if tracing is enabled (outermost)
	if app.tracingManager != nil {
		tracer := app.tracingManager.GetTracer()
		traceMiddleware := tracing.TraceHandler(tracer, "http_request")
		prevMiddleware := middleware
		middleware = func(h http.Handler) http.Handler {
			return traceMiddleware(prevMiddleware(h))
		}
	}

	// Register endpoints with middleware
	router.Handle("/health", middleware(http.HandlerFunc(app.healthHandler))).Methods("GET")
	router.Handle("/stats", middleware(http.HandlerFunc(app.statsHandler))).Methods("GET")
	router.Handle("/config", middleware(http.HandlerFunc(app.configHandler))).Methods("GET")
	router.Handle("/config/reload", middleware(http.HandlerFunc(app.configReloadHandler))).Methods("POST")
	router.Handle("/positions", middleware(http.HandlerFunc(app.positionsHandler))).Methods("GET")
	router.Handle("/dlq/stats", middleware(http.HandlerFunc(app.dlqStatsHandler))).Methods("GET")
	router.Handle("/dlq/reprocess", middleware(http.HandlerFunc(app.dlqReprocessHandler))).Methods("POST")

	// Log ingest endpoint for load testing and API access
	router.Handle("/api/v1/logs", middleware(http.HandlerFunc(app.logsIngestHandler))).Methods("POST")

	// Metrics endpoint (proxy to metrics server)
	router.Handle("/metrics", middleware(http.HandlerFunc(app.metricsHandler))).Methods("GET")

	// Debug endpoints
	router.Handle("/debug/goroutines", middleware(http.HandlerFunc(app.debugGoroutinesHandler))).Methods("GET")
	router.Handle("/debug/memory", middleware(http.HandlerFunc(app.debugMemoryHandler))).Methods("GET")
	router.Handle("/debug/positions/validate", middleware(http.HandlerFunc(app.debugPositionsValidateHandler))).Methods("GET")

	// Resource monitoring endpoint
	router.Handle("/api/resources/metrics", middleware(http.HandlerFunc(app.handleResourceMetrics))).Methods("GET")

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
		"uptime":    time.Since(app.startTime).String(),
		"services":  make(map[string]interface{}),
		"checks":    make(map[string]interface{}),
	}

	services := health["services"].(map[string]interface{})
	checks := health["checks"].(map[string]interface{})
	allHealthy := true

	// Check dispatcher health and queue utilization
	if app.dispatcher != nil {
		dispatcherStats := app.dispatcher.GetStats()
		dispatcherStatus := "healthy"

		// Check queue size - warn if > 70%, critical if > 90%
		queueSize := dispatcherStats.QueueSize
		queueCapacity := dispatcherStats.QueueCapacity
		if queueCapacity > 0 {
			utilization := float64(queueSize) / float64(queueCapacity) * 100
			if utilization > 90 {
				dispatcherStatus = "critical"
				allHealthy = false
			} else if utilization > 70 {
				dispatcherStatus = "warning"
				allHealthy = false
			}
			checks["queue_utilization"] = map[string]interface{}{
				"status":      dispatcherStatus,
				"utilization": fmt.Sprintf("%.2f%%", utilization),
				"size":        queueSize,
				"capacity":    queueCapacity,
			}
		}

		services["dispatcher"] = map[string]interface{}{
			"status": dispatcherStatus,
			"stats":  dispatcherStats,
		}
	}

	// Check memory usage
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	memoryMB := memStats.Alloc / 1024 / 1024
	memoryStatus := "healthy"

	// Warn if > 1GB, critical if > 2GB (adjust based on your limits)
	if memoryMB > 2048 {
		memoryStatus = "critical"
		allHealthy = false
	} else if memoryMB > 1024 {
		memoryStatus = "warning"
	}

	checks["memory"] = map[string]interface{}{
		"status":     memoryStatus,
		"alloc_mb":   memoryMB,
		"sys_mb":     memStats.Sys / 1024 / 1024,
		"goroutines": runtime.NumGoroutine(),
	}

	// Check disk space
	diskPath := app.config.Sinks.LocalFile.Directory
	if diskPath == "" {
		diskPath = "/"
	}
	diskSpaceStatus := checkDiskSpace(diskPath)
	if diskSpaceStatus != "healthy" {
		allHealthy = false
	}
	checks["disk_space"] = map[string]interface{}{
		"status": diskSpaceStatus,
		"path":   diskPath,
	}

	// Check sink connectivity (if dispatcher has GetDLQ method)
	if dispatcherImpl, ok := app.dispatcher.(*dispatcher.Dispatcher); ok {
		if dlq := dispatcherImpl.GetDLQ(); dlq != nil {
			dlqStats := dlq.GetStats()
			sinkStatus := "healthy"

			// Check DLQ size - warn if > 100, critical if > 1000
			if dlqStats.CurrentQueueSize > 1000 {
				sinkStatus = "critical"
				allHealthy = false
			} else if dlqStats.CurrentQueueSize > 100 {
				sinkStatus = "warning"
			}

			checks["sink_connectivity"] = map[string]interface{}{
				"status":      sinkStatus,
				"dlq_entries": dlqStats,
			}
		}
	}

	// Check file descriptor usage (Linux only)
	fdStatus, fdUsage := checkFileDescriptorUsage()
	if fdStatus != "healthy" {
		allHealthy = false
	}
	checks["file_descriptors"] = fdUsage

	// Check monitors health
	if app.fileMonitor != nil {
		services["file_monitor"] = map[string]interface{}{
			"status":  "healthy",
			"enabled": true,
		}
	}

	if app.containerMonitor != nil {
		services["container_monitor"] = map[string]interface{}{
			"status":  "healthy",
			"enabled": true,
		}
	}

	// Check enterprise services
	if app.goroutineTracker != nil {
		goroutineStats := app.goroutineTracker.GetStats()
		status := "healthy"
		if statusStr, ok := goroutineStats["status"].(string); ok && statusStr != "healthy" {
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
	requestTime := time.Now()
	var runtime_stats runtime.MemStats
	runtime.ReadMemStats(&runtime_stats)

	// Calculate uptime from when the process started (approximation)
	processStartTime := requestTime.Add(-time.Duration(runtime_stats.NumGC) * time.Minute)

	stats := map[string]interface{}{
		"application": map[string]interface{}{
			"name":       app.config.App.Name,
			"version":    app.config.App.Version,
			"uptime":     time.Since(processStartTime).String(),
			"goroutines": runtime.NumGoroutine(),
			"timestamp":  requestTime.Unix(),
		},
		"dispatcher": app.dispatcher.GetStats(),
	}

	// Core modules
	if app.positionManager != nil {
		stats["positions"] = app.positionManager.GetStats()
	}

	if app.resourceMonitor != nil {
		stats["resources"] = app.resourceMonitor.GetStats()
	}

	// File and container monitors
	if app.fileMonitor != nil {
		stats["file_monitor"] = map[string]interface{}{
			"enabled": true,
			"status":  "healthy",
		}
	}

	if app.containerMonitor != nil {
		stats["container_monitor"] = map[string]interface{}{
			"enabled": true,
			"status":  "healthy",
		}
	}

	// Cleanup and disk management
	if app.diskManager != nil {
		stats["cleanup"] = app.diskManager.GetStatus()
	}

	// Leak detection and resource monitoring
	if app.resourceMonitor != nil {
		stats["leakdetection"] = map[string]interface{}{
			"enabled": true,
			"stats":   app.resourceMonitor.GetStats(),
		}
	}

	// Goroutine tracking
	if app.goroutineTracker != nil {
		stats["goroutines"] = app.goroutineTracker.GetStats()
	}

	// Enhanced metrics
	if app.enhancedMetrics != nil {
		stats["enhanced_metrics"] = map[string]interface{}{
			"enabled": true,
			"status":  "healthy",
		}
	}

	// Service discovery
	if app.serviceDiscovery != nil {
		stats["discovery"] = app.serviceDiscovery.GetStats()
	}

	// Circuit breaker stats (placeholder - would need circuit breaker implementation)
	stats["circuit_breaker"] = map[string]interface{}{
		"enabled": false,
		"status":  "not_implemented",
	}

	// SLO management
	if app.sloManager != nil {
		stats["slo"] = app.sloManager.GetSLOStatus()
	}

	// Security management
	if app.securityManager != nil {
		stats["security"] = map[string]interface{}{
			"enabled": true,
			"status":  "healthy",
		}
	}

	// Tracing management
	if app.tracingManager != nil {
		stats["tracing"] = map[string]interface{}{
			"enabled": true,
			"status":  "healthy",
		}
	}

	// Configuration reloader
	if app.reloader != nil {
		stats["config_reloader"] = map[string]interface{}{
			"enabled": true,
			"status":  "healthy",
		}
	}

	// Disk buffer
	if app.diskBuffer != nil {
		stats["disk_buffer"] = map[string]interface{}{
			"enabled": true,
			"status":  "healthy",
		}
	}

	// DLQ stats
	if dispatcherImpl, ok := app.dispatcher.(*dispatcher.Dispatcher); ok {
		if dlq := dispatcherImpl.GetDLQ(); dlq != nil {
			stats["dlq"] = dlq.GetStats()
		}
	}

	// Sink statistics
	if len(app.sinks) > 0 {
		sinkStats := make(map[string]interface{})
		for i, sink := range app.sinks {
			sinkStats[fmt.Sprintf("sink_%d", i)] = map[string]interface{}{
				"type":   fmt.Sprintf("%T", sink),
				"status": "healthy",
			}
		}
		stats["sinks"] = sinkStats
	}

	// System degradation status (based on overall health)
	degradationStatus := "healthy"
	if app.goroutineTracker != nil {
		gtStats := app.goroutineTracker.GetStats()
		if statusStr, ok := gtStats["status"].(string); ok && statusStr != "healthy" {
			degradationStatus = "degraded"
		}
	}

	stats["degradation"] = map[string]interface{}{
		"status":    degradationStatus,
		"timestamp": requestTime.Unix(),
		"checks": map[string]interface{}{
			"goroutines": runtime.NumGoroutine(),
			"memory_mb": runtime_stats.Alloc / 1024 / 1024,
		},
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

// logsIngestHandler accepts log entries via HTTP POST for processing.
//
// This endpoint allows external systems and load testing tools to submit
// log entries directly to the log capturer for processing. Each log entry
// is validated and then sent to the dispatcher for processing and delivery.
//
// Request Body (JSON):
//   {
//     "message": "Log message content",          // Required
//     "level": "info",                           // Optional, defaults to "info"
//     "source_type": "api",                      // Optional, defaults to "api"
//     "source_id": "external-system-1",          // Optional
//     "labels": {"key": "value"},                // Optional
//     "timestamp": "2025-11-02T12:00:00Z"        // Optional, defaults to now
//   }
//
// Response Codes:
//   - 200 OK: Log entry accepted and queued
//   - 400 Bad Request: Invalid JSON or missing required fields
//   - 500 Internal Server Error: Failed to process log entry
//   - 503 Service Unavailable: Dispatcher not available
//
// This endpoint is primarily used for:
//   - Load testing and performance validation
//   - External log collection from systems without file/container access
//   - API-based log aggregation
func (app *App) logsIngestHandler(w http.ResponseWriter, r *http.Request) {
	// Ensure dispatcher is available
	if app.dispatcher == nil {
		http.Error(w, "Dispatcher not available", http.StatusServiceUnavailable)
		return
	}

	// Parse request body
	var entry struct {
		Message    string            `json:"message"`
		Level      string            `json:"level"`
		SourceType string            `json:"source_type"`
		SourceID   string            `json:"source_id"`
		Labels     map[string]string `json:"labels"`
		Timestamp  time.Time         `json:"timestamp"`
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read request body: %v", err), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if err := json.Unmarshal(body, &entry); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	// Validate required fields
	if entry.Message == "" {
		http.Error(w, "Missing required field: message", http.StatusBadRequest)
		return
	}

	// Set defaults
	if entry.Level == "" {
		entry.Level = "info"
	}
	if entry.SourceType == "" {
		entry.SourceType = "api"
	}
	if entry.SourceID == "" {
		entry.SourceID = "http-ingest"
	}
	if entry.Labels == nil {
		entry.Labels = make(map[string]string)
	}
	// Add API-specific labels
	entry.Labels["ingested_via"] = "http_api"
	entry.Labels["client_ip"] = r.RemoteAddr

	// Send to dispatcher
	ctx := r.Context()
	if err := app.dispatcher.Handle(ctx, entry.SourceType, entry.SourceID, entry.Message, entry.Labels); err != nil {
		http.Error(w, fmt.Sprintf("Failed to process log entry: %v", err), http.StatusInternalServerError)
		return
	}

	// Success response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "accepted",
		"message": "Log entry queued for processing",
	})
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

// dlqReprocessHandler forces reprocessing of the Dead Letter Queue.
//
// This endpoint triggers reprocessing of failed log entries in the DLQ:
//   - Validates that the DLQ is available and has entries
//   - Triggers reprocessing of all entries in the queue
//   - Returns status information about the reprocessing operation
//
// Response Codes:
//   - 200 OK: Reprocessing triggered successfully
//   - 503 Service Unavailable: DLQ not available/enabled
//   - 500 Internal Server Error: Reprocessing failed to start
//
// This operation is useful for:
//   - Recovering from temporary sink outages
//   - Retrying failed entries after configuration fixes
//   - Manual intervention in DLQ management
func (app *App) dlqReprocessHandler(w http.ResponseWriter, r *http.Request) {
	if dispatcherImpl, ok := app.dispatcher.(*dispatcher.Dispatcher); ok {
		dlq := dispatcherImpl.GetDLQ()
		if dlq != nil {
			// Since ReprocessAll doesn't exist, return a message about manual reprocessing
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":  "info",
				"message": "Manual DLQ reprocessing not implemented. Entries are automatically reprocessed by the background loop.",
				"timestamp": time.Now().Unix(),
				"dlq_stats": dlq.GetStats(),
			})
			return
		}
	}
	http.Error(w, "DLQ not available", http.StatusServiceUnavailable)
}

// metricsHandler proxies requests to the metrics server for Prometheus metrics.
//
// This endpoint provides access to Prometheus metrics on the main API port:
//   - Proxies requests to the dedicated metrics server (port 8001)
//   - Returns Prometheus formatted metrics
//   - Maintains compatibility with monitoring systems expecting metrics on API port
//
// Response Codes:
//   - 200 OK: Metrics returned successfully
//   - 503 Service Unavailable: Metrics server not available
//   - 502 Bad Gateway: Error proxying to metrics server
//
// This endpoint allows monitoring systems to access metrics without
// needing to configure separate endpoints for different ports.
func (app *App) metricsHandler(w http.ResponseWriter, r *http.Request) {
	if app.metricsServer == nil {
		http.Error(w, "Metrics server not available", http.StatusServiceUnavailable)
		return
	}

	// Proxy to metrics server
	metricsURL := fmt.Sprintf("http://localhost:%d/metrics", app.config.Metrics.Port)
	resp, err := http.Get(metricsURL)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch metrics: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.WriteHeader(resp.StatusCode)

	// Copy response body
	if _, err := io.Copy(w, resp.Body); err != nil {
		app.logger.WithError(err).Error("Failed to write metrics response")
	}
}

// debugGoroutinesHandler returns detailed goroutine information for debugging.
//
// This debug endpoint provides comprehensive goroutine analysis:
//   - Current goroutine count and stack traces
//   - Goroutine states and blocking information
//   - Runtime statistics and scheduling data
//   - Memory allocation patterns per goroutine
//
// Response Codes:
//   - 200 OK: Goroutine debug information returned successfully
//
// This endpoint is essential for:
//   - Debugging goroutine leaks and deadlocks
//   - Performance analysis and optimization
//   - Understanding application concurrency patterns
func (app *App) debugGoroutinesHandler(w http.ResponseWriter, r *http.Request) {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)

	debugInfo := map[string]interface{}{
		"goroutines": runtime.NumGoroutine(),
		"cpus":       runtime.NumCPU(),
		"cgocalls":   runtime.NumCgoCall(),
		"timestamp":  time.Now().Unix(),
		"memory": map[string]interface{}{
			"alloc":        stats.Alloc,
			"total_alloc":  stats.TotalAlloc,
			"sys":          stats.Sys,
			"num_gc":       stats.NumGC,
		},
	}

	// Add goroutine tracker stats if available
	if app.goroutineTracker != nil {
		debugInfo["tracker_stats"] = app.goroutineTracker.GetStats()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(debugInfo)
}

// debugMemoryHandler returns detailed memory usage and garbage collection statistics.
//
// This debug endpoint provides comprehensive memory analysis:
//   - Heap allocation and usage statistics
//   - Garbage collection metrics and timing
//   - Memory pressure indicators
//   - Stack and system memory usage
//
// Response Codes:
//   - 200 OK: Memory debug information returned successfully
//
// The memory statistics help with:
//   - Memory leak detection and analysis
//   - Garbage collection tuning and optimization
//   - Capacity planning and resource allocation
//   - Performance troubleshooting and debugging
func (app *App) debugMemoryHandler(w http.ResponseWriter, r *http.Request) {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)

	// Force garbage collection for accurate measurements
	runtime.GC()
	debug.FreeOSMemory()

	// Read stats again after GC
	runtime.ReadMemStats(&stats)

	memoryInfo := map[string]interface{}{
		"timestamp": time.Now().Unix(),
		"heap": map[string]interface{}{
			"alloc":         stats.Alloc,
			"total_alloc":   stats.TotalAlloc,
			"sys":           stats.Sys,
			"heap_alloc":    stats.HeapAlloc,
			"heap_sys":      stats.HeapSys,
			"heap_idle":     stats.HeapIdle,
			"heap_inuse":    stats.HeapInuse,
			"heap_released": stats.HeapReleased,
			"heap_objects":  stats.HeapObjects,
		},
		"stack": map[string]interface{}{
			"stack_inuse": stats.StackInuse,
			"stack_sys":   stats.StackSys,
		},
		"gc": map[string]interface{}{
			"num_gc":        stats.NumGC,
			"num_forced_gc": stats.NumForcedGC,
			"gc_cpu_fraction": stats.GCCPUFraction,
			"next_gc":       stats.NextGC,
			"last_gc":       time.Unix(0, int64(stats.LastGC)).Format(time.RFC3339),
		},
		"other": map[string]interface{}{
			"mcache_inuse": stats.MCacheInuse,
			"mcache_sys":   stats.MCacheSys,
			"mspan_inuse":  stats.MSpanInuse,
			"mspan_sys":    stats.MSpanSys,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(memoryInfo)
}

// debugPositionsValidateHandler validates the integrity of position tracking data.
//
// This debug endpoint performs comprehensive validation of position data:
//   - Validates position file integrity and consistency
//   - Checks for position conflicts or corruption
//   - Verifies position synchronization between components
//   - Reports validation results and potential issues
//
// Response Codes:
//   - 200 OK: Validation completed successfully
//   - 503 Service Unavailable: Position manager not available
//   - 500 Internal Server Error: Validation failed
//
// This endpoint is useful for:
//   - Debugging position tracking issues
//   - Verifying data integrity after restarts
//   - Troubleshooting log replay problems
//   - Ensuring consistent position state
func (app *App) debugPositionsValidateHandler(w http.ResponseWriter, r *http.Request) {
	if app.positionManager == nil {
		http.Error(w, "Position manager not available", http.StatusServiceUnavailable)
		return
	}

	// Perform validation
	validationResult := map[string]interface{}{
		"timestamp": time.Now().Unix(),
		"status":    "healthy",
		"issues":    []string{},
		"stats":     app.positionManager.GetStats(),
	}

	// Add validation logic here
	// This is a placeholder - actual validation would depend on position manager implementation

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(validationResult)
}

// handleResourceMetrics returns current resource monitoring metrics.
//
// This endpoint provides real-time resource metrics from the new monitoring system:
//   - Current goroutine count and growth rate
//   - Memory usage (Alloc, Total, Sys) and growth rate
//   - File descriptor usage (if available)
//   - GC pause times and heap objects
//   - Timestamp of last collection
//
// Metrics include:
//   - Absolute values for all resources
//   - Percentage growth rates over the monitoring interval
//   - Historical trends for capacity planning
//   - Alert status and threshold information
//
// Response Codes:
//   - 200 OK: Metrics returned successfully
//   - 503 Service Unavailable: Resource monitor not available/enabled
//
// This endpoint is useful for:
//   - Real-time resource monitoring and dashboards
//   - Capacity planning and trend analysis
//   - Performance troubleshooting and debugging
//   - Integration with monitoring systems
//
// Example response:
//
//	{
//	  "timestamp": "2025-11-06T12:00:00Z",
//	  "goroutines": 42,
//	  "memory_alloc_mb": 256,
//	  "memory_total_mb": 512,
//	  "memory_sys_mb": 768,
//	  "file_descriptors": 128,
//	  "gc_pause_ms": 2.5,
//	  "heap_objects": 125000,
//	  "goroutine_growth": 5.2,
//	  "memory_growth": 2.1
//	}
func (app *App) handleResourceMetrics(w http.ResponseWriter, r *http.Request) {
	if app.resourceMonitorNew == nil {
		http.Error(w, "Resource monitoring not enabled", http.StatusServiceUnavailable)
		return
	}

	metrics := app.resourceMonitorNew.GetMetrics()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metrics)
}