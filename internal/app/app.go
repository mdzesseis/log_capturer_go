// Package app provides the main application implementation for SSW Logs Capture Go.
//
// This package contains the core application logic that orchestrates all components
// including log monitors, dispatchers, sinks, and enterprise features like security,
// tracing, and SLO monitoring.
//
// The App struct is the main entry point that:
//   - Initializes and coordinates all components
//   - Manages the application lifecycle (start, stop, graceful shutdown)
//   - Provides HTTP endpoints for health checks, metrics, and configuration
//   - Handles enterprise features when enabled
//
// Example usage:
//
//	app, err := app.New("/path/to/config.yaml")
//	if err != nil {
//		log.Fatal(err)
//	}
//	if err := app.Run(); err != nil {
//		log.Fatal(err)
//	}
package app

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"ssw-logs-capture/internal/config"
	"ssw-logs-capture/internal/metrics"
	"ssw-logs-capture/internal/monitors"
	"ssw-logs-capture/internal/processing"
	"ssw-logs-capture/pkg/anomaly"
	"ssw-logs-capture/pkg/buffer"
	"ssw-logs-capture/pkg/cleanup"
	"ssw-logs-capture/pkg/discovery"
	"ssw-logs-capture/pkg/goroutines"
	"ssw-logs-capture/pkg/hotreload"
	"ssw-logs-capture/pkg/leakdetection"
	"ssw-logs-capture/pkg/positions"
	"ssw-logs-capture/pkg/security"
	"ssw-logs-capture/pkg/slo"
	"ssw-logs-capture/pkg/tracing"
	"ssw-logs-capture/pkg/types"

	"github.com/sirupsen/logrus"
)

// App represents the main application instance that coordinates all log capture,
// processing, and delivery components.
//
// The App struct maintains references to all initialized components and manages
// their lifecycle. It supports both basic log capture functionality and enterprise
// features like security, tracing, SLO monitoring, and advanced observability.
//
// Core Components:
//   - taskManager: Manages background tasks and provides heartbeat monitoring
//   - dispatcher: Orchestrates log entry processing and delivery to sinks
//   - positionManager: Tracks file reading positions for resumable operations
//   - processor: Applies transformations and filtering to log entries
//   - fileMonitor: Monitors filesystem changes and reads log files
//   - containerMonitor: Monitors Docker container logs via Docker API
//
// Enterprise Components (when enabled):
//   - securityManager: Handles authentication, authorization, and audit logging
//   - tracingManager: Provides distributed tracing capabilities
//   - sloManager: Monitors service level objectives and error budgets
//   - goroutineTracker: Tracks goroutine usage and detects potential leaks
//
// The App implements graceful shutdown, configuration hot-reloading, and
// comprehensive health monitoring across all components.
type App struct {
	config    *types.Config
	logger    *logrus.Logger

	// Core components - fundamental services for log capture and processing
	taskManager      types.TaskManager                  // Manages background tasks and provides heartbeat monitoring
	dispatcher       types.Dispatcher                   // Orchestrates log entry processing and delivery to sinks
	positionManager  *positions.PositionBufferManager  // Tracks file reading positions for resumable operations
	processor        *processing.LogProcessor           // Applies transformations and filtering to log entries
	fileMonitor      *monitors.FileMonitor              // Monitors filesystem changes and reads log files
	containerMonitor *monitors.ContainerMonitor         // Monitors Docker container logs via Docker API
	diskManager      *cleanup.DiskSpaceManager          // Manages disk space and performs cleanup operations
	resourceMonitor  *leakdetection.ResourceMonitor     // Monitors system resources and detects potential leaks
	diskBuffer       *buffer.DiskBuffer                 // Provides persistent buffering for log entries
	reloader         *hotreload.ConfigReloader          // Handles configuration hot-reloading
	anomalyDetector  *anomaly.AnomalyDetector           // Detects anomalies in log patterns and system behavior

	// Enterprise features - advanced capabilities for production environments
	securityManager   *security.AuthManager          // Handles authentication, authorization, and audit logging
	tracingManager    *tracing.TracingManager        // Provides distributed tracing capabilities with OpenTelemetry
	sloManager        *slo.SLOManager                // Monitors service level objectives and manages error budgets
	goroutineTracker  *goroutines.GoroutineTracker   // Tracks goroutine usage and detects potential memory leaks
	serviceDiscovery  *discovery.ServiceDiscovery    // Handles automatic service discovery

	sinks []types.Sink // Collection of configured output destinations (Loki, local files, etc.)

	// HTTP and metrics infrastructure
	httpServer      *http.Server              // Main HTTP server for API endpoints
	metricsServer   *metrics.MetricsServer    // Prometheus metrics server
	enhancedMetrics *metrics.EnhancedMetrics  // Advanced metrics collection and reporting

	// Application lifecycle management
	ctx        context.Context      // Root context for application lifecycle
	cancel     context.CancelFunc   // Cancel function for graceful shutdown
	configFile string               // Path to the configuration file
	wg         sync.WaitGroup       // WaitGroup for coordinating goroutine shutdown
}

