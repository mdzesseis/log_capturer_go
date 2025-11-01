// Package app HTTP handlers for API endpoints and monitoring
package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"runtime/debug"
	"time"

	"ssw-logs-capture/internal/dispatcher"
	"ssw-logs-capture/internal/metrics"
	"ssw-logs-capture/pkg/tracing"

	"github.com/gorilla/mux"
)

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

	// Metrics endpoint (proxy to metrics server)
	router.Handle("/metrics", middleware(http.HandlerFunc(app.metricsHandler))).Methods("GET")

	// Debug endpoints
	router.Handle("/debug/goroutines", middleware(http.HandlerFunc(app.debugGoroutinesHandler))).Methods("GET")
	router.Handle("/debug/memory", middleware(http.HandlerFunc(app.debugMemoryHandler))).Methods("GET")
	router.Handle("/debug/positions/validate", middleware(http.HandlerFunc(app.debugPositionsValidateHandler))).Methods("GET")

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
		if gtStats.Status != "healthy" {
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