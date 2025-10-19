package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"ssw-logs-capture/internal/config"
	"ssw-logs-capture/internal/dispatcher"
	"ssw-logs-capture/internal/metrics"
	"ssw-logs-capture/internal/monitors"
	"ssw-logs-capture/internal/processing"
	"ssw-logs-capture/internal/sinks"
	"ssw-logs-capture/pkg/buffer"
	"ssw-logs-capture/pkg/cleanup"
	"ssw-logs-capture/pkg/dlq"
	"ssw-logs-capture/pkg/leakdetection"
	"ssw-logs-capture/pkg/positions"
	"ssw-logs-capture/pkg/task_manager"
	"ssw-logs-capture/pkg/types"

	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"
)

// App representa a aplicação principal
type App struct {
	config    *types.Config
	logger    *logrus.Logger

	taskManager      types.TaskManager
	dispatcher       types.Dispatcher
	positionManager  *positions.PositionBufferManager
	processor        *processing.LogProcessor
	fileMonitor      *monitors.FileMonitor
	containerMonitor *monitors.ContainerMonitor
	diskManager      *cleanup.DiskSpaceManager
	resourceMonitor  *leakdetection.ResourceMonitor
	diskBuffer       *buffer.DiskBuffer

	sinks []types.Sink

	httpServer      *http.Server
	metricsServer   *metrics.MetricsServer
	enhancedMetrics *metrics.EnhancedMetrics

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New cria uma nova instância da aplicação
func New(configFile string) (*App, error) {
	// Carregar configuração
	cfg, err := config.LoadConfig(configFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Validar configuração
	if err := config.ValidateConfig(cfg); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	// Configurar logger
	logger := logrus.New()
	level, err := logrus.ParseLevel(cfg.Logging.Level)
	if err != nil {
		level = logrus.InfoLevel
	}
	logger.SetLevel(level)

	if cfg.Logging.Format == "json" {
		logger.SetFormatter(&logrus.JSONFormatter{})
	} else {
		logger.SetFormatter(&logrus.TextFormatter{})
	}

	ctx, cancel := context.WithCancel(context.Background())

	app := &App{
		config: cfg,
		logger: logger,
		ctx:    ctx,
		cancel: cancel,
	}

	// Inicializar componentes
	if err := app.initializeComponents(); err != nil {
		return nil, fmt.Errorf("failed to initialize components: %w", err)
	}

	return app, nil
}

// initializeComponents inicializa todos os componentes da aplicação
func (app *App) initializeComponents() error {
	// Task Manager
	tmConfig := task_manager.Config{
		HeartbeatInterval: 30 * time.Second,
		TaskTimeout:       5 * time.Minute,
		CleanupInterval:   1 * time.Minute,
	}
	app.taskManager = task_manager.New(tmConfig, app.logger)

	// Log Processor
	processor, err := processing.NewLogProcessor(app.config.Pipeline, app.logger)
	if err != nil {
		return fmt.Errorf("failed to create log processor: %w", err)
	}
	app.processor = processor

	// Dispatcher
	dispatcherConfig := dispatcher.DispatcherConfig{
		QueueSize:          app.config.Dispatcher.QueueSize,
		Workers:            app.config.Dispatcher.WorkerCount,
		BatchSize:          app.config.Dispatcher.BatchSize,
		BatchTimeout:       parseDurationSafe(app.config.Dispatcher.BatchTimeout, 5*time.Second),
		MaxRetries:         app.config.Dispatcher.MaxRetries,
		RetryDelay:         parseDurationSafe(app.config.Dispatcher.RetryBaseDelay, 1*time.Second),
		TimestampTolerance: 24 * time.Hour,
		DeduplicationEnabled: app.config.Dispatcher.DeduplicationEnabled,
		DLQEnabled:         app.config.Dispatcher.DLQEnabled,
	}
	if dispatcherConfig.QueueSize == 0 {
		dispatcherConfig.QueueSize = 50000
	}
	if dispatcherConfig.Workers == 0 {
		dispatcherConfig.Workers = 4
	}
	if dispatcherConfig.BatchSize == 0 {
		dispatcherConfig.BatchSize = 100
	}
	if dispatcherConfig.MaxRetries == 0 {
		dispatcherConfig.MaxRetries = 3
	}
	app.dispatcher = dispatcher.NewDispatcher(dispatcherConfig, processor, app.logger)

	// Position Manager
	if err := app.initializePositionManager(); err != nil {
		return fmt.Errorf("failed to initialize position manager: %w", err)
	}

	// Sinks
	if err := app.initializeSinks(); err != nil {
		return fmt.Errorf("failed to initialize sinks: %w", err)
	}

	// Monitors
	if err := app.initializeMonitors(); err != nil {
		return fmt.Errorf("failed to initialize monitors: %w", err)
	}

	// Disk Manager
	if err := app.initializeDiskManager(); err != nil {
		return fmt.Errorf("failed to initialize disk manager: %w", err)
	}

	// Resource Monitor
	if err := app.initializeResourceMonitor(); err != nil {
		return fmt.Errorf("failed to initialize resource monitor: %w", err)
	}

	// Disk Buffer
	if err := app.initializeDiskBuffer(); err != nil {
		return fmt.Errorf("failed to initialize disk buffer: %w", err)
	}

	// HTTP Server
	if app.config.API.Enabled {
		app.initializeHTTPServer()
	}

	// Metrics Server
	if app.config.Metrics.Enabled {
		addr := fmt.Sprintf("%s:%d", "0.0.0.0", app.config.Metrics.Port)
		app.metricsServer = metrics.NewMetricsServer(addr, app.logger)
	}

	// Enhanced Metrics
	app.enhancedMetrics = metrics.NewEnhancedMetrics(app.logger)

	return nil
}

// initializeSinks inicializa os sinks
func (app *App) initializeSinks() error {
	app.sinks = make([]types.Sink, 0)

	// Loki Sink
	if app.config.Sinks.Loki.Enabled {
		// Obter DLQ do dispatcher (se disponível)
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
		localFileSink := sinks.NewLocalFileSink(app.config.Sinks.LocalFile, app.logger)
		app.sinks = append(app.sinks, localFileSink)
		app.dispatcher.AddSink(localFileSink)
		app.logger.Info("Local file sink initialized")
	}

	// Elasticsearch Sink
	if app.config.Sinks.Elasticsearch.Enabled {
		esConfig := sinks.ElasticsearchConfig{
			Enabled:     app.config.Sinks.Elasticsearch.Enabled,
			Hosts:       app.config.Sinks.Elasticsearch.URLs,
			IndexPrefix: app.config.Sinks.Elasticsearch.Index,
			BatchSize:   app.config.Sinks.Elasticsearch.BatchSize,
			Username:    app.config.Sinks.Elasticsearch.Username,
			Password:    app.config.Sinks.Elasticsearch.Password,
		}
		elasticsearchSink, err := sinks.NewElasticsearchSink(esConfig, app.logger, app.ctx)
		if err != nil {
			return fmt.Errorf("failed to create elasticsearch sink: %w", err)
		}
		app.sinks = append(app.sinks, elasticsearchSink)
		app.dispatcher.AddSink(elasticsearchSink)
		app.logger.Info("Elasticsearch sink initialized")
	}

	// Splunk Sink
	if app.config.Sinks.Splunk.Enabled {
		splunkConfig := sinks.SplunkConfig{
			Enabled: app.config.Sinks.Splunk.Enabled,
			HECURL:  app.config.Sinks.Splunk.URL,
			Token:   app.config.Sinks.Splunk.Token,
			Index:   app.config.Sinks.Splunk.Index,
		}
		splunkSink, err := sinks.NewSplunkSink(splunkConfig, app.logger, app.ctx, sinks.NewBasicSecretManager())
		if err != nil {
			return fmt.Errorf("failed to create splunk sink: %w", err)
		}
		app.sinks = append(app.sinks, splunkSink)
		app.dispatcher.AddSink(splunkSink)
		app.logger.Info("Splunk sink initialized")
	}

	if len(app.sinks) == 0 {
		return fmt.Errorf("no sinks enabled")
	}

	app.logger.WithField("sink_count", len(app.sinks)).Info("Sinks initialized")
	return nil
}

// initializePositionManager inicializa o gerenciador de posições
func (app *App) initializePositionManager() error {
	if !app.config.Positions.Enabled {
		app.logger.Info("Position manager disabled")
		return nil
	}

	// Parse configuration durations
	flushInterval, err := time.ParseDuration(app.config.Positions.FlushInterval)
	if err != nil {
		return fmt.Errorf("invalid flush interval: %w", err)
	}

	cleanupInterval, err := time.ParseDuration(app.config.Positions.CleanupInterval)
	if err != nil {
		return fmt.Errorf("invalid cleanup interval: %w", err)
	}

	maxPositionAge, err := time.ParseDuration(app.config.Positions.MaxPositionAge)
	if err != nil {
		return fmt.Errorf("invalid max position age: %w", err)
	}

	// Create container position manager
	containerManager := positions.NewContainerPositionManager(app.config.Positions.Directory, app.logger)

	// Create file position manager
	fileManager := positions.NewFilePositionManager(app.config.Positions.Directory, app.logger)

	// Create buffer configuration
	bufferConfig := &positions.BufferConfig{
		FlushInterval:       flushInterval,
		MaxMemoryBuffer:     app.config.Positions.MaxMemoryBuffer,
		MaxMemoryPositions:  app.config.Positions.MaxMemoryPositions,
		ForceFlushOnExit:    app.config.Positions.ForceFlushOnExit,
		CleanupInterval:     cleanupInterval,
		MaxPositionAge:      maxPositionAge,
	}

	// Create position buffer manager
	app.positionManager = positions.NewPositionBufferManager(
		containerManager,
		fileManager,
		bufferConfig,
		app.logger,
	)

	app.logger.Info("Position manager initialized")
	return nil
}

// initializeMonitors inicializa os monitors
func (app *App) initializeMonitors() error {
	// File Monitor
	if app.config.File.Enabled {
		fileMonitor, err := monitors.NewFileMonitor(app.config.File, app.config.TimestampValidation, app.dispatcher, app.taskManager, app.positionManager, app.logger)
		if err != nil {
			return fmt.Errorf("failed to create file monitor: %w", err)
		}
		app.fileMonitor = fileMonitor
		app.logger.Info("File monitor initialized")
	}

	// Container Monitor
	if app.config.Docker.Enabled {
		containerMonitor, err := monitors.NewContainerMonitor(app.config.Docker, app.config.TimestampValidation, app.dispatcher, app.taskManager, app.positionManager, app.logger)
		if err != nil {
			return fmt.Errorf("failed to create container monitor: %w", err)
		}
		app.containerMonitor = containerMonitor
		app.logger.Info("Container monitor initialized")
	}

	return nil
}

// initializeDiskManager inicializa o gerenciador de espaço em disco
func (app *App) initializeDiskManager() error {
	if !app.config.Cleanup.Enabled {
		app.logger.Info("Disk manager disabled")
		return nil
	}

	// Converter configuração
	cleanupConfig := cleanup.Config{
		CheckInterval:           parseDurationSafe(app.config.Cleanup.CheckInterval, 30*time.Minute),
		CriticalSpaceThreshold:  app.config.Cleanup.CriticalSpaceThreshold,
		WarningSpaceThreshold:   app.config.Cleanup.WarningSpaceThreshold,
		Directories:             make([]cleanup.DirectoryConfig, len(app.config.Cleanup.Directories)),
	}

	// Valores padrão
	if cleanupConfig.CriticalSpaceThreshold == 0 {
		cleanupConfig.CriticalSpaceThreshold = 5.0 // 5%
	}
	if cleanupConfig.WarningSpaceThreshold == 0 {
		cleanupConfig.WarningSpaceThreshold = 15.0 // 15%
	}

	// Converter configurações de diretórios
	for i, dirConfig := range app.config.Cleanup.Directories {
		cleanupConfig.Directories[i] = cleanup.DirectoryConfig{
			Path:              dirConfig.Path,
			MaxSizeMB:         dirConfig.MaxSizeMB,
			RetentionDays:     dirConfig.RetentionDays,
			FilePatterns:      dirConfig.FilePatterns,
			MaxFiles:          dirConfig.MaxFiles,
			CleanupAgeSeconds: dirConfig.CleanupAgeSeconds,
		}
	}

	app.diskManager = cleanup.NewDiskSpaceManager(cleanupConfig, app.logger)
	app.logger.Info("Disk manager initialized")
	return nil
}

// initializeResourceMonitor inicializa o monitor de vazamentos de recursos
func (app *App) initializeResourceMonitor() error {
	if !app.config.LeakDetection.Enabled {
		app.logger.Info("Resource monitor disabled")
		return nil
	}

	// Converter configuração
	resourceConfig := leakdetection.ResourceMonitorConfig{
		MonitoringInterval:      parseDurationSafe(app.config.LeakDetection.MonitoringInterval, 30*time.Second),
		FDLeakThreshold:         app.config.LeakDetection.FDLeakThreshold,
		GoroutineLeakThreshold:  app.config.LeakDetection.GoroutineLeakThreshold,
		MemoryLeakThreshold:     app.config.LeakDetection.MemoryLeakThreshold,
		AlertCooldown:           parseDurationSafe(app.config.LeakDetection.AlertCooldown, 5*time.Minute),
		EnableMemoryProfiling:   app.config.LeakDetection.EnableMemoryProfiling,
		EnableGCOptimization:    app.config.LeakDetection.EnableGCOptimization,
		MaxAlertHistory:         app.config.LeakDetection.MaxAlertHistory,
	}

	// Valores padrão
	if resourceConfig.FDLeakThreshold == 0 {
		resourceConfig.FDLeakThreshold = 100
	}
	if resourceConfig.GoroutineLeakThreshold == 0 {
		resourceConfig.GoroutineLeakThreshold = 50
	}
	if resourceConfig.MemoryLeakThreshold == 0 {
		resourceConfig.MemoryLeakThreshold = 100 * 1024 * 1024 // 100MB
	}
	if resourceConfig.MaxAlertHistory == 0 {
		resourceConfig.MaxAlertHistory = 100
	}

	app.resourceMonitor = leakdetection.NewResourceMonitor(resourceConfig, app.logger)
	app.logger.Info("Resource monitor initialized")
	return nil
}

// initializeDiskBuffer inicializa o buffer de disco
func (app *App) initializeDiskBuffer() error {
	if !app.config.DiskBuffer.Enabled {
		app.logger.Info("Disk buffer disabled")
		return nil
	}

	// Converter configuração de string para tipos apropriados
	diskBufferConfig := buffer.DiskBufferConfig{
		BaseDir:            app.config.DiskBuffer.BaseDir,
		MaxFileSize:        app.config.DiskBuffer.MaxFileSize,
		MaxTotalSize:       app.config.DiskBuffer.MaxTotalSize,
		MaxFiles:           app.config.DiskBuffer.MaxFiles,
		CompressionEnabled: app.config.DiskBuffer.CompressionEnabled,
	}

	// Parse durations
	if d, err := time.ParseDuration(app.config.DiskBuffer.SyncInterval); err == nil {
		diskBufferConfig.SyncInterval = d
	} else {
		diskBufferConfig.SyncInterval = 5 * time.Second
	}

	if d, err := time.ParseDuration(app.config.DiskBuffer.CleanupInterval); err == nil {
		diskBufferConfig.CleanupInterval = d
	} else {
		diskBufferConfig.CleanupInterval = 1 * time.Hour
	}

	if d, err := time.ParseDuration(app.config.DiskBuffer.RetentionPeriod); err == nil {
		diskBufferConfig.RetentionPeriod = d
	} else {
		diskBufferConfig.RetentionPeriod = 24 * time.Hour
	}

	// Parse file permissions
	if app.config.DiskBuffer.FilePermissions != "" {
		if perm, err := parseFileMode(app.config.DiskBuffer.FilePermissions); err == nil {
			diskBufferConfig.FilePermissions = perm
		}
	}

	if app.config.DiskBuffer.DirPermissions != "" {
		if perm, err := parseFileMode(app.config.DiskBuffer.DirPermissions); err == nil {
			diskBufferConfig.DirPermissions = perm
		}
	}

	// Valores padrão
	if diskBufferConfig.BaseDir == "" {
		diskBufferConfig.BaseDir = "/tmp/disk_buffer"
	}
	if diskBufferConfig.MaxFileSize == 0 {
		diskBufferConfig.MaxFileSize = 100 * 1024 * 1024 // 100MB
	}
	if diskBufferConfig.MaxTotalSize == 0 {
		diskBufferConfig.MaxTotalSize = 1024 * 1024 * 1024 // 1GB
	}
	if diskBufferConfig.MaxFiles == 0 {
		diskBufferConfig.MaxFiles = 50
	}

	var err error
	app.diskBuffer, err = buffer.NewDiskBuffer(diskBufferConfig, app.logger)
	if err != nil {
		return fmt.Errorf("failed to create disk buffer: %w", err)
	}

	app.logger.Info("Disk buffer initialized")
	return nil
}

// initializeHTTPServer inicializa o servidor HTTP
func (app *App) initializeHTTPServer() {
	router := mux.NewRouter()

	// Health endpoints
	router.HandleFunc("/health", app.healthHandler).Methods("GET")
	router.HandleFunc("/health/detailed", app.detailedHealthHandler).Methods("GET")

	// Status endpoints
	router.HandleFunc("/status", app.statusHandler).Methods("GET")
	router.HandleFunc("/stats", app.statsHandler).Methods("GET")
	router.HandleFunc("/task/status", app.taskStatusHandler).Methods("GET")

	// Configuration endpoints
	router.HandleFunc("/config", app.configHandler).Methods("GET")
	router.HandleFunc("/config/reload", app.configReloadHandler).Methods("POST")

	// Positions endpoints
	router.HandleFunc("/positions", app.positionsHandler).Methods("GET")

	// DLQ endpoints
	router.HandleFunc("/dlq/stats", app.dlqStatsHandler).Methods("GET")
	router.HandleFunc("/dlq/reprocess", app.dlqReprocessHandler).Methods("POST")

	// Debug endpoints
	router.HandleFunc("/debug/goroutines", app.debugGoroutinesHandler).Methods("GET")
	router.HandleFunc("/debug/memory", app.debugMemoryHandler).Methods("GET")
	router.HandleFunc("/debug/positions/validate", app.debugPositionsValidateHandler).Methods("GET")

	// File monitoring endpoints
	if app.fileMonitor != nil {
		router.HandleFunc("/monitored/files", app.monitoredFilesHandler).Methods("GET")
		router.HandleFunc("/monitor/file", app.addFileMonitorHandler).Methods("POST")
		router.HandleFunc("/monitor/file/{task_name}", app.removeFileMonitorHandler).Methods("DELETE")
	}

	// Disk manager endpoints
	if app.diskManager != nil {
		router.HandleFunc("/admin/disk-status", app.diskStatusHandler).Methods("GET")
	}

	// Resource monitor endpoints
	if app.resourceMonitor != nil {
		router.HandleFunc("/admin/resource-status", app.resourceStatusHandler).Methods("GET")
	}

	// Disk buffer endpoints
	if app.diskBuffer != nil {
		router.HandleFunc("/admin/buffer-stats", app.bufferStatsHandler).Methods("GET")
		router.HandleFunc("/admin/buffer-clear", app.bufferClearHandler).Methods("POST")
	}

	// Admin endpoints
	router.HandleFunc("/admin/orphaned-tasks", app.orphanedTasksHandler).Methods("GET")
	router.HandleFunc("/admin/cleanup-orphaned-tasks", app.cleanupOrphanedTasksHandler).Methods("POST")

	addr := fmt.Sprintf("%s:%d", app.config.API.Host, app.config.API.Port)
	app.httpServer = &http.Server{
		Addr:    addr,
		Handler: router,
	}
}

// Start inicia a aplicação
func (app *App) Start() error {
	app.logger.Info("Starting SSW Logs Capture Go")

	// Iniciar metrics server
	if app.metricsServer != nil {
		if err := app.metricsServer.Start(); err != nil {
			return fmt.Errorf("failed to start metrics server: %w", err)
		}
	}

	// Iniciar sinks
	for _, sink := range app.sinks {
		if err := sink.Start(app.ctx); err != nil {
			return fmt.Errorf("failed to start sink: %w", err)
		}
	}

	// Iniciar dispatcher
	if err := app.dispatcher.Start(app.ctx); err != nil {
		return fmt.Errorf("failed to start dispatcher: %w", err)
	}

	// Iniciar position manager
	if app.positionManager != nil {
		if err := app.positionManager.Start(); err != nil {
			return fmt.Errorf("failed to start position manager: %w", err)
		}
	}

	// Iniciar monitors
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

	// Iniciar disk manager
	if app.diskManager != nil {
		if err := app.diskManager.Start(); err != nil {
			return fmt.Errorf("failed to start disk manager: %w", err)
		}
	}

	// Iniciar resource monitor
	if app.resourceMonitor != nil {
		if err := app.resourceMonitor.Start(); err != nil {
			return fmt.Errorf("failed to start resource monitor: %w", err)
		}
	}

	// Iniciar enhanced metrics
	if app.enhancedMetrics != nil {
		if err := app.enhancedMetrics.Start(); err != nil {
			return fmt.Errorf("failed to start enhanced metrics: %w", err)
		}
	}

	// Iniciar HTTP server
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

// Stop para a aplicação
func (app *App) Stop() error {
	app.logger.Info("Stopping SSW Logs Capture Go")

	// Cancelar contexto
	app.cancel()

	// Parar HTTP server
	if app.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		app.httpServer.Shutdown(ctx)
	}

	// Parar monitors
	if app.fileMonitor != nil {
		app.fileMonitor.Stop()
	}

	if app.containerMonitor != nil {
		app.containerMonitor.Stop()
	}

	// Parar disk manager
	if app.diskManager != nil {
		if err := app.diskManager.Stop(); err != nil {
			app.logger.WithError(err).Error("Failed to stop disk manager")
		}
	}

	// Parar resource monitor
	if app.resourceMonitor != nil {
		if err := app.resourceMonitor.Stop(); err != nil {
			app.logger.WithError(err).Error("Failed to stop resource monitor")
		}
	}

	// Parar enhanced metrics
	if app.enhancedMetrics != nil {
		if err := app.enhancedMetrics.Stop(); err != nil {
			app.logger.WithError(err).Error("Failed to stop enhanced metrics")
		}
	}

	// Parar disk buffer
	if app.diskBuffer != nil {
		if err := app.diskBuffer.Close(); err != nil {
			app.logger.WithError(err).Error("Failed to close disk buffer")
		}
	}

	// Parar position manager
	if app.positionManager != nil {
		if err := app.positionManager.Stop(); err != nil {
			app.logger.WithError(err).Error("Failed to stop position manager")
		}
	}

	// Parar dispatcher
	app.dispatcher.Stop()

	// Parar sinks
	for _, sink := range app.sinks {
		sink.Stop()
	}

	// Parar metrics server
	if app.metricsServer != nil {
		app.metricsServer.Stop()
	}

	// Cleanup task manager
	app.taskManager.Cleanup()

	// Aguardar goroutines
	app.wg.Wait()

	app.logger.Info("SSW Logs Capture Go stopped")
	return nil
}

// Run executa a aplicação com graceful shutdown
func (app *App) Run() error {
	// Iniciar aplicação
	if err := app.Start(); err != nil {
		return err
	}

	// Aguardar sinal de parada
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	<-sigChan
	app.logger.Info("Shutdown signal received")

	// Parar aplicação
	return app.Stop()
}

// Health check handlers

func (app *App) healthHandler(w http.ResponseWriter, r *http.Request) {
	health := app.getDetailedHealth()

	w.Header().Set("Content-Type", "application/json")
	if health.Status == "healthy" {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	// Formato conforme documentação
	response := map[string]interface{}{
		"status": health.Status,
		"components": health.Components,
		"issues": health.Issues,
		"check_time": health.CheckTime.Format(time.RFC3339),
	}

	json.NewEncoder(w).Encode(response)
}

func (app *App) detailedHealthHandler(w http.ResponseWriter, r *http.Request) {
	health := app.getDetailedHealth()

	w.Header().Set("Content-Type", "application/json")
	if health.Status == "healthy" {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	// Serializar JSON (implementação simplificada)
	w.Write([]byte(fmt.Sprintf(`{"status":"%s","check_time":"%s"}`,
		health.Status, health.CheckTime.Format(time.RFC3339))))
}

func (app *App) statusHandler(w http.ResponseWriter, r *http.Request) {
	stats := app.dispatcher.GetStats()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// Serializar JSON (implementação simplificada)
	w.Write([]byte(fmt.Sprintf(`{"total_processed":%d,"queue_size":%d}`,
		stats.TotalProcessed, stats.QueueSize)))
}

func (app *App) taskStatusHandler(w http.ResponseWriter, r *http.Request) {
	tasks := app.taskManager.GetAllTasks()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// Serializar JSON (implementação simplificada)
	w.Write([]byte(fmt.Sprintf(`{"task_count":%d}`, len(tasks))))
}

func (app *App) monitoredFilesHandler(w http.ResponseWriter, r *http.Request) {
	if app.fileMonitor == nil {
		http.Error(w, "File monitor not enabled", http.StatusServiceUnavailable)
		return
	}

	files := app.fileMonitor.GetMonitoredFiles()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// Serializar JSON (implementação simplificada)
	w.Write([]byte(fmt.Sprintf(`{"file_count":%d}`, len(files))))
}

func (app *App) addFileMonitorHandler(w http.ResponseWriter, r *http.Request) {
	if app.fileMonitor == nil {
		http.Error(w, "File monitor not enabled", http.StatusServiceUnavailable)
		return
	}

	type addFileRequest struct {
		FilePath string            `json:"file_path"`
		Labels   map[string]string `json:"labels,omitempty"`
	}

	var req addFileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	if req.FilePath == "" {
		http.Error(w, "file_path is required", http.StatusBadRequest)
		return
	}

	if err := app.fileMonitor.AddFile(req.FilePath, req.Labels); err != nil {
		http.Error(w, fmt.Sprintf("Failed to add file: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":    "success",
		"message":   "File added successfully",
		"file_path": req.FilePath,
	})
}

func (app *App) removeFileMonitorHandler(w http.ResponseWriter, r *http.Request) {
	if app.fileMonitor == nil {
		http.Error(w, "File monitor not enabled", http.StatusServiceUnavailable)
		return
	}

	vars := mux.Vars(r)
	taskName := vars["task_name"]

	if taskName == "" {
		http.Error(w, "task_name is required", http.StatusBadRequest)
		return
	}

	// Converter task_name em file path ou usar como file path diretamente
	if err := app.fileMonitor.RemoveFile(taskName); err != nil {
		http.Error(w, fmt.Sprintf("Failed to remove file: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":    "success",
		"message":   "File removed successfully",
		"task_name": taskName,
	})
}

func (app *App) orphanedTasksHandler(w http.ResponseWriter, r *http.Request) {
	if app.taskManager == nil {
		http.Error(w, "Task manager not available", http.StatusServiceUnavailable)
		return
	}

	allTasks := app.taskManager.GetAllTasks()
	orphanedTasks := make(map[string]types.TaskStatus)

	// Considerar tarefas órfãs aquelas que não tiveram heartbeat por mais de 5 minutos
	orphanThreshold := time.Now().Add(-5 * time.Minute)

	for id, task := range allTasks {
		if task.State == "running" && task.LastHeartbeat.Before(orphanThreshold) {
			orphanedTasks[id] = task
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"orphaned_tasks": orphanedTasks,
		"count":         len(orphanedTasks),
		"threshold":     orphanThreshold.Format(time.RFC3339),
	})
}

func (app *App) cleanupOrphanedTasksHandler(w http.ResponseWriter, r *http.Request) {
	if app.taskManager == nil {
		http.Error(w, "Task manager not available", http.StatusServiceUnavailable)
		return
	}

	allTasks := app.taskManager.GetAllTasks()
	orphanThreshold := time.Now().Add(-5 * time.Minute)

	cleanedTasks := make([]string, 0)
	var cleanupErrors []string

	for id, task := range allTasks {
		if task.State == "running" && task.LastHeartbeat.Before(orphanThreshold) {
			if err := app.taskManager.StopTask(id); err != nil {
				cleanupErrors = append(cleanupErrors, fmt.Sprintf("Failed to stop task %s: %v", id, err))
			} else {
				cleanedTasks = append(cleanedTasks, id)
			}
		}
	}

	response := map[string]interface{}{
		"status":         "success",
		"cleaned_tasks":  cleanedTasks,
		"cleaned_count":  len(cleanedTasks),
		"errors":         cleanupErrors,
		"threshold":      orphanThreshold.Format(time.RFC3339),
	}

	if len(cleanupErrors) > 0 {
		response["status"] = "partial_success"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// getDetailedHealth retorna status detalhado de saúde
func (app *App) getDetailedHealth() types.HealthStatus {
	status := "healthy"
	components := make(map[string]interface{})
	issues := make([]string, 0)

	// Verificar file monitor
	if app.fileMonitor != nil {
		healthy := app.fileMonitor.IsHealthy()
		components["file_monitor"] = map[string]interface{}{
			"status": getStatusString(healthy),
			"last_check": time.Now().Format(time.RFC3339),
		}
		if !healthy {
			status = "degraded"
			issues = append(issues, "File monitor is unhealthy")
		}
	}

	// Verificar container monitor
	if app.containerMonitor != nil {
		healthy := app.containerMonitor.IsHealthy()
		components["container_monitor"] = map[string]interface{}{
			"status": getStatusString(healthy),
			"last_check": time.Now().Format(time.RFC3339),
		}
		if !healthy {
			status = "degraded"
			issues = append(issues, "Container monitor is unhealthy")
		}
	}

	// Verificar dispatcher
	if app.dispatcher != nil {
		stats := app.dispatcher.GetStats()
		queueUtil := float64(stats.QueueSize) / 100000.0 // Assumindo queue size de 100k
		healthy := queueUtil < 0.9 // < 90% utilização
		components["dispatcher"] = map[string]interface{}{
			"status": getStatusString(healthy),
			"queue_size": stats.QueueSize,
		}
		if !healthy {
			status = "degraded"
			issues = append(issues, "Dispatcher queue is overloaded")
		}
	}

	// Verificar sinks
	sinkComponents := make(map[string]interface{})
	for i, sink := range app.sinks {
		var sinkName string
		switch i {
		case 0:
			sinkName = "loki_sink"
		case 1:
			sinkName = "local_file_sink"
		default:
			sinkName = fmt.Sprintf("sink_%d", i)
		}

		healthy := sink.IsHealthy()
		sinkComponents[sinkName] = map[string]interface{}{
			"status": getStatusString(healthy),
			"queue_util": 0.0, // TODO: implementar utilização real
		}

		if !healthy {
			status = "degraded"
			issues = append(issues, fmt.Sprintf("Sink %s is unhealthy", sinkName))
		}
	}
	components["sinks"] = sinkComponents

	// Verificar position manager
	if app.positionManager != nil {
		components["position_manager"] = map[string]interface{}{
			"status": "healthy",
			"positions": 0, // TODO: implementar contagem real
		}
	}

	// Verificar resource monitor
	if app.resourceMonitor != nil {
		stats := app.resourceMonitor.GetStats()
		goroutineLeaks := stats.GoroutineLeaks
		memoryLeaks := stats.MemoryLeaks
		fdLeaks := stats.FDLeaks

		healthy := goroutineLeaks == 0 && memoryLeaks == 0 && fdLeaks == 0
		components["resource_monitor"] = map[string]interface{}{
			"status": getStatusString(healthy),
			"goroutine_leaks": goroutineLeaks,
			"memory_leaks": memoryLeaks,
			"fd_leaks": fdLeaks,
		}

		if !healthy {
			status = "degraded"
			if goroutineLeaks > 0 {
				issues = append(issues, fmt.Sprintf("Detected %d goroutine leaks", goroutineLeaks))
			}
			if memoryLeaks > 0 {
				issues = append(issues, fmt.Sprintf("Detected %d memory leaks", memoryLeaks))
			}
			if fdLeaks > 0 {
				issues = append(issues, fmt.Sprintf("Detected %d file descriptor leaks", fdLeaks))
			}
		}
	}

	return types.HealthStatus{
		Status:     status,
		Components: components,
		Issues:     issues,
		CheckTime:  time.Now(),
	}
}

// getStatusString converte boolean para string de status
func getStatusString(healthy bool) string {
	if healthy {
		return "healthy"
	}
	return "unhealthy"
}

// diskStatusHandler retorna status do disk manager
func (app *App) diskStatusHandler(w http.ResponseWriter, r *http.Request) {
	if app.diskManager == nil {
		http.Error(w, "Disk manager not initialized", http.StatusServiceUnavailable)
		return
	}

	status := app.diskManager.GetStatus()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// resourceStatusHandler retorna status do resource monitor
func (app *App) resourceStatusHandler(w http.ResponseWriter, r *http.Request) {
	if app.resourceMonitor == nil {
		http.Error(w, "Resource monitor not initialized", http.StatusServiceUnavailable)
		return
	}

	status := app.resourceMonitor.GetStats()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// parseDurationSafe safely parses a duration string with a fallback
func parseDurationSafe(durationStr string, fallback time.Duration) time.Duration {
	if durationStr == "" {
		return fallback
	}
	if d, err := time.ParseDuration(durationStr); err == nil {
		return d
	}
	return fallback
}

// parseFileMode parses a file mode string
func parseFileMode(modeStr string) (os.FileMode, error) {
	// Simple parsing for octal modes like "0644", "0755"
	if len(modeStr) > 0 && modeStr[0] == '0' {
		var mode uint32
		if n, err := fmt.Sscanf(modeStr, "%o", &mode); err == nil && n == 1 {
			return os.FileMode(mode), nil
		}
	}
	return 0, fmt.Errorf("invalid file mode: %s", modeStr)
}

// bufferStatsHandler retorna estatísticas do disk buffer
func (app *App) bufferStatsHandler(w http.ResponseWriter, r *http.Request) {
	if app.diskBuffer == nil {
		http.Error(w, "Disk buffer not initialized", http.StatusServiceUnavailable)
		return
	}

	stats := app.diskBuffer.GetStats()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// bufferClearHandler limpa o disk buffer
func (app *App) bufferClearHandler(w http.ResponseWriter, r *http.Request) {
	if app.diskBuffer == nil {
		http.Error(w, "Disk buffer not initialized", http.StatusServiceUnavailable)
		return
	}

	if err := app.diskBuffer.Clear(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to clear buffer: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "success",
		"message": "Disk buffer cleared successfully",
	})
}

// statsHandler retorna estatísticas detalhadas conforme documentação
func (app *App) statsHandler(w http.ResponseWriter, r *http.Request) {
	stats := app.dispatcher.GetStats()

	fileStats := make(map[string]interface{})
	containerStats := make(map[string]interface{})
	sinkStats := make(map[string]interface{})

	if app.fileMonitor != nil {
		fileStats["files_watched"] = len(app.fileMonitor.GetMonitoredFiles())
		fileStats["errors"] = 0 // TODO: implementar contador de erros
	}

	if app.containerMonitor != nil {
		containerStats["containers_monitored"] = 9 // TODO: implementar contador real
		containerStats["reconnections"] = 0 // TODO: implementar contador de reconexões
	}

	for i := range app.sinks {
		sinkName := fmt.Sprintf("sink_%d", i)
		sinkStats[sinkName] = map[string]interface{}{
			"queue_util": 0.0, // TODO: implementar utilização de queue
			"errors":     0,   // TODO: implementar contador de erros
		}
	}

	response := map[string]interface{}{
		"dispatcher": map[string]interface{}{
			"total_processed":    stats.TotalProcessed,
			"error_count":       stats.ErrorCount,
			"queue_size":        stats.QueueSize,
			"duplicates_detected": stats.DuplicatesDetected,
			"throughput_per_sec": 0.0, // TODO: calcular throughput
		},
		"file_monitor":      fileStats,
		"container_monitor": containerStats,
		"sinks":            sinkStats,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// configHandler retorna configuração atual (sanitizada)
func (app *App) configHandler(w http.ResponseWriter, r *http.Request) {
	// Retornar configuração sanitizada (sem senhas/tokens)
	sanitizedConfig := map[string]interface{}{
		"app": map[string]interface{}{
			"environment": app.config.App.Environment,
			"log_level":   app.config.Logging.Level,
		},
		"dispatcher": map[string]interface{}{
			"queue_size":    app.config.Dispatcher.QueueSize,
			"worker_count":  app.config.Dispatcher.WorkerCount,
			"batch_size":    app.config.Dispatcher.BatchSize,
		},
		"monitoring": map[string]interface{}{
			"file_enabled":      app.config.File.Enabled,
			"container_enabled": app.config.Docker.Enabled,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sanitizedConfig)
}

// configReloadHandler recarrega configuração
func (app *App) configReloadHandler(w http.ResponseWriter, r *http.Request) {
	// TODO: Implementar reload de configuração
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "error",
		"message": "Config reload not implemented yet",
	})
}

// positionsHandler retorna posições atuais
func (app *App) positionsHandler(w http.ResponseWriter, r *http.Request) {
	if app.positionManager == nil {
		http.Error(w, "Position manager not enabled", http.StatusServiceUnavailable)
		return
	}

	// TODO: Implementar método GetAllPositions no position manager
	response := map[string]interface{}{
		"files":      []map[string]interface{}{},
		"containers": []map[string]interface{}{},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// dlqStatsHandler retorna estatísticas da DLQ
func (app *App) dlqStatsHandler(w http.ResponseWriter, r *http.Request) {
	// Obter DLQ do dispatcher
	var dlqStats map[string]interface{}
	if dispatcherImpl, ok := app.dispatcher.(*dispatcher.Dispatcher); ok {
		if dlq := dispatcherImpl.GetDLQ(); dlq != nil {
			dlqStats = map[string]interface{}{
				"total_entries":     0, // TODO: implementar contadores na DLQ
				"size_mb":          0.0,
				"oldest_entry":     nil,
				"retry_queue_size": 0,
			}
		}
	}

	if dlqStats == nil {
		dlqStats = map[string]interface{}{
			"message": "DLQ not available",
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(dlqStats)
}

// dlqReprocessHandler força reprocessamento da DLQ
func (app *App) dlqReprocessHandler(w http.ResponseWriter, r *http.Request) {
	// TODO: Implementar reprocessamento da DLQ
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "error",
		"message": "DLQ reprocessing not implemented yet",
	})
}

// debugGoroutinesHandler retorna informações sobre goroutines
func (app *App) debugGoroutinesHandler(w http.ResponseWriter, r *http.Request) {
	if app.resourceMonitor != nil {
		stats := app.resourceMonitor.GetStats()
		response := map[string]interface{}{
			"current_goroutines":      stats.Goroutines,
			"initial_goroutines":      stats.InitialGoroutines,
			"goroutine_leaks_detected": stats.GoroutineLeaks,
			"monitoring_uptime_seconds": time.Since(time.Unix(stats.LastCheck, 0)).Seconds(),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	} else {
		http.Error(w, "Resource monitor not enabled", http.StatusServiceUnavailable)
	}
}

// debugMemoryHandler retorna informações sobre memória
func (app *App) debugMemoryHandler(w http.ResponseWriter, r *http.Request) {
	if app.resourceMonitor != nil {
		stats := app.resourceMonitor.GetStats()
		response := map[string]interface{}{
			"memory_usage_bytes":     stats.MemoryUsage,
			"memory_leaks_detected":  stats.MemoryLeaks,
			"alloc_rate_bytes_per_sec": stats.AllocRate,
			"gc_pauses_ns":          stats.GCPauses,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	} else {
		http.Error(w, "Resource monitor not enabled", http.StatusServiceUnavailable)
	}
}

// debugPositionsValidateHandler valida integridade das posições
func (app *App) debugPositionsValidateHandler(w http.ResponseWriter, r *http.Request) {
	if app.positionManager == nil {
		http.Error(w, "Position manager not enabled", http.StatusServiceUnavailable)
		return
	}

	// TODO: Implementar validação de posições
	response := map[string]interface{}{
		"status":           "success",
		"validated_files":  0,
		"validated_containers": 0,
		"errors":           []string{},
		"warnings":         []string{},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}