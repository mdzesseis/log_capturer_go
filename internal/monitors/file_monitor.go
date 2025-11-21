// Unificado FileMonitor (file_monitor1.go + métricas e labels de file_monitor.go/file_monitor2.go)
// Este arquivo contém:
// ✔ descoberta avançada de arquivos (pipeline + diretórios + filtros)
// ✔ métricas completas (processamento, tailer, erros)
// ✔ labels combinados (file_name + labels do monitor original)
// ✔ arquitetura anti-leak com tail nxadm

package monitors

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/nxadm/tail"
	"github.com/sirupsen/logrus"

	"ssw-logs-capture/internal/metrics"
	"ssw-logs-capture/pkg/positions"
	"ssw-logs-capture/pkg/types"
)

// ===================================================================================
// CONFIG
// ===================================================================================
const (
	defaultNumWorkers      = 4
	defaultMaxJobsInQueue  = 1000
	defaultShutdownTimeout = 10 * time.Second
)

// ===================================================================================
// WORKER POOL
// ===================================================================================
type workerPool struct {
	jobsChannel chan *workerJob
	wg          sync.WaitGroup
	dispatcher  types.Dispatcher
	logger      *logrus.Logger
}

type workerJob struct {
	line       string
	sourcePath string
	timestamp  time.Time
}

func newWorkerPool(ctx context.Context, numWorkers int, queueSize int, dispatcher types.Dispatcher, logger *logrus.Logger) *workerPool {
	pool := &workerPool{
		jobsChannel: make(chan *workerJob, queueSize),
		dispatcher:  dispatcher,
		logger:      logger,
	}

	pool.wg.Add(numWorkers)
	for i := 0; i < numWorkers; i++ {
		go pool.worker(ctx, i)
	}

	logger.WithFields(logrus.Fields{
		"component":   "file_monitor",
		"worker_pool": "started",
		"num_workers": numWorkers,
		"queue_size":  queueSize,
	}).Info("Worker pool iniciado")

	return pool
}

func (p *workerPool) worker(ctx context.Context, id int) {
	defer p.wg.Done()

	p.logger.WithFields(logrus.Fields{
		"component": "file_monitor",
		"worker_id": id,
	}).Debug("Worker iniciado e aguardando jobs")

	for job := range p.jobsChannel {
		select {
		case <-ctx.Done():
			p.logger.WithField("worker_id", id).Debug("Desligamento detectado, saindo")
			return
		default:
		}

		if err := p.processLogLine(ctx, job); err != nil {
			p.logger.WithError(err).WithFields(logrus.Fields{
				"worker_id":   id,
				"source_path": job.sourcePath,
			}).Warn("Erro ao processar linha de log")

			metrics.ErrorsTotal.WithLabelValues("file_monitor", "process_log_line").Inc()
			// Track failed processing (no retry queue implemented - entry is dropped)
			metrics.FileMonitorRetryFailed.WithLabelValues("file_monitor").Inc()
			metrics.FileMonitorDropsTotal.WithLabelValues("file_monitor", "process_error").Inc()
		}
	}

	p.logger.WithField("worker_id", id).Debug("Canal de jobs fechado. Encerrando worker")
}

func (p *workerPool) processLogLine(ctx context.Context, job *workerJob) error {
	labels := map[string]string{
		"source":    "file_monitor",
		"file_path": job.sourcePath,
		"file_name": filepath.Base(job.sourcePath),
		"job":       "log_capturer",
	}

	if err := p.dispatcher.Handle(ctx, "file", job.sourcePath, job.line, labels); err != nil {
		return fmt.Errorf("failed to send to dispatcher: %w", err)
	}

	// Métrica: log processado
	metrics.LogsProcessedTotal.WithLabelValues("file", job.sourcePath, "file_monitor").Inc()

	return nil
}

func (p *workerPool) close() {
	close(p.jobsChannel)
	p.wg.Wait()
}

// ===================================================================================
// TAILER
// ===================================================================================
type logTailer struct {
	tailer     *tail.Tail
	pool       *workerPool
	wg         sync.WaitGroup
	sourcePath string
	logger     *logrus.Logger
}

