// Package app initialization methods for component setup and configuration
package app

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"ssw-logs-capture/internal/dispatcher"
	"ssw-logs-capture/internal/metrics"
	"ssw-logs-capture/internal/monitors"
	"ssw-logs-capture/internal/processing"
	"ssw-logs-capture/internal/sinks"
	"ssw-logs-capture/pkg/anomaly"
	"ssw-logs-capture/pkg/buffer"
	"ssw-logs-capture/pkg/cleanup"
	"ssw-logs-capture/pkg/discovery"
	"ssw-logs-capture/pkg/dlq"
	"ssw-logs-capture/pkg/errors"
	"ssw-logs-capture/pkg/goroutines"
	"ssw-logs-capture/pkg/hotreload"
	"ssw-logs-capture/pkg/leakdetection"
	"ssw-logs-capture/pkg/positions"
	"ssw-logs-capture/pkg/security"
	"ssw-logs-capture/pkg/slo"
	"ssw-logs-capture/pkg/task_manager"
	"ssw-logs-capture/pkg/tracing"
	"ssw-logs-capture/pkg/types"

	"github.com/gorilla/mux"
)

// initCoreServices initializes the fundamental services required for log processing.
//
// This method sets up the core components in the following order:
//   1. Task Manager: Provides background task coordination and heartbeat monitoring
//   2. Log Processor: Configures transformation pipelines and filtering rules
//   3. Dispatcher: Sets up the core orchestration engine with batching and retry logic
//
// The dispatcher is configured with:
//   - Queue size for buffering incoming log entries
//   - Worker count for parallel processing
//   - Batch settings for optimized sink delivery
//   - Retry logic with exponential backoff
//   - Dead letter queue integration when enabled
//
// Returns:
//   - error: Initialization error from any core service component
func (app *App) initCoreServices() error {
	app.taskManager = task_manager.New(task_manager.Config{
		HeartbeatInterval: 30 * time.Second,
		TaskTimeout:       5 * time.Minute,
		CleanupInterval:   1 * time.Minute,
	}, app.logger)

	processor, err := processing.NewLogProcessor(types.PipelineConfig{
		Enabled: app.config.Processing.Enabled,
		File:    app.config.Processing.PipelinesFile,
	}, app.logger)
	if err != nil {
		return fmt.Errorf("failed to create log processor: %w", err)
	}
	app.processor = processor

	dispatcherConfig := dispatcher.DispatcherConfig{
		QueueSize:    app.config.Dispatcher.QueueSize,
		Workers:      app.config.Dispatcher.WorkerCount,
		BatchSize:    app.config.Dispatcher.BatchSize,
		BatchTimeout: parseDurationSafe(app.config.Dispatcher.BatchTimeout, 5*time.Second),
		MaxRetries:   app.config.Dispatcher.MaxRetries,
		RetryDelay:   parseDurationSafe(app.config.Dispatcher.RetryBaseDelay, 1*time.Second),
		DLQEnabled:   app.config.Dispatcher.DLQEnabled,
	}
	app.dispatcher = dispatcher.NewDispatcher(dispatcherConfig, processor, app.logger)

	return nil
}

// initSinks initializes and configures all output destinations for log entries.
//
// This method sets up the configured sinks based on the application configuration:
//   - Loki Sink: For sending logs to Grafana Loki with push API integration
//   - Local File Sink: For writing logs to local filesystem with rotation
//   - Additional sinks can be added following the types.Sink interface
//
// Each enabled sink is:
//   1. Instantiated with its specific configuration
//   2. Added to the internal sinks collection for lifecycle management
//   3. Registered with the dispatcher for log entry delivery
//
// The function ensures at least one sink is enabled, as the application
// requires at least one output destination to be functional.
//
// For Loki sinks, dead letter queue integration is automatically configured
// when the dispatcher supports DLQ functionality.
//
// Returns:
//   - error: Configuration error or sink initialization failure
func (app *App) initSinks() error {
	app.sinks = make([]types.Sink, 0)

	// Loki Sink
	if app.config.Sinks.Loki.Enabled {
		var deadLetterQueue *dlq.DeadLetterQueue
		if dispatcherImpl, ok := app.dispatcher.(*dispatcher.Dispatcher); ok {
			deadLetterQueue = dispatcherImpl.GetDLQ()
		}
		lokiSink := sinks.NewLokiSink(app.config.Sinks.Loki, app.logger, deadLetterQueue)
		app.sinks = append(app.sinks, lokiSink)
		app.dispatcher.AddSink(lokiSink)
		app.logger.Info("Loki sink initialized")
	}

	// Local File Sink
	if app.config.Sinks.LocalFile.Enabled {
		// Convert LocalFileSinkConfig to LocalFileConfig for compatibility
		localFileConfig := types.LocalFileConfig{
			Enabled:                   app.config.Sinks.LocalFile.Enabled,
			Directory:                 app.config.Sinks.LocalFile.Directory,
			Filename:                  app.config.Sinks.LocalFile.Filename,
			FilenamePattern:           app.config.Sinks.LocalFile.FilenamePattern,
			FilenamePatternFiles:      app.config.Sinks.LocalFile.FilenamePatternFiles,
			FilenamePatternContainers: app.config.Sinks.LocalFile.FilenamePatternContainers,
			MaxFileSize:               app.config.Sinks.LocalFile.MaxFileSize,
			MaxFiles:                  app.config.Sinks.LocalFile.MaxFiles,
			Compress:                  app.config.Sinks.LocalFile.Compress,
			OutputFormat:              app.config.Sinks.LocalFile.OutputFormat,
			TextFormat:                app.config.Sinks.LocalFile.TextFormat,
			QueueSize:                 app.config.Sinks.LocalFile.QueueSize,
		}
		localFileSink := sinks.NewLocalFileSink(localFileConfig, app.logger)
		app.sinks = append(app.sinks, localFileSink)
		app.dispatcher.AddSink(localFileSink)
		app.logger.Info("Local file sink initialized")
	}

	if len(app.sinks) == 0 {
		return fmt.Errorf("no sinks enabled")
	}

	app.logger.WithField("sink_count", len(app.sinks)).Info("Sinks initialized")
	return nil
}