// New creates a new App instance with the specified configuration file.
//
// This function performs complete application initialization including:
//   - Loading and validating configuration from the specified file
//   - Setting up structured logging with appropriate level and format
//   - Initializing all core components (dispatcher, monitors, processors)
//   - Configuring sinks for log output destinations
//   - Setting up enterprise features when enabled
//   - Preparing HTTP servers for API endpoints and metrics
//
// The function validates the configuration before proceeding with component
// initialization to fail fast on configuration errors.
//
// Parameters:
//   - configFile: Path to the YAML configuration file
//
// Returns:
//   - *App: Fully initialized application instance ready to start
//   - error: Configuration loading/validation error or component initialization error
//
// Example:
//
//	app, err := New("/etc/log-capturer/config.yaml")
//	if err != nil {
//		log.Fatalf("Failed to create app: %v", err)
//	}
func New(configFile string) (*App, error) {
	cfg, err := config.LoadConfig(configFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	if err := config.ValidateConfig(cfg); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	logger := logrus.New()
	level, err := logrus.ParseLevel(cfg.App.LogLevel)
	if err != nil {
		level = logrus.InfoLevel
	}
	logger.SetLevel(level)

	if cfg.App.LogFormat == "json" {
		logger.SetFormatter(&logrus.JSONFormatter{})
	} else {
		logger.SetFormatter(&logrus.TextFormatter{})
	}

	ctx, cancel := context.WithCancel(context.Background())

	app := &App{
		config:     cfg,
		logger:     logger,
		ctx:        ctx,
		cancel:     cancel,
		configFile: configFile,
	}

	// Debug: Log server configuration
	logger.WithFields(logrus.Fields{
		"server_enabled": cfg.Server.Enabled,
		"server_host":    cfg.Server.Host,
		"server_port":    cfg.Server.Port,
	}).Info("Server configuration loaded")

	if err := app.initializeComponents(); err != nil {
		return nil, fmt.Errorf("failed to initialize components: %w", err)
	}

	return app, nil
}

// initializeComponents performs sequential initialization of all application components.
//
// This method orchestrates the initialization process in the correct order to ensure
// dependencies are satisfied. The initialization sequence is:
//   1. Core services (task manager, dispatcher, processor)
//   2. Output sinks (Loki, local file, etc.)
//   3. Input monitors (file monitor, container monitor)
//   4. Auxiliary services (position manager, disk management, etc.)
//   5. Configuration reloader (if hot reload enabled)
//   6. HTTP servers (API and metrics)
//
// If any component fails to initialize, the entire initialization process is aborted
// and an error is returned with context about which component failed.
//
// Returns:
//   - error: Component initialization error with details about the failed component
func (app *App) initializeComponents() error {
	if err := app.initCoreServices(); err != nil {
		return err
	}
	if err := app.initSinks(); err != nil {
		return err
	}
	// Position manager must be initialized before monitors
	if err := app.initializePositionManager(); err != nil {
		return err
	}
	if err := app.initMonitors(); err != nil {
		return err
	}
	if err := app.initAuxiliaryServices(); err != nil {
		return err
	}
	if err := app.initReloader(); err != nil {
		return err
	}
	if err := app.initServiceDiscovery(); err != nil {
		return err
	}
	app.initHTTPServer()
	app.initMetricsServer()
	return nil
}

// Start begins the application lifecycle by starting all initialized components in the correct order.
//
// The startup sequence ensures dependencies are satisfied:
//   1. Metrics server (independent, can start first)
//   2. Output sinks (must be ready before processing begins)
//   3. Dispatcher (core orchestration, depends on sinks)
//   4. Position manager (file state tracking)
//   5. Input monitors (file and container monitoring)
//   6. Auxiliary services (disk management, resource monitoring, etc.)
//   7. Configuration reloader (hot reload capability)
//   8. Enterprise features (security, tracing, SLO monitoring)
//   9. HTTP server (API endpoints, started in background goroutine)
//
// Each component's Start method is called with the application context,
// allowing for coordinated shutdown when the context is cancelled.
//
// If any component fails to start, the startup process is aborted and
// an error is returned with details about the failed component.
//
// The HTTP server is started in a separate goroutine to avoid blocking
// the main application flow. The WaitGroup ensures proper coordination
// during shutdown.
//
// Returns:
//   - error: Component startup failure with specific component context
func (app *App) Start() error {
	app.logger.Info("Starting SSW Logs Capture Go")

	if app.metricsServer != nil {
		if err := app.metricsServer.Start(); err != nil {
			return fmt.Errorf("failed to start metrics server: %w", err)
		}
	}
	for _, sink := range app.sinks {
		if err := sink.Start(app.ctx); err != nil {
			return fmt.Errorf("failed to start sink: %w", err)
		}
	}
	if err := app.dispatcher.Start(app.ctx); err != nil {
		return fmt.Errorf("failed to start dispatcher: %w", err)
	}
	if app.positionManager != nil {
		if err := app.positionManager.Start(); err != nil {
			return fmt.Errorf("failed to start position manager: %w", err)
		}
	}
	if app.fileMonitor != nil {
		if err := app.fileMonitor.Start(app.ctx); err != nil {
			return fmt.Errorf("failed to start file monitor: %w", err)
		}
	}
	if app.containerMonitor != nil {
		if err := app.containerMonitor.Start(app.ctx); err != nil {
			return fmt.Errorf("failed to start container monitor: %w", err)
		}
	}
	if app.diskManager != nil {
		if err := app.diskManager.Start(); err != nil {
			return fmt.Errorf("failed to start disk manager: %w", err)
		}
	}
	if app.resourceMonitor != nil {
		if err := app.resourceMonitor.Start(); err != nil {
			return fmt.Errorf("failed to start resource monitor: %w", err)
		}
	}
	if app.enhancedMetrics != nil {
		if err := app.enhancedMetrics.Start(); err != nil {
			return fmt.Errorf("failed to start enhanced metrics: %w", err)
		}
	}
	if app.anomalyDetector != nil {
		if err := app.anomalyDetector.Start(); err != nil {
			return fmt.Errorf("failed to start anomaly detector: %w", err)
		}
	}
	if app.reloader != nil {
		if err := app.reloader.Start(); err != nil {
			return fmt.Errorf("failed to start config reloader: %w", err)
		}
	}

	// Start enterprise features
	if app.goroutineTracker != nil {
		if err := app.goroutineTracker.Start(app.ctx); err != nil {
			return fmt.Errorf("failed to start goroutine tracker: %w", err)
		}
	}
	if app.sloManager != nil {
		if err := app.sloManager.Start(app.ctx); err != nil {
			return fmt.Errorf("failed to start SLO manager: %w", err)
		}
	}
	if app.serviceDiscovery != nil {
		if err := app.serviceDiscovery.Start(); err != nil {
			return fmt.Errorf("failed to start service discovery: %w", err)
		}
	}

	if app.httpServer != nil {
		app.wg.Add(1)
		go func() {
			defer app.wg.Done()
			app.logger.WithField("addr", app.httpServer.Addr).Info("Starting HTTP server")
			if err := app.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				app.logger.WithError(err).Error("HTTP server error")
			}
		}()
	}

	app.logger.Info("SSW Logs Capture Go started successfully")
	return nil
}