func newLogTailer(ctx context.Context, path string, pool *workerPool, config types.FileMonitorServiceConfig, logger *logrus.Logger) (*logTailer, error) {
	tailConfig := tail.Config{
		Follow:   true,
		ReOpen:   true,
		Location: determineSeekPosition(config),
		Poll:     false,
	}

	// Track when old logs are being ignored (seeking to end)
	if config.IgnoreOldTimestamps {
		metrics.FileMonitorOldLogsIgnored.WithLabelValues("file_monitor", path).Inc()
	}

	t, err := tail.TailFile(path, tailConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to tail file %s: %w", path, err)
	}

	lt := &logTailer{
		tailer:     t,
		pool:       pool,
		sourcePath: path,
		logger:     logger,
	}

	lt.wg.Add(1)
	go lt.run(ctx)

	logger.WithFields(logrus.Fields{
		"component":   "file_monitor",
		"file_path":   path,
		"follow":      tailConfig.Follow,
		"reopen":      tailConfig.ReOpen,
		"use_polling": tailConfig.Poll,
	}).Info("Tailer iniciado para arquivo")

	return lt, nil
}

func determineSeekPosition(config types.FileMonitorServiceConfig) *tail.SeekInfo {
	if config.IgnoreOldTimestamps {
		// Note: FileMonitorOldLogsIgnored will be incremented per file in newLogTailer
		return &tail.SeekInfo{Offset: 0, Whence: io.SeekEnd}
	}

	switch config.SeekStrategy {
	case "end":
		return &tail.SeekInfo{Offset: 0, Whence: io.SeekEnd}
	case "recent":
		offset := int64(config.SeekRecentBytes)
		if offset == 0 {
			offset = 1048576 // 1MB
		}
		return &tail.SeekInfo{Offset: -offset, Whence: io.SeekEnd}
	case "beginning":
		fallthrough
	default:
		return &tail.SeekInfo{Offset: 0, Whence: io.SeekStart}
	}
}

func (lt *logTailer) run(ctx context.Context) {
	defer lt.wg.Done()
	defer lt.tailer.Cleanup()

	lt.logger.WithFields(logrus.Fields{
		"component": "file_monitor",
		"file_path": lt.tailer.Filename,
	}).Debug("Iniciando tailing de arquivo")

	for {
		select {
		case <-ctx.Done():
			lt.logger.WithField("file_path", lt.tailer.Filename).Debug("Sinal de desligamento recebido")

			if err := lt.tailer.Stop(); err != nil {
				lt.logger.WithError(err).Warn("Erro ao parar tailer")
			}

			lt.logger.WithField("file_path", lt.tailer.Filename).Debug("Tailer parado. Encerrando goroutine")
			return

		case line, ok := <-lt.tailer.Lines:
			if !ok {
				lt.logger.WithField("file_path", lt.tailer.Filename).Debug("Canal 'Lines' fechado")

				if err := lt.tailer.Err(); err != nil {
					lt.logger.WithError(err).Warn("Erro final do tailer")
					metrics.ErrorsTotal.WithLabelValues("file_monitor", "tailer_error").Inc()
				}
				return
			}

			if line.Err != nil {
				lt.logger.WithError(line.Err).Warn("Erro de linha")
				metrics.ErrorsTotal.WithLabelValues("file_monitor", "line_error").Inc()
				continue
			}

			job := &workerJob{
				line:       line.Text,
				sourcePath: lt.sourcePath,
				timestamp:  line.Time,
			}

			select {
			case <-ctx.Done():
				lt.logger.Debug("Desligamento durante envio ao pool. Descartando última linha")
				metrics.FileMonitorDropsTotal.WithLabelValues("file_monitor", "shutdown").Inc()
				return
			case lt.pool.jobsChannel <- job:
				// enviado com sucesso
			default:
				// Queue is full, drop the log entry
				lt.logger.WithFields(logrus.Fields{
					"file_path": lt.sourcePath,
				}).Warn("Job queue full, dropping log entry")
				metrics.FileMonitorDropsTotal.WithLabelValues("file_monitor", "queue_full").Inc()
			}
		}
	}
}