// initMonitors initializes the input sources that monitor and capture log entries.
//
// This method configures the available monitoring components:
//
// File Monitor:
//   - Watches specified directories for file changes using inotify/polling
//   - Supports recursive directory monitoring and symlink following
//   - Implements include/exclude patterns for selective monitoring
//   - Integrates with position manager for resumable reading
//   - Configurable read intervals and buffer sizes for performance tuning
//
// Container Monitor:
//   - Connects to Docker daemon via Unix socket or TCP
//   - Monitors container logs using Docker Events API
//   - Supports container filtering by labels and names
//   - Implements connection pooling and automatic reconnection
//   - Includes health checking and graceful error handling
//
// Both monitors integrate with:
//   - Timestamp validation for log entry enrichment
//   - Dispatcher for log entry delivery
//   - Task manager for lifecycle coordination
//   - Position manager for reading state persistence
//
// Returns:
//   - error: Monitor configuration or initialization failure
func (app *App) initMonitors() error {
	// File Monitor
	if app.config.FileMonitorService.Enabled {
		// Construir a configuração legada para manter a compatibilidade da função NewFileMonitor por enquanto
		fileConfig := types.FileConfig{
			Enabled:            app.config.FileMonitorService.Enabled,
			PollInterval:       parseDurationSafe(app.config.FileMonitorService.PollInterval, 10*time.Second),
			BufferSize:         app.config.FileMonitorService.ReadBufferSize,
			PositionsPath:      app.config.Positions.Directory,
			WatchDirectories:   app.config.FilesConfig.WatchDirectories,
			IncludePatterns:    app.config.FilesConfig.IncludePatterns,
			ExcludePatterns:    app.config.FilesConfig.ExcludePatterns,
			ExcludeDirectories: app.config.FilesConfig.ExcludeDirectories,
			ReadInterval:       parseDurationSafe(app.config.FileMonitorService.ReadInterval, 200*time.Millisecond),
			Recursive:          app.config.FileMonitorService.Recursive,
			FollowSymlinks:     app.config.FileMonitorService.FollowSymlinks,
		}
		fileMonitor, err := monitors.NewFileMonitor(fileConfig, app.config.TimestampValidation, app.dispatcher, app.taskManager, app.positionManager, app.logger)
		if err != nil {
			return fmt.Errorf("failed to create file monitor: %w", err)
		}
		app.fileMonitor = fileMonitor
		app.logger.Info("File monitor initialized")
	}

	// Container Monitor
	if app.config.ContainerMonitor.Enabled {
		// Construir a configuração legada para manter a compatibilidade
		dockerConfig := types.DockerConfig{
			Enabled:           app.config.ContainerMonitor.Enabled,
			SocketPath:        app.config.ContainerMonitor.SocketPath,
			MaxConcurrent:     app.config.ContainerMonitor.MaxConcurrent,
			ReconnectInterval: parseDurationSafe(app.config.ContainerMonitor.ReconnectInterval, 30*time.Second),
			HealthCheckDelay:  parseDurationSafe(app.config.ContainerMonitor.HealthCheckDelay, 30*time.Second),
			IncludeLabels:     app.config.ContainerMonitor.IncludeLabels,
			ExcludeLabels:     app.config.ContainerMonitor.ExcludeLabels,
			IncludeNames:      app.config.ContainerMonitor.IncludeNames,
			ExcludeNames:      app.config.ContainerMonitor.ExcludeNames,
		}
		containerMonitor, err := monitors.NewContainerMonitor(dockerConfig, app.config.TimestampValidation, app.dispatcher, app.taskManager, app.positionManager, app.logger)
		if err != nil {
			return fmt.Errorf("failed to create container monitor: %w", err)
		}
		app.containerMonitor = containerMonitor
		app.logger.Info("Container monitor initialized")
	}

	return nil
}