// Stop performs graceful shutdown of all application components.
//
// The shutdown sequence is designed to:
//   1. Cancel the application context to signal all components
//   2. Shutdown HTTP server with timeout to complete in-flight requests
//   3. Stop input monitors to prevent new log entries
//   4. Stop auxiliary services and cleanup resources
//   5. Stop enterprise features with proper cleanup
//   6. Close persistent storage (disk buffer, position manager)
//   7. Stop the dispatcher and drain remaining log entries
//   8. Stop output sinks and flush any buffered data
//   9. Stop metrics server and cleanup task manager
//
// Each component's Stop method is called with appropriate timeouts
// to ensure the application doesn't hang during shutdown. Errors
// from individual components are logged but don't prevent the
// shutdown of other components.
//
// The WaitGroup ensures all background goroutines complete before
// the method returns.
//
// Returns:
//   - error: Always returns nil; individual component errors are logged
func (app *App) Stop() error {
	app.logger.Info("Stopping SSW Logs Capture Go")
	app.cancel()

	if app.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		app.httpServer.Shutdown(ctx)
	}

	if app.fileMonitor != nil {
		app.fileMonitor.Stop()
	}
	if app.containerMonitor != nil {
		app.containerMonitor.Stop()
	}
	if app.diskManager != nil {
		if err := app.diskManager.Stop(); err != nil {
			app.logger.WithError(err).Error("Failed to stop disk manager")
		}
	}
	if app.resourceMonitor != nil {
		if err := app.resourceMonitor.Stop(); err != nil {
			app.logger.WithError(err).Error("Failed to stop resource monitor")
		}
	}
	if app.enhancedMetrics != nil {
		if err := app.enhancedMetrics.Stop(); err != nil {
			app.logger.WithError(err).Error("Failed to stop enhanced metrics")
		}
	}
	if app.anomalyDetector != nil {
		if err := app.anomalyDetector.Stop(); err != nil {
			app.logger.WithError(err).Error("Failed to stop anomaly detector")
		}
	}
	if app.reloader != nil {
		if err := app.reloader.Stop(); err != nil {
			app.logger.WithError(err).Error("Failed to stop config reloader")
		}
	}

	// Stop enterprise features
	if app.goroutineTracker != nil {
		if err := app.goroutineTracker.Stop(); err != nil {
			app.logger.WithError(err).Error("Failed to stop goroutine tracker")
		}
	}
	if app.sloManager != nil {
		if err := app.sloManager.Stop(); err != nil {
			app.logger.WithError(err).Error("Failed to stop SLO manager")
		}
	}
	if app.serviceDiscovery != nil {
		if err := app.serviceDiscovery.Stop(); err != nil {
			app.logger.WithError(err).Error("Failed to stop service discovery")
		}
	}
	if app.tracingManager != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := app.tracingManager.Shutdown(ctx); err != nil {
			app.logger.WithError(err).Error("Failed to shutdown tracing manager")
		}
	}

	if app.diskBuffer != nil {
		if err := app.diskBuffer.Close(); err != nil {
			app.logger.WithError(err).Error("Failed to close disk buffer")
		}
	}
	if app.positionManager != nil {
		if err := app.positionManager.Stop(); err != nil {
			app.logger.WithError(err).Error("Failed to stop position manager")
		}
	}

	app.dispatcher.Stop()

	for _, sink := range app.sinks {
		sink.Stop()
	}

	if app.metricsServer != nil {
		app.metricsServer.Stop()
	}

	app.taskManager.Cleanup()
	app.wg.Wait()

	app.logger.Info("SSW Logs Capture Go stopped")
	return nil
}

// Run starts the application and blocks until a shutdown signal is received.
//
// This is the main entry point for running the application in daemon mode.
// It performs the following sequence:
//   1. Calls Start() to initialize and start all components
//   2. Sets up signal handling for SIGINT and SIGTERM
//   3. Blocks waiting for shutdown signals
//   4. Calls Stop() when a signal is received for graceful shutdown
//
// The signal handling allows for graceful shutdown when the process
// receives interrupt signals from the operating system or process managers
// like systemd.
//
// This method is typically called from main() after creating the App instance.
//
// Returns:
//   - error: Startup error from Start() or shutdown error from Stop()
//
// Example:
//
//	app, err := New("config.yaml")
//	if err != nil {
//		log.Fatal(err)
//	}
//	if err := app.Run(); err != nil {
//		log.Fatal(err)
//	}
func (app *App) Run() error {
	if err := app.Start(); err != nil {
		return err
	}
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	app.logger.Info("Shutdown signal received")
	return app.Stop()
}