func (lt *logTailer) stop() {
	lt.wg.Wait()
}

// ===================================================================================
// FILE MONITOR
// ===================================================================================
type FileMonitor struct {
	workerPool *workerPool
	tailers    map[string]*logTailer // path -> tailer
	tailersMux sync.RWMutex

	ctxMux sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	config types.FileMonitorServiceConfig

	dispatcher      types.Dispatcher
	taskManager     types.TaskManager
	positionManager *positions.PositionBufferManager
	logger          *logrus.Logger

	running    bool
	runningMux sync.RWMutex
}

func NewFileMonitor(config types.FileMonitorServiceConfig, dispatcher types.Dispatcher, taskManager types.TaskManager, positionManager *positions.PositionBufferManager, logger *logrus.Logger) (*FileMonitor, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger é obrigatório")
	}
	if dispatcher == nil {
		return nil, fmt.Errorf("dispatcher é obrigatório")
	}

	return &FileMonitor{
		config:          config,
		dispatcher:      dispatcher,
		taskManager:     taskManager,
		positionManager: positionManager,
		logger:          logger,
		tailers:         make(map[string]*logTailer),
		running:         false,
	}, nil
}

func (fm *FileMonitor) Start(ctx context.Context) error {
	fm.runningMux.Lock()
	if fm.running {
		fm.runningMux.Unlock()
		return fmt.Errorf("file monitor já está em execução")
	}
	fm.running = true
	fm.runningMux.Unlock()

	fm.logger.WithFields(logrus.Fields{
		"component":    "file_monitor",
		"approach":     "nxadm-tail-producer-consumer",
		"worker_count": defaultNumWorkers,
		"queue_size":   defaultMaxJobsInQueue,
	}).Info("Iniciando File Monitor com arquitetura anti-leak")

	fm.ctxMux.Lock()
	fm.ctx, fm.cancel = context.WithCancel(ctx)
	fm.ctxMux.Unlock()

	fm.workerPool = newWorkerPool(fm.ctx, defaultNumWorkers, defaultMaxJobsInQueue, fm.dispatcher, fm.logger)

	if err := fm.startTailers(); err != nil {
		fm.cancel()
		return fmt.Errorf("failed to start tailers: %w", err)
	}

	fm.logger.Info("File Monitor iniciado com sucesso")
	return nil
}

func (fm *FileMonitor) startTailers() error {
	filePaths, err := fm.resolveFilePaths()
	if err != nil {
		return fmt.Errorf("failed to resolve file paths: %w", err)
	}

	if len(filePaths) == 0 {
		fm.logger.Warn("Nenhum arquivo para monitorar. File monitor iniciado mas inativo")
		return nil
	}

	fm.logger.WithField("file_count", len(filePaths)).Info("Iniciando tailers para arquivos")

	fm.tailersMux.Lock()
	defer fm.tailersMux.Unlock()

	for _, path := range filePaths {
		tailer, err := newLogTailer(fm.ctx, path, fm.workerPool, fm.config, fm.logger)
		if err != nil {
			fm.logger.WithError(err).WithField("file_path", path).Warn("Falha ao iniciar tailer")
			metrics.ErrorsTotal.WithLabelValues("file_monitor", "start_tailer").Inc()
			continue
		}

		fm.tailers[path] = tailer

		fm.logger.WithField("file_path", path).Debug("Tailer iniciado")
	}

	if len(fm.tailers) == 0 {
		return fmt.Errorf("failed to start any tailer")
	}

	// Update total files monitored metric
	metrics.UpdateTotalFilesMonitored(len(fm.tailers))

	fm.logger.WithField("active_tailers", len(fm.tailers)).Info("Tailers iniciados com sucesso")
	return nil
}