// initAuxiliaryServices initializes supporting services that enhance application reliability and observability.
//
// This method sets up auxiliary components in the following order:
//   1. Position Manager: Persistent tracking of file reading positions
//   2. Disk Manager: Automated cleanup and space management
//   3. Resource Monitor: System resource leak detection
//   4. Disk Buffer: Persistent log entry buffering for reliability
//   5. Anomaly Detector: Pattern-based anomaly detection in logs
//   6. Enhanced Metrics: Advanced metrics collection and reporting
//   7. Enterprise Features: Security, tracing, SLO monitoring, goroutine tracking
//
// Each service is conditionally initialized based on configuration:
//   - Position Manager: Enables resumable log reading after restarts
//   - Disk Manager: Prevents disk space exhaustion with automated cleanup
//   - Resource Monitor: Detects memory leaks, goroutine leaks, and FD exhaustion
//   - Disk Buffer: Provides persistent storage during sink unavailability
//   - Anomaly Detector: Machine learning-based anomaly detection
//   - Enhanced Metrics: Additional Prometheus metrics beyond basic counters
//
// Enterprise features include advanced security, distributed tracing,
// SLO monitoring, and comprehensive resource tracking.
//
// Returns:
//   - error: Service initialization failure with specific component context
func (app *App) initAuxiliaryServices() error {
	var err error
	// Position Manager is initialized earlier (before monitors)
	// Disk Manager
	if err = app.initializeDiskManager(); err != nil {
		return err
	}
	// Resource Monitor
	if err = app.initializeResourceMonitor(); err != nil {
		return err
	}
	// Disk Buffer
	if err = app.initializeDiskBuffer(); err != nil {
		return err
	}
	// Anomaly Detector
	if app.config.AnomalyDetection.Enabled {
		// Convert types.AnomalyDetectionConfig to anomaly.Config
		anomalyConfig := anomaly.Config{
			Enabled:              app.config.AnomalyDetection.Enabled,
			Algorithm:            app.config.AnomalyDetection.Algorithm,
			SensitivityThreshold: convertSensitivityLevel(app.config.AnomalyDetection.SensitivityLevel),
			WindowSize:           app.config.AnomalyDetection.WindowSize,
			TrainingInterval:     "1h",  // Default training interval
			MinTrainingSamples:   app.config.AnomalyDetection.MinSamples,
			MaxTrainingSamples:   10000, // Default max samples
			Features:             []string{"text", "statistical", "temporal", "pattern"},
			ModelConfig:          make(map[string]interface{}),
			WhitelistPatterns:    []string{},
			BlacklistPatterns:    []string{},
			AutoTuning:           app.config.AnomalyDetection.TrainingEnabled,
			AlertThreshold:       app.config.AnomalyDetection.Thresholds.ErrorRateSpike,
			SaveModel:            true,
			ModelPath:            app.config.AnomalyDetection.ModelPath,
		}

		anomalyDetector, err := anomaly.NewAnomalyDetector(anomalyConfig, app.logger)
		if err != nil {
			return fmt.Errorf("failed to create anomaly detector: %w", err)
		}
		app.anomalyDetector = anomalyDetector
		app.logger.Info("Anomaly detector initialized")
	}

	// Enhanced Metrics
	app.enhancedMetrics = metrics.NewEnhancedMetrics(app.logger)

	// Initialize enterprise features
	if err = app.initializeEnterpriseFeatures(); err != nil {
		return err
	}

	return nil
}

