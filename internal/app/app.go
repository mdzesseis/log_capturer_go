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

	sinks []types.Sink

	httpServer    *http.Server
	metricsServer *metrics.MetricsServer

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

	// HTTP Server
	if app.config.API.Enabled {
		app.initializeHTTPServer()
	}

	// Metrics Server
	if app.config.Metrics.Enabled {
		addr := fmt.Sprintf("%s:%d", "0.0.0.0", app.config.Metrics.Port)
		app.metricsServer = metrics.NewMetricsServer(addr, app.logger)
	}

	return nil
}

// initializeSinks inicializa os sinks
func (app *App) initializeSinks() error {
	app.sinks = make([]types.Sink, 0)

	// Loki Sink
	if app.config.Sinks.Loki.Enabled {
		lokiSink := sinks.NewLokiSink(app.config.Sinks.Loki, app.logger)
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
		FlushInterval:    flushInterval,
		MaxMemoryBuffer:  app.config.Positions.MaxMemoryBuffer,
		ForceFlushOnExit: app.config.Positions.ForceFlushOnExit,
		CleanupInterval:  cleanupInterval,
		MaxPositionAge:   maxPositionAge,
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
		fileMonitor, err := monitors.NewFileMonitor(app.config.File, app.dispatcher, app.taskManager, app.positionManager, app.logger)
		if err != nil {
			return fmt.Errorf("failed to create file monitor: %w", err)
		}
		app.fileMonitor = fileMonitor
		app.logger.Info("File monitor initialized")
	}

	// Container Monitor
	if app.config.Docker.Enabled {
		containerMonitor, err := monitors.NewContainerMonitor(app.config.Docker, app.dispatcher, app.taskManager, app.positionManager, app.logger)
		if err != nil {
			return fmt.Errorf("failed to create container monitor: %w", err)
		}
		app.containerMonitor = containerMonitor
		app.logger.Info("Container monitor initialized")
	}

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
	router.HandleFunc("/task/status", app.taskStatusHandler).Methods("GET")

	// File monitoring endpoints
	if app.fileMonitor != nil {
		router.HandleFunc("/monitored/files", app.monitoredFilesHandler).Methods("GET")
		router.HandleFunc("/monitor/file", app.addFileMonitorHandler).Methods("POST")
		router.HandleFunc("/monitor/file/{task_name}", app.removeFileMonitorHandler).Methods("DELETE")
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
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"healthy"}`))
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

	// Verificar sinks
	sinkStatus := make(map[string]interface{})
	for i, sink := range app.sinks {
		sinkName := fmt.Sprintf("sink_%d", i)
		healthy := sink.IsHealthy()
		sinkStatus[sinkName] = map[string]interface{}{
			"healthy": healthy,
		}

		if !healthy {
			status = "degraded"
			issues = append(issues, fmt.Sprintf("Sink %s is unhealthy", sinkName))
		}
	}
	components["sinks"] = sinkStatus

	// Verificar monitors
	monitorStatus := make(map[string]interface{})
	if app.fileMonitor != nil {
		healthy := app.fileMonitor.IsHealthy()
		monitorStatus["file"] = map[string]interface{}{
			"healthy": healthy,
		}
		if !healthy {
			status = "degraded"
			issues = append(issues, "File monitor is unhealthy")
		}
	}

	if app.containerMonitor != nil {
		healthy := app.containerMonitor.IsHealthy()
		monitorStatus["container"] = map[string]interface{}{
			"healthy": healthy,
		}
		if !healthy {
			status = "degraded"
			issues = append(issues, "Container monitor is unhealthy")
		}
	}
	components["monitors"] = monitorStatus

	return types.HealthStatus{
		Status:     status,
		Components: components,
		Issues:     issues,
		CheckTime:  time.Now(),
	}
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