// resolveFilePaths: implementação avançada (pipeline, directories, watch_directories)
func (fm *FileMonitor) resolveFilePaths() ([]string, error) {
	// Dedup
	seen := make(map[string]struct{}, 32)
	add := func(p string, out *[]string) {
		if p == "" {
			return
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		*out = append(*out, p)
	}

	paths := make([]string, 0, 32)

	// 1) arquivos explícitos via pipeline
	if fm.config.PipelineConfig != nil {
		if rawFiles, ok := fm.config.PipelineConfig["files"].([]interface{}); ok {
			for _, rf := range rawFiles {
				m, ok := rf.(map[interface{}]interface{})
				if !ok {
					continue
				}
				enabled := true
				if v, ok := m["enabled"]; ok {
					if b, ok2 := v.(bool); ok2 {
						enabled = b
					}
				}
				if !enabled {
					continue
				}
				if p, ok := m["path"].(string); ok && p != "" {
					add(p, &paths)
				}
			}
		}

		if len(paths) > 0 {
			// filtra arquivos existentes
			existing := make([]string, 0, len(paths))
			for _, p := range paths {
				if st, err := os.Stat(p); err == nil && !st.IsDir() {
					existing = append(existing, p)
				} else {
					fm.logger.WithField("file_path", p).Warn("Explicit file missing, skipping")
				}
			}
			if len(existing) > 0 {
				fm.logger.WithField("files", existing).Info("Using explicit existing files from file_pipeline (precedence 1)")
				return existing, nil
			}
			fm.logger.Warn("No explicit files exist; evaluating directory entries")
		}
	}

	// 2) diretórios via pipeline
	if fm.config.PipelineConfig != nil {
		expanded := fm.expandPipelineDirectories()
		if len(expanded) > 0 {
			for _, p := range expanded {
				add(p, &paths)
			}
			fm.logger.WithField("files", len(paths)).Info("Using directories from file_pipeline (precedence 2)")
			return paths, nil
		}
	}

	// 3) watch_directories
	if len(fm.config.WatchDirectories) > 0 {
		for _, p := range fm.config.WatchDirectories {
			add(p, &paths)
		}
		fm.logger.WithField("files", paths).Info("Using watch_directories (precedence 3)")
		return paths, nil
	}

	fm.logger.Warn("No files found from pipeline (files/directories) or watch_directories - file monitor inactive")
	return []string{}, nil
}

func (fm *FileMonitor) expandPipelineDirectories() []string {
	cfg := fm.config.PipelineConfig
	if cfg == nil {
		return nil
	}
	rawDirs, ok := cfg["directories"].([]interface{})
	if !ok || len(rawDirs) == 0 {
		return nil
	}

	includeHidden := false
	maxFiles := 0
	var maxFileSize int64 = 0
	if rawMon, ok := cfg["monitoring"].(map[interface{}]interface{}); ok {
		if v, ok := rawMon["include_hidden"]; ok {
			if b, ok2 := v.(bool); ok2 {
				includeHidden = b
			}
		}
		if v, ok := rawMon["max_files"]; ok {
			switch t := v.(type) {
			case int:
				maxFiles = t
			case int64:
				maxFiles = int(t)
			}
		}
		if v, ok := rawMon["max_file_size"]; ok {
			switch t := v.(type) {
			case int:
				maxFileSize = int64(t)
			case int64:
				maxFileSize = t
			}
		}
	}

	var results []string
	seen := make(map[string]struct{}, 64)
	add := func(p string) {
		if p == "" {
			return
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		results = append(results, p)
	}

	matchAny := func(name string, patterns []string) bool {
		if len(patterns) == 0 {
			return false
		}
		for _, pat := range patterns {
			if ok, _ := filepath.Match(pat, name); ok {
				return true
			}
		}
		return false
	}

	for _, rd := range rawDirs {
		m, ok := rd.(map[interface{}]interface{})
		if !ok {
			continue
		}
		enabled := true
		if v, ok := m["enabled"]; ok {
			if b, ok2 := v.(bool); ok2 {
				enabled = b
			}
		}
		if !enabled {
			continue
		}
		dirPath, _ := m["path"].(string)
		if dirPath == "" {
			continue
		}

		var include []string
		if raw, ok := m["patterns"].([]interface{}); ok {
			for _, it := range raw {
				if s, ok2 := it.(string); ok2 {
					include = append(include, s)
				}
			}
		}
		var exclude []string
		if raw, ok := m["exclude_patterns"].([]interface{}); ok {
			for _, it := range raw {
				if s, ok2 := it.(string); ok2 {
					exclude = append(exclude, s)
				}
			}
		}
		var excludeDirs []string
		if raw, ok := m["exclude_directories"].([]interface{}); ok {
			for _, it := range raw {
				if s, ok2 := it.(string); ok2 {
					excludeDirs = append(excludeDirs, s)
				}
			}
		}
		recursive := false
		if v, ok := m["recursive"]; ok {
			if b, ok2 := v.(bool); ok2 {
				recursive = b
			}
		}

		if !recursive {
			entries, err := os.ReadDir(dirPath)
			if err != nil {
				fm.logger.WithError(err).Warn("Failed to read directory")
				continue
			}
			for _, de := range entries {
				if de.IsDir() {
					continue
				}
				name := de.Name()
				if !includeHidden && len(name) > 0 && name[0] == '.' {
					continue
				}
				if len(include) > 0 && !matchAny(name, include) {
					continue
				}
				if matchAny(name, exclude) {
					continue
				}
				full := filepath.Join(dirPath, name)
				if maxFileSize > 0 {
					if fi, err := os.Stat(full); err == nil {
						if fi.Size() > maxFileSize {
							continue
						}
					}
				}
				add(full)
				if maxFiles > 0 && len(results) >= maxFiles {
					return results
				}
			}
			continue
		}

		_ = filepath.WalkDir(dirPath, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				for _, ex := range excludeDirs {
					if d.Name() == ex {
						return filepath.SkipDir
					}
				}
				if !includeHidden && len(d.Name()) > 0 && d.Name()[0] == '.' {
					return filepath.SkipDir
				}
				return nil
			}
			name := filepath.Base(path)
			if !includeHidden && len(name) > 0 && name[0] == '.' {
				return nil
			}
			if len(include) > 0 && !matchAny(name, include) {
				return nil
			}
			if matchAny(name, exclude) {
				return nil
			}
			if maxFileSize > 0 {
				if fi, err := os.Stat(path); err == nil {
					if fi.Size() > maxFileSize {
						return nil
					}
				}
			}
			add(path)
			if maxFiles > 0 && len(results) >= maxFiles {
				return filepath.SkipDir
			}
			return nil
		})
	}

	return results
}