// initializePositionManager sets up persistent file position tracking.
//
// This method initializes the position manager component responsible for:
//   - Tracking file read positions across application restarts
//   - Buffering position updates in memory for performance
//   - Periodic flushing of positions to persistent storage
//   - Cleanup of stale position entries for removed files
//
// Position tracking configuration includes:
//   - Storage directory for position files
//   - Flush interval for position persistence
//   - Memory buffer size for position updates
//   - Cleanup interval for stale position removal
//   - Maximum age for position entry retention
//
// The position manager is essential for resumable log reading after
// application restarts, preventing duplicate log entry processing
// and ensuring reliable log delivery.
//
// When position tracking is disabled, this method logs the status
// and returns without error.
//
// Returns:
//   - error: Position manager initialization or directory creation failure
func (app *App) initializePositionManager() error {
	if !app.config.Positions.Enabled {
		app.logger.Info("Position manager disabled")
		return nil
	}

	// Criar diretório se não existir
	if err := ensureDirectoryExists(app.config.Positions.Directory); err != nil {
		return fmt.Errorf("failed to create positions directory: %w", err)
	}

	// Create individual position managers
	containerManager := positions.NewContainerPositionManager(app.config.Positions.Directory, app.logger)
	fileManager := positions.NewFilePositionManager(app.config.Positions.Directory, app.logger)

	// Configurar position buffer manager
	bufferConfig := &positions.BufferConfig{
		FlushInterval:      parseDurationSafe(app.config.Positions.FlushInterval, 30*time.Second),
		MaxMemoryBuffer:    app.config.Positions.MaxMemoryBuffer,
		MaxMemoryPositions: app.config.Positions.MaxMemoryPositions,
		ForceFlushOnExit:   app.config.Positions.ForceFlushOnExit,
		CleanupInterval:    parseDurationSafe(app.config.Positions.CleanupInterval, 5*time.Minute),
		MaxPositionAge:     parseDurationSafe(app.config.Positions.MaxPositionAge, 24*time.Hour),
	}

	positionManager := positions.NewPositionBufferManager(containerManager, fileManager, bufferConfig, app.logger)

	app.positionManager = positionManager
	app.logger.Info("Position manager initialized successfully")
	return nil
}

// initializeDiskManager sets up automated disk space monitoring and cleanup.
//
// This method initializes the disk space manager responsible for:
//   - Monitoring disk usage across configured paths
//   - Automated cleanup when space thresholds are exceeded
//   - Warning alerts before reaching critical space levels
//   - Configurable cleanup strategies (oldest first, size-based, etc.)
//
// Monitored paths include:
//   - Local file sink output directories
//   - Position file storage locations
//   - Temporary and buffer directories
//   - Log rotation and archive locations
//
// The disk manager prevents:
//   - Disk space exhaustion that could cause application failures
//   - Unbounded growth of log files and position data
//   - System-wide disk space issues from log accumulation
//
// Cleanup strategies respect retention periods and file importance
// to balance disk usage with data availability requirements.
//
// When disk cleanup is disabled, this method logs the status
// and returns without error.
//
// Returns:
//   - error: Disk manager initialization or configuration error
func (app *App) initializeDiskManager() error {
	if !app.config.DiskCleanup.Enabled {
		app.logger.Info("Disk manager disabled")
		return nil
	}

	diskConfig := cleanup.Config{
		Directories: []cleanup.DirectoryConfig{
			{
				Path:              app.config.Sinks.LocalFile.Directory,
				MaxSizeMB:         int64(app.config.DiskCleanup.MaxSizeGB * 1024),
				RetentionDays:     7,
				FilePatterns:      []string{"*.log", "*.txt"},
				MaxFiles:          100,
				CleanupAgeSeconds: 86400, // 24 hours
			},
			{
				Path:              app.config.Positions.Directory,
				MaxSizeMB:         1024, // 1GB max for positions
				RetentionDays:     7,
				FilePatterns:      []string{"*.pos"},
				MaxFiles:          50,
				CleanupAgeSeconds: 86400,
			},
		},
		CheckInterval:           parseDurationSafe(app.config.DiskCleanup.CheckInterval, 5*time.Minute),
		CriticalSpaceThreshold: 5.0,  // 5% critical
		WarningSpaceThreshold:  15.0, // 15% warning
	}

	diskManager := cleanup.NewDiskSpaceManager(diskConfig, app.logger)

	app.diskManager = diskManager
	app.logger.Info("Disk manager initialized successfully")
	return nil
}

// initializeResourceMonitor sets up system resource leak detection and monitoring.
//
// This method initializes the resource monitor component responsible for:
//   - Tracking goroutine count and detecting potential leaks
//   - Monitoring memory usage and allocation patterns
//   - File descriptor usage tracking and leak detection
//   - Automated alerting when resource thresholds are exceeded
//
// Resource monitoring includes:
//   - Periodic sampling of system resource usage
//   - Threshold-based alerting for abnormal resource consumption
//   - Historical trending for capacity planning
//   - Integration with external monitoring systems via webhooks
//
// The resource monitor helps detect:
//   - Memory leaks in long-running applications
//   - Goroutine leaks from improper cleanup
//   - File descriptor exhaustion from unclosed resources
//   - Performance degradation from resource contention
//
// Alert configuration supports:
//   - Configurable thresholds for each resource type
//   - Webhook notifications for external monitoring integration
//   - Rate limiting to prevent alert flooding
//
// When resource monitoring is disabled, this method logs the status
// and returns without error.
//
// Returns:
//   - error: Resource monitor initialization or configuration error
func (app *App) initializeResourceMonitor() error {
	if !app.config.ResourceMonitoring.Enabled {
		app.logger.Info("Resource monitor disabled")
		return nil
	}

	resourceConfig := leakdetection.ResourceMonitorConfig{
		MonitoringInterval:      parseDurationSafe(app.config.ResourceMonitoring.CheckInterval, 30*time.Second),
		FDLeakThreshold:         int64(app.config.ResourceMonitoring.FDThreshold),
		GoroutineLeakThreshold:  int64(app.config.ResourceMonitoring.GoroutineThreshold),
		MemoryLeakThreshold:     int64(app.config.ResourceMonitoring.MemoryThresholdMB) * 1024 * 1024, // Convert MB to bytes
		AlertCooldown:           5 * time.Minute, // Default alert cooldown
		EnableMemoryProfiling:   false,
		EnableGCOptimization:    false,
		MaxAlertHistory:         100,
	}

	resourceMonitor := leakdetection.NewResourceMonitor(resourceConfig, app.logger)

	app.resourceMonitor = resourceMonitor
	app.logger.Info("Resource monitor initialized successfully")
	return nil
}

// initializeDiskBuffer sets up persistent buffering for log entries during sink unavailability.
//
// This method initializes the disk buffer component responsible for:
//   - Persistent storage of log entries when sinks are unavailable
//   - Compression and encryption of buffered data
//   - Automatic recovery and replay after sink reconnection
//   - Configurable retention and cleanup policies
//
// Disk buffer features include:
//   - Write-ahead logging for durability guarantees
//   - Configurable compression to reduce storage requirements
//   - Optional encryption for sensitive log data
//   - Automatic file rotation based on size and age limits
//   - Recovery mechanisms for corrupted buffer files
//
// The disk buffer provides:
//   - Reliability during temporary sink outages
//   - Backpressure handling during high log volume
//   - Data durability across application restarts
//   - Performance optimization through batched writes
//
// Configuration options include:
//   - Maximum file size and total buffer size limits
//   - Flush and sync intervals for performance tuning
//   - Compression and encryption enablement
//   - Retention period for automatic cleanup
//
// When disk buffering is disabled, this method logs the status
// and returns without error.
//
// Returns:
//   - error: Disk buffer initialization or directory creation failure
func (app *App) initializeDiskBuffer() error {
	if !app.config.DiskBuffer.Enabled {
		app.logger.Info("Disk buffer disabled")
		return nil
	}

	// Criar diretório se não existir
	if err := ensureDirectoryExists(app.config.DiskBuffer.Directory); err != nil {
		return fmt.Errorf("failed to create disk buffer directory: %w", err)
	}

	bufferConfig := buffer.DiskBufferConfig{
		BaseDir:           app.config.DiskBuffer.Directory,
		MaxFileSize:       app.config.DiskBuffer.MaxFileSize,
		MaxTotalSize:      app.config.DiskBuffer.MaxTotalSize,
		MaxFiles:          100, // Default max files
		CompressionEnabled: app.config.DiskBuffer.CompressionEnabled,
		SyncInterval:      parseDurationSafe(app.config.DiskBuffer.SyncInterval, 30*time.Second),
		CleanupInterval:   parseDurationSafe(app.config.DiskBuffer.FlushInterval, 5*time.Minute),
		RetentionPeriod:   parseDurationSafe(app.config.DiskBuffer.RetentionPeriod, 24*time.Hour),
		FilePermissions:   0644,
		DirPermissions:    0755,
	}

	diskBuffer, err := buffer.NewDiskBuffer(bufferConfig, app.logger)
	if err != nil {
		return fmt.Errorf("failed to create disk buffer: %w", err)
	}

	app.diskBuffer = diskBuffer
	app.logger.Info("Disk buffer initialized successfully")
	return nil
}