// Stop
func (fm *FileMonitor) Stop() error {
	fm.runningMux.Lock()
	if !fm.running {
		fm.runningMux.Unlock()
		fm.logger.Warn("File Monitor já está parado")
		return nil
	}
	fm.running = false
	fm.runningMux.Unlock()

	fm.logger.WithField("component", "file_monitor").Info("Parando File Monitor...")

	fm.ctxMux.RLock()
	cancel := fm.cancel
	fm.ctxMux.RUnlock()

	if cancel != nil {
		cancel()
	}

	fm.logger.Info("Aguardando tailers pararem...")
	fm.stopAllTailers()
	fm.logger.Info("Todos os tailers parados")

	fm.logger.Info("Fechando worker pool...")
	if fm.workerPool != nil {
		fm.workerPool.close()
	}
	fm.logger.Info("Worker pool fechado")

	done := make(chan struct{})
	go func() {
		fm.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		fm.logger.Info("File Monitor parado graciosamente")
	case <-time.After(defaultShutdownTimeout):
		fm.logger.Warn("Timeout aguardando File Monitor parar")
	}

	fm.logger.Info("File Monitor encerrado")
	return nil
}

func (fm *FileMonitor) stopAllTailers() {
	fm.tailersMux.Lock()
	defer fm.tailersMux.Unlock()

	for path, tailer := range fm.tailers {
		fm.logger.WithField("file_path", path).Debug("Parando tailer")
		tailer.stop()
	}

	fm.tailers = make(map[string]*logTailer)
}