// initializeEnterpriseFeatures initializes all enterprise security and observability features.
//
// This method sets up advanced capabilities for production environments:
//
// Security Manager:
//   - Multi-factor authentication and authorization
//   - Role-based access control (RBAC)
//   - Audit logging and compliance features
//   - Rate limiting and intrusion detection
//
// Distributed Tracing:
//   - OpenTelemetry integration for request tracing
//   - Span collection and correlation across services
//   - Distributed context propagation
//   - Integration with tracing backends (Jaeger, Zipkin)
//
// SLO Manager:
//   - Service Level Objective definition and monitoring
//   - Error budget calculation and alerting
//   - Compliance tracking and reporting
//   - Integration with Prometheus for metrics collection
//
// Goroutine Tracker:
//   - Real-time goroutine monitoring and leak detection
//   - Memory allocation tracking and optimization
//   - Performance profiling and resource analysis
//   - Automated alerting for resource threshold violations
//
// Each feature is conditionally initialized based on configuration,
// allowing for flexible deployment across different environments.
//
// Returns:
//   - error: Enterprise feature initialization failure with component context
func (app *App) initializeEnterpriseFeatures() error {
	// Security Manager
	if app.config.Security.Enabled {
		authConfig := security.AuthConfig{
			Enabled:        app.config.Security.Authentication.Enabled,
			Method:         app.config.Security.Authentication.Method,
			Users:          make(map[string]security.User),
			Tokens:         make(map[string]string),
			JWTSecret:      app.config.Security.Authentication.JWT.Secret,
			SessionTimeout: parseDurationSafe(app.config.Security.Authentication.SessionTimeout, 24*time.Hour),
			MaxAttempts:    app.config.Security.Authentication.MaxAttempts,
			LockoutTime:    parseDurationSafe(app.config.Security.Authentication.LockoutTime, 15*time.Minute),
		}
		securityManager := security.NewAuthManager(authConfig, app.logger)
		app.securityManager = securityManager
		app.logger.Info("Security manager initialized")
	}

	// Distributed Tracing
	if app.config.Tracing.Enabled {
		tracingConfig := tracing.TracingConfig{
			Enabled:        app.config.Tracing.Enabled,
			ServiceName:    app.config.Tracing.ServiceName,
			ServiceVersion: app.config.Tracing.ServiceVersion,
			Environment:    app.config.App.Environment, // Use app environment
			Exporter:       app.config.Tracing.Exporter,
			Endpoint:       app.config.Tracing.Endpoint,
			SampleRate:     app.config.Tracing.SampleRate,
			BatchTimeout:   parseDurationSafe(app.config.Tracing.BatchTimeout, 10*time.Second),
			MaxBatchSize:   app.config.Tracing.BatchSize,
			Headers:        app.config.Tracing.Headers,
		}
		tracingManager, err := tracing.NewTracingManager(tracingConfig, app.logger)
		if err != nil {
			return errors.New("CONFIG_INVALID", "app", "init_tracing", "failed to initialize tracing manager")
		}
		app.tracingManager = tracingManager
		app.logger.Info("Distributed tracing initialized")
	}

	// SLO Manager
	if app.config.SLO.Enabled {
		sloManagerConfig := slo.SLOManagerConfig{
			Enabled:            app.config.SLO.Enabled,
			PrometheusURL:      app.config.SLO.PrometheusURL,
			EvaluationInterval: parseDurationSafe(app.config.SLO.QueryInterval, 1*time.Minute),
			SLOs:               make([]slo.SLOConfig, 0), // Convert objectives if needed
			AlertWebhook:       app.config.SLO.Alerting.WebhookURL,
			RetentionPeriod:    30 * 24 * time.Hour, // Default 30 days
		}
		sloManager, err := slo.NewSLOManager(sloManagerConfig, app.logger)
		if err != nil {
			return errors.New("CONFIG_INVALID", "app", "init_slo", "failed to initialize SLO manager")
		}
		app.sloManager = sloManager
		app.logger.Info("SLO manager initialized")
	}

	// Goroutine Tracker
	if app.config.GoroutineTracking.Enabled {
		goroutineConfig := goroutines.GoroutineConfig{
			Enabled:              app.config.GoroutineTracking.Enabled,
			CheckInterval:        parseDurationSafe(app.config.GoroutineTracking.CheckInterval, 30*time.Second),
			LeakThreshold:        app.config.GoroutineTracking.LeakThreshold,
			MaxGoroutines:        1000, // Default max goroutines
			WarnThreshold:        500,  // Default warning threshold
			TrackingEnabled:      app.config.GoroutineTracking.DetailedProfiling,
			StackTraceOnLeak:     app.config.GoroutineTracking.StackTraceDepth > 0,
			AlertWebhook:         app.config.GoroutineTracking.AlertWebhook,
			RetentionPeriod:      24 * time.Hour, // Default retention
		}
		goroutineTracker := goroutines.NewGoroutineTracker(goroutineConfig, app.logger)
		app.goroutineTracker = goroutineTracker
		app.logger.Info("Goroutine tracker initialized")
	}

	return nil
}

// initReloader initializes the configuration hot-reload functionality.
//
// This method sets up the configuration reloader component when hot reload
// is enabled in the application configuration. The reloader provides:
//   - File system monitoring for configuration file changes
//   - Automatic configuration validation before applying changes
//   - Callback-based notification system for configuration updates
//   - Graceful handling of configuration reload failures
//
// The reloader integrates with the application lifecycle by:
//   - Monitoring the original configuration file for modifications
//   - Triggering validation and reload callbacks when changes are detected
//   - Providing API endpoints for manual configuration reload
//   - Maintaining configuration history for rollback scenarios
//
// When hot reload is disabled, this method logs the status and returns
// without error, allowing the application to continue normal operation.
//
// Returns:
//   - error: Reloader initialization failure or configuration error
func (app *App) initReloader() error {
	if !app.config.HotReload.Enabled {
		app.logger.Info("Hot reload disabled")
		return nil
	}
	hotReloadConfig := hotreload.Config{
		Enabled:           app.config.HotReload.Enabled,
		WatchInterval:     parseDurationSafe(app.config.HotReload.CheckInterval, 5*time.Second),
		DebounceInterval:  parseDurationSafe(app.config.HotReload.GracePeriod, 2*time.Second),
		WatchFiles:        app.config.HotReload.WatchFiles,
		ValidateOnReload:  !app.config.HotReload.ValidateOnly, // Invert logic
		BackupOnReload:    app.config.HotReload.BackupConfig,
		BackupDirectory:   "./backups", // Default backup directory
		MaxBackups:        10,          // Default max backups
		NotifyWebhook:     "",          // No webhook by default
		FailsafeMode:      true,        // Enable failsafe by default
	}
	reloader, err := hotreload.NewConfigReloader(hotReloadConfig, app.configFile, app.logger)
	if err != nil {
		return fmt.Errorf("failed to create config reloader: %w", err)
	}
	reloader.SetCallbacks(app.handleConfigReload, nil, nil)
	app.reloader = reloader
	app.logger.Info("Hot reload enabled")
	return nil
}

// initServiceDiscovery initializes the service discovery functionality.
//
// This function sets up automatic service discovery for monitoring sources
// including Docker containers, file-based services, and future Kubernetes integration.
func (app *App) initServiceDiscovery() error {
	if !app.config.ServiceDiscovery.Enabled {
		app.logger.Info("Service discovery disabled")
		return nil
	}

	discoveryConfig := discovery.Config{
		Enabled:        app.config.ServiceDiscovery.Enabled,
		UpdateInterval: parseDurationSafe(app.config.ServiceDiscovery.UpdateInterval, 30*time.Second),
		DockerEnabled:  app.config.ServiceDiscovery.DockerEnabled,
		FileEnabled:    app.config.ServiceDiscovery.FileEnabled,
		DockerConfig: discovery.DockerDiscoveryConfig{
			SocketPath:     app.config.ServiceDiscovery.Docker.SocketPath,
			RequiredLabels: app.config.ServiceDiscovery.Docker.RequiredLabels,
			ExcludeLabels:  app.config.ServiceDiscovery.Docker.ExcludeLabels,
			RequireLabel:   app.config.ServiceDiscovery.Docker.RequireLabel,
			PipelineLabel:  app.config.ServiceDiscovery.Docker.PipelineLabel,
			ComponentLabel: app.config.ServiceDiscovery.Docker.ComponentLabel,
			TenantLabel:    app.config.ServiceDiscovery.Docker.TenantLabel,
		},
		FileConfig: discovery.FileDiscoveryConfig{
			WatchPaths:     app.config.ServiceDiscovery.File.WatchPaths,
			ConfigFiles:    app.config.ServiceDiscovery.File.ConfigFiles,
			RequiredLabels: app.config.ServiceDiscovery.File.RequiredLabels,
			AutoDetectLogs: app.config.ServiceDiscovery.File.AutoDetectLogs,
		},
	}

	serviceDiscovery, err := discovery.NewServiceDiscovery(discoveryConfig, app.logger)
	if err != nil {
		return fmt.Errorf("failed to create service discovery: %w", err)
	}

	app.serviceDiscovery = serviceDiscovery
	app.logger.Info("Service discovery initialized")
	return nil
}

// handleConfigReload processes configuration changes during hot reload operations.
//
// This callback method is invoked by the configuration reloader when a
// configuration change is detected and validated. It performs:
//   - Configuration transition from old to new settings
//   - Component-specific configuration updates where possible
//   - Logging of configuration changes for audit purposes
//   - Graceful fallback handling for unsupported changes
//
// Parameters:
//   - oldConfig: Previous configuration state before reload
//   - newConfig: New configuration state after validation
//
// Returns:
//   - error: Configuration application error or validation failure
//
// Note: Some configuration changes require a full application restart
// to take effect. This method provides best-effort hot reloading for
// runtime-configurable parameters while logging guidance for changes
// that require restart.
func (app *App) handleConfigReload(oldConfig, newConfig *types.Config) error {
	app.logger.Warn("Configuration reload triggered. Applying changes...")
	// For now, we just replace the config. A more granular approach would be needed for a true zero-downtime reload.
	app.config = newConfig
	app.logger.Info("Configuration has been reloaded. A full restart may be required for some changes to take effect.")
	return nil
}

// initHTTPServer configures the main HTTP server for API endpoints.
//
// This method sets up the HTTP server that provides:
//   - Health check endpoints for load balancer integration
//   - Statistics and metrics endpoints for monitoring
//   - Configuration management endpoints
//   - Enterprise feature endpoints (when enabled)
//   - Administrative endpoints for operational management
//
// The server configuration includes:
//   - Gorilla mux router for flexible route handling
//   - Middleware integration for security and tracing
//   - Graceful shutdown support with timeout handling
//   - Configurable host and port binding
//
// Security middleware is automatically applied when the security
// manager is available, providing authentication and authorization
// for protected endpoints.
//
// Tracing middleware is applied when distributed tracing is enabled,
// providing request correlation and performance monitoring.
//
// When the HTTP server is disabled in configuration, this method
// returns without creating the server instance.
func (app *App) initHTTPServer() {
	app.logger.WithField("enabled", app.config.Server.Enabled).Debug("Checking HTTP server configuration")
	if !app.config.Server.Enabled {
		app.logger.Info("HTTP server disabled in configuration")
		return
	}
	router := mux.NewRouter()
	app.registerHandlers(router)
	addr := fmt.Sprintf("%s:%d", app.config.Server.Host, app.config.Server.Port)
	app.httpServer = &http.Server{
		Addr:    addr,
		Handler: router,
	}
	app.logger.WithField("addr", addr).Info("HTTP server initialized")
}

// initMetricsServer configures the Prometheus metrics server.
//
// This method sets up a dedicated HTTP server for Prometheus metrics
// collection that provides:
//   - Application performance metrics and counters
//   - Component-specific operational statistics
//   - Resource usage and health indicators
//   - Custom business metrics for monitoring
//
// The metrics server runs on a separate port from the main API server
// to allow for different security and access policies. This separation
// enables:
//   - Unrestricted access to metrics for monitoring systems
//   - Different authentication requirements
//   - Independent scaling and load balancing
//   - Isolation of monitoring traffic
//
// Metrics include:
//   - Standard Go runtime metrics (goroutines, memory, GC)
//   - Application-specific counters and histograms
//   - Component health and performance indicators
//   - Enterprise feature metrics when available
//
// When metrics collection is disabled in configuration, this method
// returns without creating the metrics server instance.
func (app *App) initMetricsServer() {
	if !app.config.Metrics.Enabled {
		return
	}
	addr := fmt.Sprintf(":%d", app.config.Metrics.Port)
	app.metricsServer = metrics.NewMetricsServer(addr, app.logger)
}

// ensureDirectoryExists creates a directory path if it doesn't already exist.
//
// This utility function provides safe directory creation with proper error
// handling and validation. It's used throughout the application to ensure
// required directories exist before attempting file operations.
//
// The function performs the following checks and operations:
//   1. Validates that the directory path is not empty
//   2. Checks if the path already exists and is a directory
//   3. Creates the directory with appropriate permissions (0755)
//   4. Handles edge cases like existing files at the target path
//
// Parameters:
//   - dir: Directory path to create (absolute or relative)
//
// Returns:
//   - error: Directory creation error or validation failure
//
// The function uses os.MkdirAll to create parent directories as needed,
// similar to the Unix 'mkdir -p' command. The created directories have
// permissions 0755 (owner read/write/execute, group/other read/execute).
//
// Common usage scenarios:
//   - Creating log output directories
//   - Setting up position file storage
//   - Initializing disk buffer directories
//   - Preparing enterprise feature storage locations
func ensureDirectoryExists(dir string) error {
	if dir == "" {
		return fmt.Errorf("directory path cannot be empty")
	}

	// Verificar se já existe
	if stat, err := os.Stat(dir); err == nil {
		if !stat.IsDir() {
			return fmt.Errorf("path exists but is not a directory: %s", dir)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to check directory: %w", err)
	}

	// Criar diretório com permissões apropriadas
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	return nil
}

// convertSensitivityLevel converts sensitivity level string to float64
func convertSensitivityLevel(level string) float64 {
	switch level {
	case "low":
		return 0.5
	case "medium":
		return 0.7
	case "high":
		return 0.9
	default:
		return 0.7 // Default to medium
	}
}

// parseDurationSafe safely parses a duration string with fallback handling.
//
// This utility function provides robust duration parsing for configuration
// values that may be missing, malformed, or invalid. It prevents application
// startup failures due to configuration errors by providing sensible defaults.
//
// Parameters:
//   - durationStr: Duration string in Go time.Duration format (e.g., "5s", "1m", "24h")
//   - fallback: Default duration to use if parsing fails or string is empty
//
// Returns:
//   - time.Duration: Parsed duration or fallback value
//
// The function handles common configuration scenarios:
//   - Empty or missing configuration values
//   - Invalid duration format strings
//   - Malformed time unit specifications
//
// This approach ensures the application can start with reasonable defaults
// even when configuration is incomplete or contains errors.
func parseDurationSafe(durationStr string, fallback time.Duration) time.Duration {
	if durationStr == "" {
		return fallback
	}
	if d, err := time.ParseDuration(durationStr); err == nil {
		return d
	}
	return fallback
}